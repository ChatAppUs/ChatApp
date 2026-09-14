package mesh

// calls.go — honest call feasibility for mesh-native calls.
//
// A real-time call needs sustained, low-latency, bidirectional capacity. The
// mesh can guarantee delivery of queued traffic (store-and-forward), but a
// live call is only honest when the destination is a direct, fresh neighbour.
// CallFeasible reports the three states the UI must act on:
//
//   "direct"      — fresh direct neighbour: a live call may proceed.
//   "multihop"    — only relay-consenting neighbours are known: delivery is
//                   likely but latency/jitter are unguaranteed. The caller
//                   should warn the user (or degrade to low-bitrate audio).
//   "unreachable" — no fresh neighbours: a live call cannot proceed; the
//                   client should fall back to a voice note or queued message.

import "time"

// CallFeasibility is the result of CallFeasible.
type CallFeasibility string

const (
	// CallDirect means a fresh direct neighbour exists.
	CallDirect CallFeasibility = "direct"
	// CallMultihop means only relay paths are available.
	CallMultihop CallFeasibility = "multihop"
	// CallUnreachable means no fresh neighbour can carry the call.
	CallUnreachable CallFeasibility = "unreachable"
)

// directFreshWindow is how fresh a direct neighbour must be to sustain a
// live call (beacons fire every 5s, so this tolerates ~6 misses).
const directFreshWindow = 30 * time.Second

// CallFeasible reports whether a live call to dst can proceed, degrade, or
// should fall back to a voice note, based on current route knowledge.
func (n *Node) CallFeasible(dst string) CallFeasibility {
	now := time.Now()
	var bestDirect *Neighbor
	var bestRelay *Neighbor
	for _, nb := range n.routes.Neighbors() {
		if now.Sub(nb.LastSeen) > neighborMaxAge {
			continue
		}
		if nb.DeviceID == dst {
			if bestDirect == nil || nb.Score(now) > bestDirect.Score(now) {
				bestDirect = nb
			}
			continue
		}
		if nb.RelayOK {
			if bestRelay == nil || nb.Score(now) > bestRelay.Score(now) {
				bestRelay = nb
			}
		}
	}
	if bestDirect != nil && now.Sub(bestDirect.LastSeen) <= directFreshWindow {
		return CallDirect
	}
	if bestRelay != nil {
		return CallMultihop
	}
	return CallUnreachable
}

// ShouldFallBackToVoiceNote is the explicit policy the clients must follow:
// when the topology cannot sustain a live call, offer the voice-note path.
func (n *Node) ShouldFallBackToVoiceNote(dst string) bool {
	return n.CallFeasible(dst) == CallUnreachable
}
