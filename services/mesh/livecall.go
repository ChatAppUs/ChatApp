package mesh

// livecall.go — the offline live-call media plane.
//
// calls.go answers "may a live call proceed?" from topology alone. This file
// implements what a live call actually is once the answer is not
// "unreachable": a real-time session of small, latency-sensitive frames that
// must survive the loss, reordering and jitter of a multi-hop radio mesh.
//
// Five moving parts:
//
//  1. Signalling: offer/answer/ring/hangup ride KindCallSignal as the
//     existing CallSignal JSON, so ringing UIs keep working unchanged.
//  2. Media framing: a compact binary header (sequence, timestamp, flags,
//     bitrate class) followed by one codec frame. Opus/AMR encode and decode
//     stay on the device; the engine transports and schedules frames.
//  3. A jitter buffer that reorders, adapts its playout delay to measured
//     jitter, and conceals a frame that did not arrive in time.
//  4. Forward error correction: every group of fecGroupSize media frames is
//     accompanied by one XOR parity frame, so a single lost frame in a group
//     is reconstructed instead of concealed.
//  5. An adaptive bitrate controller that lowers the frame class on measured
//     loss and raises it again when the link is stable.
//
// Security and honesty requirements are honoured: media frames are sealed
// under a per-call key derived from the per-peer session key and the call id
// (rotating on an epoch interval, so frames sealed under an expired epoch are
// rejected rather than silently accepted), caller identity is verified by the
// existing session-key cryptography, route health is re-checked on every
// tick, and a call whose route dies is ended with an explicit fallback
// recommendation instead of pretending to continue.

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Media-plane packet kinds
// ---------------------------------------------------------------------------

// The call-plane packet kinds (KindCallMedia, KindCallFec, KindCallPing,
// KindCallPong, KindCallBye) are declared in packet.go alongside the rest of
// the envelope vocabulary.

// IsCallPlaneKind reports whether a kind belongs to the live-call media
// plane. Media-plane packets are consumed by the call manager and are never
// handed to the application handler; signalling (KindCallSignal) is not part
// of this plane and keeps its existing app-visible behaviour.
func IsCallPlaneKind(k PacketKind) bool {
	switch k {
	case KindCallMedia, KindCallFec, KindCallPing, KindCallPong, KindCallBye:
		return true
	}
	return false
}

// ---------------------------------------------------------------------------
// Media framing
// ---------------------------------------------------------------------------

const (
	frameHeaderSize = 12 // seq(4) ts(4) flags(1) class(1) len(2)
	frameFlagFec    = 0x01
)

// MediaFrame is one real-time audio frame as transported by the mesh. Data is
// one codec frame (the device's encoder decides the codec); the engine only
// schedules and transports it.
type MediaFrame struct {
	Seq    uint32
	TsMs   uint32
	Class  int    // bitrate class (index into bitrateClasses)
	Fec    bool   // true when Data is an XOR parity body
	CallID string // not on the wire header; carried per packet context
	Data   []byte
}

// EncodeFrame serialises a frame into its wire body (header + payload).
func EncodeFrame(f *MediaFrame) []byte {
	buf := make([]byte, frameHeaderSize+len(f.Data))
	binary.BigEndian.PutUint32(buf[0:4], f.Seq)
	binary.BigEndian.PutUint32(buf[4:8], f.TsMs)
	var flags byte
	if f.Fec {
		flags |= frameFlagFec
	}
	buf[8] = flags
	buf[9] = byte(f.Class)
	binary.BigEndian.PutUint16(buf[10:12], uint16(len(f.Data)))
	copy(buf[frameHeaderSize:], f.Data)
	return buf
}

