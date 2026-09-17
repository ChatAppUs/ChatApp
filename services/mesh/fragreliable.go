package mesh

// fragreliable.go — retransmission windows for fragmented reliable transfers.
//
// SendReliable gives a single-datagram payload acknowledgement and bounded
// retries (see reliability.go), and SendLarge fragments a large payload — but
// its fragments are individually best-effort: one lost fragment sinks the
// whole group and the sender never learns which parts arrived. This file
// closes that gap with a selective-repeat protocol over the ordinary packet
// format:
//
//   - The sender splits the payload (see fragment.go), retains the chunks and
//     sends them through a bounded window: at most largeWindow fragments are
//     outstanding at once, so a burst cannot monopolise a congested radio.
//   - The receiver acknowledges every fragment copy it accepts, naming the
//     transfer and the fragment index. The acknowledgement rides the existing
//     KindAck control packet, sealed under the per-peer session key.
//   - The sender retransmits only the fragments still missing (selective
//     repeat), with exponential backoff per fragment, until every fragment is
//     acknowledged, the transfer's lifetime expires, or its retry budget is
//     exhausted (dead letter).
//   - When the receiver reassembles and verifies the digest, it returns a
//     done acknowledgement, so the sender can release the payload even if a
//     straggling per-fragment acknowledgement is still in flight.
//
// The receiver delivers the payload to the application exactly once, no
// matter how many copies were retransmitted, and never before the SHA-256
// digest the sender committed to verifies (see fragment.go).

import (
	"encoding/json"
	"errors"
	"sync"
	"time"
)

// fragAckVersion is the structured-acknowledgement format version.
const fragAckVersion = 1

// Protocol tunables. The window bounds bytes in flight (window × MTU); the
// attempt budget and lifetime are per fragment, matching the single-datagram
// reliable transfer's honesty rules.
const (
	largeWindow      = 8
	largeMaxAttempts = 8
)

// fragAck is the JSON body of a structured (fragment) acknowledgement. It is
// encrypted under the per-peer session key like the legacy proof string, so
// only the origin can read which fragments arrived.
type fragAck struct {
	V     int  `json:"v"` // fragAckVersion
	Index int  `json:"i,omitempty"`
	Done  bool `json:"done,omitempty"`
}

// LargeTransferView is the caller-visible state of one fragmented reliable
// transfer. State uses the same DeliveryState vocabulary as SendReliable.
type LargeTransferView struct {
	ID        string        `json:"id"`
	FragID    string        `json:"frag_id"`
	Dst       string        `json:"dst"`
	Kind      PacketKind    `json:"kind"`
	State     DeliveryState `json:"state"`
	Total     int           `json:"total_fragments"`
	Acked     int           `json:"acked_fragments"`
	Attempts  int           `json:"max_attempts"`
	CreatedAt time.Time     `json:"created_at"`
	UpdatedAt time.Time     `json:"updated_at"`
}

// largeTransfer is the sender-side state of one fragmented reliable transfer.
type largeTransfer struct {
	ID       string
	FragID   string
	Dst      string
	Kind     PacketKind
	Total    int
	digest   []byte
	chunks   [][]byte
	acked    []bool
	ackedCt  int
	attempts []int
	dueAt    []time.Time

	State     DeliveryState
	CreatedAt time.Time
	UpdatedAt time.Time
}

// largeTracker owns every fragmented reliable transfer of a node.
type largeTracker struct {
	mu      sync.Mutex
	active  map[string]*largeTransfer
	order   []string
	maxOpen int
	now     func() time.Time
}

func newLargeTracker() *largeTracker {
	return &largeTracker{
		active:  make(map[string]*largeTransfer),
		maxOpen: 64,
		now:     time.Now,
	}
}

// SetNow overrides the tracker's clock (tests drive delivery deterministically).
func (t *largeTracker) SetNow(now func() time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.now = now
}

// Create registers a new fragmented reliable transfer. The payload must have
// been split by SplitPayload already (the caller owns the policy).
func (t *largeTracker) Create(fragID, dst string, kind PacketKind, chunks [][]byte, digest []byte) *largeTransfer {
	now := t.now()
	lt := &largeTransfer{
		ID:        newPacketID(),
		FragID:    fragID,
		Dst:       dst,
		Kind:      kind,
		Total:     len(chunks),
		digest:    append([]byte(nil), digest...),
		chunks:    chunks,
		acked:     make([]bool, len(chunks)),
		attempts:  make([]int, len(chunks)),
		dueAt:     make([]time.Time, len(chunks)),
		State:     StateQueued,
		CreatedAt: now,
		UpdatedAt: now,
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.active[lt.ID] = lt
	t.order = append(t.order, lt.ID)
	for len(t.active) > t.maxOpen && len(t.order) > 0 {
		victim := t.order[0]
		t.order = t.order[1:]
		if _, ok := t.active[victim]; ok {
			delete(t.active, victim)
		}
	}
	return lt
}

// Known reports whether an id belongs to a fragmented reliable transfer.
func (t *largeTracker) Known(id string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	_, ok := t.active[id]
	return ok
}

// AckFrag marks one fragment acknowledged. It returns true when the
// acknowledgement was new and the transfer became fully acknowledged.
func (t *largeTracker) AckFrag(id string, index int) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	lt := t.active[id]
	if lt == nil || lt.State != StateQueued && lt.State != StateRelaying {
		return false
	}
	if index < 0 || index >= lt.Total || lt.acked[index] {
		return false
	}
	lt.acked[index] = true
	lt.ackedCt++
	lt.UpdatedAt = t.now()
	return lt.ackedCt == lt.Total
}

