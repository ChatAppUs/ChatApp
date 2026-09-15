package mesh

// fragment.go — MTU-bounded fragmentation and reassembly.
//
// Radio links carry far smaller datagrams than the logical payloads the mesh
// must transport (voice notes, media, long messages). Bluetooth LE negotiates
// an ATT MTU as low as 20 bytes and up to 517; Wi-Fi Direct UDP carries about
// 1400. A logical payload larger than the radio MTU is therefore split into
// fixed-size fragments, and every fragment is encrypted with its own AEAD
// nonce and carried as an ordinary Packet. Because a fragment is a normal
// packet, routing, TTL, hop counting, duplicate suppression, relay quotas and
// store-and-forward all apply to it unchanged.
//
// The receiver reassembles fragments under bounded memory and time, ignores
// duplicate fragment delivery, expires groups whose missing fragments never
// arrive, and verifies a SHA-256 digest over the complete payload before
// handing anything to the application. A payload that already fits in one
// datagram keeps the original single-packet wire shape, so small messages are
// byte-for-byte unchanged and remain interoperable with peers that predate
// fragmentation.

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"sync"
	"time"
)

const (
	// DefaultMaxPayload is the fragment payload ceiling in bytes. 512 fits
	// within one datagram on every supported radio (including a negotiated
	// Bluetooth LE ATT MTU) while leaving room for the JSON envelope, and it
	// is far below the 2 MiB packet wire-size ceiling enforced by
	// UnmarshalPacket.
	DefaultMaxPayload = 512

	// maxFragments bounds the advertised fragment count so a hostile packet
	// cannot claim a huge group and force unbounded allocation.
	maxFragments = 4096

	// Reassembly caps bound memory so a malfunctioning or hostile peer
	// cannot exhaust a relaying device.
	maxAssemblies    = 256
	maxAssemblyBytes = 8 * 1024 * 1024
	assemblyTTL      = 5 * time.Minute
)

var (
	// ErrFragmentInvalid reports malformed or contradictory fragment metadata.
	ErrFragmentInvalid = errors.New("mesh: invalid fragment metadata")
	// ErrFragmentDigestMismatch reports a reassembled payload that does not
	// match the digest committed by the sender.
	ErrFragmentDigestMismatch = errors.New("mesh: reassembled payload digest mismatch")
)

// SplitPayload divides a plaintext into MTU-bounded chunks. It returns the
// chunks, a fresh fragment-group id and a SHA-256 digest over the complete
// plaintext that the receiver checks after reassembly.
//
// A payload that already fits in maxPayload is returned as a single chunk with
// an empty group id, so the caller emits a plain (unfragmented) packet and
// interop with pre-fragmentation peers is preserved.
func SplitPayload(plaintext []byte, maxPayload int) (chunks [][]byte, fragID string, digest []byte, err error) {
	if maxPayload <= 0 {
		maxPayload = DefaultMaxPayload
	}
	sum := sha256.Sum256(plaintext)
	if len(plaintext) <= maxPayload {
		return [][]byte{plaintext}, "", sum[:], nil
	}
	for off := 0; off < len(plaintext); off += maxPayload {
		end := off + maxPayload
		if end > len(plaintext) {
			end = len(plaintext)
		}
		// Copy so each fragment owns its bytes; the caller encrypts and may
		// hold them past this call.
		chunk := make([]byte, end-off)
		copy(chunk, plaintext[off:end])
		chunks = append(chunks, chunk)
	}
	if len(chunks) > maxFragments {
		return nil, "", nil, ErrFragmentInvalid
	}
	return chunks, newPacketID(), sum[:], nil
}

// assembly is one in-progress reassembly group.
type assembly struct {
	total   int
	digest  []byte
	parts   map[int][]byte
	bytes   int
	updated time.Time
}