// DecodeFrame parses a wire body produced by EncodeFrame.
func DecodeFrame(b []byte) (*MediaFrame, error) {
	if len(b) < frameHeaderSize {
		return nil, errors.New("mesh: media frame truncated")
	}
	n := int(binary.BigEndian.Uint16(b[10:12]))
	if len(b) < frameHeaderSize+n {
		return nil, errors.New("mesh: media frame length exceeds body")
	}
	return &MediaFrame{
		Seq:   binary.BigEndian.Uint32(b[0:4]),
		TsMs:  binary.BigEndian.Uint32(b[4:8]),
		Fec:   b[8]&frameFlagFec != 0,
		Class: int(b[9]),
		Data:  b[frameHeaderSize : frameHeaderSize+n],
	}, nil
}

// ---------------------------------------------------------------------------
// Bitrate classes (adaptive bitrate control)
// ---------------------------------------------------------------------------

// bitrateClasses are the adaptive bitrate steps, in bit/s. The classes are
// deliberately speech-shaped: a class survives multi-hop Bluetooth/Wi-Fi
// relay chains, unlike the 32-64 kb/s+ of internet-era codecs.
var bitrateClasses = []int{8000, 12000, 16000, 24000, 32000}

const (
	defaultBitrateClass   = 2 // 16 kb/s
	callLossHighThreshold = 0.20
	callLossLowThreshold  = 0.02
	callKeyEpochSecs      = 60
)

// ---------------------------------------------------------------------------
// Jitter buffer
// ---------------------------------------------------------------------------

const (
	jitterBaseDelayMs   = 60
	jitterMaxDelayMs    = 240
	frameSlotMs         = 20 // nominal duration of one audio frame
	jitterConcealMs     = frameSlotMs
	jitterReorderWindow = 16 // frames ahead of playout held for reordering

	// fecGraceSlots is how long playout holds a hole before concealing, in
	// frame slots. It spans one full FEC group so the parity frame has a
	// realistic chance to arrive and repair the hole instead of the playout
	// concealing past it.
	fecGraceSlots = fecGroupSize
)

// JitterBuffer reorders frames by sequence, adapts its playout delay to
// measured jitter, and reports concealment when playout must proceed without
// the next frame. It is not concurrency-safe; the owning session serialises
// access.
type JitterBuffer struct {
	targetMs int
	ewmaJit  float64
	nextSeq  uint32
	started  bool
	pending  map[uint32]*MediaFrame
	lastPop  *MediaFrame
	// playedAny records whether playout has delivered a frame; before the
	// first delivery, a lower-sequence arrival may still rewind nextSeq.
	playedAny bool
	// holeMs accumulates playout time (ms) spent waiting on the current
	// hole; a hole older than the FEC grace window is concealed.
	holeMs int
}

// NewJitterBuffer creates a jitter buffer with the minimum playout delay.
func NewJitterBuffer() *JitterBuffer {
	return &JitterBuffer{targetMs: jitterBaseDelayMs, pending: make(map[uint32]*MediaFrame)}
}

// Push inserts a frame. Frames far behind playout are dropped; frames too far
// ahead beyond the reorder window are accepted (bounded map) — the map cap is
// enforced by evicting the oldest pending frame.
func (j *JitterBuffer) Push(f *MediaFrame) {
	if !j.started {
		j.started = true
		j.nextSeq = f.Seq
	}
	if _, dup := j.pending[f.Seq]; dup {
		return
	}
	if f.Seq < j.nextSeq {
		if !j.playedAny {
			// An earlier frame arrived before playout began: rewind the
			// playout point so start-of-session reordering cannot lose
			// it, and keep the frame for playout.
			j.nextSeq = f.Seq
			j.holeMs = 0
		} else {
			// The slot was already played or concealed, and a late or
			// repaired frame for it cannot join the playout timeline.
			return
		}
	}
	if len(j.pending) >= jitterReorderWindow {
		// Evict the lowest sequence: it is the closest to playout, and a
		// buffer that must drop should drop what it would play first so
		// latency cannot grow without bound.
		lowest := uint32(0)
		first := true
		for seq := range j.pending {
			if first || seq < lowest {
				lowest, first = seq, false
			}
		}
		delete(j.pending, lowest)
	}
	cp := *f
	j.pending[f.Seq] = &cp
	if f.Seq == j.nextSeq {
		j.holeMs = 0
	}
}

