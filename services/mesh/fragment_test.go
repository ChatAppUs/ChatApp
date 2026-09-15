package mesh

import (
	"bytes"
	"sync"
	"testing"
	"time"
)

// TestEnvelopeVersionGate covers the forward-compatibility anchor: this build
// stamps a known version, a future version fails closed, and the legacy zero
// value stays accepted so a rolling upgrade does not partition the mesh.
func TestEnvelopeVersionGate(t *testing.T) {
	p := NewPacket(KindMessage, "a", "b", 8)
	if p.Version != EnvelopeVersion {
		t.Fatalf("new packet version = %d, want %d", p.Version, EnvelopeVersion)
	}
	if err := p.Validate(); err != nil {
		t.Fatalf("valid packet rejected: %v", err)
	}

	// A future, incompatible version must round-trip through the wire gate as
	// an error rather than being misparsed.
	p.Version = EnvelopeVersion + 1
	data, err := p.Marshal()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if _, err := UnmarshalPacket(data); err == nil {
		t.Fatal("future envelope version was accepted; must fail closed")
	}

	// The legacy zero value predates versioning and must still be accepted.
	legacy := NewPacket(KindMessage, "a", "b", 8)
	legacy.Version = 0
	legacyData, err := legacy.Marshal()
	if err != nil {
		t.Fatalf("marshal legacy: %v", err)
	}
	if _, err := UnmarshalPacket(legacyData); err != nil {
		t.Fatalf("legacy envelope rejected: %v", err)
	}
}

// TestPacketValidateRejectsMalformed covers the structural gate applied before
// routing, including inconsistent fragment metadata.
func TestPacketValidateRejectsMalformed(t *testing.T) {
	base := func() *Packet { return NewPacket(KindMessage, "a", "b", 8) }

	cases := map[string]func(p *Packet){
		"no source":      func(p *Packet) { p.Src = "" },
		"no id":          func(p *Packet) { p.ID = "" },
		"no route":       func(p *Packet) { p.Dst = ""; p.GroupID = "" },
		"negative ttl":   func(p *Packet) { p.TTL = -1 },
		"frag no id":     func(p *Packet) { p.FragTotal = 3; p.FragIndex = 0 },
		"frag bad index": func(p *Packet) { p.FragTotal = 3; p.FragIndex = 5; p.FragID = "g" },
		"frag no digest": func(p *Packet) { p.FragTotal = 2; p.FragIndex = 0; p.FragID = "g" },
	}
	for name, mutate := range cases {
		p := base()
		mutate(p)
		if err := p.Validate(); err == nil {
			t.Errorf("%s: malformed packet was accepted", name)
		}
	}

	// A group packet with no direct destination is legitimate.
	g := NewPacket(KindGroupMessage, "a", "", 8)
	g.GroupID = "room"
	if err := g.Validate(); err != nil {
		t.Errorf("group packet rejected: %v", err)
	}
}

// TestSplitPayloadSingleChunk confirms a payload that fits in one datagram is
// left intact with no fragment group, preserving interop with peers that
// predate fragmentation.
func TestSplitPayloadSingleChunk(t *testing.T) {
	chunks, fragID, sum, err := SplitPayload([]byte("short message"), DefaultMaxPayload)
	if err != nil {
		t.Fatalf("SplitPayload: %v", err)
	}
	if len(chunks) != 1 || fragID != "" {
		t.Fatalf("small payload was fragmented: chunks=%d fragID=%q", len(chunks), fragID)
	}
	if !bytes.Equal(chunks[0], []byte("short message")) {
		t.Fatal("single chunk did not preserve the payload")
	}
	if len(sum) != 32 {
		t.Fatalf("digest length = %d, want 32", len(sum))
	}
}

