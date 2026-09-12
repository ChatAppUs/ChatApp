package mesh

// node.go — the mesh node: ties transport, routing, crypto, and
// store-and-forward together. A node can run as a member device or a pure
// relay. It discovers peers, sends/receives encrypted packets, forwards
// multi-hop, and delivers to the application layer.

import (
	"sync"
	"time"
)

// Handler is the application-layer callback for a decrypted, delivered packet.
type Handler func(p *Packet, plaintext []byte)

// Node is a single device on the mesh.
type Node struct {
	DeviceID string
	Key      *IdentityKey
	Kind     string // "member" | "relay"

	transport Transport
	routes    *RouteTable
	queue     *Queue
	handler   Handler

	beaconSeq int64
	stop      chan struct{}
	mu        sync.Mutex
}

// NodeConfig configures a mesh node.
type NodeConfig struct {
	DeviceID  string
	Key       *IdentityKey
	Kind      string
	Transport Transport
	Handler   Handler
}

// NewNode creates a mesh node. The transport's inbound callback is wired to
// the node's HandleInbound so every received datagram is processed.
func NewNode(cfg NodeConfig) *Node {
	if cfg.Kind == "" {
		cfg.Kind = "member"
	}
	n := &Node{
		DeviceID:  cfg.DeviceID,
		Key:       cfg.Key,
		Kind:      cfg.Kind,
		transport: cfg.Transport,
		routes:    NewRouteTable(),
		queue:     NewQueue(1000, 7*24*time.Hour),
		handler:   cfg.Handler,
		stop:      make(chan struct{}),
	}
	// Wire the transport's inbound callback to this node.
	if udp, ok := cfg.Transport.(*UDPTransport); ok {
		udp.onPkt = n.HandleInbound
	}
	return n
}

// Start begins the discovery beacon loop.
func (n *Node) Start() { go n.beaconLoop() }

// Stop halts the node.
func (n *Node) Stop() {
	select {
	case <-n.stop:
	default:
		close(n.stop)
	}
	if n.transport != nil {
		_ = n.transport.Close()
	}
}

// beaconLoop broadcasts presence periodically so nearby peers discover us.
func (n *Node) beaconLoop() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-n.stop:
			return
		case <-ticker.C:
			n.mu.Lock()
			n.beaconSeq++
			seq := n.beaconSeq
			n.mu.Unlock()
			b := &Beacon{
				DeviceID:  n.DeviceID,
				Kind:      n.Kind,
				Transport: "local_wifi",
				Addr:      n.transport.Addr(),
				Seq:       seq,
			}
			data, err := MarshalBeacon(b)
			if err != nil {
				continue
			}
			// Broadcast to the local broadcast address (best-effort).
			_ = n.transport.Send("255.255.255.255:0", data)
		}
	}
}

// HandleInbound processes a raw datagram from a peer.
func (n *Node) HandleInbound(addr string, data []byte) {
	// Try beacon first.
	if b, err := UnmarshalBeacon(data); err == nil && b.DeviceID != "" {
		n.routes.Upsert(b)
		return
	}
	p, err := UnmarshalPacket(data)
	if err != nil {
		return
	}
	n.route(p)
}

// Send encrypts and enqueues a packet for a destination.
func (n *Node) Send(kind PacketKind, dst string, plaintext []byte) (string, error) {
	p := NewPacket(kind, n.DeviceID, dst, 8)
	ct, nonce, err := Encrypt(n.Key, plaintext)
	if err != nil {
		return "", err
	}
	p.Payload = ct
	p.Nonce = nonce
	n.queue.Enqueue(p)
	n.flush()
	return p.ID, nil
}

// SendGroup encrypts and enqueues a group message (broadcast to group id).
func (n *Node) SendGroup(kind PacketKind, groupID string, plaintext []byte) (string, error) {
	p := NewPacket(kind, n.DeviceID, "", 8)
	p.GroupID = groupID
	ct, nonce, err := Encrypt(n.Key, plaintext)
	if err != nil {
		return "", err
	}
	p.Payload = ct
	p.Nonce = nonce
	n.queue.Enqueue(p)
	n.flush()
	return p.ID, nil
}

// route forwards a packet toward its destination (or delivers locally).
func (n *Node) route(p *Packet) {
	// Dedup.
	if n.routes.Seen(p.ID) {
		return
	}
	// Deliver locally if it's for us.
	if p.Dst == n.DeviceID || (p.Dst == "" && p.GroupID != "") {
		if n.handler != nil {
			pt, err := Decrypt(n.Key, p.Payload, p.Nonce)
			if err == nil {
				n.handler(p, pt)
			}
		}
		return
	}
	// Forward one hop (bounded by TTL).
	if TTLExpired(p) {
		return
	}
	p.TTL--
	p.Hops++
	n.flush()
}

// flush attempts to deliver queued packets to known neighbors.
func (n *Node) flush() {
	for _, p := range n.queue.Pending(time.Now()) {
		for _, nb := range n.routes.Neighbors() {
			if !nb.RelayOK && nb.DeviceID != p.Dst {
				continue
			}
			data, err := p.Marshal()
			if err != nil {
				continue
			}
			if err := n.transport.Send(nb.Addr, data); err == nil {
				n.queue.Remove(p.ID)
				break
			}
		}
	}
}