// Pop advances playout. It returns the next in-order frame, or nil with
// concealed=true when the next frame is missing and playout must conceal.
// deltaMs is the time elapsed since the previous call, used to advance the
// playout sequence and adapt the delay target.
func (j *JitterBuffer) Pop(deltaMs int) (*MediaFrame, bool) {
	if !j.started {
		return nil, false
	}
	if f, ok := j.pending[j.nextSeq]; ok {
		delete(j.pending, j.nextSeq)
		j.lastPop = f
		j.nextSeq++
		j.playedAny = true
		j.holeMs = 0
		// Jitter is the deviation of the actual inter-frame arrival pacing
		// from one nominal frame slot. Only lateness disturbs playout.
		dev := deltaMs - frameSlotMs
		if dev < 0 {
			dev = 0
		}
		j.ewmaJit = 0.875*j.ewmaJit + 0.125*float64(dev)
		j.adapt()
		return f, false
	}
	// The next frame has not arrived yet. Hold the hole for the FEC grace
	// window so parity can repair it; once the window is spent, conceal one
	// frame slot and continue.
	j.holeMs += deltaMs
	if j.holeMs < fecGraceSlots*frameSlotMs {
		return nil, false
	}
	j.holeMs = 0
	j.nextSeq++
	j.lastPop = nil
	j.ewmaJit = 0.875*j.ewmaJit + 0.125*float64(jitterConcealMs)
	j.adapt()
	return nil, true
}

// adapt moves the target playout delay with measured jitter, clamped.
func (j *JitterBuffer) adapt() {
	target := jitterBaseDelayMs + int(j.ewmaJit)*2
	if target < jitterBaseDelayMs {
		target = jitterBaseDelayMs
	}
	if target > jitterMaxDelayMs {
		target = jitterMaxDelayMs
	}
	j.targetMs = target
}

// TargetDelayMs reports the current adaptive playout delay.
func (j *JitterBuffer) TargetDelayMs() int { return j.targetMs }

// Buffered reports how many frames are held for reordering.
func (j *JitterBuffer) Buffered() int { return len(j.pending) }

// ---------------------------------------------------------------------------
// FEC: XOR parity over a group of frames
// ---------------------------------------------------------------------------

const fecGroupSize = 4

// fecParity computes the XOR parity body over the group's data frames. All
// frames in a group must share the same length for the parity to be
// reconstructable, so the caller pads frames to a common length.
func fecParity(frames []*MediaFrame) []byte {
	var max int
	for _, f := range frames {
		if len(f.Data) > max {
			max = len(f.Data)
		}
	}
	body := make([]byte, max)
	for _, f := range frames {
		for i, b := range f.Data {
			body[i] ^= b
		}
	}
	return body
}

// ---------------------------------------------------------------------------
// Call session
// ---------------------------------------------------------------------------

// CallState is the lifecycle state of one call session.
type CallState string

const (
	CallRinging CallState = "ringing"
	CallActive  CallState = "active"
	CallEnded   CallState = "ended"
)

// callStats is the measured picture of a live session, exposed so clients and
// tests can see the truth about the link.
type callStats struct {
	FramesSent    int     `json:"frames_sent"`
	FramesRecv    int     `json:"frames_recv"`
	FramesFec     int     `json:"frames_fec_recovered"`
	FramesConceal int     `json:"frames_concealed"`
	LossRate      float64 `json:"loss_rate"`
	RTTMs         float64 `json:"rtt_ms"`
	JitterMs      float64 `json:"jitter_ms"`
	Bitrate       int     `json:"bitrate"`
	Class         int     `json:"class"`
}