// TestFragmentRoundTripReassembles drives a payload far larger than the radio
// MTU through split -> per-fragment encrypt -> decrypt -> reassemble and checks
// the recovered bytes and digest.
func TestFragmentRoundTripReassembles(t *testing.T) {
	key := MustKey()
	original := bytes.Repeat([]byte("voice-note-payload-"), 400) // ~7.6 KB, many fragments

	chunks, fragID, sum, err := SplitPayload(original, DefaultMaxPayload)
	if err != nil {
		t.Fatalf("SplitPayload: %v", err)
	}
	if fragID == "" {
		t.Fatal("large payload was not fragmented")
	}
	if len(chunks) < 10 {
		t.Fatalf("expected many fragments, got %d", len(chunks))
	}
	for i, c := range chunks {
		if len(c) > DefaultMaxPayload {
			t.Fatalf("fragment %d exceeds MTU ceiling: %d bytes", i, len(c))
		}
	}

	r := NewReassembler()
	var got []byte
	complete := false
	// Deliver out of order to prove order independence.
	for _, i := range shuffled(len(chunks)) {
		ct, nonce, err := Encrypt(key, chunks[i])
		if err != nil {
			t.Fatalf("encrypt fragment %d: %v", i, err)
		}
		p := &Packet{
			Version:   EnvelopeVersion,
			ID:        newPacketID(),
			Src:       "peer",
			Dst:       "me",
			Kind:      KindVoiceMessage,
			FragIndex: i,
			FragTotal: len(chunks),
			FragID:    fragID,
			FragSum:   sum,
			Payload:   ct,
			Nonce:     nonce,
		}
		pt, err := Decrypt(key, p.Payload, p.Nonce)
		if err != nil {
			t.Fatalf("decrypt fragment %d: %v", i, err)
		}
		out, done, err := r.Add(p, pt)
		if err != nil {
			t.Fatalf("Add fragment %d: %v", i, err)
		}
		if done {
			got = out
			complete = true
		}
	}
	if !complete {
		t.Fatal("payload never completed despite every fragment arriving")
	}
	if !bytes.Equal(got, original) {
		t.Fatal("reassembled payload does not match the original")
	}
	if r.Open() != 0 {
		t.Fatalf("reassembly group was not released: %d open", r.Open())
	}
}

// TestReassemblerIgnoresDuplicateFragment confirms a re-delivered fragment is
// ignored rather than counted twice, so a duplicate cannot complete a group
// with a hole in it.
func TestReassemblerIgnoresDuplicateFragment(t *testing.T) {
	p := func(idx int, chunk []byte) (*Packet, []byte) {
		return &Packet{
			Version:   EnvelopeVersion,
			ID:        newPacketID(),
			Src:       "peer",
			Dst:       "me",
			FragIndex: idx,
			FragTotal: 2,
			FragID:    "g1",
			FragSum:   sumOf([]byte("aabb")),
		}, chunk
	}
	r := NewReassembler()
	p0, c0 := p(0, []byte("aa"))
	if _, done, err := r.Add(p0, c0); done || err != nil {
		t.Fatalf("first fragment: done=%v err=%v", done, err)
	}
	// Same index again: must be ignored, group must NOT complete.
	pDup, cDup := p(0, []byte("aa"))
	if _, done, err := r.Add(pDup, cDup); done || err != nil {
		t.Fatalf("duplicate fragment completed the group: done=%v err=%v", done, err)
	}
	p1, c1 := p(1, []byte("bb"))
	out, done, err := r.Add(p1, c1)
	if err != nil || !done {
		t.Fatalf("final fragment: done=%v err=%v", done, err)
	}
	if !bytes.Equal(out, []byte("aabb")) {
		t.Fatalf("reassembled %q, want %q", out, "aabb")
	}
}

// TestReassemblerRejectsDigestMismatch confirms tampered or corrupted content
// is refused instead of delivered to the application.
func TestReassemblerRejectsDigestMismatch(t *testing.T) {
	r := NewReassembler()
	p := &Packet{
		Version:   EnvelopeVersion,
		ID:        newPacketID(),
		Src:       "peer",
		Dst:       "me",
		FragIndex: 0,
		FragTotal: 2,
		FragID:    "g1",
		FragSum:   sumOf([]byte("good content")),
	}
	if _, _, err := r.Add(p, []byte("evil")); err != nil {
		t.Fatalf("first fragment: %v", err)
	}
	p2 := *p
	p2.ID = newPacketID()
	p2.FragIndex = 1
	if _, _, err := r.Add(&p2, []byte("payload")); err != ErrFragmentDigestMismatch {
		t.Fatalf("digest mismatch err = %v, want %v", err, ErrFragmentDigestMismatch)
	}
}

// TestReassemblerRejectsContradictoryMetadata confirms a second fragment that
// redefines the group's total or digest fails closed and drops the group.
func TestReassemblerRejectsContradictoryMetadata(t *testing.T) {
	r := NewReassembler()
	p := &Packet{
		Version:   EnvelopeVersion,
		ID:        newPacketID(),
		Src:       "peer",
		Dst:       "me",
		FragIndex: 0,
		FragTotal: 2,
		FragID:    "g1",
		FragSum:   sumOf([]byte("xxxx")),
	}
	if _, _, err := r.Add(p, []byte("x")); err != nil {
		t.Fatalf("first fragment: %v", err)
	}
	bad := *p
	bad.ID = newPacketID()
	bad.FragIndex = 1
	bad.FragTotal = 9 // contradicts the group
	if _, _, err := r.Add(&bad, []byte("x")); err != ErrFragmentInvalid {
		t.Fatalf("contradictory metadata err = %v, want %v", err, ErrFragmentInvalid)
	}
	if r.Open() != 0 {
		t.Fatal("contradicted group was not dropped")
	}
}

