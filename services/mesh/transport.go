package mesh

// transport.go — local peer-to-peer transport for the offline mesh.
//
// The primary transport is a UDP socket bound to the device's local network
// (hotspot / local Wi-Fi). Peers on the same local network discover each other
// via periodic presence beacons and exchange packets directly. This is the
// "local Wi-Fi / hotspot" transport. Bluetooth and Wi-Fi Direct adapters are
// provided by the native clients (Android/iOS) and feed packets into the same
// engine through the Transport interface.

import (
	"encoding/json"
	"log"
	"net"
	"sync"
)

// Transport is the interface a physical link (UDP local network, Bluetooth,
// Wi-Fi Direct) must implement to carry mesh packets.
type Transport interface {
	// Send delivers a raw packet to a peer address.
	Send(addr string, data []byte) error
	// Addr returns this transport's local address (for discovery beacons).
	Addr() string
	// Close shuts the transport down.
	Close() error
}

// UDPTransport is a UDP socket on the local network (hotspot / local Wi-Fi).
type UDPTransport struct {
	conn *net.UDPConn
	addr string
	mu   sync.Mutex
	onPkt func(addr string, data []byte)
}

// NewUDPTransport binds a UDP socket on the given port (0 = ephemeral) and
// starts a read loop. onPkt is invoked for every datagram received.
func NewUDPTransport(port int, onPkt func(addr string, data []byte)) (*UDPTransport, error) {
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{Port: port})
	if err != nil {
		return nil, err
	}
	t := &UDPTransport{conn: conn, addr: conn.LocalAddr().String(), onPkt: onPkt}
	go t.readLoop()
	return t, nil
}

func (t *UDPTransport) readLoop() {
	buf := make([]byte, 65535)
	for {
		n, addr, err := t.conn.ReadFromUDP(buf)
		if err != nil {
			return
		}
		data := make([]byte, n)
		copy(data, buf[:n])
		if t.onPkt != nil {
			t.onPkt(addr.String(), data)
		}
	}
}

// Send delivers a datagram to a peer address.
func (t *UDPTransport) Send(addr string, data []byte) error {
	ua, err := net.ResolveUDPAddr("udp4", addr)
	if err != nil {
		return err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	_, err = t.conn.WriteToUDP(data, ua)
	return err
}

// Addr returns the local socket address.
func (t *UDPTransport) Addr() string { return t.addr }

// Close shuts the transport down.
func (t *UDPTransport) Close() error { return t.conn.Close() }

// ---- Discovery beacon ----

// Beacon is the plaintext presence announcement a device broadcasts so nearby
// peers can discover it. It carries only routing metadata (device id, kind,
// transport address) — never message content.
type Beacon struct {
	DeviceID  string `json:"device_id"`
	Kind      string `json:"kind"` // "member" | "relay"
	Transport string `json:"transport"` // "local_wifi" | "bluetooth" | "wifi_direct"
	Addr      string `json:"addr"`
	Seq       int64  `json:"seq"`
}

// MarshalBeacon serializes a beacon.
func MarshalBeacon(b *Beacon) ([]byte, error) { return json.Marshal(b) }

// UnmarshalBeacon parses a beacon.
func UnmarshalBeacon(data []byte) (*Beacon, error) {
	var b Beacon
	if err := json.Unmarshal(data, &b); err != nil {
		return nil, err
	}
	return &b, nil
}

// logf is a small logger hook (kept minimal to avoid a dependency).
func logf(format string, args ...any) { log.Printf("[mesh] "+format, args...) }
