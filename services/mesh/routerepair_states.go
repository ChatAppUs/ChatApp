package mesh

import (
	"sync"
	"time"
)

type RepairState string

const (
	RepairIdle     RepairState = "idle"
	RepairProbing  RepairState = "probing"
	RepairActive   RepairState = "active"
	RepairCoolDown RepairState = "cooldown"
)

type RepairEntry struct {
	Dest      string
	State     RepairState
	Fails     int
	Attempts  int
	LastProbe time.Time
	LastFail  time.Time
	ActiveVia string
	EnteredAt time.Time
}

type repairSM struct {
	mu            sync.Mutex
	entries       map[string]*RepairEntry
	maxProbes     int
	probeBackoffMs int
	cooldownMs    int
}

func newRepairSM() *repairSM {
	return &repairSM{
		entries:        make(map[string]*RepairEntry),
		maxProbes:      5,
		probeBackoffMs: 200,
		cooldownMs:     30000,
	}
}

func (s *repairSM) OnLinkFailure(dest, failedNb string) RepairState {
	s.mu.Lock(); defer s.mu.Unlock()
	entry, ok := s.entries[dest]
	if !ok {
		entry = &RepairEntry{Dest: dest}
		s.entries[dest] = entry
	}
	now := time.Now()
	switch entry.State {
	case RepairIdle:
		entry.State = RepairProbing
		entry.Attempts = 1
		entry.LastProbe = now
		entry.LastFail = now
		entry.EnteredAt = now
	case RepairProbing:
		if entry.Attempts >= s.maxProbes {
			entry.State = RepairCoolDown
			entry.EnteredAt = now
		} else {
			backoff := time.Duration(s.probeBackoffMs*entry.Attempts) * time.Millisecond
			if now.Sub(entry.LastProbe) >= backoff {
				entry.Attempts++
				entry.LastProbe = now
			}
		}
	case RepairActive:
		entry.State = RepairProbing
		entry.Attempts = 1
		entry.LastProbe = now
		entry.LastFail = now
		entry.ActiveVia = ""
		entry.EnteredAt = now
	case RepairCoolDown:
	}
	entry.Fails++
	entry.LastFail = now
	return entry.State
}

func (s *repairSM) OnProbeSuccess(dest, via string) RepairState {
	s.mu.Lock(); defer s.mu.Unlock()
	entry, ok := s.entries[dest]
	if !ok { return RepairIdle }
	entry.State = RepairActive
	entry.ActiveVia = via
	entry.EnteredAt = time.Now()
	return entry.State
}

func (s *repairSM) OnLinkRecovered(dest string) {
	s.mu.Lock(); defer s.mu.Unlock()
	entry, ok := s.entries[dest]
	if !ok { return }
	entry.State = RepairIdle
	entry.ActiveVia = ""
	entry.Attempts = 0
	entry.Fails = 0
}

func (s *repairSM) Tick(now time.Time) []string {
	s.mu.Lock(); defer s.mu.Unlock()
	var recovered []string
	for dest, entry := range s.entries {
		if entry.State == RepairCoolDown && now.Sub(entry.EnteredAt) >= time.Duration(s.cooldownMs)*time.Millisecond {
			entry.State = RepairIdle
			entry.Attempts = 0
			recovered = append(recovered, dest)
		}
	}
	return recovered
}

func (s *repairSM) Get(dest string) (RepairEntry, bool) {
	s.mu.Lock(); defer s.mu.Unlock()
	entry, ok := s.entries[dest]
	if !ok { return RepairEntry{}, false }
	return *entry, true
}

func (s *repairSM) Snapshot() []RepairEntry {
	s.mu.Lock(); defer s.mu.Unlock()
	out := make([]RepairEntry, 0, len(s.entries))
	for _, e := range s.entries { out = append(out, *e) }
	return out
}