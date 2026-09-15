package mesh

// reliability.go — delivery guarantees: acknowledgements, bounded retries with
// backoff, alternate-path selection, and an explicit, user-visible delivery
// state machine including a dead-letter state.
//
// Why this exists
// ---------------
// The mesh analyses list reliability as a first-class requirement, not a
// nicety: "A route can disappear when a phone sleeps, moves, changes group
// owner or leaves. The protocol needs ACKs, retries, alternate paths, route
// expiry, duplicate handling and a user-visible delivery state." Route expiry
// and duplicate handling already existed (routing.go); ACKs, retries,
// alternate paths and the delivery state did not.
//
// Design
// ------
// A *transfer* is the sender's logical unit of reliable delivery. Each
// transmission attempt is a fresh packet with a fresh packet id (so
// intermediate-node dedup does not suppress a retry — routing.go only forwards
// a repeat id when it carries strictly more remaining TTL) but the SAME
// transfer id, so the destination can collapse duplicates to exactly one
// application delivery while still acknowledging every copy it receives. That
// combination is what makes a lost ACK or a lost first copy converge.
//
// Acknowledgements are end-to-end control packets. They ride the same routing,
// TTL, relay-quota and store-and-forward machinery as data, but are queued at
// PriorityControl so a congested relay still returns them promptly.
//
// Retries re-encrypt the payload with a fresh AEAD nonce rather than replaying
// the previous ciphertext, so no two transmissions of a transfer ever share a
// (key, nonce) pair.

import (
	"errors"
	"sort"
	"sync"
	"time"
)

// DeliveryState is the user-visible state of a reliable mesh transfer. Clients
// surface it directly: a message the user sent is not "sent" until it reaches
// StateAcked.
type DeliveryState string

const (
	// StateQueued means the transfer is created and awaiting its first send.
	StateQueued DeliveryState = "queued"
	// StateRelaying means a transmission is in flight (attempt >= 1) and the
	// sender is waiting for an acknowledgement.
	StateRelaying DeliveryState = "relaying"
	// StateAcked means the destination acknowledged the transfer. The payload
	// reached the intended recipient; intermediate hops may still be relaying
	// duplicate copies, which the destination collapses.
	StateAcked DeliveryState = "acked"
	// StateExpired means the transfer outlived its lifetime without being
	// acknowledged. It is a terminal state.
	StateExpired DeliveryState = "expired"
	// StateDeadLetter means the retry budget was exhausted. It is a terminal
	// state and is distinct from expired: the mesh stopped trying before the
	// payload's own lifetime ran out, which usually indicates an unreachable
	// peer rather than a slow one.
	StateDeadLetter DeliveryState = "dead_letter"
)

// IsTerminal reports whether no further attempt will be made.
func (s DeliveryState) IsTerminal() bool {
	return s == StateAcked || s == StateExpired || s == StateDeadLetter
}

// Reliability timing. These are the defaults the node uses; the tracker takes
// them as parameters so tests can drive the state machine deterministically
// instead of sleeping.
const (
	// ackTimeout is how long a sender waits for a destination ACK before
	// re-transmitting. Beacons fire every 5s, and a one-hop exchange on a
	// local radio completes well inside this window.
	ackTimeout = 6 * time.Second
	// maxTransferAttempts bounds retries. Five attempts across the
	// exponential backoff below spans roughly 90 seconds of trying, which
	// covers a peer that moved out of range briefly without letting a truly
	// unreachable destination occupy the queue indefinitely.
	maxTransferAttempts = 5
	// transferTTL is the hard lifetime of a reliable transfer.
	transferTTL = 30 * time.Minute
	// maxBackoff caps the exponential retry interval.
	maxBackoff = 60 * time.Second
	// maxReliablePayload bounds a reliable transfer's plaintext. Larger
	// payloads use the fragmentation path (fragment.go), whose parts are
	// individually best-effort; retaining a whole media file for retry on a
	// memory-constrained relay is the wrong trade.
	maxReliablePayload = 64 * 1024
	// transferRetention bounds how many transfers a node retains, so the
	// tracker cannot grow without bound on a long-lived device.
	transferRetention = 4096
	// receiveDedupeRetention bounds the receiver's delivered-transfer set.
	receiveDedupeRetention = 8192
)

// ErrTransferTooLarge reports a payload beyond the reliable-transfer ceiling.
var ErrTransferTooLarge = errors.New("mesh: payload exceeds the reliable transfer ceiling")

// Transfer is one reliable delivery and its state.
type Transfer struct {
	ID       string
	Dst      string
	Kind     PacketKind
	Priority Priority

	State    DeliveryState
	Attempts int
	// PacketID is the id of the most recent transmission.
	PacketID string
	// LastHop is the neighbor the most recent attempt was handed to, so the
	// next attempt prefers a different path (alternate-path retry).
	LastHop string
	// LastErr records why an attempt failed, for diagnostics.
	LastErr string

	CreatedAt   time.Time
	NextAttempt time.Time
	ExpiresAt   time.Time

	payload []byte
}