// callSession is one live call between this node and a peer.
type callSession struct {
	ID       string
	Peer     string
	Incoming bool
	State    CallState

	// key epoch rotation: frames sealed under an old epoch are rejected.
	epoch   uint32
	epochAt time.Time

	// adaptive bitrate
	class      int
	sendSeq    uint32
	fecPending []*MediaFrame

	// receiver side
	jbuf     *JitterBuffer
	lastTick time.Time

	// measurements
	sent, recv, fecRecovered, concealed int
	rttEwma                             float64
	lossEwma                            float64
	rttProbeAt                          time.Time
	rttProbeSeq                         uint32
	rttSentAt                           time.Time

	updatedAt time.Time
}

// callKey derives the per-call, per-epoch media key from the peer session
// key. HMAC-SHA256 binds the derivation to the session key so knowledge of a
// call id alone reveals nothing.
func callKey(sessionKey *IdentityKey, callID string, epoch uint32) [32]byte {
	var out [32]byte
	mac := hmac.New(sha256.New, sessionKey[:])
	mac.Write([]byte("chatapp-mesh-call-v1:"))
	mac.Write([]byte(callID))
	var eb [4]byte
	binary.BigEndian.PutUint32(eb[:], epoch)
	mac.Write(eb[:])
	copy(out[:], mac.Sum(nil))
	return out
}

// CallManager owns every live call session of a node. It consumes call-plane
// packets (media, parity, probes, teardown) and mirrors signalling state; it
// never touches ordinary traffic.
type CallManager struct {
	n        *Node
	mu       sync.Mutex
	sessions map[string]*callSession // call id -> session
}

func newCallManager(n *Node) *CallManager {
	return &CallManager{n: n, sessions: make(map[string]*callSession)}
}

// Offer starts an outgoing call to dst with the given codec. It returns the
// call id. The offer rides KindCallSignal so ringing UIs see it.
func (m *CallManager) Offer(dst, codec string) (string, error) {
	if dst == "" {
		return "", errors.New("mesh: call destination is required")
	}
	if m.n.CallFeasible(dst) == CallUnreachable {
		return "", errors.New("mesh: no live route to call destination")
	}
	callID := newPacketID()
	s := &callSession{
		ID: callID, Peer: dst, State: CallRinging,
		class:     defaultBitrateClass,
		jbuf:      NewJitterBuffer(),
		epochAt:   m.n.now(),
		updatedAt: m.n.now(),
	}
	m.mu.Lock()
	m.sessions[callID] = s
	m.mu.Unlock()
	_, err := m.n.Send(KindCallSignal, dst, mustJSON(&CallSignal{
		CallID: callID, Type: "offer", Kind: "audio", SentAt: m.n.now().UnixMilli(),
	}))
	return callID, err
}

// Answer accepts an incoming call on this side and tells the caller. The
// callee's session is promoted to active immediately: the caller learns the
// same fact from the answer signal, so neither side waits on a round trip
// that only one of them needs.
func (m *CallManager) Answer(callID, codec string) error {
	m.mu.Lock()
	s := m.sessions[callID]
	if s == nil || s.State != CallRinging || !s.Incoming {
		m.mu.Unlock()
		return errors.New("mesh: no ringing incoming call with that id")
	}
	s.State = CallActive
	s.lastTick = m.n.now()
	s.rttProbeAt = m.n.now()
	peer := s.Peer
	m.mu.Unlock()
	_, err := m.n.Send(KindCallSignal, peer, mustJSON(&CallSignal{
		CallID: callID, Type: "answer", Kind: "audio", SentAt: m.n.now().UnixMilli(),
	}))
	return err
}

// Accept promotes a session to active after the peer accepted. For an
// outgoing call, Accept is called when the answer signal arrives (handled
// inside InboundSignal); for an incoming call it promotes after Answer.
func (m *CallManager) Accept(callID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.sessions[callID]
	if s == nil || s.State == CallEnded {
		return errors.New("mesh: unknown call")
	}
	s.State = CallActive
	s.lastTick = m.n.now()
	s.rttProbeAt = m.n.now()
	return nil
}

