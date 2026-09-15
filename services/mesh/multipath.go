package mesh

// multipath.go — explicit multipath selection for the mesh.
//
// The forwarding buffer fans a packet out to a bounded set of neighbours, but
// it did not *choose* paths: it took the top-scoring candidates with no
// notion of path diversity. This file adds explicit multipath selection — a
// node picks a set of relays that are both high-scoring and topologically
// diverse (different transports, different last hops), so a single radio or
// a single congested relay does not become the bottleneck for a transfer.

import (
	"sort"
	"time"
)

// selectMultipath returns up to max relays for a destination, preferring
// high-scoring neighbours and enforcing transport diversity so the chosen
// paths do not all ride the same radio. The destination itself is always the
// first candidate when directly known.
//
// For a group message (dst == ""), the packet is a broadcast: every
// directly-connected neighbour — member or relay — is a candidate, so a group
// message reaches all peers in range, not only relays.
func (rt *RouteTable) selectMultipath(dst string, max int) []*Neighbor {
	now := time.Now()
	neighbors := rt.Neighbors()
	sort.SliceStable(neighbors, func(i, j int) bool {
		return neighbors[i].Score(now) > neighbors[j].Score(now)
	})

	out := make([]*Neighbor, 0, max)
	seen := make(map[string]struct{}, max)
	transports := make(map[string]struct{}, max)

	// Group broadcast: flood to every directly-connected neighbour.
	if dst == "" {
		for _, nb := range neighbors {
			if len(out) >= max {
				break
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

	// Direct destination first.
	for _, nb := range neighbors {
		if nb.DeviceID == dst {
			out = append(out, nb)
			seen[nb.DeviceID] = struct{}{}
			transports[nb.Transport] = struct{}{}
			break
		}
	}

	// Then high-scoring relays, preferring a transport we have not used yet.
	for _, nb := range neighbors {
		if len(out) >= max {
			break
		}
		if _, dup := seen[nb.DeviceID]; dup {
			continue
		}
		if !nb.RelayOK {
			continue
		}
		if now.Before(nb.UnavailableUntil) {
			continue
		}
		// Prefer a diverse transport: if we already picked a relay on this
		// transport, only take another one when no diverse option remains.
		if _, used := transports[nb.Transport]; used {
			continue
		}
		seen[nb.DeviceID] = struct{}{}
		transports[nb.Transport] = struct{}{}
		out = append(out, nb)
	}

	// Fill any remaining slots with the best relays regardless of transport.
	for _, nb := range neighbors {
		if len(out) >= max {
			break
		}
		if _, dup := seen[nb.DeviceID]; dup {
			continue
		}
		if !nb.RelayOK {
			continue
		}
		if now.Before(nb.UnavailableUntil) {
			continue
		}
		seen[nb.DeviceID] = struct{}{}
		out = append(out, nb)
	}
	return out
}
