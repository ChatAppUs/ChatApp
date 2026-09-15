package mesh

// routing.go — multi-hop routing, TTL, and duplicate suppression.
//
// Routing is destination-aware controlled flooding with store-and-forward:
// a node forwards a packet to its known neighbors (bounded by TTL), and
// packets persist in a bounded queue until a route to the destination appears.
// Packet IDs deduplicate against loops; TTL bounds packet lifetime.

import (
	"sync"
	"time"
)

type RouteTable struct {
	mu      sync.Mutex
	neigh   map[string]*Neighbor
	seen    map[string]time.Time
	bestTTL map[string]int
	maxSeen int
}

type Neighbor struct {
	DeviceID  string
	Addr      string
	Transport string
	LastSeen  time.Time
	RelayOK   bool
}

func NewRouteTable() *RouteTable {
	return &RouteTable{
		neigh: make(map[string]*Neighbor),
		seen: make(map[string]time.Time),
		bestTTL: make(map[string]int),
		maxSeen: 10000,
	}
}

func (rt *RouteTable) Upsert(b *Beacon) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	n, ok := rt.neigh[b.DeviceID]
	if !ok {
		n = &Neighbor{DeviceID: b.DeviceID}
		rt.neigh[b.DeviceID] = n
	}
	n.Addr = b.Addr
	n.Transport = b.Transport
	n.RelayOK = b.Kind == "relay"
	n.LastSeen = time.Now()
}

func (rt *RouteTable) SetRelay(deviceID string, ok bool) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if n, found := rt.neigh[deviceID]; found {
		n.RelayOK = ok
	}
}

func (rt *RouteTable) Remove(deviceID string) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	delete(rt.neigh, deviceID)
}

func (rt *RouteTable) Neighbors() []*Neighbor {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	out := make([]*Neighbor, 0, len(rt.neigh))
	for _, n := range rt.neigh {
		cp := *n
		out = append(out, &cp)
	}
	return out
}

// pruneSeenLocked removes the oldest packet identity from BOTH dedup maps.
// Keeping bestTTL bounded is essential: the previous implementation bounded
// seen but allowed bestTTL to grow forever on a long-lived relay.
func (rt *RouteTable) pruneSeenLocked() {
	if len(rt.seen) <= rt.maxSeen {
		return
	}
	oldest := time.Now()
	var oldestKey string
	for k, v := range rt.seen {
		if v.Before(oldest) {
			oldest = v
			oldestKey = k
		}
	}
	if oldestKey != "" {
		delete(rt.seen, oldestKey)
		delete(rt.bestTTL, oldestKey)
	}
}

func (rt *RouteTable) SeenBetter(id string, ttl int) (improve, first bool) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if best, ok := rt.bestTTL[id]; ok && ttl <= best {
		return false, false
	}
	rt.bestTTL[id] = ttl
	_, seenBefore := rt.seen[id]
	if !seenBefore {
		rt.seen[id] = time.Now()
		rt.pruneSeenLocked()
	}
	return true, !seenBefore
}

func (rt *RouteTable) Seen(id string) bool {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if _, ok := rt.seen[id]; ok {
		return true
	}
	rt.seen[id] = time.Now()
	rt.bestTTL[id] = 0
	rt.pruneSeenLocked()
	return false
}

func transportScore(transport string) int {
	switch transport {
	case "wifi_direct":
		return 3
	case "local_wifi":
		return 2
	case "bluetooth":
		return 1
	default:
		return 0
	}
}

func (n *Neighbor) Score(now time.Time) int {
	s := transportScore(n.Transport) * 10
	if n.RelayOK {
		s += 100
	}
	age := now.Sub(n.LastSeen)
	switch {
	case age < 30*time.Second:
		s += 5
	case age < 120*time.Second:
		s += 2
	}
	return s
}

func (rt *RouteTable) BestRelay(exclude string) *Neighbor {
	var best *Neighbor
	now := time.Now()
	for _, n := range rt.Neighbors() {
		if !n.RelayOK || n.DeviceID == exclude {
			continue
		}
		if best == nil || n.Score(now) > best.Score(now) {
			best = n
		}
	}
	return best
}

func (rt *RouteTable) Expire(maxAge time.Duration) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	cutoff := time.Now().Add(-maxAge)
	for id, n := range rt.neigh {
		if n.LastSeen.Before(cutoff) {
			delete(rt.neigh, id)
		}
	}
}

func TTLExpired(p *Packet) bool { return p.TTL <= 0 }

func (rt *RouteTable) Knows(id string) bool {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	_, ok := rt.seen[id]
	return ok
}