// Hangup ends a call locally and tells the peer.
func (m *CallManager) Hangup(callID string) {
	m.mu.Lock()
	s := m.sessions[callID]
	if s == nil || s.State == CallEnded {
		m.mu.Unlock()
		return
	}
	peer := s.Peer
	s.State = CallEnded
	s.updatedAt = m.n.now()
	m.mu.Unlock()
	body, _ := m.seal(callID, peer, []byte("bye"))
	if body != nil {
		_, _ = m.n.Send(KindCallBye, peer, body)
	}
}

// SendFrame hands one encoder frame to an active outgoing session. It
// fragments nothing: a frame must fit one datagram (call frames are small by
// design), and oversized frames are refused loudly rather than silently
// degraded.
func (m *CallManager) SendFrame(callID string, data []byte) error {
	if len(data) == 0 {
		return errors.New("mesh: empty media frame")
	}
	if frameHeaderSize+len(data) > DefaultMaxPayload {
		return errors.New("mesh: media frame exceeds datagram budget")
	}
	m.mu.Lock()
	s := m.sessions[callID]
	if s == nil || s.State != CallActive {
		m.mu.Unlock()
		return errors.New("mesh: call is not active")
	}
	f := &MediaFrame{Seq: s.sendSeq, TsMs: uint32(m.n.now().UnixMilli() & 0xffffffff), Class: s.class, Data: append([]byte(nil), data...)}
	s.sendSeq++
	s.sent++
	s.fecPending = append(s.fecPending, f)
	emitFec := len(s.fecPending) == fecGroupSize
	group := s.fecPending
	if emitFec {
		s.fecPending = nil
	}
	peer, class := s.Peer, s.class
	m.mu.Unlock()

	if err := m.sendFramePacket(callID, peer, f); err != nil {
		return err
	}
	if emitFec {
		par := &MediaFrame{Seq: f.Seq, TsMs: f.TsMs, Class: class, Fec: true, Data: fecParity(group)}
		return m.sendFramePacket(callID, peer, par)
	}
	return nil
}

// RecvFrame returns the next playout frame for an incoming session. A nil
// frame with concealed=true means playout advanced without the frame (the
// client synthesises comfort noise or repeats its decoder state).
func (m *CallManager) RecvFrame(callID string) (*MediaFrame, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.sessions[callID]
	if s == nil || s.State != CallActive {
		return nil, false, errors.New("mesh: call is not active")
	}
	now := m.n.now()
	delta := int(now.Sub(s.lastTick).Milliseconds())
	if delta < 0 {
		delta = 0
	}
	if delta < frameSlotMs {
		// Playout clock not due yet.
		return nil, false, nil
	}
	s.lastTick = now
	f, concealed := s.jbuf.Pop(delta)
	if concealed {
		s.concealed++
		m.updateLossLocked(s)
	}
	if f != nil {
		s.recv++
	}
	return f, concealed, nil
}

// Stats returns the measured state of one session.
func (m *CallManager) Stats(callID string) (callStats, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.sessions[callID]
	if s == nil {
		return callStats{}, false
	}
	total := s.recv + s.concealed
	loss := 0.0
	if total > 0 {
		loss = float64(s.concealed) / float64(total)
	}
	return callStats{
		FramesSent: s.sent, FramesRecv: s.recv, FramesFec: s.fecRecovered,
		FramesConceal: s.concealed, LossRate: loss, RTTMs: s.rttEwma,
		JitterMs: float64(s.jbuf.TargetDelayMs()), Bitrate: bitrateClasses[s.class],
		Class: s.class,
	}, true
}

// FallbackRecommended reports whether a call's route died and the voice-note
// path should be offered instead.
func (m *CallManager) FallbackRecommended(callID string) bool {
	m.mu.Lock()
	s := m.sessions[callID]
	m.mu.Unlock()
	if s == nil {
		return false
	}
	return s.State == CallEnded && m.n.ShouldFallBackToVoiceNote(s.Peer)
}

// --- wire helpers -----------------------------------------------------------

