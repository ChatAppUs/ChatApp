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

// TTLExpired reports whether a packet's TTL has been exhausted.
func TTLExpired(p *Packet) bool { return p.TTL <= 0 }
