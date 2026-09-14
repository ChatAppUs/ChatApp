package mesh

// node.go — the mesh node: ties transport, routing, crypto, and
// store-and-forward together. A node can run as a member device or a pure
// relay. It discovers peers, sends/receives encrypted packets, forwards
// multi-hop (with per-source relay quotas), and delivers to the application
// layer.
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
	peerKeys  map[string][]byte        // device id -> pinned Ed25519 public key (TOFU)
	limiter   *RelayLimiter            // per-source relay quota (abuse prevention)
	kem       *KeyExchange             // X25519 key agreement (per-peer session keys)
	peerKEM   map[string]AdvertisedKEM // device id -> peer's advertised KEM key
	replay    *ReplayFilter            // per-source anti-replay sequence windows

	seqCtr int64
	stop   chan struct{}
	mu     sync.Mutex
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
		limiter:   NewRelayLimiter(DefaultRelayQuota()),
		kem:       NewKeyExchange(0),
		peerKEM:   make(map[string]AdvertisedKEM),
		replay:    NewReplayFilter(),
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
			// left range or powered down, and prune idle relay buckets so
			// the quota map stays bounded.
			n.routes.Expire(neighborMaxAge)
			n.limiter.Prune(time.Now(), 10*time.Minute)
			n.mu.Lock()
			n.seqCtr++
			seq := n.seqCtr
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
				// The beacon advertises this device's key-agreement public
				// key; it is signed together with the route so peers derive
				// authenticated per-peer session keys (no MITM downgrade).
				if sb, err = n.signer.SignBeacon(b, n.kem.Public(), n.kem.Epoch); err == nil {
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
		// A (re)discovered neighbour is exactly when store-and-forward
		// must drain: packets queued while the peer was out of range or
		// the link partitioned become deliverable the moment the route
		// appears again.
		n.flush()
		return
	}
	// Signed beacon: verify the signature and the pinned device-key binding
	// before trusting the advertised route. A mismatch (a peer impersonating
	// an already-known device id with a different key) is rejected.
	if sb, err := UnmarshalSignedBeacon(data); err == nil && len(sb.Sig) > 0 {
		if b, err := VerifySignedBeacon(sb, n.peerKeys); err == nil {
			n.routes.Upsert(b)
			// Pin the peer's advertised key-agreement key (signed, so it is
			// bound to the verified device identity). A newer epoch replaces
			// the stored key after a peer's rotation.
			if len(sb.KEMPub) == KeySize {
				n.mu.Lock()
				prev, had := n.peerKEM[b.DeviceID]
				if !had || sb.KEMEpoch >= prev.Epoch {
					n.peerKEM[b.DeviceID] = AdvertisedKEM{Public: append([]byte(nil), sb.KEMPub...), Epoch: sb.KEMEpoch}
				}
				n.mu.Unlock()
			}
			n.flush()
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
	key := n.sessionKeyFor(dst)
	ct, nonce, err := Encrypt(key, plaintext)
	if err != nil {
		return "", err
	}
	p.Payload = ct
	p.Nonce = nonce
	p.Seq = n.nextSeq()
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
	p.Seq = n.nextSeq()
	n.routes.Seen(p.ID)
	n.queue.Enqueue(p)
	n.flush()
	return p.ID, nil
}

// route forwards a packet toward its destination (or delivers locally).
func (n *Node) route(p *Packet) {
	// Dedup with reach improvement: a later copy carrying strictly more
	// remaining TTL is forwarded (it reaches nodes earlier copies could not).
	// Local delivery happens only on the FIRST copy: improved duplicates
	// extend reach but must never re-deliver to the application.
	// Anti-replay: reject re-transmissions of already-accepted sequence
	// numbers per source before any dedup/delivery work (see replay.go).
	// Packets without a sequence number (legacy senders) pass through.
	improve, first := n.routes.SeenBetter(p.ID, p.TTL)
	if !improve {
		return
	}
	// Deliver locally if it's for us. Anti-replay runs AFTER the payload
	// authenticates (see replay.go): a forged packet must never be able to
	// advance or poison the receiver's replay window.
	if p.Dst == n.DeviceID || (p.Dst == "" && p.GroupID != "") {
		if first && n.handler != nil {
			if pt, err := n.decryptPayload(p); err == nil {
				if p.Seq != 0 && !n.replay.Check(p.Src, p.Seq) {
					return
				}
				n.handler(p, pt)
			}
		}
		return
	}
	// Forward one hop (bounded by TTL), subject to the per-source relay
	// quota: a flooded device cannot monopolise this relay's radio/queue.
	if TTLExpired(p) {
		return
	}
	if !n.limiter.Allow(p.Src, time.Now()) {
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
	now := time.Now()
	for _, p := range n.queue.Pending(now) {
		neighbors := n.routes.Neighbors()
		// Destination-aware: when the destination is itself a known
		// neighbour, deliver directly and skip flooding.
		direct := (*Neighbor)(nil)
		for i := range neighbors {
			if neighbors[i].DeviceID == p.Dst {
				direct = neighbors[i]
				break
			}
		}
		sent := false
		if direct != nil {
			if data, err := p.Marshal(); err == nil {
				if err := n.transport.Send(direct.Addr, data); err == nil {
					sent = true
				}
			}
		}
		if !sent {
			// Controlled flooding: hand the packet to every eligible
			// neighbour (relay consent required, the final destination
			// always tried). Upstream dedup (RouteTable.Seen) bounds the
			// flood, so multi-path topologies make progress from every
			// branch instead of betting on a single score-chosen hop.
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
					sent = true
				}
			}
		}
		if sent {
			n.queue.Remove(p.ID)
		}
	}
}