// seal encrypts a call-plane body under the call's current epoch key.
func (m *CallManager) seal(callID, peer string, body []byte) ([]byte, uint32) {
	m.mu.Lock()
	var epoch uint32
	if s := m.sessions[callID]; s != nil {
		epoch = s.epoch
	}
	m.mu.Unlock()
	return m.sealEpoch(callID, peer, body, epoch)
}

// sealEpoch seals a body under an explicit epoch without taking the manager
// lock. Callers inside tick already hold m.mu; the epoch comes from the
// session being iterated.
func (m *CallManager) sealEpoch(callID, peer string, body []byte, epoch uint32) ([]byte, uint32) {
	sk := m.n.sessionKeyFor(peer)
	key := callKey(sk, callID, epoch)
	ik := IdentityKey(key)
	ct, nonce, err := Encrypt(&ik, body)
	if err != nil {
		return nil, epoch
	}
	// Body: epoch(4) || nonce || ciphertext — the epoch rides every sealed
	// call-plane packet so the receiver can derive the right key.
	out := make([]byte, 4, 4+len(nonce)+len(ct))
	binary.BigEndian.PutUint32(out, epoch)
	out = append(out, nonce...)
	out = append(out, ct...)
	return out, epoch
}

// open decrypts a sealed call-plane body, refusing stale epochs. It tolerates
// the current epoch and the immediately previous one (a frame in flight while
// the epoch rotates is legitimate; anything older is a replay).
func (m *CallManager) open(callID, peer string, body []byte) ([]byte, bool) {
	if len(body) < 4+NonceSize {
		return nil, false
	}
	epoch := binary.BigEndian.Uint32(body[:4])
	sk := m.n.sessionKeyFor(peer)
	m.mu.Lock()
	var cur uint32
	if s := m.sessions[callID]; s != nil {
		cur = s.epoch
	}
	m.mu.Unlock()
	if epoch != cur && epoch+1 != cur {
		return nil, false
	}
	key := callKey(sk, callID, epoch)
	ik := IdentityKey(key)
	nonce := body[4 : 4+NonceSize]
	ct := body[4+NonceSize:]
	pt, err := Decrypt(&ik, ct, nonce)
	if err != nil {
		return nil, false
	}
	return pt, true
}

// sendFramePacket encrypts and queues one media-plane packet.
func (m *CallManager) sendFramePacket(callID, peer string, f *MediaFrame) error {
	sealed, _ := m.seal(callID, peer, EncodeFrame(f))
	if sealed == nil {
		return errors.New("mesh: media seal failed")
	}
	p := NewPacket(KindCallMedia, m.n.DeviceID, peer, m.n.maxHops)
	if f.Fec {
		p.Kind = KindCallFec
	}
	p.Payload = sealed
	p.Seq = m.n.nextSeq()
	m.n.routes.Seen(p.ID)
	if err := m.n.pfifo.Enqueue(p); err != nil {
		return err
	}
	m.n.flush()
	return nil
}

// --- inbound -----------------------------------------------------------------

// InboundPlane consumes a call-plane packet. It returns true when the packet
// belonged to the call plane (whether or not a session matched), because
// media-plane packets must never leak to the application handler.
func (m *CallManager) InboundPlane(p *Packet, plaintext []byte) bool {
	if !IsCallPlaneKind(p.Kind) {
		return false
	}
	switch p.Kind {
	case KindCallMedia, KindCallFec:
		m.inboundMedia(p)
	case KindCallPing:
		m.inboundPing(p, plaintext)
	case KindCallPong:
		m.inboundPong(p)
	case KindCallBye:
		m.inboundBye(p)
	}
	return true
}

