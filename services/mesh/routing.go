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
	DeviceID         string
	Addr             string
	Transport        string
	LastSeen         time.Time
	RelayOK          bool // whether this neighbor consents to relay
	FailureCount     int
	UnavailableUntil time.Time
}

func NewRouteTable() *RouteTable {
	return &RouteTable{
		neigh:   make(map[string]*Neighbor),
		seen:    make(map[string]time.Time),
		bestTTL: make(map[string]int),
		maxSeen: 10000,
	}
}

func (rt *RouteTable) Upsert(b *Beacon) {
	if b == nil || b.DeviceID == "" || len(b.DeviceID) > 256 || b.Addr == "" || len(b.Addr) > 512 || len(b.Transport) > 64 {
		return
	}
	if b.Kind != "" && b.Kind != "member" && b.Kind != "relay" {
		return
	}
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

// MarkFailure temporarily quarantines a neighbour after repeated send errors.
// A failed link is removed from candidate selection before beacon expiry, then
// allowed to recover after a bounded cooldown.
func (rt *RouteTable) MarkFailure(deviceID string, now time.Time) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	n, ok := rt.neigh[deviceID]
	if !ok {
		return
	}
	n.FailureCount++
	if n.FailureCount >= 3 {
		n.UnavailableUntil = now.Add(30 * time.Second)
	}
}

// MarkSuccess clears a neighbour's transient send-failure penalty.
func (rt *RouteTable) MarkSuccess(deviceID string) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if n, ok := rt.neigh[deviceID]; ok {
		n.FailureCount = 0
		n.UnavailableUntil = time.Time{}
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

const routeSeenTTL = 10 * time.Minute

func (rt *RouteTable) pruneSeenLocked(now time.Time) {
	for id, seenAt := range rt.seen {
		if now.Sub(seenAt) > routeSeenTTL {
			delete(rt.seen, id)
			delete(rt.bestTTL, id)
		}
	}
	for len(rt.seen) > rt.maxSeen {
		var oldestID string
		var oldest time.Time
		for id, seenAt := range rt.seen {
			if oldestID == "" || seenAt.Before(oldest) {
				oldestID, oldest = id, seenAt
			}
		}
		if oldestID == "" {
			break
		}
		delete(rt.seen, oldestID)
		delete(rt.bestTTL, oldestID)
	}
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
	now := time.Now()
	rt.pruneSeenLocked(now)
	if best, ok := rt.bestTTL[id]; ok && ttl <= best {
		return false, false
	}
	rt.bestTTL[id] = ttl
	_, seenBefore := rt.seen[id]
	if !seenBefore {
		rt.seen[id] = time.Now()
		rt.pruneSeenLocked(now)
	}
	return true, !seenBefore
}

func (rt *RouteTable) Seen(id string) bool {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.pruneSeenLocked(time.Now())
	if _, ok := rt.seen[id]; ok {
		return true
	}
	rt.seen[id] = time.Now()
	rt.bestTTL[id] = 0
	rt.pruneSeenLocked(time.Now())
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
	if now.Before(n.UnavailableUntil) {
		return -1000
	}
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
