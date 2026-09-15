package mesh

// native_transport.go — native Bluetooth and Wi-Fi Direct transports for the
// offline mesh, plus the automatic transport selection required by
// Anonymous.md §5.3 ("Internet available? NO → Local Wi-Fi / Wi-Fi Direct →
// Bluetooth → Mesh networking → Store-and-forward").
//
// Platform boundary: a process cannot open a Bluetooth or Wi-Fi Direct radio
// from portable Go — the OS owns those stacks. What the native clients do is
// open the radio (Android: BluetoothServerSocket/BluetoothSocket via
// RFCOMM, WifiP2pManager group socket; iOS: CoreBluetooth GATT, Network
// framework P2P) and expose it as a local socket. BridgeTransport then carries
// mesh packets over that socket, so routing, TTL, dedup, crypto and
// store-and-forward all run in this engine unchanged.
//
// TCPTransport is the stream transport used over both bridges; UDPTransport
// (transport.go) remains the local-Wi-Fi/hotspot link. AutoTransport picks the
// first available link in the §5.3 order and re-evaluates on demand.

import (
	"errors"
	"io"
	"net"
	"sync"
	"time"
)

// inboundSetter is implemented by transports that accept an inbound packet
// callback after construction, so a Node can wire itself to any transport
// kind (not only the UDP one).
type inboundSetter interface {
	SetInbound(func(addr string, data []byte))
}

// SetInbound installs the inbound packet callback (implements inboundSetter).
//
// The callback is read on the receive goroutine, so it is published under the
// same mutex the reader holds while loading it (see UDPTransport.readLoop).
// Assigning it unlocked would be a data race against a datagram in flight.
func (t *UDPTransport) SetInbound(fn func(addr string, data []byte)) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.onPkt = fn
}

// ---------------------------------------------------------------------------
// Stream transport (Bluetooth RFCOMM bridge, Wi-Fi Direct group socket)
// ---------------------------------------------------------------------------

// StreamKind identifies which physical radio a bridge carries.
type StreamKind string

const (
	// KindBluetooth is an RFCOMM bridge (Android BluetoothSocket / iOS GATT).
	KindBluetooth StreamKind = "bluetooth"
	// KindWifiDirect is a Wi-Fi Direct / P2P group socket.
	KindWifiDirect StreamKind = "wifi_direct"
)

// TCPTransport carries length-delimited mesh packets over a stream socket. It
// is used for the Bluetooth RFCOMM bridge and the Wi-Fi Direct group socket:
// both present as a byte stream to the process, differing only in the radio
// underneath, which is why they share one implementation.
//
// Frames are length-prefixed (4-byte big-endian) because a stream has no
// datagram boundaries.
type TCPTransport struct {
	kind     StreamKind
	listener net.Listener
	mu       sync.Mutex
	conns    map[string]net.Conn
	addr     string
	onPkt    func(addr string, data []byte)
	stop     chan struct{}
	once     sync.Once
}

// NewTCPTransport listens on a local address and accepts bridge connections.
// listen is the platform-provided bind address (e.g. "127.0.0.1:0" for the
// RFCOMM bridge, or the P2P group address).
func NewTCPTransport(kind StreamKind, listen string, onPkt func(addr string, data []byte)) (*TCPTransport, error) {
	ln, err := net.Listen("tcp", listen)
	if err != nil {
		return nil, err
	}
	t := &TCPTransport{
		kind:     kind,
		listener: ln,
		conns:    make(map[string]net.Conn),
		addr:     ln.Addr().String(),
		onPkt:    onPkt,
		stop:     make(chan struct{}),
	}
	go t.acceptLoop()
	return t, nil
}

// SetInbound installs the inbound packet callback (implements inboundSetter).
func (t *TCPTransport) SetInbound(fn func(addr string, data []byte)) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.onPkt = fn
}

// Kind returns the radio this bridge carries.
func (t *TCPTransport) Kind() StreamKind { return t.kind }

// Transport returns the wire name used in beacons ("bluetooth" | "wifi_direct").
func (t *TCPTransport) Transport() string { return string(t.kind) }

func (t *TCPTransport) acceptLoop() {
	for {
		conn, err := t.listener.Accept()
		if err != nil {
			select {
			case <-t.stop:
				return
			default:
				continue
			}
		}
		t.register(conn)
	}
}

func (t *TCPTransport) register(conn net.Conn) {
	addr := conn.RemoteAddr().String()
	t.mu.Lock()
	t.conns[addr] = conn
	t.mu.Unlock()
	go t.readLoop(addr, conn)
}

func (t *TCPTransport) readLoop(addr string, conn net.Conn) {
	defer func() {
		t.mu.Lock()
		delete(t.conns, addr)
		t.mu.Unlock()
		_ = conn.Close()
	}()
	for {
		data, err := readFrame(conn)
		if err != nil {
			return
		}
		t.mu.Lock()
		fn := t.onPkt
		t.mu.Unlock()
		if fn != nil {
			fn(addr, data)
		}
	}
}