// nextSeq returns the next per-sender sequence number (monotonic, used by
// receivers' anti-replay windows).
func (n *Node) nextSeq() int64 {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.seqCtr++
	return n.seqCtr
}

// sessionKeyFor selects the AEAD key for a payload: the per-peer ECDH session
// key when the destination has advertised a key-agreement key (the hardened
// path), otherwise the pre-shared identity key (legacy/native fallback until
// native clients adopt session keys). Group payloads keep the shared group
// key model (n.Key); per-member group key management is a separate work item.
func (n *Node) sessionKeyFor(dst string) *IdentityKey {
	if dst == "" {
		return n.Key
	}
	n.mu.Lock()
	adv, ok := n.peerKEM[dst]
	n.mu.Unlock()
	if !ok {
		return n.Key
	}
	sk, err := SessionKey(n.kem, n.DeviceID, adv.Public, dst)
	if err != nil {
		return n.Key
	}
	return sk
}

// decryptPayload opens a delivered packet: the per-peer session key first,
// falling back to the pre-shared identity key (interop with legacy/native
// senders and with peers whose rotation we have not observed yet).
func (n *Node) decryptPayload(p *Packet) ([]byte, error) {
	if p.Dst == n.DeviceID {
		n.mu.Lock()
		adv, ok := n.peerKEM[p.Src]
		n.mu.Unlock()
		if ok {
			if sk, err := SessionKey(n.kem, n.DeviceID, adv.Public, p.Src); err == nil {
				if pt, err := Decrypt(sk, p.Payload, p.Nonce); err == nil {
					return pt, nil
				}
			}
		}
	}
	return Decrypt(n.Key, p.Payload, p.Nonce)
}

// RotateSessions regenerates this device's key-agreement key pair and bumps
// the epoch. Beacons advertise the new key within one beacon interval; peers
// pin the newer epoch when it arrives and subsequent unicast traffic uses the
// fresh session key. Until then traffic falls back to the pre-shared key, so
// rotation is graceful (no lost packets, at legacy security until re-seen).
func (n *Node) RotateSessions() error {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.kem.Rotate()
}

// Routes exposes the route table for simulators and tooling. Production code
// should not mutate another node's routes.
func (n *Node) Routes() *RouteTable { return n.routes }

// SetHandler installs or replaces the application-layer delivery callback
// (used by simulators to observe delivery at a chosen node).
func (n *Node) SetHandler(h Handler) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.handler = h
}
