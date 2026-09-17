package mesh

// transport_policy.go — the transport selection engine (§97).
//
// Native clients expose up to three radio transports (Bluetooth, Wi-Fi
// Direct, local Wi-Fi/hotspot) plus two logical paths (store-and-forward
// relay, internet). Which link a packet takes must NOT be hardwired: the
// spec requires a policy-driven engine that weighs availability, user
// preference, cost, battery, bandwidth, latency, privacy and security.
//
// Design:
//
//   - A TransportCandidate describes one usable path with its live metrics.
//   - A TransportPolicy assigns each candidate a base priority class plus a
//     set of weights; the policy can be swapped at runtime from a config
//     file or admin control without touching the engine.
//   - SelectTransport scores every candidate and returns the best one, plus
//     the full ranking for diagnostics. Ties break deterministically by
//     (class, name) so tests and logs are stable.
//
// Security invariant: a faster transport never weakens security. Candidates
// must declare that they provide the same authentication and integrity
// guarantees as every other transport (the mesh packet layer enforces this
// cryptographically for all of them); a candidate that cannot (Secure=false)
// is excluded from selection outright, whatever its score.

import (
	"math"
	"sort"
	"strings"
	"sync"
	"time"
)

// TransportClass orders candidate kinds from most to least preferred by
// default. Lower is better.
type TransportClass int

const (
	ClassDirectSecure  TransportClass = iota // 1. existing direct secure local link
	ClassHighBandwidth                       // 2. best local high-bandwidth link
	ClassLowBandwidth                        // 3. Bluetooth / local low-bandwidth link
	ClassStoreForward                        // 4. store-and-forward relay
	ClassInternet                            // 5. internet transport (last resort)
)

// String renders the class for logs and status.
func (c TransportClass) String() string {
	switch c {
	case ClassDirectSecure:
		return "direct_secure"
	case ClassHighBandwidth:
		return "high_bandwidth"
	case ClassLowBandwidth:
		return "low_bandwidth"
	case ClassStoreForward:
		return "store_forward"
	case ClassInternet:
		return "internet"
	}
	return "unknown"
}

// TransportCandidate is one selectable path at decision time.
type TransportCandidate struct {
	// Name identifies the transport ("wifi_direct", "bluetooth", "local_wifi",
	// "store_forward", "internet"). Never empty.
	Name string
	// Class is the policy's default rank for this transport kind.
	Class TransportClass
	// Available is true when the radio/path is usable right now.
	Available bool
	// Secure must be true: the path provides the same authentication and
	// integrity as the rest of the mesh (the packet crypto does the actual
	// enforcement; this flag gates admission).
	Secure bool
	// BandwidthKBps is the measured or advertised throughput.
	BandwidthKBps int64
	// LatencyMS is the measured round-trip latency. Zero means unknown.
	LatencyMS int64
	// CostScore is a 0..1 penalty (0 free, 1 expensive: metered data, paid relay).
	CostScore float64
	// BatteryCost is a 0..1 penalty per byte (Bluetooth ~1, Wi-Fi ~0.3).
	BatteryCost float64
	// PrivacyScore is a 0..1 bonus (1 = fully local/offline path).
	PrivacyScore float64
	// Preferred is true when the user pinned this transport.
	Preferred bool
	// LastErrorAt, if within CooldownWindow, demotes a flapping link.
	LastErrorAt time.Time
}

// TransportWeights control how much each criterion moves the score. They are
// exported so a policy file (or admin control) can retune the engine without
// code changes. Zero-value weights fall back to DefaultTransportWeights.
type TransportWeights struct {
	Bandwidth float64
	Latency   float64
	Cost      float64
	Battery   float64
	Privacy   float64
	// ClassStep is the score gap between consecutive classes: a better-class
	// link only loses to a lower-class link when the lower-class link is
	// dramatically better on the weighted criteria.
	ClassStep float64
}

// DefaultTransportWeights are balanced for phones on battery.
func DefaultTransportWeights() TransportWeights {
	return TransportWeights{
		Bandwidth: 1.0,
		Latency:   1.5,
		Cost:      2.0,
		Battery:   3.0,
		Privacy:   1.0,
		ClassStep: 5.0,
	}
}

