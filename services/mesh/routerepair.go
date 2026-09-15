package mesh

// routerepair.go — proactive route repair for the mesh.
//
// The mesh previously reacted to a failed link only by marking the neighbour
// failed and quarantining it for a cooldown (routing.go MarkFailure). That is
// passive: a packet whose best route just died waits for the next beacon or
// the cooldown to expire. This file adds proactive route repair — when a
// neighbour fails, the node immediately re-selects the best remaining relay
// and re-queues the packet toward it, so a single dead link does not stall a
// transfer until the next discovery cycle.

import (
	"sort"
	"time"
)

// repairCandidates returns the best relay candidates for a destination,
// excluding the given failed neighbour, ordered by route score. This is the
// re-selection step of route repair: after a link fails, the node picks the
// next-best path instead of waiting.
func (rt *RouteTable) repairCandidates(dst, exclude string, max int) []*Neighbor {
	now := time.Now()
	neighbors := rt.Neighbors()
	sort.SliceStable(neighbors, func(i, j int) bool {
		return neighbors[i].Score(now) > neighbors[j].Score(now)
	})
	out := make([]*Neighbor, 0, max)
	seen := make(map[string]struct{}, max)
	for _, nb := range neighbors {
		if len(out) >= max {
			break
		}
		if nb.DeviceID == exclude || nb.DeviceID == dst {
			continue
		}
		if !nb.RelayOK {
			continue
		}
		if now.Before(nb.UnavailableUntil) {
			continue
		}
		if _, dup := seen[nb.DeviceID]; dup {
			continue
		}
		seen[nb.DeviceID] = struct{}{}
		out = append(out, nb)
	}
	return out
}

// RepairRoute re-routes a packet after a failed hop. It returns true if the
// packet was handed to a new relay (or delivered locally), false if no
// alternate route exists yet (the packet stays buffered for store-and-forward).
func (n *Node) RepairRoute(p *Packet, failedNeighbor string) bool {
	if p == nil {
		return false
	}
	// If the destination is directly known and reachable, prefer it.
	if p.Dst != "" {
		for _, nb := range n.routes.Neighbors() {
			if nb.DeviceID == p.Dst && !time.Now().Before(nb.UnavailableUntil) {
				if n.sendTo(nb.Addr, p) {
					n.routes.MarkSuccess(nb.DeviceID)
					return true
				}
			}
		}
	}
	// Otherwise pick the best remaining relay.
	candidates := n.routes.repairCandidates(p.Dst, failedNeighbor, n.maxFanout)
	for _, nb := range candidates {
		if n.sendTo(nb.Addr, p) {
			n.routes.MarkSuccess(nb.DeviceID)
			return true
		}
		n.routes.MarkFailure(nb.DeviceID, time.Now())
	}
	return false
}