// TransferView is a copy of a transfer's observable state, safe to hand to
// API handlers and clients.
type TransferView struct {
	ID          string        `json:"id"`
	Dst         string        `json:"dst"`
	Kind        PacketKind    `json:"kind"`
	State       DeliveryState `json:"state"`
	Attempts    int           `json:"attempts"`
	CreatedAt   string        `json:"created_at"`
	ExpiresAt   string        `json:"expires_at"`
	NextAttempt string        `json:"next_attempt,omitempty"`
	LastHop     string        `json:"last_hop,omitempty"`
	LastErr     string        `json:"last_error,omitempty"`
}

// view renders a transfer for external consumption.
func (t *Transfer) view() TransferView {
	v := TransferView{
		ID:        t.ID,
		Dst:       t.Dst,
		Kind:      t.Kind,
		State:     t.State,
		Attempts:  t.Attempts,
		CreatedAt: t.CreatedAt.Format(time.RFC3339Nano),
		ExpiresAt: t.ExpiresAt.Format(time.RFC3339Nano),
		LastHop:   t.LastHop,
		LastErr:   t.LastErr,
	}
	if !t.NextAttempt.IsZero() {
		v.NextAttempt = t.NextAttempt.Format(time.RFC3339Nano)
	}
	return v
}

// transferTracker owns the sender-side transfer state machine and the
// receiver-side delivered-transfer set. It is safe for concurrent use.
type transferTracker struct {
	mu     sync.Mutex
	active map[string]*Transfer
	// order preserves creation order for bounded eviction.
	order []string
	// delivered is the receiver-side set of transfer ids already handed to
	// the application, so retries collapse to exactly-once delivery.
	delivered map[string]time.Time

	ackTimeout   time.Duration
	maxAttempts  int
	ttl          time.Duration
	maxActive    int
	maxDelivered int
	now          func() time.Time
}

// NewTransferTracker creates a tracker with explicit timing parameters.
func NewTransferTracker(ackTimeout time.Duration, maxAttempts int, ttl time.Duration) *transferTracker {
	if ackTimeout <= 0 {
		ackTimeout = 6 * time.Second
	}
	if maxAttempts <= 0 {
		maxAttempts = maxTransferAttempts
	}
	if ttl <= 0 {
		ttl = transferTTL
	}
	return &transferTracker{
		active:       make(map[string]*Transfer),
		delivered:    make(map[string]time.Time),
		ackTimeout:   ackTimeout,
		maxAttempts:  maxAttempts,
		ttl:          ttl,
		maxActive:    transferRetention,
		maxDelivered: receiveDedupeRetention,
		now:          time.Now,
	}
}

// Create registers a transfer and returns its id.
func (t *transferTracker) Create(dst string, kind PacketKind, pri Priority, payload []byte) *Transfer {
	now := t.now()
	tr := &Transfer{
		ID:          newPacketID(),
		Dst:         dst,
		Kind:        kind,
		Priority:    pri,
		State:       StateQueued,
		CreatedAt:   now,
		NextAttempt: now,
		ExpiresAt:   now.Add(t.ttl),
		payload:     append([]byte(nil), payload...),
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.active[tr.ID] = tr
	t.order = append(t.order, tr.ID)
	// Bounded retention: evict the oldest terminal transfer first, then the
	// oldest transfer overall. Never let the map grow without bound.
	for len(t.active) > t.maxActive && len(t.order) > 0 {
		victim := ""
		for _, id := range t.order {
			if cur, ok := t.active[id]; ok && cur.State.IsTerminal() {
				victim = id
				break
			}
		}
		if victim == "" {
			victim = t.order[0]
		}
		t.removeLocked(victim)
	}
	return tr
}

// removeLocked drops one transfer and its order entry. Caller holds the mutex.
func (t *transferTracker) removeLocked(id string) {
	delete(t.active, id)
	for i, v := range t.order {
		if v == id {
			t.order = append(t.order[:i], t.order[i+1:]...)
			return
		}
	}
}

// Get returns a copy of one transfer's state, if known.
func (t *transferTracker) Get(id string) (TransferView, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	tr, ok := t.active[id]
	if !ok {
		return TransferView{}, false
	}
	return tr.view(), true
}

// Snapshot returns every retained transfer, newest first.
func (t *transferTracker) Snapshot() []TransferView {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]TransferView, 0, len(t.active))
	for _, tr := range t.active {
		out = append(out, tr.view())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt > out[j].CreatedAt })
	return out
}

// Counts reports how many transfers sit in each state — the aggregate the
// status endpoint exposes.
func (t *transferTracker) Counts() map[string]int {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := map[string]int{
		string(StateQueued):     0,
		string(StateRelaying):   0,
		string(StateAcked):      0,
		string(StateExpired):    0,
		string(StateDeadLetter): 0,
	}
	for _, tr := range t.active {
		out[string(tr.State)]++
	}
	return out
}

