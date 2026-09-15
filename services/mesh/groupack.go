package mesh

// groupack.go — per-member acknowledgements for group messages.
//
// Group payloads were best-effort: a group message was flooded to members and
// the sender had no way to know who received it. This file adds a group
// acknowledgement primitive. A group message carries a transfer id; each
// member that receives it returns a signed acknowledgement naming the group
// and the transfer id, and the sender tracks how many members have
// acknowledged. This is the per-member ACK the mesh analyses list as
// outstanding — it needs per-member keys, which groupkey.go provides.

import (
	"errors"
	"sync"
	"time"
)

// GroupAckState is the aggregate delivery state of one group transfer.
type GroupAckState string

const (
	// GroupAckPending means the group transfer is in flight.
	GroupAckPending GroupAckState = "pending"
	// GroupAckPartial means some but not all members acknowledged.
	GroupAckPartial GroupAckState = "partial"
	// GroupAckComplete means every known member acknowledged.
	GroupAckComplete GroupAckState = "complete"
)

// GroupTransfer tracks one group message's per-member acknowledgements.
type GroupTransfer struct {
	ID        string
	GroupID   string
	Members   []string
	Acked     map[string]bool
	State     GroupAckState
	CreatedAt time.Time
}

// groupAckTracker owns the sender-side group acknowledgement state.
type groupAckTracker struct {
	mu        sync.Mutex
	active    map[string]*GroupTransfer
	order     []string
	maxActive int
	now       func() time.Time
}

// NewGroupAckTracker creates a group acknowledgement tracker.
func NewGroupAckTracker() *groupAckTracker {
	return &groupAckTracker{
		active:    make(map[string]*GroupTransfer),
		maxActive: 1024,
		now:       time.Now,
	}
}

// SetNow overrides the tracker's clock for deterministic tests.
func (t *groupAckTracker) SetNow(now func() time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if now != nil {
		t.now = now
	}
}

// Create registers a group transfer for the given members.
func (t *groupAckTracker) Create(id, groupID string, members []string) *GroupTransfer {
	now := t.now()
	tr := &GroupTransfer{
		ID:        id,
		GroupID:   groupID,
		Members:   append([]string(nil), members...),
		Acked:     make(map[string]bool),
		State:     GroupAckPending,
		CreatedAt: now,
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.active[id] = tr
	t.order = append(t.order, id)
	// Bounded retention: drop the oldest transfer when over capacity.
	for len(t.active) > t.maxActive && len(t.order) > 0 {
		oldest := t.order[0]
		t.order = t.order[1:]
		delete(t.active, oldest)
	}
	return tr
}

// Ack records a member's acknowledgement and recomputes the aggregate state.
// It reports whether the aggregate state changed (e.g. pending -> partial).
func (t *groupAckTracker) Ack(id, member string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	tr, ok := t.active[id]
	if !ok {
		return false
	}
	if tr.Acked[member] {
		return false
	}
	tr.Acked[member] = true
	prev := tr.State
	acked := 0
	for _, m := range tr.Members {
		if tr.Acked[m] {
			acked++
		}
	}
	switch {
	case acked == 0:
		tr.State = GroupAckPending
	case acked >= len(tr.Members):
		tr.State = GroupAckComplete
	default:
		tr.State = GroupAckPartial
	}
	return tr.State != prev
}

// Get returns a copy of one group transfer's state.
func (t *groupAckTracker) Get(id string) (GroupTransfer, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	tr, ok := t.active[id]
	if !ok {
		return GroupTransfer{}, false
	}
	cp := *tr
	cp.Members = append([]string(nil), tr.Members...)
	cp.Acked = make(map[string]bool, len(tr.Acked))
	for k, v := range tr.Acked {
		cp.Acked[k] = v
	}
	return cp, true
}

// Snapshot returns every retained group transfer, newest first.
func (t *groupAckTracker) Snapshot() []GroupTransfer {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]GroupTransfer, 0, len(t.active))
	for _, tr := range t.active {
		cp := *tr
		cp.Members = append([]string(nil), tr.Members...)
		cp.Acked = make(map[string]bool, len(tr.Acked))
		for k, v := range tr.Acked {
			cp.Acked[k] = v
		}
		out = append(out, cp)
	}
	return out
}

// ErrGroupAckNoMembers is returned when a group transfer is created with no
// members to acknowledge.
var ErrGroupAckNoMembers = errors.New("mesh: group transfer has no members")