// withDefaults fills zero weights from the defaults (per-field).
func (w TransportWeights) withDefaults() TransportWeights {
	d := DefaultTransportWeights()
	if w.Bandwidth == 0 {
		w.Bandwidth = d.Bandwidth
	}
	if w.Latency == 0 {
		w.Latency = d.Latency
	}
	if w.Cost == 0 {
		w.Cost = d.Cost
	}
	if w.Battery == 0 {
		w.Battery = d.Battery
	}
	if w.Privacy == 0 {
		w.Privacy = d.Privacy
	}
	if w.ClassStep == 0 {
		w.ClassStep = d.ClassStep
	}
	return w
}

// CooldownWindow is how long a transport that just errored stays demoted.
const CooldownWindow = 15 * time.Second

// cooldownPenalty subtracts up to 2 points from a candidate that failed
// recently, easing linearly back to zero as the window expires.
func cooldownPenalty(lastError time.Time, now time.Time) float64 {
	if lastError.IsZero() {
		return 0
	}
	elapsed := now.Sub(lastError)
	if elapsed >= CooldownWindow {
		return 0
	}
	frac := 1 - float64(elapsed)/float64(CooldownWindow)
	return 2 * frac
}

// ScoreCandidate computes a candidate's score under weights. Higher wins.
// Ineligible candidates (unavailable or insecure) return (0, false).
func ScoreCandidate(c TransportCandidate, w TransportWeights, now time.Time) (float64, bool) {
	if !c.Available || !c.Secure || strings.TrimSpace(c.Name) == "" {
		return 0, false
	}
	w = w.withDefaults()

	// Base score from class: better class = higher floor.
	base := -float64(c.Class) * w.ClassStep

	// Bandwidth: log2 scaling so 1 Mbps vs 2 Mbps matters, 10 vs 11 doesn't.
	bw := float64(0)
	if c.BandwidthKBps > 0 {
		bw = log2f(1+float64(c.BandwidthKBps)) * w.Bandwidth
	}
	// Latency: lower is better; unknown latency contributes nothing.
	lat := float64(0)
	if c.LatencyMS > 0 {
		lat = -log2f(1+float64(c.LatencyMS)) * w.Latency
	}
	userBonus := float64(0)
	if c.Preferred {
		userBonus = w.ClassStep // user preference overrides one class step
	}
	score := base + bw + lat +
		(1-c.CostScore)*w.Cost*2 +
		(1-c.BatteryCost)*w.Battery +
		c.PrivacyScore*w.Privacy +
		userBonus -
		cooldownPenalty(c.LastErrorAt, now)
	return score, true
}

func log2f(v float64) float64 {
	// bit-twiddling-free log2 approximation via repeated squaring is
	// overkill; math.Log2 is fine here and deterministic enough for ranking.
	return math.Log2(v)
}

// TransportSelector owns the active policy and ranks candidates. Safe for
// concurrent use; the policy can be swapped at runtime (admin control §110).
type TransportSelector struct {
	mu      sync.Mutex
	weights TransportWeights
}

// NewTransportSelector builds a selector with the default weights.
func NewTransportSelector() *TransportSelector {
	return &TransportSelector{weights: DefaultTransportWeights()}
}

// SetWeights swaps the weights (policy hot-reload). Zero fields inherit
// defaults per-field.
func (s *TransportSelector) SetWeights(w TransportWeights) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.weights = w.withDefaults()
}

// Weights returns a copy of the current weights.
func (s *TransportSelector) Weights() TransportWeights {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.weights
}

// RankedTransport is a scored candidate with its final position.
type RankedTransport struct {
	Candidate TransportCandidate
	Score     float64
}

// SelectTransport ranks every candidate and returns the winner plus the full
// ranking (best first, ineligible candidates excluded). If nothing qualifies,
// ok is false and the caller falls back to store-and-forward (which is always
// available and secure by construction).
func (s *TransportSelector) SelectTransport(cands []TransportCandidate, now time.Time) (TransportCandidate, []RankedTransport, bool) {
	s.mu.Lock()
	w := s.weights
	s.mu.Unlock()

	ranked := make([]RankedTransport, 0, len(cands))
	for _, c := range cands {
		if score, ok := ScoreCandidate(c, w, now); ok {
			ranked = append(ranked, RankedTransport{Candidate: c, Score: score})
		}
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].Score != ranked[j].Score {
			return ranked[i].Score > ranked[j].Score
		}
		ci, cj := ranked[i].Candidate, ranked[j].Candidate
		if ci.Class != cj.Class {
			return ci.Class < cj.Class
		}
		return ci.Name < cj.Name
	})
	if len(ranked) == 0 {
		return TransportCandidate{}, nil, false
	}
	return ranked[0].Candidate, ranked, true
}

