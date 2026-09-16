package mesh

// voicenote.go — codec-aware voice-note transfer over the mesh.
//
// The mesh moves small packets over intermittent radios, so a recorded voice
// note travels as a sequence of independently acknowledged chunks (each chunk
// rides the reliable-transfer path, with retries and acks — see
// node_reliable.go). The full audio is checksummed with SHA-256; a note is
// only delivered to the application when every chunk has arrived and the
// digest matches, so a corrupted or partial assembly is never surfaced as a
// playable recording.
//
// Transfer is resumable in both directions:
//   - The receiver keeps per-note assembly state, so chunks that arrive out of
//     order, late (after a partition heals — store-and-forward delays them)
//     or duplicated are merged idempotently.
//   - The sender can query which chunk indexes the receiver is still missing
//     and re-send only those (SendVoiceNoteResume), avoiding re-transferring
//     audio that was already acknowledged.
//
// Nothing here promises real-time playback; a note is delivered only once it
// is fully received, which is the fallback path when CallFeasible reports the
// topology cannot sustain a live call.

import (
	"crypto/sha256"
	"errors"
	"encoding/hex"
	"encoding/json"
	"sort"
	"sync"
)

// DefaultVoiceChunkSize is the chunk size used when the caller does not
// specify one. It fits comfortably inside the reliable-transfer payload
// ceiling while keeping per-chunk retransmission cheap on lossy radios.
const DefaultVoiceChunkSize = 16 * 1024

// VoiceChunk is the plaintext carried inside one voice-note packet.
type VoiceChunk struct {
	Xfer     string `json:"xfer"`               // reliable transfer id of this chunk
	Index    int    `json:"index"`              // chunk position, 0-based
	Total    int    `json:"total"`              // number of chunks in the note
	Codec    string `json:"codec"`              // e.g. "opus", "amr", "aac"
	Checksum string `json:"checksum"`           // hex SHA-256 of the full audio
	Data     []byte `json:"data"`               // codec bytes for this chunk
}

// VoiceNote is a fully received, verified voice note handed to the app.
type VoiceNote struct {
	Xfer     string // id of the first chunk's transfer
	Codec    string
	Checksum string
	Audio    []byte
	Size     int
}

// voiceAssembly is the receiver-side state of one in-progress note.
type voiceAssembly struct {
	src      string
	codec    string
	checksum string
	total    int
	chunks   map[int][]byte
}

func (a *voiceAssembly) missing() []int {
	var out []int
	for i := 0; i < a.total; i++ {
		if _, ok := a.chunks[i]; !ok {
			out = append(out, i)
		}
	}
	sort.Ints(out)
	return out
}

// VoiceNoteStore tracks in-progress voice-note assemblies for one node.
type VoiceNoteStore struct {
	mu     sync.Mutex
	byKey  map[string]*voiceAssembly // src|checksum -> assembly
	done   map[string]bool           // src|checksum -> already delivered
}

func NewVoiceNoteStore() *VoiceNoteStore {
	return &VoiceNoteStore{byKey: map[string]*voiceAssembly{}, done: map[string]bool{}}
}

func voiceKey(src, checksum string) string { return src + "|" + checksum }

// Offer adds one chunk to the assembly state. It returns the completed note
// exactly once (duplicates and re-acks never re-deliver), or nil while the
// note is incomplete or fails verification.
func (s *VoiceNoteStore) Offer(src string, c *VoiceChunk) *VoiceNote {
	if c == nil || c.Total <= 0 || c.Index < 0 || c.Index >= c.Total || c.Checksum == "" {
		return nil
	}
	key := voiceKey(src, c.Checksum)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.done[key] {
		return nil
	}
	a, ok := s.byKey[key]
	if !ok {
		a = &voiceAssembly{src: src, codec: c.Codec, checksum: c.Checksum, total: c.Total, chunks: map[int][]byte{}}
		s.byKey[key] = a
	}
	if c.Index < a.total {
		if _, have := a.chunks[c.Index]; !have {
			cp := append([]byte(nil), c.Data...)
			a.chunks[c.Index] = cp
		}
	}
	if len(a.chunks) < a.total {
		return nil
	}
	var full []byte
	for i := 0; i < a.total; i++ {
		full = append(full, a.chunks[i]...)
	}
	sum := sha256.Sum256(full)
	if hex.EncodeToString(sum[:]) != a.checksum {
		// Corrupt assembly: discard so a re-transferred note can start clean.
		delete(s.byKey, key)
		return nil
	}
	delete(s.byKey, key)
	s.done[key] = true
	return &VoiceNote{Xfer: c.Xfer, Codec: a.codec, Checksum: a.checksum, Audio: full, Size: len(full)}
}

// Missing reports which chunk indexes the receiver still needs for the note
// identified by (src, checksum). ok is false when nothing has arrived yet.
func (s *VoiceNoteStore) Missing(src, checksum string) (missing []int, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, present := s.byKey[voiceKey(src, checksum)]
	if !present {
		return nil, false
	}
	return a.missing(), true
}