// AckDone marks the transfer acknowledged from the receiver's completed
// reassembly. It tolerates missing per-fragment acknowledgements.
func (t *largeTracker) AckDone(id string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	lt := t.active[id]
	if lt == nil || lt.State.IsTerminal() {
		return false
	}
	for i := range lt.acked {
		lt.acked[i] = true
	}
	lt.ackedCt = lt.Total
	lt.State = StateAcked
	lt.UpdatedAt = t.now()
	t.releaseLocked(lt)
	return true
}

// MarkDelivered reports whether the application handler should run for this
// transfer: exactly once, on the first completed reassembly.
func (t *largeTracker) MarkDelivered(id string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	lt := t.active[id]
	if lt == nil {
		return false
	}
	if lt.State == StateAcked {
		return false
	}
	for i := range lt.acked {
		lt.acked[i] = true
	}
	lt.ackedCt = lt.Total
	lt.State = StateAcked
	lt.UpdatedAt = t.now()
	t.releaseLocked(lt)
	return true
}

// releaseLocked frees the retained chunks once the transfer is settled.
func (t *largeTracker) releaseLocked(lt *largeTransfer) {
	lt.chunks = nil
}

// fragDue names one fragment the window is due to (re)transmit.
type fragDue struct {
	TransferID string
	FragID     string
	Digest     []byte
	Dst        string
	Kind       PacketKind
	Total      int
	Index      int
	Chunk      []byte
}

// Tick advances every live transfer: it returns the fragments to transmit
// this round (bounded by the window), expires or dead-letters transfers whose
// budget or lifetime ran out, and retires settled ones.
func (t *largeTracker) Tick() []fragDue {
	now := t.now()
	t.mu.Lock()
	defer t.mu.Unlock()
	var due []fragDue
	for _, id := range t.order {
		lt := t.active[id]
		if lt == nil {
			continue
		}
		if lt.State == StateAcked {
			// Retain briefly for observation, then reap.
			if now.Sub(lt.UpdatedAt) > 2*time.Minute {
				delete(t.active, id)
			}
			continue
		}
		if now.Sub(lt.CreatedAt) > transferTTL {
			lt.State = StateExpired
			lt.chunks = nil
			continue
		}
		if lt.ackedCt == lt.Total {
			lt.State = StateAcked
			lt.chunks = nil
			continue
		}
		// Selective repeat: retransmit only missing fragments, bounded by
		// the window and per-fragment backoff.
		sent := 0
		maxAttempts := 0
		for i := 0; i < lt.Total && sent < largeWindow; i++ {
			if lt.acked[i] {
				continue
			}
			if lt.attempts[i] >= largeMaxAttempts {
				lt.State = StateDeadLetter
				lt.chunks = nil
				break
			}
			if !lt.dueAt[i].IsZero() && now.Before(lt.dueAt[i]) {
				continue
			}
			lt.attempts[i]++
			if lt.attempts[i] > maxAttempts {
				maxAttempts = lt.attempts[i]
			}
			backoff := ackTimeout
			for b := 1; b < lt.attempts[i]; b++ {
				backoff *= 2
				if backoff >= maxBackoff {
					backoff = maxBackoff
					break
				}
			}
			lt.dueAt[i] = now.Add(backoff)
			lt.State = StateRelaying
			due = append(due, fragDue{
				TransferID: lt.ID, FragID: lt.FragID, Digest: lt.digest,
				Dst: lt.Dst, Kind: lt.Kind, Total: lt.Total,
				Index: i, Chunk: lt.chunks[i],
			})
			sent++
		}
		lt.UpdatedAt = now
	}
	return due
}

// View renders the transfer for callers and the status API.
func (lt *largeTransfer) View() LargeTransferView {
	maxAttempts := 0
	for _, a := range lt.attempts {
		if a > maxAttempts {
			maxAttempts = a
		}
	}
	return LargeTransferView{
		ID: lt.ID, FragID: lt.FragID, Dst: lt.Dst, Kind: lt.Kind,
		State: lt.State, Total: lt.Total, Acked: lt.ackedCt,
		Attempts: maxAttempts, CreatedAt: lt.CreatedAt, UpdatedAt: lt.UpdatedAt,
	}
}

// Snapshot returns every tracked transfer, oldest first.
func (t *largeTracker) Snapshot() []LargeTransferView {
	t.mu.Lock()
	defer t.mu.Unlock()
	views := make([]LargeTransferView, 0, len(t.active))
	for _, id := range t.order {
		if lt := t.active[id]; lt != nil {
			views = append(views, lt.View())
		}
	}
	return views
}

