package mesh

// sim.go — deterministic in-memory mesh simulation.
//
// SimBus wires SimLink transports together with an explicit adjacency map so
// large topologies (chains, grids, partitions and heals) can be exercised
// without OS sockets. Node.flush's normal transport.Send calls deliver through
// the bus, so the production routing/queue/crypto code runs unmodified.

import (
	"fmt"
	"sync"
)

// SimLink is an in-memory Transport owned by a SimBus.
type SimLink struct {
	mu     sync.Mutex
	addr   string
	bus    *SimBus
	onPkt  func(addr string, data []byte)
	closed bool
}

func (l *SimLink) Send(dst string, data []byte) error {
	l.mu.Lock()
	bus, closed := l.bus, l.closed
	l.mu.Unlock()
	if closed {
		return fmt.Errorf("link closed")
	}
	return bus.deliver(l.addr, dst, data)
}

func (l *SimLink) Addr() string { return l.addr }
func (l *SimLink) Close() error { l.mu.Lock(); l.closed = true; l.mu.Unlock(); return nil }

// SimBus is a simulated network: links attached by address, adjacency declared
// with Wire/Unwire, delivery only along wired edges (partitions are real).
// SetLossRate enables deterministic radio-style packet loss: each send is
// dropped with probability p using a seeded PRNG, so the reliable-transfer
// retransmission path can be exercised reproducibly.
type SimBus struct {
	mu       sync.Mutex
	links    map[string]*SimLink
	wires    map[string][]string // addr -> directly reachable peer addrs
	drops    int
	lossPct  int    // 0..100, chance a send is dropped
	rng      uint64 // xorshift64* state, seeded in SetLossRate
	lostLoss int    // count of loss-injected drops
}

// NewSimBus creates an empty simulated network.
func NewSimBus() *SimBus {
	return &SimBus{links: make(map[string]*SimLink), wires: make(map[string][]string)}
}

// SetLossRate turns on deterministic loss injection. pct is the percentage of
// sends dropped (0..100); seed makes the sequence reproducible.
func (b *SimBus) SetLossRate(pct int, seed uint64) {
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	if seed == 0 {
		seed = 0x9E3779B97F4A7C15
	}
	b.mu.Lock()
	b.lossPct, b.rng = pct, seed
	b.mu.Unlock()
}

// nextRand advances the xorshift64* state and returns a value in [0,100).
func (b *SimBus) nextRand() int {
	x := b.rng
	x ^= x >> 12
	x ^= x << 25
	x ^= x >> 27
	b.rng = x
	return int((x * 0x2545F4914F6CDD1D >> 33) % 100)
}

// LossStats reports how many sends were dropped by loss injection.
func (b *SimBus) LossStats() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.lostLoss
}

// Attach creates a link for addr and wires its inbound callback to onPkt.
func (b *SimBus) Attach(addr string, onPkt func(peer string, data []byte)) *SimLink {
	b.mu.Lock()
	defer b.mu.Unlock()
	l := &SimLink{addr: addr, bus: b, onPkt: onPkt}
	b.links[addr] = l
	return l
}

// Detach removes a link entirely (device left the simulation).
func (b *SimBus) Detach(addr string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.links, addr)
	delete(b.wires, addr)
}

// Wire declares one-hop reachability src→dst.
func (b *SimBus) Wire(src, dst string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, p := range b.wires[src] {
		if p == dst {
			return
		}
	}
	b.wires[src] = append(b.wires[src], dst)
}

// Unwire removes the direct src→dst edge (partition simulation).
func (b *SimBus) Unwire(src, dst string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	peers := b.wires[src]
	out := peers[:0]
	for _, p := range peers {
		if p != dst {
			out = append(out, p)
		}
	}
	b.wires[src] = out
}

// deliver routes a datagram from srcAddr to dstAddr if an edge exists and the
// destination link is live; otherwise it counts as a drop.
func (b *SimBus) deliver(srcAddr, dstAddr string, data []byte) error {
	b.mu.Lock()
	l, ok := b.links[dstAddr]
	wired := false
	for _, p := range b.wires[srcAddr] {
		if p == dstAddr {
			wired = true
			break
		}
	}
	if !ok || !wired {
		b.drops++
	}
	// Deterministic loss injection: drop the send before invoking the
	// receiver callback, mirroring an unreliable radio link.
	if ok && wired && b.lossPct > 0 && b.nextRand() < b.lossPct {
		b.drops++
		b.lostLoss++
		b.mu.Unlock()
		return fmt.Errorf("simulated loss %s -> %s", srcAddr, dstAddr)
	}
	var cb func(addr string, data []byte)
	if ok {
		cb = l.onPkt
	}
	b.mu.Unlock()
	if !ok || !wired {
		return fmt.Errorf("no route %s -> %s", srcAddr, dstAddr)
	}
	if cb != nil {
		cb(srcAddr, data)
	}
	return nil
}

// SetInbound installs the inbound packet callback (implements inboundSetter).
func (l *SimLink) SetInbound(cb func(addr string, data []byte)) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.onPkt = cb
}

// HasEdge reports whether src can reach dst in one hop.
func (b *SimBus) HasEdge(src, dst string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, p := range b.wires[src] {
		if p == dst {
			return true
		}
	}
	return false
}

// Stats returns the drop counter.
func (b *SimBus) Stats() (dropped int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.drops
}
