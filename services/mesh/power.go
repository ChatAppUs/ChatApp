package mesh

// power.go — battery and resource management for the offline mesh (§101).
//
// Mesh networking is expensive on battery-powered devices. This module owns
// every resource knob in one place so no caller ever runs an unrestricted
// scan loop or an unbounded queue:
//
//   - ScanIntervals: adaptive discovery scan cadence. Scans back off when the
//     radio is idle and the device is on battery, and tighten when peers are
//     active, when the user expects traffic, or when the device is charging.
//   - ConnectionLimits: cap on simultaneously maintained peer links.
//   - QueueLimits: cap on buffered packets per link (enforced with the
//     priority queue, which already drops lowest class first).
//   - Relay quotas: enforced by RelayLimiter (see relaylimit.go); this module
//     scales the quota down on low battery.
//   - Battery-aware / charging-aware behaviour: a single PowerState input
//     (level percent, charging flag, power-saver flag) drives everything.
//
// The manager is purely advisory state + pure functions over that state, so
// native clients can mirror it exactly and so tests can simulate days of
// operation deterministically.

import (
	"sync"
	"time"
)

// PowerState is the device's current power situation, as reported by the OS.
type PowerState struct {
	// BatteryPercent is 0..100, or -1 when unknown (desktops, CI).
	BatteryPercent int
	// Charging is true when externally powered.
	Charging bool
	// PowerSaver is true when the OS power-saver / low-power mode is active.
	PowerSaver bool
}

// PowerStateUnknown is the safe default: behave as if the battery is healthy.
var PowerStateUnknown = PowerState{BatteryPercent: -1}

// ScanIntervals is the full set of scan cadences, in milliseconds.
type ScanIntervals struct {
	// Active is the aggressive cadence used right after boot, after a topology
	// change, or while the user is actively messaging. Fast, bounded.
	ActiveMS int64
	// Idle is the relaxed cadence once discovery has settled.
	IdleMS int64
	// Doze is the deep-sleep cadence used when on battery, power-saver is on
	// and no traffic has flowed for a while. Never zero: the mesh must still
	// make progress.
	DozeMS int64
}

// DefaultScanIntervals are tuned for phones on battery: a 5s beacon period
// pairs with a 10s active scan; doze never exceeds two minutes.
func DefaultScanIntervals() ScanIntervals {
	return ScanIntervals{ActiveMS: 10_000, IdleMS: 30_000, DozeMS: 120_000}
}

// ClampScanIntervals rejects pathological configurations (zero or >10 min
// cadences) so a bad policy file can never fully silence discovery.
func ClampScanIntervals(in ScanIntervals) ScanIntervals {
	const minMS = 2_000
	const maxMS = 600_000
	clamp := func(v, def int64) int64 {
		if v <= 0 {
			return def
		}
		if v < minMS {
			return minMS
		}
		if v > maxMS {
			return maxMS
		}
		return v
	}
	d := DefaultScanIntervals()
	return ScanIntervals{
		ActiveMS: clamp(in.ActiveMS, d.ActiveMS),
		IdleMS:   clamp(in.IdleMS, d.IdleMS),
		DozeMS:   clamp(in.DozeMS, d.DozeMS),
	}
}

// ConnectionLimits bounds simultaneous peer maintenance.
type ConnectionLimits struct {
	// MaxPeers on battery; MaxPeersCharging when externally powered. Fewer
	// peers means fewer radios held open.
	MaxPeers          int
	MaxPeersCharging  int
	// MaxQueuePerLink bounds packets buffered per link. The priority queue
	// enforces this and sheds lowest-class traffic first.
	MaxQueuePerLink   int
}

// DefaultConnectionLimits returns the phone-sized defaults.
func DefaultConnectionLimits() ConnectionLimits {
	return ConnectionLimits{
		MaxPeers:         8,
		MaxPeersCharging: 16,
		MaxQueuePerLink:  256,
	}
}

// DefaultPowerState is the conservative assumption when no OS battery
// callback has fired yet: on battery, not charging, 50% level.
func DefaultPowerState() PowerState {
	return PowerState{BatteryPercent: 50, Charging: false, PowerSaver: false}
}

// PowerManager tracks recent activity and answers the two questions the node
// asks every tick: "how often should I scan?" and "may I keep this link?".
type PowerManager struct {
	mu            sync.Mutex
	state         PowerState
	intervals     ScanIntervals
	limits        ConnectionLimits
	lastTrafficAt time.Time // last inbound or outbound packet, zero = never
	lastScanAt    time.Time
	topologyAt    time.Time // last neighbor add/remove (triggers active mode)
}

