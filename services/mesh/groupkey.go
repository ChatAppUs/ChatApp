package mesh

// groupkey.go — per-group sender-key management and rotation.
//
// Group payloads previously used the node's static identity key, so every
// member of every group shared one key and there was no way to evict a member
// or bound the damage of a leaked key. This file gives each group its own
// AES-256 sender key, derived from a fresh CSPRNG secret, and rotates it on a
// schedule or on demand. A rotated key is advertised to members inside a
// signed group-key packet, so a member that missed the rotation cannot read
// traffic sealed under the new key until it receives the advertisement.
//
// This is the sender-key model used by Signal-style group messaging: one
// symmetric key per group, rotated to evict members and bound key lifetime.
// It deliberately does not attempt per-member pairwise keys (which would make
// a group message N encryptions); the mesh analyses list group sender-key
// rotation as the required primitive, and this implements it.

import (
	"crypto/rand"
	"errors"
	"sync"
	"time"
)

// GroupKey is a group's current AES-256 sender key and its epoch.
type GroupKey struct {
	Key   IdentityKey
	Epoch int64
}

// groupKeyEntry is one group's key state.
type groupKeyEntry struct {
	key       GroupKey
	createdAt time.Time
}

// GroupKeyManager owns per-group sender keys and their rotation.
type GroupKeyManager struct {
	mu      sync.Mutex
	groups  map[string]*groupKeyEntry
	now     func() time.Time
	// rotationInterval is how often a group key is rotated on the schedule.
	rotationInterval time.Duration
}

// NewGroupKeyManager creates a group key manager. rotationInterval <= 0 uses
// the default (24h); a group key is rotated when it is older than the
// interval, or immediately on RotateGroup.
func NewGroupKeyManager(rotationInterval time.Duration) *GroupKeyManager {
	if rotationInterval <= 0 {
		rotationInterval = 24 * time.Hour
	}
	return &GroupKeyManager{
		groups:           make(map[string]*groupKeyEntry),
		now:              time.Now,
		rotationInterval: rotationInterval,
	}
}

// SetNow overrides the manager's clock for deterministic tests.
func (m *GroupKeyManager) SetNow(now func() time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if now != nil {
		m.now = now
	}
}

// KeyFor returns the current key for a group, rotating it first if it is past
// its rotation interval. It creates the group's key on first use.
func (m *GroupKeyManager) KeyFor(groupID string) (GroupKey, error) {
	if groupID == "" {
		return GroupKey{}, errors.New("mesh: group id is required")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	e, ok := m.groups[groupID]
	if !ok {
		k, err := newGroupKey(1)
		if err != nil {
			return GroupKey{}, err
		}
		m.groups[groupID] = &groupKeyEntry{key: k, createdAt: now}
		return k, nil
	}
	// Rotate on schedule: a key older than the interval is replaced so a
	// long-lived group does not keep one key forever.
	if now.Sub(e.createdAt) >= m.rotationInterval {
		k, err := newGroupKey(e.key.Epoch + 1)
		if err != nil {
			return GroupKey{}, err
		}
		e.key = k
		e.createdAt = now
		return k, nil
	}
	return e.key, nil
}

// RotateGroup forces a rotation for a group, returning the new key. This is
// how a member is evicted: the new key is advertised to the remaining members
// and the evicted member never receives it.
func (m *GroupKeyManager) RotateGroup(groupID string) (GroupKey, error) {
	if groupID == "" {
		return GroupKey{}, errors.New("mesh: group id is required")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.groups[groupID]
	next := int64(1)
	if ok {
		next = e.key.Epoch + 1
	}
	k, err := newGroupKey(next)
	if err != nil {
		return GroupKey{}, err
	}
	m.groups[groupID] = &groupKeyEntry{key: k, createdAt: m.now()}
	return k, nil
}

// AdoptKey installs a group key received from a signed group-key advertisement
// (e.g. after a rotation by the group owner). A newer epoch replaces an older
// one; an older or equal epoch is ignored so a stale advertisement cannot
// roll a group back to a revoked key.
func (m *GroupKeyManager) AdoptKey(groupID string, k GroupKey) error {
	if groupID == "" {
		return errors.New("mesh: group id is required")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if e, ok := m.groups[groupID]; ok && k.Epoch <= e.key.Epoch {
		return nil
	}
	m.groups[groupID] = &groupKeyEntry{key: k, createdAt: m.now()}
	return nil
}

// Epoch returns the current epoch for a group, or 0 if the group is unknown.
func (m *GroupKeyManager) Epoch(groupID string) int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	if e, ok := m.groups[groupID]; ok {
		return e.key.Epoch
	}
	return 0
}

// Groups returns the set of group ids this node holds keys for.
func (m *GroupKeyManager) Groups() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, 0, len(m.groups))
	for id := range m.groups {
		out = append(out, id)
	}
	return out
}

func newGroupKey(epoch int64) (GroupKey, error) {
	var k IdentityKey
	if _, err := rand.Read(k[:]); err != nil {
		return GroupKey{}, err
	}
	return GroupKey{Key: k, Epoch: epoch}, nil
}