// InboundSignal mirrors a KindCallSignal into session state. It always
// returns false: signalling keeps its existing app-visible delivery so
// ringing UIs continue to work.
func (m *CallManager) InboundSignal(p *Packet, plaintext []byte) bool {
	sig, err := UnmarshalCallSignal(plaintext)
	if err != nil || sig.CallID == "" {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	switch sig.Type {
	case "offer":
		if _, ok := m.sessions[sig.CallID]; !ok {
			m.sessions[sig.CallID] = &callSession{
				ID: sig.CallID, Peer: p.Src, Incoming: true, State: CallRinging,
				class: defaultBitrateClass, jbuf: NewJitterBuffer(), epochAt: m.n.now(),
			}
		}
	case "answer":
		if s := m.sessions[sig.CallID]; s != nil && !s.Incoming && s.State == CallRinging {
			s.State = CallActive
			s.lastTick = m.n.now()
			s.rttProbeAt = m.n.now()
		}
	case "hangup":
		if s := m.sessions[sig.CallID]; s != nil {
			s.State = CallEnded
		}
	}
	return false
}

func (m *CallManager) inboundMedia(p *Packet) {
	m.mu.Lock()
	var match *callSession
	for _, s := range m.sessions {
		if s.Peer == p.Src && s.State == CallActive {
			match = s
			break
		}
	}
	m.mu.Unlock()
	if match == nil {
		return
	}
	body, ok := m.open(match.ID, p.Src, p.Payload)
	if !ok {
		return
	}
	f, err := DecodeFrame(body)
	if err != nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if f.Fec {
		m.tryRecoverLocked(match, f)
		return
	}
	s := match
	// Hold the frame; recovery may still rewrite its Data if it was lost.
	s.jbuf.Push(f)
	s.recv++
}

// tryRecoverLocked attempts to reconstruct exactly one missing member of the
// parity group the FEC frame belongs to.
func (m *CallManager) tryRecoverLocked(s *callSession, par *MediaFrame) {
	// Group members are the fecGroupSize sequence slots ending at par.Seq.
	base := par.Seq - uint32(fecGroupSize) + 1
	var members []*MediaFrame
	missing := -1
	for i := 0; i < fecGroupSize; i++ {
		seq := base + uint32(i)
		if f, ok := s.jbuf.pending[seq]; ok {
			members = append(members, f)
		} else if missing == -1 {
			missing = i
		} else {
			// Two or more missing: unrecoverable.
			return
		}
	}
	if missing == -1 || len(members) != fecGroupSize-1 {
		return
	}
	body := append([]byte(nil), par.Data...)
	for _, f := range members {
		for i, b := range f.Data {
			body[i] ^= b
		}
	}
	recovered := &MediaFrame{Seq: base + uint32(missing), TsMs: par.TsMs, Class: par.Class, Data: body}
	s.jbuf.Push(recovered)
	s.fecRecovered++
}

func (m *CallManager) inboundPing(p *Packet, plaintext []byte) {
	// Echo: pong carries the ping body back verbatim.
	po := NewPacket(KindCallPong, m.n.DeviceID, p.Src, m.n.maxHops)
	po.Payload = append([]byte(nil), p.Payload...)
	po.Seq = m.n.nextSeq()
	m.n.routes.Seen(po.ID)
	if err := m.n.pfifo.Enqueue(po); err == nil {
		m.n.flush()
	}
}

func (m *CallManager) inboundPong(p *Packet) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range m.sessions {
		if s.Peer == p.Src && s.State == CallActive && !s.rttSentAt.IsZero() {
			rtt := float64(m.n.now().Sub(s.rttSentAt).Milliseconds())
			if s.rttEwma == 0 {
				s.rttEwma = rtt
			} else {
				s.rttEwma = 0.875*s.rttEwma + 0.125*rtt
			}
			s.rttSentAt = time.Time{}
			return
		}
	}
}

func (m *CallManager) inboundBye(p *Packet) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range m.sessions {
		if s.Peer == p.Src && s.State != CallEnded {
			s.State = CallEnded
		}
	}
}

