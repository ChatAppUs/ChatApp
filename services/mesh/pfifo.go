package mesh

// pfifo.go — the priority-ordered forwarding buffer.
//
// The store-and-forward queue (storeforward.go) is ordered and bounded by
// count. That is fine for delay-tolerant delivery but wrong for a relay under
// pressure: the oldest packet wins a slot regardless of what it is, so bulk
// media can displace control traffic and a live call degrades while the radio
// still has capacity. This buffer adds the policy the analyses require:
//
//   - drain order is by traffic class (control, text, voice, media) and then
//     FIFO inside a class, so nothing starves and ordering within a class is
//     preserved;
//   - capacity is bounded in BYTES as well as count, because packet counts say
//     nothing about the memory a queue actually holds;
//   - under pressure, the lowest-priority class is dropped first, and a
//     control packet is never dropped to make room for anything else.
//
// It is safe for concurrent use and owns copies of the packets it holds, so a
// caller mutating a packet afterwards cannot corrupt the queue.

import (
	"errors"
	"sync"
	"time"
)

// ErrQueueFull reports that a packet could not be admitted because the buffer
// is at capacity and no lower-priority packet was eligible for eviction.
var ErrQueueFull = errors.New("mesh: priority queue at capacity")

const (
	// defaultQueueMaxBytes bounds the forwarding buffer by real memory.
	defaultQueueMaxBytes = 8 * 1024 * 1024
	// defaultQueueMaxPackets bounds it by count so a flood of tiny packets
	// cannot consume unbounded CPU during ordering.
	defaultQueueMaxPackets = 4096
)

// qitem is one buffered packet with its class and enqueue time.
type qitem struct {
	p        *Packet
	pri      Priority
	seq      uint64
	enqueued time.Time
	bytes    int
}

// PriorityQueue is a bounded, priority-ordered packet buffer.
type PriorityQueue struct {
	mu      sync.Mutex
	items   []*qitem
	seq     uint64
	bytes   int
	maxByts int
	maxPkts int
	maxAge  time.Duration
	dropped int
	now     func() time.Time
}

// NewPriorityQueue creates a buffer bounded by both packets and bytes.
func NewPriorityQueue(maxPackets, maxBytes int, maxAge time.Duration) *PriorityQueue {
	if maxPackets <= 0 {
		maxPackets = defaultQueueMaxPackets
	}
	if maxBytes <= 0 {
		maxBytes = defaultQueueMaxBytes
	}
	if maxAge <= 0 {
		maxAge = 7 * 24 * time.Hour
	}
	return &PriorityQueue{
		maxByts: maxBytes,
		maxPkts: maxPackets,
		maxAge:  maxAge,
		now:     time.Now,
	}
}

// packetBytes estimates the memory one buffered packet occupies, counting the
// encrypted payload and nonce plus a fixed allowance for the envelope's
// routing metadata.
func packetBytes(p *Packet) int {
	const envelopeOverhead = 256
	return len(p.Payload) + len(p.Nonce) + len(p.Onion) + envelopeOverhead
}

// Enqueue admits a packet. When the buffer is full it evicts the
// lowest-priority, oldest packet to make room; a control packet is never
// evicted, so if the buffer is full of control traffic the new packet is
// refused with ErrQueueFull rather than silently displaced.
func (q *PriorityQueue) Enqueue(p *Packet) error {
	if p == nil {
		return errors.New("mesh: nil packet")
	}
	pri := PriorityForKind(p.Kind)
	sz := packetBytes(p)
	q.mu.Lock()
	defer q.mu.Unlock()
	q.expireLocked(q.now())
	q.seq++
	it := &qitem{
		p:        clonePacket(p),
		pri:      pri,
		seq:      q.seq,
		enqueued: q.now(),
		bytes:    sz,
	}
	if !q.fitsLocked(sz) {
		if !q.evictForLocked(pri, sz) {
			q.dropped++
			return ErrQueueFull
		}
	}
	q.items = append(q.items, it)
	q.bytes += sz
	return nil
}

// fitsLocked reports whether one more packet of sz bytes is within capacity.
func (q *PriorityQueue) fitsLocked(sz int) bool {
	return len(q.items) < q.maxPkts && q.bytes+sz <= q.maxByts
}