// Pending reports transfers that are not yet terminal.
func (t *transferTracker) Pending() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	n := 0
	for _, tr := range t.active {
		if !tr.State.IsTerminal() {
			n++
		}
	}
	return n
}

// Ack marks a transfer acknowledged. It reports whether this is the first
// acknowledgement for the transfer, so the caller can react once.
func (t *transferTracker) Ack(id string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	tr, ok := t.active[id]
	if !ok {
		// An ACK for a transfer this node no longer retains (already
		// acknowledged and evicted, or belonging to another sender) is
		// harmless: report "already settled" rather than an error.
		return false
	}
	if tr.State == StateAcked {
		return false
	}
	tr.State = StateAcked
	tr.NextAttempt = time.Time{}
	tr.LastErr = ""
	return true
}

// Tick advances the sender-side state machine and returns the transfers that
// must be (re)transmitted now. It performs no I/O, so it is directly testable.
//
// On the first tick after creation — which runs immediately, because Create
// sets NextAttempt to the creation time — the state moves from queued to
// relaying, so a transfer never claims to be "awaiting first send" once a send
// has actually started.
//
// For each live transfer:
//   - past its lifetime                        -> expired (terminal)
//   - retry budget exhausted                   -> dead letter (terminal)
//   - an attempt is due                        -> consume one attempt and
//     return it for transmission
func (t *transferTracker) Tick() []*Transfer {
	now := t.now()
	t.mu.Lock()
	defer t.mu.Unlock()
	var due []*Transfer
	for _, tr := range t.active {
		if tr.State.IsTerminal() {
			continue
		}
		if now.After(tr.ExpiresAt) {
			tr.State = StateExpired
			tr.NextAttempt = time.Time{}
			tr.payload = nil
			continue
		}
		if now.Before(tr.NextAttempt) {
			continue
		}
		if tr.Attempts >= t.maxAttempts {
			// Out of attempts but still inside the payload lifetime:
			// dead-letter, which is distinguishable from expiry.
			tr.State = StateDeadLetter
			tr.NextAttempt = time.Time{}
			tr.payload = nil
			continue
		}
		tr.Attempts++
		tr.State = StateRelaying
		// Exponential backoff with a cap, measured from the attempt.
		backoff := t.ackTimeout
		for i := 1; i < tr.Attempts; i++ {
			backoff *= 2
			if backoff >= maxBackoff {
				backoff = maxBackoff
				break
			}
		}
		tr.NextAttempt = now.Add(backoff)
		due = append(due, tr)
	}
	return due
}

// Payload returns the retained plaintext for a retransmission, or nil when the
// transfer is terminal or its payload was released.
func (t *transferTracker) Payload(id string) []byte {
	t.mu.Lock()
	defer t.mu.Unlock()
	tr, ok := t.active[id]
	if !ok || tr.payload == nil {
		return nil
	}
	return append([]byte(nil), tr.payload...)
}

// NotePacket records the id of the current transmission without touching the
// remembered hop. transmit() must NOT clobber LastHop: flushTo() consults it to
// choose an alternate path on the next attempt, so overwriting it with an empty
// value here would silently disable alternate-path retry.
func (t *transferTracker) NotePacket(id, packetID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if tr, ok := t.active[id]; ok {
		tr.PacketID = packetID
	}
}

// NoteHop records which neighbor an attempt was handed to, so a retry can
// prefer a different path.
func (t *transferTracker) NoteHop(id, hop, packetID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if tr, ok := t.active[id]; ok {
		tr.LastHop = hop
		tr.PacketID = packetID
	}
}

// NoteError records why an attempt failed.
func (t *transferTracker) NoteError(id string, err error) {
	if err == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if tr, ok := t.active[id]; ok {
		tr.LastErr = err.Error()
	}
}

// MarkDelivered applies receiver-side exactly-once semantics: it reports true
// the first time a transfer id is seen, and false for every duplicate copy.
// A retry therefore never re-delivers to the application, but the caller still
// acknowledges each copy so a lost ACK converges.
func (t *transferTracker) MarkDelivered(id string) bool {
	now := t.now()
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, dup := t.delivered[id]; dup {
		return false
	}
	t.delivered[id] = now
	if len(t.delivered) > t.maxDelivered {
		// Bounded: drop the oldest half so the set cannot grow forever.
		type kv struct {
			id string
			at time.Time
		}
		all := make([]kv, 0, len(t.delivered))
		for k, v := range t.delivered {
			all = append(all, kv{k, v})
		}
		sort.Slice(all, func(i, j int) bool { return all[i].at.Before(all[j].at) })
		for i := 0; i < len(all)/2; i++ {
			delete(t.delivered, all[i].id)
		}
	}
	return true
}

// SetNow overrides the tracker's clock. Tests use it to drive the state
// machine deterministically instead of sleeping.
func (t *transferTracker) SetNow(now func() time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if now != nil {
		t.now = now
	}
}