// Send writes a length-prefixed frame to a peer, dialing it when no live
// connection exists yet (the bridge address is dialable).
func (t *TCPTransport) Send(addr string, data []byte) error {
	t.mu.Lock()
	conn, ok := t.conns[addr]
	t.mu.Unlock()
	if !ok {
		var err error
		conn, err = net.DialTimeout("tcp", addr, 5*time.Second)
		if err != nil {
			return err
		}
		t.register(conn)
	}
	return writeFrame(conn, data)
}

// Addr returns the local listening address.
func (t *TCPTransport) Addr() string { return t.addr }

// Close shuts the transport down.
func (t *TCPTransport) Close() error {
	var err error
	t.once.Do(func() {
		close(t.stop)
		err = t.listener.Close()
		t.mu.Lock()
		for _, c := range t.conns {
			_ = c.Close()
		}
		t.conns = make(map[string]net.Conn)
		t.mu.Unlock()
	})
	return err
}

// Peers returns the addresses of connected bridge peers.
func (t *TCPTransport) Peers() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]string, 0, len(t.conns))
	for a := range t.conns {
		out = append(out, a)
	}
	return out
}

// writeFrame writes a 4-byte big-endian length followed by the payload.
func writeFrame(w io.Writer, data []byte) error {
	if len(data) > 0xFFFFFFFF {
		return errors.New("mesh: frame too large")
	}
	hdr := []byte{byte(len(data) >> 24), byte(len(data) >> 16), byte(len(data) >> 8), byte(len(data))}
	if _, err := w.Write(hdr); err != nil {
		return err
	}
	_, err := w.Write(data)
	return err
}

// readFrame reads one length-prefixed frame.
func readFrame(r io.Reader) ([]byte, error) {
	var hdr [4]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return nil, err
	}
	n := int(hdr[0])<<24 | int(hdr[1])<<16 | int(hdr[2])<<8 | int(hdr[3])
	if n < 0 || n > 1<<20 {
		return nil, errors.New("mesh: invalid frame length")
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, err
	}
	return buf, nil
}

// ---------------------------------------------------------------------------
// Automatic transport selection — Anonymous.md §5.3
// ---------------------------------------------------------------------------

// TransportPreference is the §5.3 fallback order when there is no Internet:
// local Wi-Fi / hotspot, then Wi-Fi Direct, then Bluetooth.
var TransportPreference = []string{"local_wifi", "wifi_direct", "bluetooth"}

// AutoTransport owns several links and always routes through the highest
// preference one that is currently available. When none is available it
// reports "none" and the node's queue holds packets for store-and-forward.
type AutoTransport struct {
	mu     sync.Mutex
	links  map[string]Transport
	active Transport
	onPkt  func(addr string, data []byte)
}

// NewAutoTransport creates an empty, auto-selecting transport.
func NewAutoTransport(onPkt func(addr string, data []byte)) *AutoTransport {
	return &AutoTransport{links: make(map[string]Transport), onPkt: onPkt}
}

// Add registers a link under its transport name and (re)selects the active one.
func (a *AutoTransport) Add(name string, t Transport) {
	a.mu.Lock()
	a.links[name] = t
	if setter, ok := t.(inboundSetter); ok {
		setter.SetInbound(func(addr string, data []byte) {
			a.mu.Lock()
			fn := a.onPkt
			a.mu.Unlock()
			if fn != nil {
				fn(addr, data)
			}
		})
	}
	a.mu.Unlock()
	a.Reselect()
}

// Reselect picks the active link in preference order.
func (a *AutoTransport) Reselect() {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, name := range TransportPreference {
		if t, ok := a.links[name]; ok {
			a.active = t
			return
		}
	}
	a.active = nil
}

// Active returns the transport name currently carrying traffic, or "none".
func (a *AutoTransport) Active() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.active == nil {
		return "none"
	}
	if named, ok := a.active.(interface{ Transport() string }); ok {
		return named.Transport()
	}
	if named, ok := a.active.(interface{ Kind() StreamKind }); ok {
		return string(named.Kind())
	}
	return "local_wifi"
}

// Remove drops a link (e.g. a radio was switched off) and reselects.
func (a *AutoTransport) Remove(name string) {
	a.mu.Lock()
	delete(a.links, name)
	a.mu.Unlock()
	a.Reselect()
}

// Send routes through the active link, or errors when nothing is available so
// the caller keeps the packet queued (store-and-forward).
func (a *AutoTransport) Send(addr string, data []byte) error {
	a.mu.Lock()
	active := a.active
	a.mu.Unlock()
	if active == nil {
		return errors.New("mesh: no transport available (store-and-forward)")
	}
	return active.Send(addr, data)
}

// Addr returns the active link's address, or "" when nothing is available.
func (a *AutoTransport) Addr() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.active == nil {
		return ""
	}
	return a.active.Addr()
}

// Close shuts every link down.
func (a *AutoTransport) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, t := range a.links {
		_ = t.Close()
	}
	a.links = make(map[string]Transport)
	a.active = nil
	return nil
}