// updateLossLocked recomputes the EWMA loss and adapts the bitrate class.
func (m *CallManager) updateLossLocked(s *callSession) {
	total := s.recv + s.concealed
	if total == 0 {
		return
	}
	loss := float64(s.concealed) / float64(total)
	s.lossEwma = 0.875*s.lossEwma + 0.125*loss
	switch {
	case s.lossEwma > callLossHighThreshold && s.class > 0:
		s.class--
	case s.lossEwma < callLossLowThreshold && s.class < len(bitrateClasses)-1:
		s.class++
	}
}

// --- maintenance -------------------------------------------------------------

// tick advances every session: key-epoch rotation, RTT probes, route-health
// handoff. It is called from the node's reliability loop.
//
// State mutation happens under the manager lock, but every packet send is
// deferred until the lock is released: the simulated (and real) transport
// delivers inbound packets synchronously, so a ping sent while m.mu is held
// would come back as a pong that re-enters the manager and deadlocks on the
// non-reentrant mutex.
func (m *CallManager) tick(now time.Time) {
	type deferredPing struct {
		id, peer string
		epoch    uint32
	}
	var pings []deferredPing
	var byes []deferredPing

	m.mu.Lock()
	for id, s := range m.sessions {
		if s.State == CallEnded {
			// Terminal sessions are retained briefly for stats, then reaped.
			if now.Sub(s.updatedAt) > 2*time.Minute {
				delete(m.sessions, id)
			}
			continue
		}
		s.updatedAt = now
		// Key-epoch rotation.
		if now.Sub(s.epochAt) > callKeyEpochSecs*time.Second {
			s.epoch++
			s.epochAt = now
		}
		if s.State != CallActive {
			continue
		}
		// RTT probe: mark it under the lock, send below without it.
		if s.rttSentAt.IsZero() && (s.rttProbeAt.IsZero() || now.Sub(s.rttProbeAt) > 2*time.Second) {
			s.rttSentAt = now
			s.rttProbeAt = now
			pings = append(pings, deferredPing{id: id, peer: s.Peer, epoch: s.epoch})
		}
		// Route health handoff: a call that lost its route is ended honestly
		// with a fallback recommendation instead of pretending to continue.
		if m.n.CallFeasible(s.Peer) == CallUnreachable {
			s.State = CallEnded
			s.updatedAt = now
			byes = append(byes, deferredPing{id: id, peer: s.Peer, epoch: s.epoch})
		}
	}
	m.mu.Unlock()

	for _, pr := range pings {
		if body, _ := m.sealEpoch(pr.id, pr.peer, []byte("ping"), pr.epoch); body != nil {
			p := NewPacket(KindCallPing, m.n.DeviceID, pr.peer, m.n.maxHops)
			p.Payload = body
			p.Seq = m.n.nextSeq()
			m.n.routes.Seen(p.ID)
			if m.n.pfifo.Enqueue(p) == nil {
				m.n.flush()
			} else {
				// The probe never entered the wire: clear the marker so the
				// next tick can try again.
				m.mu.Lock()
				if s := m.sessions[pr.id]; s != nil {
					s.rttSentAt = time.Time{}
				}
				m.mu.Unlock()
			}
		}
	}
	for _, by := range byes {
		if body, _ := m.sealEpoch(by.id, by.peer, []byte("route_lost"), by.epoch); body != nil {
			_, _ = m.n.Send(KindCallBye, by.peer, body)
		}
	}
}

// CallStateOf reports the lifecycle state of one call session ("" when the
// id is unknown, e.g. after stats retention reaps it).
func (m *CallManager) CallStateOf(callID string) CallState {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s := m.sessions[callID]; s != nil {
		return s.State
	}
	return ""
}

// CallSessionIDs lists the ids of all known sessions (any state).
func (m *CallManager) CallSessionIDs() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	ids := make([]string, 0, len(m.sessions))
	for id := range m.sessions {
		ids = append(ids, id)
	}
	return ids
}

// ---------------------------------------------------------------------------
// Node integration
// ---------------------------------------------------------------------------

// CallManager exposes the node's live-call manager.
func (n *Node) CallManager() *CallManager { return n.calls }