// encodeVoiceChunk marshals one chunk for the reliable-transfer path.
func encodeVoiceChunk(c *VoiceChunk) ([]byte, error) { return json.Marshal(c) }

// decodeVoiceChunk parses a delivered plaintext as a voice chunk.
func decodeVoiceChunk(plaintext []byte) (*VoiceChunk, bool) {
	var c VoiceChunk
	if err := json.Unmarshal(plaintext, &c); err != nil {
		return nil, false
	}
	if c.Xfer == "" || c.Total <= 0 || c.Index < 0 || c.Index >= c.Total || c.Checksum == "" || c.Codec == "" {
		return nil, false
	}
	return &c, true
}

// SendVoiceNote transfers a recorded voice note to dst as individually
// acknowledged chunks. It returns the note's transfer key (the first chunk's
// reliable transfer id) and the note checksum. Chunks larger than the
// reliable ceiling are rejected rather than silently split again.
func (n *Node) SendVoiceNote(dst, codec string, audio []byte, chunkSize int) (xfer, checksum string, err error) {
	if dst == "" {
		return "", "", errors.New("mesh: destination is required")
	}
	if codec == "" {
		return "", "", errors.New("mesh: codec is required")
	}
	if len(audio) == 0 {
		return "", "", errors.New("mesh: empty voice note")
	}
	if chunkSize <= 0 {
		chunkSize = DefaultVoiceChunkSize
	}
	if chunkSize > maxReliablePayload-256 {
		return "", "", ErrTransferTooLarge
	}
	sum := sha256.Sum256(audio)
	checksum = hex.EncodeToString(sum[:])
	total := (len(audio) + chunkSize - 1) / chunkSize
	first := ""
	for i := 0; i < total; i++ {
		end := (i + 1) * chunkSize
		if end > len(audio) {
			end = len(audio)
		}
		chunk := audio[i*chunkSize : end]
		// The chunk's own xfer id is minted by SendReliable; the receiver
		// groups chunks by (source, checksum), not by per-chunk id.
		id, err := n.SendReliable(KindVoiceMessage, dst, mustJSON(&VoiceChunk{
			Xfer: checksum, Index: i, Total: total, Codec: codec, Checksum: checksum, Data: chunk,
		}))
		if err != nil {
			return first, checksum, err
		}
		if i == 0 {
			first = id
		}
	}
	return first, checksum, nil
}

// SendVoiceNoteResume re-sends only the chunk indexes a receiver reports as
// missing. Pass the missing list obtained from the receiver (either pushed
// back over the mesh or read from VoiceNoteStore on the receiving node in a
// simulation/test). It returns the number of chunks re-sent.
func (n *Node) SendVoiceNoteResume(dst, codec, checksum string, audio []byte, chunkSize int, missing []int) (int, error) {
	if chunkSize <= 0 {
		chunkSize = DefaultVoiceChunkSize
	}
	total := (len(audio) + chunkSize - 1) / chunkSize
	sent := 0
	for _, i := range missing {
		if i < 0 || i >= total {
			continue
		}
		end := (i + 1) * chunkSize
		if end > len(audio) {
			end = len(audio)
		}
		if _, err := n.SendReliable(KindVoiceMessage, dst, mustJSON(&VoiceChunk{
			Xfer: checksum, Index: i, Total: total, Codec: codec, Checksum: checksum, Data: audio[i*chunkSize : end],
		})); err != nil {
			return sent, err
		}
		sent++
	}
	return sent, nil
}

// SetVoiceNoteHandler installs the callback invoked once per fully received,
// checksum-verified voice note. When installed, raw voice chunks are consumed
// by the assembler and are not passed to the generic packet handler.
func (n *Node) SetVoiceNoteHandler(fn func(src string, note *VoiceNote)) {
	n.mu.Lock()
	n.onVoiceNote = fn
	n.voiceNotes = NewVoiceNoteStore()
	n.mu.Unlock()
}

// VoiceNoteProgress reports the receiver's missing chunk list for a note.
func (n *Node) VoiceNoteProgress(src, checksum string) ([]int, bool) {
	if n.voiceNotes == nil {
		return nil, false
	}
	return n.voiceNotes.Missing(src, checksum)
}

// offerVoiceNote feeds one delivered chunk into the assembler and fires the
// app callback when the note completes. It reports whether the packet was
// consumed as a voice chunk.
func (n *Node) offerVoiceNote(p *Packet, plaintext []byte) bool {
	c, ok := decodeVoiceChunk(plaintext)
	if !ok {
		return false
	}
	if n.voiceNotes == nil {
		return false
	}
	if note := n.voiceNotes.Offer(p.Src, c); note != nil && n.onVoiceNote != nil {
		n.onVoiceNote(p.Src, note)
	}
	return true
}
