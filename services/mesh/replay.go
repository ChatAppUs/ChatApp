package mesh

// replay.go — anti-replay protection for mesh packets.
//
// Deduplication by random packet id (routing.go) stops relay loops but not
// replays: a captured packet re-injected days later carries a fresh random
// id and would be accepted again. Sequence numbers close that hole. Each
// sender stamps packets with a monotonic per-sender Seq; receivers keep an
// IPsec-style sliding bitmap window per source and reject any sequence
// number that is older than the window or already accepted.

import "sync"

// replayBits is the anti-replay window width (1024 packets).
const replayBits = 1024

// replayWindow is a sliding bitmap over sequence numbers
// [base, base+replayBits).
type replayWindow struct {
	base   int64
	bitmap [replayBits / 64]uint64
}

// accept records seq and reports whether it is new (not a replay). Seq 0 is
// always rejected: senders start numbering at 1.
func (w *replayWindow) accept(seq int64) bool {
	if seq <= 0 || seq < w.base {
		return false
	}
	if seq >= w.base+replayBits {
		// Advance the window to [seq-replayBits+1, seq].
		shift := seq - (w.base + replayBits - 1)
		if shift >= replayBits {
			w.bitmap = [replayBits / 64]uint64{}
		} else {
			for i := range w.bitmap {
				var v uint64
				ni := i + int(shift/64)
				if ni < len(w.bitmap) {
					v = w.bitmap[ni] >> (shift % 64)
				}
				ni2 := ni + 1
				if shift%64 != 0 && ni2 < len(w.bitmap) {
					v |= w.bitmap[ni2] << (64 - shift%64)
				}
				w.bitmap[i] = v
			}
		}
		w.base = seq - replayBits + 1
	}
	idx := seq - w.base
	if w.bitmap[idx/64]&(1<<(idx%64)) != 0 {
		return false
	}
	w.bitmap[idx/64] |= 1 << (idx % 64)
	return true
}

// ReplayFilter keeps one sliding window per source device id.
type ReplayFilter struct {
	mu      sync.Mutex
	windows map[string]*replayWindow
}

// NewReplayFilter creates an empty filter.
func NewReplayFilter() *ReplayFilter {
	return &ReplayFilter{windows: make(map[string]*replayWindow)}
}

// Check reports whether (src, seq) is new, recording it when it is.
func (f *ReplayFilter) Check(src string, seq int64) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	w, ok := f.windows[src]
	if !ok {
		w = &replayWindow{base: 1}
		f.windows[src] = w
	}
	return w.accept(seq)
}