// FallbackStoreForward is the always-eligible candidate of last resort: the
// store-and-forward relay path is authenticated and available wherever the
// mesh has a relay, so selection can never dead-end.
func FallbackStoreForward() TransportCandidate {
	return TransportCandidate{
		Name:      "store_forward",
		Class:     ClassStoreForward,
		Available: true,
		Secure:    true,
		CostScore: 0.2,
	}
}

// TransportPolicy bundles the configurable selection weights with the scorer.
// Hosts may adjust weights at runtime (user preference, low-power mode); the
// security floor in ScoreCandidate is not negotiable.
type TransportPolicy struct {
	Weights TransportWeights
	sel     *TransportSelector
}

// NewTransportPolicy returns a policy with balanced default weights.
func NewTransportPolicy() *TransportPolicy {
	return &TransportPolicy{Weights: DefaultTransportWeights(), sel: NewTransportSelector()}
}

// Select ranks candidates under the current weights and returns the winner
// plus the full ranking for observability.
func (p *TransportPolicy) Select(cands []TransportCandidate, now time.Time) (TransportCandidate, []RankedTransport, bool) {
	p.sel.SetWeights(p.Weights)
	return p.sel.SelectTransport(cands, now)
}

// Order re-ranks node neighbours through the policy: the route-table score
// decides eligibility, the policy decides the send order. Neighbours whose
// transport class or radio health scores poorly are tried last, and failed
// neighbours keep their cooldown penalty so a dead link is not retried first
// on the next packet.
func (p *TransportPolicy) Order(nbs []*Neighbor, now time.Time) []*Neighbor {
	if len(nbs) < 2 {
		return nbs
	}
	type ranked struct {
		nb    *Neighbor
		score float64
	}
	out := make([]ranked, 0, len(nbs))
	for _, nb := range nbs {
		c := TransportCandidate{
			Name:          nb.DeviceID,
			Class:         classForTransport(nb.Transport),
			BandwidthKBps: int64(bandwidthHintFor(classForTransport(nb.Transport))),
			LatencyMS:     int64(latencyHintFor(classForTransport(nb.Transport)) * 1000),
			PrivacyScore:  1.0, // every in-engine transport is authenticated + encrypted
			LastErrorAt:   nb.UnavailableUntil,
		}
		score, ok := ScoreCandidate(c, p.Weights, now)
		if !ok {
			score = -1e9 // ineligible: send strictly last
		}
		out = append(out, ranked{nb: nb, score: score})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].score > out[j].score })
	ordered := make([]*Neighbor, len(out))
	for i, r := range out {
		ordered[i] = r.nb
	}
	return ordered
}

// classForTransport maps a neighbour's advertised transport name to its policy
// class. Unknown transports are treated as low-bandwidth local links — they
// still carry authenticated, encrypted packets, just with conservative hints.
func classForTransport(transport string) TransportClass {
	switch transport {
	case "local_wifi", "wifi_direct":
		return ClassHighBandwidth
	case "bluetooth", "ble":
		return ClassLowBandwidth
	case "internet":
		return ClassInternet
	default:
		return ClassLowBandwidth
	}
}

// bandwidthHintFor is the conservative throughput hint per class when the
// caller has no measurement (bytes per second). Real measurements, when
// available, replace the hint via the candidate fields.
func bandwidthHintFor(c TransportClass) float64 {
	switch c {
	case ClassHighBandwidth:
		return 20_000_000 // ~20 MB/s
	case ClassLowBandwidth:
		return 200_000 // ~200 KB/s
	case ClassInternet:
		return 5_000_000
	default:
		return 1_000_000
	}
}

// latencyHintFor is the conservative one-way latency hint per class (seconds).
func latencyHintFor(c TransportClass) float64 {
	switch c {
	case ClassHighBandwidth:
		return 0.005
	case ClassLowBandwidth:
		return 0.15
	case ClassInternet:
		return 0.05
	default:
		return 0.05
	}
}