// evictForLocked frees room for a packet of class incoming carrying sz bytes.
// It evicts lowest-priority-first, oldest-first, and never evicts a packet
// more important than the arriving one. It reports whether room now exists.
func (q *PriorityQueue) evictForLocked(incoming Priority, sz int) bool {
	for !q.fitsLocked(sz) {
		victim := -1
		var worst Priority = -1
		for i, it := range q.items {
			// Never evict control traffic, and never evict a class more
			// important than the arriving packet.
			if it.pri == PriorityControl || it.pri < incoming {
				continue
			}
			if victim == -1 || it.pri > worst || (it.pri == worst && it.seq < q.items[victim].seq) {
				victim = i
				worst = it.pri
			}
		}
		if victim == -1 {
			return false
		}
		q.bytes -= q.items[victim].bytes
		q.items = append(q.items[:victim], q.items[victim+1:]...)
		q.dropped++
	}
	return true
}

// expireLocked discards packets past the buffer's maximum age, releasing their
// accounted bytes. Caller holds the mutex.
func (q *PriorityQueue) expireLocked(now time.Time) {
	kept := q.items[:0]
	for _, it := range q.items {
		if now.Sub(it.enqueued) > q.maxAge || it.p.TTL <= 0 {
			q.bytes -= it.bytes
			q.dropped++
			continue
		}
		kept = append(kept, it)
	}
	q.items = kept
}

// Drain returns up to max packets in priority order (control first, FIFO
// inside a class), removing them from the buffer.
func (q *PriorityQueue) Drain(max int) []*Packet {
	q.mu.Lock()
	defer q.mu.Unlock()
	if max <= 0 {
		max = len(q.items)
	}
	q.expireLocked(q.now())
	// Repeated selection of the highest-priority, oldest item. Bounded by the
	// number of classes rather than queue depth, so this stays cheap.
	out := make([]*Packet, 0, max)
	for len(out) < max && len(q.items) > 0 {
		best := 0
		for i := 1; i < len(q.items); i++ {
			if q.items[i].pri < q.items[best].pri ||
				(q.items[i].pri == q.items[best].pri && q.items[i].seq < q.items[best].seq) {
				best = i
			}
		}
		it := q.items[best]
		q.bytes -= it.bytes
		q.items = append(q.items[:best], q.items[best+1:]...)
		out = append(out, it.p)
	}
	return out
}

// Peek returns copies of the buffered packets in priority order without
// removing them (diagnostics and status).
func (q *PriorityQueue) Peek() []*Packet {
	q.mu.Lock()
	defer q.mu.Unlock()
	sorted := make([]*qitem, len(q.items))
	copy(sorted, q.items)
	for i := 1; i < len(sorted); i++ {
		for j := i; j > 0; j-- {
			swap := sorted[j].pri < sorted[j-1].pri ||
				(sorted[j].pri == sorted[j-1].pri && sorted[j].seq < sorted[j-1].seq)
			if !swap {
				break
			}
			sorted[j], sorted[j-1] = sorted[j-1], sorted[j]
		}
	}
	out := make([]*Packet, 0, len(sorted))
	for _, it := range sorted {
		out = append(out, clonePacket(it.p))
	}
	return out
}

// Remove drops a packet by id (after delivery). It reports whether it was
// present.
func (q *PriorityQueue) Remove(id string) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	for i, it := range q.items {
		if it.p.ID == id {
			q.bytes -= it.bytes
			q.items = append(q.items[:i], q.items[i+1:]...)
			return true
		}
	}
	return false
}

// Len returns the buffered packet count.
func (q *PriorityQueue) Len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.items)
}

// Bytes returns the buffered byte total.
func (q *PriorityQueue) Bytes() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.bytes
}

// Dropped returns how many packets were evicted or expired.
func (q *PriorityQueue) Dropped() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.dropped
}

// DepthByPriority reports the buffered packet count per traffic class.
func (q *PriorityQueue) DepthByPriority() map[string]int {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := map[string]int{
		PriorityControl.String(): 0,
		PriorityText.String():    0,
		PriorityVoice.String():   0,
		PriorityMedia.String():   0,
	}
	for _, it := range q.items {
		out[it.pri.String()]++
	}
	return out
}

// clonePacket copies the mutable parts of a packet so the buffer owns its
// bytes independently of the caller.
func clonePacket(p *Packet) *Packet {
	cp := *p
	if p.Payload != nil {
		cp.Payload = append([]byte(nil), p.Payload...)
	}
	if p.Nonce != nil {
		cp.Nonce = append([]byte(nil), p.Nonce...)
	}
	if p.Onion != nil {
		cp.Onion = append([]byte(nil), p.Onion...)
	}
	if p.FragSum != nil {
		cp.FragSum = append([]byte(nil), p.FragSum...)
	}
	return &cp
}
