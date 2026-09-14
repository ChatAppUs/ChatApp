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

// RouteTable tracks known neighbors and their last-seen time.
type RouteTable struct {
	mu      sync.Mutex
	neigh   map[string]*Neighbor // device id -> neighbor
	seen    map[string]time.Time // packet id -> first seen (dedup)
	bestTTL map[string]int       // packet id -> highest TTL forwarded so far
	maxSeen int
}

// Neighbor is a known peer on the mesh.
type Neighbor struct {
	DeviceID  string
	Addr      string
	Transport string
	LastSeen  time.Time
	RelayOK   bool // whether this neighbor consents to relay
}

// NewRouteTable creates an empty route table.
func NewRouteTable() *RouteTable {
	return &RouteTable{
		neigh:   make(map[string]*Neighbor),
		seen:    make(map[string]time.Time),
		bestTTL: make(map[string]int),
		maxSeen: 10000,
	}
}

// Upsert records or refreshes a neighbor from a beacon.
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

// SetRelay marks a neighbor as consenting to relay.
func (rt *RouteTable) SetRelay(deviceID string, ok bool) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if n, found := rt.neigh[deviceID]; found {
		n.RelayOK = ok
	}
}

// Neighbors returns a snapshot of known neighbors.
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

// SeenBetter is dedup with reach improvement: the first copy of a packet is
// forwarded; later copies are dropped UNLESS they carry strictly more
// remaining TTL than any copy seen before — that copy can reach nodes the
// earlier, more meandering copies could not, so it is forwarded too.
//
// It reports two facts the caller needs:
//
//	improve — this copy should be forwarded (first copy, or strictly better
//	          reach than any copy seen before).
//	first   — this is the FIRST copy of this packet id ever seen. Only the
//	          first copy may be delivered to the application: without this
//	          rule, every TTL-improved duplicate would be handed to the
//	          handler again (duplicate delivery bug).
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
		if len(rt.seen) > rt.maxSeen {
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
			}
		}
	}
	return true, !seenBefore
}

// Seen reports whether a packet id was already processed (dedup) and records
// it if not. Returns true if the packet is a duplicate.
func (rt *RouteTable) Seen(id string) bool {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if _, ok := rt.seen[id]; ok {
		return true
	}
	rt.seen[id] = time.Now()
	if len(rt.seen) > rt.maxSeen {
		// Bounded dedup cache: drop oldest entries.
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
		}
	}
	return false
}

// transportScore ranks a link type by expected throughput for relaying
// (higher carries more traffic). Mirrors the Anonymous.md §5.3 link order.
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

// Score ranks a neighbor for relay selection: relay consent dominates, then
// link throughput, then freshness. Higher is better. Callers order candidate
// relays by this score so packets prefer fast, fresh, consenting links.
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

// BestRelay returns the highest-scoring relay-consenting neighbor, optionally
// excluding one device (the packet's final destination is tried separately).
// Returns nil when no eligible neighbor exists.
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

// Expire drops neighbors not seen within maxAge so stale routes (devices that
// went out of range or powered off) stop receiving forwarded traffic.
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

// TTLExpired reports whether a packet's TTL has been exhausted.
func TTLExpired(p *Packet) bool { return p.TTL <= 0 }

// Knows reports whether the packet id was already processed, without recording
// it (diagnostics for simulators and tooling).
func (rt *RouteTable) Knows(id string) bool {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	_, ok := rt.seen[id]
	return ok
}
