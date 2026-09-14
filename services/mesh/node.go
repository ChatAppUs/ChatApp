package mesh

// node.go — the mesh node: ties transport, routing, crypto, and
// store-and-forward together. A node can run as a member device or a pure
// relay. It discovers peers, sends/receives encrypted packets, forwards
// multi-hop, and delivers to the application layer.
//
// Scaling: the mesh is designed to grow without a fixed diameter. As devices
// join, coverage and reachable distance grow with the network. The packet TTL
// (max hops) is therefore configurable per node and defaults to a value that
// scales with the expected network size rather than a small fixed constant.
// Set NodeConfig.MaxHops to bound the diameter for a known topology, or leave
// it zero to use the scalable default (see scale.go).

import (
	"sort"
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
	maxHops   int
	signer    *SigningKey
	peerKeys  map[string][]byte // device id -> pinned Ed25519 public key (TOFU)

	beaconSeq int64
	stop      chan struct{}
	mu        sync.Mutex
}

// neighborMaxAge is how long a neighbor stays routable without a fresh
// beacon. Beacons fire every 5s, so this tolerates ~36 missed beacons
// before the route expires.
const neighborMaxAge = 3 * time.Minute

// NodeConfig configures a mesh node.
type NodeConfig struct {
	DeviceID  string
	Key       *IdentityKey
	Kind      string
	Transport Transport
	Handler   Handler
	// MaxHops bounds the packet TTL (mesh diameter). Zero uses the scalable
	// default (DefaultMaxHops), which grows with the expected network size so
	// coverage extends as devices increase.
	MaxHops int
	// Signer is this device's Ed25519 beacon-signing key. Zero generates one.
	Signer *SigningKey
}

// NewNode creates a mesh node. The transport's inbound callback is wired to
// the node's HandleInbound so every received datagram is processed.
func NewNode(cfg NodeConfig) *Node {
	if cfg.Kind == "" {
		cfg.Kind = "member"
	}
	maxHops := cfg.MaxHops
	if maxHops <= 0 {
		maxHops = DefaultMaxHops
	}
	signer := cfg.Signer
	if signer == nil {
		// Every node signs its own beacons; generating a key is cheap and
		// keeps device identity self-certifying with no external authority.
		signer, _ = NewSigningKey()
	}
	n := &Node{
		DeviceID:  cfg.DeviceID,
		Key:       cfg.Key,
		Kind:      cfg.Kind,
		transport: cfg.Transport,
		routes:    NewRouteTable(),
		queue:     NewQueue(1000, 7*24*time.Hour),
		handler:   cfg.Handler,
		maxHops:   maxHops,
		signer:    signer,
		peerKeys:  make(map[string][]byte),
		stop:      make(chan struct{}),
	}
	// Wire the transport's inbound callback to this node. Any transport that
	// accepts a late-bound inbound callback (UDP, the Bluetooth/Wi-Fi Direct
	// stream bridges, or the auto-selecting transport) is supported — not only
	// the UDP one.
	if setter, ok := cfg.Transport.(inboundSetter); ok {
		setter.SetInbound(n.HandleInbound)
	}
	return n
}

// Start begins the discovery beacon loop.
func (n *Node) Start() {
	if n.transport == nil {
		return
	}
	go n.beaconLoop()
}

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
			// Expire stale routes so traffic stops flowing to peers that
			// left range or powered down.
			n.routes.Expire(neighborMaxAge)
			n.mu.Lock()
			n.beaconSeq++
			seq := n.beaconSeq
			n.mu.Unlock()
			b := &Beacon{
				DeviceID:  n.DeviceID,
				Kind:      n.Kind,
				Transport: n.transportName(),
				Addr:      n.transport.Addr(),
				Seq:       seq,
			}
			var data []byte
			var err error
			if n.signer != nil {
				var sb *SignedBeacon
				if sb, err = n.signer.SignBeacon(b); err == nil {
					data, err = MarshalSignedBeacon(sb)
				}
			} else {
				data, err = MarshalBeacon(b)
			}
			if err != nil {
				continue
			}
			// Broadcast to the local broadcast address (best-effort).
			_ = n.transport.Send("255.255.255.255:0", data)
		}
	}
}

// transportName reports which physical link is currently carrying traffic, so
// the discovery beacon advertises the transport peers should route over. With
// an AutoTransport this follows the Anonymous.md §5.3 fallback order; with a
// single link it is that link's name.
func (n *Node) transportName() string {
	if n.transport == nil {
		return "none"
	}
	if named, ok := n.transport.(interface{ Active() string }); ok {
		return named.Active()
	}
	if named, ok := n.transport.(interface{ Transport() string }); ok {
		return named.Transport()
	}
	return "local_wifi"
}

// HandleInbound processes a raw datagram from a peer.
func (n *Node) HandleInbound(addr string, data []byte) {
	// Try a legacy (unsigned) beacon first; signed beacons have a different
	// top-level shape and fall through to the verified path.
	if b, err := UnmarshalBeacon(data); err == nil && b.DeviceID != "" {
		n.routes.Upsert(b)
		return
	}
	// Signed beacon: verify the signature and the pinned device-key binding
	// before trusting the advertised route. A mismatch (a peer impersonating
	// an already-known device id with a different key) is rejected.
	if sb, err := UnmarshalSignedBeacon(data); err == nil && len(sb.Sig) > 0 {
		if b, err := VerifySignedBeacon(sb, n.peerKeys); err == nil {
			n.routes.Upsert(b)
		}
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
	p := NewPacket(kind, n.DeviceID, dst, n.maxHops)
	ct, nonce, err := Encrypt(n.Key, plaintext)
	if err != nil {
		return "", err
	}
	p.Payload = ct
	p.Nonce = nonce
	n.routes.Seen(p.ID)
	n.queue.Enqueue(p)
	n.flush()
	return p.ID, nil
}

// SendGroup encrypts and enqueues a group message (broadcast to group id).
func (n *Node) SendGroup(kind PacketKind, groupID string, plaintext []byte) (string, error) {
	p := NewPacket(kind, n.DeviceID, "", n.maxHops)
	p.GroupID = groupID
	ct, nonce, err := Encrypt(n.Key, plaintext)
	if err != nil {
		return "", err
	}
	p.Payload = ct
	p.Nonce = nonce
	n.routes.Seen(p.ID)
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
	n.queue.Enqueue(p)
	n.flush()
}

// flush attempts to deliver queued packets to known neighbors, ordered by
// route quality: relay consent first, then link throughput, then freshness
// (see Neighbor.Score). The packet's final destination is always tried even
// if it does not consent to relay others' traffic.
func (n *Node) flush() {
	if n.transport == nil {
		return
	}
	for _, p := range n.queue.Pending(time.Now()) {
		neighbors := n.routes.Neighbors()
		now := time.Now()
		sort.Slice(neighbors, func(i, j int) bool {
			return neighbors[i].Score(now) > neighbors[j].Score(now)
		})
		for _, nb := range neighbors {
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
