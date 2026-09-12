package mesh

// storeforward.go — delay-tolerant store-and-forward queue.
//
// Packets persist in a bounded in-memory queue until a route to the
// destination becomes available, then are delivered. Packets expire after TTL
// or a maximum age. This mirrors the authoritative Postgres queue in the API
// service but runs locally on-device so the mesh works with no internet.

import (
	"sync"
	"time"
)

// Queue is a bounded store-and-forward packet queue.
type Queue struct {
	mu      sync.Mutex
	items   []*Packet
	maxSize int
	maxAge  time.Duration
}

// NewQueue creates a bounded queue.
func NewQueue(maxSize int, maxAge time.Duration) *Queue {
	if maxSize <= 0 {
		maxSize = 1000
	}
	if maxAge <= 0 {
		maxAge = 7 * 24 * time.Hour
	}
	return &Queue{maxSize: maxSize, maxAge: maxAge}
}

// Enqueue adds a packet to the queue, dropping the oldest if over capacity.
func (q *Queue) Enqueue(p *Packet) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.items = append(q.items, p)
	if len(q.items) > q.maxSize {
		q.items = q.items[len(q.items)-q.maxSize:]
	}
}

// Pending returns packets that are still valid (not expired) and removes
// expired ones. It does not remove delivered packets (caller marks them).
func (q *Queue) Pending(now time.Time) []*Packet {
	q.mu.Lock()
	defer q.mu.Unlock()
	valid := q.items[:0]
	for _, p := range q.items {
		age := now.Sub(time.UnixMilli(p.CreatedAt))
		if age <= q.maxAge && p.TTL > 0 {
			valid = append(valid, p)
		}
	}
	q.items = valid
	out := make([]*Packet, len(valid))
	copy(out, valid)
	return out
}

// Remove deletes a packet by id (after delivery).
func (q *Queue) Remove(id string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for i, p := range q.items {
		if p.ID == id {
			q.items = append(q.items[:i], q.items[i+1:]...)
			return
		}
	}
}

// Len returns the current queue length.
func (q *Queue) Len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.items)
}
