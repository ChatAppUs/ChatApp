package mesh

import (
	"bytes"
	"encoding/binary"
	"sync"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Frame codec
// ---------------------------------------------------------------------------

func TestMediaFrameRoundTrip(t *testing.T) {
	f := &MediaFrame{Seq: 7, TsMs: 12345, Class: 3, Data: []byte("codec-frame-bytes")}
	body := EncodeFrame(f)
	if len(body) != frameHeaderSize+len(f.Data) {
		t.Fatalf("encoded length = %d, want %d", len(body), frameHeaderSize+len(f.Data))
	}
	got, err := DecodeFrame(body)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Seq != f.Seq || got.TsMs != f.TsMs || got.Class != f.Class || got.Fec {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
	if !bytes.Equal(got.Data, f.Data) {
		t.Fatal("frame payload does not round-trip")
	}
}

func TestDecodeFrameRejectsGarbage(t *testing.T) {
	if _, err := DecodeFrame([]byte{0, 1, 2}); err == nil {
		t.Fatal("truncated header accepted")
	}
	body := make([]byte, frameHeaderSize)
	binary.BigEndian.PutUint16(body[10:12], 9999)
	if _, err := DecodeFrame(body); err == nil {
		t.Fatal("declared length beyond body accepted")
	}
}

// ---------------------------------------------------------------------------
// Jitter buffer
// ---------------------------------------------------------------------------

func TestJitterBufferReorders(t *testing.T) {
	j := NewJitterBuffer()
	for _, seq := range []uint32{2, 0, 1} {
		j.Push(&MediaFrame{Seq: seq, Data: []byte{byte(seq)}})
	}
	for want := uint32(0); want < 3; want++ {
		f, concealed := j.Pop(frameSlotMs)
		if concealed || f == nil || f.Seq != want {
			t.Fatalf("playout seq = %v concealed=%v, want %d", f, concealed, want)
		}
	}
}

func TestJitterBufferHoldsHoleThenConceals(t *testing.T) {
	j := NewJitterBuffer()
	j.Push(&MediaFrame{Seq: 0, Data: []byte("a")})
	j.Push(&MediaFrame{Seq: 2, Data: []byte("c")})
	// Frame 1 is missing. Within the FEC grace window playout must hold.
	if f, concealed := j.Pop(frameSlotMs); f == nil || f.Seq != 0 || concealed {
		t.Fatalf("first pop = %v concealed=%v", f, concealed)
	}
	if _, concealed := j.Pop(frameSlotMs); concealed {
		t.Fatal("hole concealed before the FEC grace window elapsed")
	}
	// Advancing past the grace window must conceal exactly one slot.
	concealed := false
	for i := 0; i < fecGraceSlots+2; i++ {
		if _, c := j.Pop(frameSlotMs); c {
			concealed = true
			break
		}
	}
	if !concealed {
		t.Fatal("playout never advanced past the missing frame")
	}
	// The held frame 2 must now play in order.
	f, _ := j.Pop(frameSlotMs)
	if f == nil || f.Seq != 2 {
		t.Fatalf("post-conceal frame = %v, want seq 2", f)
	}
}

func TestJitterBufferDropsTooLate(t *testing.T) {
	j := NewJitterBuffer()
	j.Push(&MediaFrame{Seq: 5, Data: []byte("x")})
	j.Pop(frameSlotMs) // starts playout, advances nextSeq past 5
	j.Push(&MediaFrame{Seq: 5, Data: []byte("late")})
	if j.Buffered() != 0 {
		t.Fatal("a frame behind playout was retained")
	}
}

func TestJitterBufferAdaptsTargetToJitter(t *testing.T) {
	j := NewJitterBuffer()
	j.Push(&MediaFrame{Seq: 0, Data: []byte("a")})
	j.Pop(3 * frameSlotMs) // a badly late first frame
	if j.TargetDelayMs() <= jitterBaseDelayMs {
		t.Fatalf("target delay did not grow with measured lateness: %d", j.TargetDelayMs())
	}
	if j.TargetDelayMs() > jitterMaxDelayMs {
		t.Fatalf("target delay escaped its clamp: %d", j.TargetDelayMs())
	}
}

// ---------------------------------------------------------------------------
// Call lifecycle over a simulated link
// ---------------------------------------------------------------------------

// callHarness wires two nodes back to back with a shared deterministic clock.
type callHarness struct {
	bus  *SimBus
	a, b *Node
	now  time.Time
	// linkLatencyMs simulates radio propagation: each inbound delivery
	// advances the shared clock before the node processes the packet, so a
	// synchronous round trip measures a non-zero RTT.
	linkLatencyMs int64
	mu            sync.Mutex
}

func newCallHarness(t *testing.T) *callHarness {
	t.Helper()
	h := &callHarness{bus: NewSimBus(), now: time.Unix(1_800_000_000, 0), linkLatencyMs: 30}
	clock := func() time.Time {
		h.mu.Lock()
		defer h.mu.Unlock()
		return h.now
	}
	// Latency-aware node builder: the link advances the shared clock before
	// delivering, mirroring real propagation delay on a multi-hop radio path.
	mk := func(id string) *Node {
		link := h.bus.Attach("sim://"+id, nil)
		n := NewNode(NodeConfig{DeviceID: id, Key: simKey, Kind: "member", Transport: link, MaxHops: 8})
		link.SetInbound(func(addr string, data []byte) {
			h.mu.Lock()
			h.now = h.now.Add(time.Duration(h.linkLatencyMs) * time.Millisecond)
			h.mu.Unlock()
			n.HandleInbound(addr, data)
		})
		return n
	}
	// Real clients always install an application handler (that is how call
	// signalling reaches the ringing UI); a no-op keeps the engine paths
	// exercised exactly as they run in production.
	h.a = mk("caller")
	h.a.handler = func(p *Packet, pt []byte) {}
	h.b = mk("callee")
	h.b.handler = func(p *Packet, pt []byte) {}
	addrA, addrB := "sim://caller", "sim://callee"
	h.bus.Wire(addrA, addrB)
	h.bus.Wire(addrB, addrA)
	h.a.routes.Upsert(&Beacon{DeviceID: h.b.DeviceID, Kind: "member", Transport: "local_wifi", Addr: addrB})
	h.b.routes.Upsert(&Beacon{DeviceID: h.a.DeviceID, Kind: "member", Transport: "local_wifi", Addr: addrA})
	h.a.SetNow(clock)
	h.b.SetNow(clock)
	return h
}

func (h *callHarness) advance(d time.Duration) {
	h.mu.Lock()
	h.now = h.now.Add(d)
	h.mu.Unlock()
}

// startActiveCall drives offer → answer → accept until both sessions report
// active, and returns the caller's call id.
func startActiveCall(t *testing.T, h *callHarness) string {
	t.Helper()
	cmA, cmB := h.a.CallManager(), h.b.CallManager()
	callID, err := cmA.Offer(h.b.DeviceID, "opus")
	if err != nil {
		t.Fatalf("offer: %v", err)
	}
	if len(cmB.CallSessionIDs()) != 1 {
		t.Fatalf("callee did not mirror the offer: %v", cmB.CallSessionIDs())
	}
	if err := cmB.Answer(cmB.CallSessionIDs()[0], "opus"); err != nil {
		t.Fatalf("answer: %v", err)
	}
	if err := cmA.Accept(callID); err != nil {
		t.Fatalf("accept: %v", err)
	}
	return callID
}

func TestCallOfferAnswerActivate(t *testing.T) {
	h := newCallHarness(t)
	cmA := h.a.CallManager()

	callID := startActiveCall(t, h)

	st, ok := cmA.Stats(callID)
	if !ok || st.Class != defaultBitrateClass {
		t.Fatalf("caller session missing or wrong class: %+v ok=%v", st, ok)
	}
	if state := cmA.CallStateOf(callID); state != CallActive {
		t.Fatalf("caller state = %q, want active", state)
	}
	if state := h.b.CallManager().CallStateOf(h.b.CallManager().CallSessionIDs()[0]); state != CallActive {
		t.Fatalf("callee state = %q, want active", state)
	}
}

func TestCallOfferRefusedWithoutRoute(t *testing.T) {
	h := newCallHarness(t)
	h.a.routes.Remove(h.b.DeviceID)
	if _, err := h.a.CallManager().Offer(h.b.DeviceID, "opus"); err == nil {
		t.Fatal("offer accepted with no route: feasibility gate failed")
	}
}

func TestCallMediaRoundTrip(t *testing.T) {
	h := newCallHarness(t)
	cmA, cmB := h.a.CallManager(), h.b.CallManager()
	callA := startActiveCall(t, h)

	// Send a full FEC group; every frame must arrive and play in order.
	var sent [][]byte
	for i := 0; i < fecGroupSize; i++ {
		frame := bytes.Repeat([]byte{byte(0xA0 + i)}, 40)
		sent = append(sent, frame)
		if err := cmA.SendFrame(callA, frame); err != nil {
			t.Fatalf("send frame %d: %v", i, err)
		}
	}

	callB := cmB.CallSessionIDs()[0]
	for i := 0; i < fecGroupSize; i++ {
		var f *MediaFrame
		var concealed bool
		for tries := 0; tries < 3; tries++ {
			h.advance(frameSlotMs * time.Millisecond)
			var err error
			f, concealed, err = cmB.RecvFrame(callB)
			if err != nil {
				t.Fatalf("recv frame: %v", err)
			}
			if f != nil || concealed {
				break
			}
		}
		if concealed || f == nil {
			t.Fatalf("frame %d missing on a clean link (concealed=%v)", i, concealed)
		}
		if !bytes.Equal(f.Data, sent[i]) {
			t.Fatalf("frame %d corrupted in transit", i)
		}
	}
}

func TestCallFECRecoversSingleLoss(t *testing.T) {
	h := newCallHarness(t)
	cmB := h.b.CallManager()
	startActiveCall(t, h)

	callB := cmB.CallSessionIDs()[0]
	cmB.mu.Lock()
	s := cmB.sessions[callB]
	cmB.mu.Unlock()
	if s == nil {
		t.Fatal("callee session missing")
	}

	// A parity group whose second member was lost in transit: frames 0, 2
	// and 3 arrived; the parity frame closes the group. Recovery must
	// reconstruct frame 1 into the playout timeline.
	var group []*MediaFrame
	for i := 0; i < fecGroupSize; i++ {
		group = append(group, &MediaFrame{Seq: uint32(i), TsMs: uint32(i) * frameSlotMs, Data: bytes.Repeat([]byte{byte(0xC0 + i)}, 32)})
	}
	for _, i := range []int{0, 2, 3} {
		s.jbuf.Push(group[i])
	}
	par := &MediaFrame{Seq: uint32(fecGroupSize) - 1, TsMs: group[3].TsMs, Fec: true, Data: fecParity(group)}
	cmB.mu.Lock()
	cmB.tryRecoverLocked(s, par)
	cmB.mu.Unlock()

	if st, _ := cmB.Stats(callB); st.FramesFec != 1 {
		t.Fatalf("fec_recovered = %d, want 1 (stats %+v)", st.FramesFec, st)
	}
	// Playout must now deliver frames 0..3 with no concealment.
	for want := 0; want < fecGroupSize; want++ {
		h.advance(frameSlotMs * time.Millisecond)
		f, concealed, err := cmB.RecvFrame(callB)
		if err != nil {
			t.Fatalf("recv: %v", err)
		}
		if concealed || f == nil || f.Seq != uint32(want) {
			t.Fatalf("playout slot %d: frame=%v concealed=%v", want, f, concealed)
		}
		if !bytes.Equal(f.Data, group[want].Data) {
			t.Fatalf("recovered frame %d bytes corrupted", want)
		}
	}
}

func TestCallFECRefusesDoubleLoss(t *testing.T) {
	h := newCallHarness(t)
	cmB := h.b.CallManager()
	startActiveCall(t, h)

	callB := cmB.CallSessionIDs()[0]
	cmB.mu.Lock()
	s := cmB.sessions[callB]
	cmB.mu.Unlock()

	var group []*MediaFrame
	for i := 0; i < fecGroupSize; i++ {
		group = append(group, &MediaFrame{Seq: uint32(i), Data: bytes.Repeat([]byte{byte(0xD0 + i)}, 32)})
	}
	// Two members missing: parity cannot reconstruct, and must not fabricate.
	for _, i := range []int{0, 1} {
		s.jbuf.Push(group[i])
	}
	par := &MediaFrame{Seq: uint32(fecGroupSize) - 1, Fec: true, Data: fecParity(group)}
	cmB.mu.Lock()
	cmB.tryRecoverLocked(s, par)
	cmB.mu.Unlock()

	if st, _ := cmB.Stats(callB); st.FramesFec != 0 {
		t.Fatalf("double loss was 'recovered': %+v", st)
	}
}

func TestCallABRLowersClassOnHeavyLoss(t *testing.T) {
	h := newCallHarness(t)
	cmB := h.b.CallManager()
	startActiveCall(t, h)
	callB := cmB.CallSessionIDs()[0]

	// Drive the loss estimator with a heavy-loss measurement window: the
	// class must step down from the default and the reported bitrate with it.
	for i := 0; i < 20; i++ {
		cmB.mu.Lock()
		s := cmB.sessions[callB]
		s.recv, s.concealed = 2, 8 // 80% loss this window
		cmB.updateLossLocked(s)
		cmB.mu.Unlock()
	}
	st, _ := cmB.Stats(callB)
	if st.Class >= defaultBitrateClass {
		t.Fatalf("class did not drop under sustained loss: %+v", st)
	}
	if st.Bitrate != bitrateClasses[st.Class] {
		t.Fatalf("reported bitrate does not match the class: %+v", st)
	}
}

func TestCallABRRaisesClassWhenStable(t *testing.T) {
	h := newCallHarness(t)
	cmB := h.b.CallManager()
	startActiveCall(t, h)
	callB := cmB.CallSessionIDs()[0]

	// First depress the class, then prove a clean link wins it back.
	for i := 0; i < 20; i++ {
		cmB.mu.Lock()
		s := cmB.sessions[callB]
		s.recv, s.concealed = 2, 8
		cmB.updateLossLocked(s)
		cmB.mu.Unlock()
	}
	for i := 0; i < 200; i++ {
		cmB.mu.Lock()
		s := cmB.sessions[callB]
		s.recv, s.concealed = 100, 0
		cmB.updateLossLocked(s)
		cmB.mu.Unlock()
	}
	if st, _ := cmB.Stats(callB); st.Class != len(bitrateClasses)-1 {
		t.Fatalf("class did not recover to the top on a clean link: %+v", st)
	}
}

func TestCallRTTMeasuredByProbe(t *testing.T) {
	h := newCallHarness(t)
	cmA := h.a.CallManager()
	callA := startActiveCall(t, h)

	// The probe fires from the maintenance tick once the probe interval has
	// passed; the pong rides back synchronously over the simulated wire.
	h.advance(3 * time.Second)
	cmA.tick(h.a.now())

	st, ok := cmA.Stats(callA)
	if !ok {
		t.Fatal("session stats missing")
	}
	if st.RTTMs <= 0 {
		t.Fatalf("rtt was never measured: %+v", st)
	}
}

func TestCallHangupEndsBothSides(t *testing.T) {
	h := newCallHarness(t)
	cmA, cmB := h.a.CallManager(), h.b.CallManager()
	callA := startActiveCall(t, h)
	callB := cmB.CallSessionIDs()[0]

	cmA.Hangup(callA)

	if state := cmA.CallStateOf(callA); state != CallEnded {
		t.Fatalf("caller state after hangup = %q", state)
	}
	if state := cmB.CallStateOf(callB); state != CallEnded {
		t.Fatalf("callee never learned of the hangup: %q", state)
	}
}

func TestCallRouteLossEndsWithFallback(t *testing.T) {
	h := newCallHarness(t)
	cmA := h.a.CallManager()
	callA := startActiveCall(t, h)

	// Partition the link: the peer disappears from the caller's route table.
	h.bus.Unwire("sim://caller", "sim://callee")
	h.a.routes.Expire(0)

	h.advance(2 * time.Second)
	cmA.tick(h.a.now())

	if state := cmA.CallStateOf(callA); state != CallEnded {
		t.Fatalf("a call on a dead route must end honestly; state = %q", state)
	}
	if !cmA.FallbackRecommended(callA) {
		t.Fatal("ended call on an unreachable route did not recommend the voice-note fallback")
	}
}

func TestCallKeyEpochRotationRejectsStale(t *testing.T) {
	sk := IdentityKey{}
	for i := range sk {
		sk[i] = byte(i + 1)
	}
	cur := callKey(&sk, "call-1", 4)
	old := callKey(&sk, "call-1", 3)
	curKey, oldKey := IdentityKey(cur), IdentityKey(old)

	body := []byte("secret frame")
	ct, nonce, err := Encrypt(&oldKey, body)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if _, err := Decrypt(&curKey, ct, nonce); err == nil {
		t.Fatal("a frame sealed under the previous epoch opened with the current key")
	}
	if pt, err := Decrypt(&oldKey, ct, nonce); err != nil || !bytes.Equal(pt, body) {
		t.Fatal("epoch key derivation is not deterministic")
	}
}

func TestCallManagerSendFrameGuards(t *testing.T) {
	h := newCallHarness(t)
	cm := h.a.CallManager()

	if err := cm.SendFrame("nope", []byte("x")); err == nil {
		t.Fatal("send on unknown call accepted")
	}
	callID, err := cm.Offer(h.b.DeviceID, "opus")
	if err != nil {
		t.Fatalf("offer: %v", err)
	}
	if err := cm.SendFrame(callID, []byte("too early")); err == nil {
		t.Fatal("send on ringing call accepted")
	}
	big := bytes.Repeat([]byte{0}, DefaultMaxPayload)
	if err := cm.SendFrame(callID, big); err == nil {
		t.Fatal("oversized frame accepted")
	}
}

func TestCallPlaneKindsNeverReachAppHandler(t *testing.T) {
	h := newCallHarness(t)
	// Replace A's handler with a recorder: nothing on the call plane may
	// reach it.
	var mu sync.Mutex
	var appPayloads [][]byte
	h.a.handler = func(p *Packet, pt []byte) {
		mu.Lock()
		appPayloads = append(appPayloads, append([]byte(nil), pt...))
		mu.Unlock()
	}

	cm := h.a.CallManager()
	callID, err := cm.Offer(h.b.DeviceID, "opus")
	if err != nil {
		t.Fatalf("offer: %v", err)
	}
	// Hangup rides the call plane; the bye must not appear as app traffic.
	cm.Hangup(callID)

	mu.Lock()
	defer mu.Unlock()
	for _, pt := range appPayloads {
		if bytes.Equal(pt, []byte("bye")) {
			t.Fatal("a call-plane teardown leaked to the application handler")
		}
	}
}