// ---------------------------------------------------------------------------
// Node integration
// ---------------------------------------------------------------------------

// SendLargeReliable sends a payload of any size (within the fragmentation
// ceiling) with per-fragment acknowledgement and a bounded retransmission
// window. The transfer id is returned for delivery-state polling
// (LargeTransfer). Only unicast is supported: group fan-out has its own
// per-member acknowledgement path (see groupack.go).
func (n *Node) SendLargeReliable(kind PacketKind, dst string, plaintext []byte) (string, error) {
	if dst == "" {
		return "", errors.New("mesh: destination is required")
	}
	if kind == KindAck {
		return "", errors.New("mesh: acknowledgements are generated internally")
	}
	if len(plaintext) == 0 {
		return "", errors.New("mesh: empty payload")
	}
	chunks, fragID, sum, err := SplitPayload(plaintext, DefaultMaxPayload)
	if err != nil {
		return "", err
	}
	if fragID == "" {
		// Fits one datagram: the single-packet reliable path is already
		// accountable and cheaper.
		return n.SendReliable(kind, dst, plaintext)
	}
	lt := n.largeRel.Create(fragID, dst, kind, chunks, sum)
	n.largeRelTick()
	return lt.ID, nil
}

// largeRelTick transmits the fragments the retransmission window is due to
// send and flushes the forwarding buffer.
func (n *Node) largeRelTick() {
	due := n.largeRel.Tick()
	for _, d := range due {
		n.transmitFragment(d)
	}
	n.flush()
}

// transmitFragment encrypts and queues one fragment of a reliable group.
// Each attempt is a fresh packet with a fresh AEAD nonce, so retries are
// never suppressed by duplicate suppression (see node_reliable.go).
func (n *Node) transmitFragment(d fragDue) {
	key := n.sessionKeyFor(d.Dst)
	ct, nonce, err := Encrypt(key, d.Chunk)
	if err != nil {
		return
	}
	p := NewPacket(d.Kind, n.DeviceID, d.Dst, n.maxHops)
	p.Payload = ct
	p.Nonce = nonce
	p.Seq = n.nextSeq()
	p.FragIndex = d.Index
	p.FragTotal = d.Total
	p.FragID = d.FragID
	p.FragSum = d.Digest
	p.Xfer = d.TransferID
	n.routes.Seen(p.ID)
	n.pfifo.Enqueue(p)
}

// sendFragAck acknowledges one fragment copy to the transfer's origin.
func (n *Node) sendFragAck(dst, transferID, fragID string, index int) {
	if dst == "" || transferID == "" {
		return
	}
	body, err := json.Marshal(fragAck{V: fragAckVersion, Index: index})
	if err != nil {
		return
	}
	n.enqueueSealedAck(dst, transferID, fragID, body)
}

// sendFragDoneAck acknowledges the completed reassembly of a fragment group.
func (n *Node) sendFragDoneAck(dst, transferID, fragID string) {
	if dst == "" || transferID == "" {
		return
	}
	body, err := json.Marshal(fragAck{V: fragAckVersion, Done: true})
	if err != nil {
		return
	}
	n.enqueueSealedAck(dst, transferID, fragID, body)
}

// enqueueSealedAck seals a structured acknowledgement body under the
// per-peer session key and queues it as control traffic.
func (n *Node) enqueueSealedAck(dst, transferID, fragID string, body []byte) {
	p := NewPacket(KindAck, n.DeviceID, dst, n.maxHops)
	ct, nonce, err := Encrypt(n.sessionKeyFor(dst), body)
	if err != nil {
		return
	}
	p.AckFor = transferID
	p.Xfer = transferID
	p.GroupID = ""
	p.FragID = fragID
	p.Payload = ct
	p.Nonce = nonce
	p.Seq = n.nextSeq()
	n.routes.Seen(p.ID)
	if err := n.pfifo.Enqueue(p); err == nil {
		n.flush()
	}
}

// LargeTransfer returns the delivery state of one fragmented reliable transfer.
func (n *Node) LargeTransfer(id string) (LargeTransferView, bool) {
	n.largeRel.mu.Lock()
	defer n.largeRel.mu.Unlock()
	lt := n.largeRel.active[id]
	if lt == nil {
		return LargeTransferView{}, false
	}
	return lt.View(), true
}

// LargeTransfers returns every tracked fragmented reliable transfer.
func (n *Node) LargeTransfers() []LargeTransferView { return n.largeRel.Snapshot() }

// PendingLargeTransfers reports how many fragmented reliable transfers are
// not yet terminal.
func (n *Node) PendingLargeTransfers() int {
	n.largeRel.mu.Lock()
	defer n.largeRel.mu.Unlock()
	pending := 0
	for _, lt := range n.largeRel.active {
		if !lt.State.IsTerminal() {
			pending++
		}
	}
	return pending
}