// NewPowerManager builds a manager. A zero intervals value uses the defaults;
// all intervals are clamped (see ClampScanIntervals).
func NewPowerManager(state PowerState, intervals ScanIntervals, limits ConnectionLimits) *PowerManager {
	return &PowerManager{
		state:     state,
		intervals: ClampScanIntervals(intervals),
		limits:    limits,
	}
}

// SetPowerState updates the device power snapshot (called from the OS
// battery callback on native clients).
func (p *PowerManager) SetPowerState(s PowerState) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.state = s
}

// MarkTraffic records that a packet just flowed, which keeps scans active.
func (p *PowerManager) MarkTraffic(now time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.lastTrafficAt = now
}

// MarkTopologyChange records a neighbor add/remove; discovery runs at the
// active cadence for a while afterwards so the route table reconverges fast.
func (p *PowerManager) MarkTopologyChange(now time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.topologyAt = now
}

// topologyActiveWindow is how long after a topology change (or last traffic)
// the node keeps scanning at the aggressive cadence.
const topologyActiveWindow = 90 * time.Second

// NextScanIn returns how long the caller should wait before the next
// discovery scan, and whether that scan is the aggressive kind.
//
// Decision matrix (first match wins):
//  1. charging + recent traffic      -> active (radios are cheap on mains)
//  2. recent traffic or topology     -> active
//  3. power-saver + battery <= 20%   -> doze
//  4. battery <= 15%                 -> doze
//  5. otherwise                      -> idle
func (p *PowerManager) NextScanIn(now time.Time) (time.Duration, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	recentTraffic := !p.lastTrafficAt.IsZero() && now.Sub(p.lastTrafficAt) < topologyActiveWindow
	recentTopo := !p.topologyAt.IsZero() && now.Sub(p.topologyAt) < topologyActiveWindow

	active := time.Duration(p.intervals.ActiveMS) * time.Millisecond
	idle := time.Duration(p.intervals.IdleMS) * time.Millisecond
	doze := time.Duration(p.intervals.DozeMS) * time.Millisecond

	switch {
	case p.state.Charging && recentTraffic:
		return active, true
	case recentTraffic || recentTopo:
		return active, true
	case p.state.PowerSaver && p.state.BatteryPercent >= 0 && p.state.BatteryPercent <= 20:
		return doze, false
	case p.state.BatteryPercent >= 0 && p.state.BatteryPercent <= 15:
		return doze, false
	default:
		return idle, false
	}
}

// MaxPeers returns the connection cap for the current power state.
func (p *PowerManager) MaxPeers() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.state.Charging {
		return p.limits.MaxPeersCharging
	}
	return p.limits.MaxPeers
}

// MaxQueuePerLink returns the per-link buffer cap.
func (p *PowerManager) MaxQueuePerLink() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.limits.MaxQueuePerLink
}

// relayScale is how much of the relay quota survives low battery. Below 15%
// (or in power-saver) the node still relays — dropping relay duty silently
// breaks the mesh for everyone — but at a quarter rate to protect the host.
func (p *PowerManager) relayScale() float64 {
	if p.state.Charging {
		return 1
	}
	if p.state.PowerSaver && p.state.BatteryPercent >= 0 && p.state.BatteryPercent <= 20 {
		return 0.25
	}
	if p.state.BatteryPercent >= 0 && p.state.BatteryPercent <= 15 {
		return 0.25
	}
	return 1
}

// ScaleRelayQuota returns the relay byte budget for this tick given the base
// per-window budget.
func (p *PowerManager) ScaleRelayQuota(baseBytes int64) int64 {
	return int64(float64(baseBytes) * p.relayScale())
}

// Status renders the manager for /status and diagnostics.
func (p *PowerManager) Status() map[string]any {
	p.mu.Lock()
	defer p.mu.Unlock()
	return map[string]any{
		"battery_percent": p.state.BatteryPercent,
		"charging":        p.state.Charging,
		"power_saver":     p.state.PowerSaver,
		"scan_intervals": map[string]int64{
			"active_ms": p.intervals.ActiveMS,
			"idle_ms":   p.intervals.IdleMS,
			"doze_ms":   p.intervals.DozeMS,
		},
		"max_peers":        p.limits.MaxPeers,
		"max_peers_chg":    p.limits.MaxPeersCharging,
		"max_queue":        p.limits.MaxQueuePerLink,
		"relay_scale":      p.relayScale(),
	}
}