// Reassembler collects decrypted fragments per (source, fragment-group) and
// yields the complete payload once every part has arrived. It is safe for
// concurrent use.
type Reassembler struct {
	mu       sync.Mutex
	open     map[string]*assembly
	bytes    int
	ttl      time.Duration
	maxOpen  int
	maxBytes int
	now      func() time.Time
}

// NewReassembler creates a reassembler with the default bounded limits.
func NewReassembler() *Reassembler {
	return &Reassembler{
		open:     make(map[string]*assembly),
		ttl:      assemblyTTL,
		maxOpen:  maxAssemblies,
		maxBytes: maxAssemblyBytes,
		now:      time.Now,
	}
}

// Add accepts one already-decrypted fragment.
//
// It returns (payload, true, nil) once the group is complete and its digest
// verifies; (nil, false, nil) while fragments are still outstanding, and when
// the fragment is a duplicate of one already held; and an error for malformed
// metadata or a digest mismatch. A packet that is not part of a fragment group
// passes straight through.
func (r *Reassembler) Add(p *Packet, chunk []byte) ([]byte, bool, error) {
	if p.FragTotal <= 1 {
		// Not fragmented: the caller has the whole payload already.
		return chunk, true, nil
	}
	if p.FragID == "" || p.FragIndex < 0 || p.FragIndex >= p.FragTotal || p.FragTotal > maxFragments {
		return nil, false, ErrFragmentInvalid
	}
	key := p.Src + "\x00" + p.FragID

	now := r.now()
	r.mu.Lock()
	defer r.mu.Unlock()
	r.expireLocked(now)

	a := r.open[key]
	if a == nil {
		if len(r.open) >= r.maxOpen {
			// Bounded memory: refuse a new group rather than evict or grow.
			return nil, false, ErrFragmentInvalid
		}
		a = &assembly{
			total:   p.FragTotal,
			digest:  append([]byte(nil), p.FragSum...),
			parts:   make(map[int][]byte),
			updated: now,
		}
		r.open[key] = a
	}
	if a.total != p.FragTotal || !bytes.Equal(a.digest, p.FragSum) {
		// Contradictory metadata for one group id: drop it and fail closed.
		r.dropLocked(key, a)
		return nil, false, ErrFragmentInvalid
	}
	if _, dup := a.parts[p.FragIndex]; dup {
		// Duplicate handling: a re-delivered fragment is ignored, not
		// counted twice.
		return nil, false, nil
	}
	if r.bytes+len(chunk) > r.maxBytes {
		r.dropLocked(key, a)
		return nil, false, ErrFragmentInvalid
	}
	part := make([]byte, len(chunk))
	copy(part, chunk)
	a.parts[p.FragIndex] = part
	a.bytes += len(part)
	r.bytes += len(part)
	a.updated = now

	if len(a.parts) < a.total {
		return nil, false, nil
	}

	full := make([]byte, 0, a.bytes)
	for i := 0; i < a.total; i++ {
		piece, ok := a.parts[i]
		if !ok {
			// Cannot happen once len(parts) == total, but never deliver a
			// payload with a hole in it.
			return nil, false, nil
		}
		full = append(full, piece...)
	}
	r.dropLocked(key, a)

	if got := sha256.Sum256(full); !bytes.Equal(got[:], a.digest) {
		return nil, false, ErrFragmentDigestMismatch
	}
	return full, true, nil
}

// Open reports how many reassembly groups are currently in progress.
func (r *Reassembler) Open() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.open)
}

// dropLocked removes a group and releases its accounted bytes. The caller must
// hold the mutex.
func (r *Reassembler) dropLocked(key string, a *assembly) {
	delete(r.open, key)
	r.bytes -= a.bytes
	if r.bytes < 0 {
		r.bytes = 0
	}
}

// expireLocked discards groups whose missing fragments never arrived, so an
// incomplete transfer cannot occupy memory indefinitely. The caller must hold
// the mutex.
func (r *Reassembler) expireLocked(now time.Time) {
	for key, a := range r.open {
		if now.Sub(a.updated) > r.ttl {
			r.dropLocked(key, a)
		}
	}
}