// TestReassemblerInvalidMetadata confirms out-of-range indices are rejected.
func TestReassemblerInvalidMetadata(t *testing.T) {
	r := NewReassembler()
	cases := []*Packet{
		{FragTotal: 3, FragIndex: 3, FragID: "g"},  // index == total
		{FragTotal: 3, FragIndex: -1, FragID: "g"}, // negative index
		{FragTotal: 3, FragIndex: 0},               // missing group id
		{FragTotal: maxFragments + 1, FragIndex: 0, FragID: "g"},
	}
	for i, p := range cases {
		if _, _, err := r.Add(p, []byte("x")); err != ErrFragmentInvalid {
			t.Errorf("case %d: err = %v, want %v", i, err, ErrFragmentInvalid)
		}
	}
}

// TestReassemblerExpiresIncompleteGroup confirms a transfer whose missing
// fragments never arrive is released rather than occupying memory forever.
func TestReassemblerExpiresIncompleteGroup(t *testing.T) {
	now := time.Now()
	r := NewReassembler()
	r.now = func() time.Time { return now }

	p := &Packet{
		Version:   EnvelopeVersion,
		ID:        newPacketID(),
		Src:       "peer",
		Dst:       "me",
		FragIndex: 0,
		FragTotal: 4,
		FragID:    "g1",
		FragSum:   sumOf([]byte("never completes")),
	}
	if _, _, err := r.Add(p, []byte("part")); err != nil {
		t.Fatalf("first fragment: %v", err)
	}
	if r.Open() != 1 {
		t.Fatalf("open groups = %d, want 1", r.Open())
	}
	// Advance past the assembly TTL; the next Add must prune the stale group.
	now = now.Add(2 * assemblyTTL)
	other := *p
	other.ID = newPacketID()
	other.FragID = "g2"
	if _, _, err := r.Add(&other, []byte("part")); err != nil {
		t.Fatalf("later fragment: %v", err)
	}
	if r.Open() != 1 {
		t.Fatalf("stale group was not expired: %d open", r.Open())
	}
}

// TestNodeSendLargeFragmentsAndDelivers drives the production node API end to
// end: a payload larger than the MTU is fragmented by SendLarge, routed across
// a three-node chain, reassembled and delivered exactly once, in order.
func TestNodeSendLargeFragmentsAndDelivers(t *testing.T) {
	bus := NewSimBus()
	addrs := simAddrs(3)

	var mu sync.Mutex
	var delivered [][]byte
	a := newSimNode(t, bus, "d0", "member", nil, 16)
	b := newSimNode(t, bus, "d1", "relay", nil, 16)
	c := newSimNode(t, bus, "d2", "member", func(p *Packet, pt []byte) {
		mu.Lock()
		delivered = append(delivered, append([]byte(nil), pt...))
		mu.Unlock()
	}, 16)
	wireChain(bus, addrs)
	// Advertise the neighbours each node can reach (on real radios the
	// discovery beacons do this).
	a.routes.Upsert(&Beacon{DeviceID: b.DeviceID, Kind: "relay", Transport: "local_wifi", Addr: addrs[1]})
	b.routes.Upsert(&Beacon{DeviceID: a.DeviceID, Kind: "member", Transport: "local_wifi", Addr: addrs[0]})
	b.routes.Upsert(&Beacon{DeviceID: c.DeviceID, Kind: "member", Transport: "local_wifi", Addr: addrs[2]})
	c.routes.Upsert(&Beacon{DeviceID: b.DeviceID, Kind: "relay", Transport: "local_wifi", Addr: addrs[1]})

	// ~5 KB: enough to require many MTU-bounded fragments.
	payload := bytes.Repeat([]byte("offline-voice-note-chunk-"), 200)
	if _, err := a.SendLarge(KindVoiceMessage, c.DeviceID, payload, false); err != nil {
		t.Fatalf("SendLarge: %v", err)
	}

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(delivered)
		mu.Unlock()
		if n >= 1 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(delivered) != 1 {
		t.Fatalf("delivered %d payloads, want exactly 1 (fragments must not leak per-part)", len(delivered))
	}
	if !bytes.Equal(delivered[0], payload) {
		t.Fatal("delivered payload does not match the original")
	}
}

// sumOf returns the SHA-256 digest used by the reassembler for a test payload.
func sumOf(b []byte) []byte {
	_, _, sum, err := SplitPayload(b, DefaultMaxPayload)
	if err != nil {
		panic(err)
	}
	return sum
}

// shuffled returns indices 0..n-1 rotated by half, so the result is never
// sequential and reassembly is proven independent of arrival order.
func shuffled(n int) []int {
	out := make([]int, 0, n)
	half := n / 2
	for i := 0; i < n; i++ {
		out = append(out, (i+half)%n)
	}
	return out
}
