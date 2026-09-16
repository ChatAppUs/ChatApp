package mesh

import (
	"crypto/sha256"

	"encoding/hex"
	"testing"
	"time"
)

func voiceTestPair(t *testing.T) (*SimBus, *Node, *Node) {
	t.Helper()
	bus := NewSimBus()
	a := newSimNode(t, bus, "va", "member", nil, 8)
	b := newSimNode(t, bus, "vb", "member", nil, 8)
	t.Cleanup(func() { a.Stop(); b.Stop() })
	bus.Wire("sim://va", "sim://vb")
	bus.Wire("sim://vb", "sim://va")
	seed := func(from, to *Node) {
		beacon := &Beacon{DeviceID: from.DeviceID, Kind: "member", Transport: "sim", Addr: "sim://" + from.DeviceID}
		sb, _ := from.signer.SignBeacon(beacon, from.kem.Public(), from.kem.Epoch)
		data, _ := MarshalSignedBeacon(sb)
		to.HandleInbound("sim://"+from.DeviceID, data)
	}
	seed(a, b)
	seed(b, a)
	return bus, a, b
}

func TestVoiceNoteRoundTrip(t *testing.T) {
	_, a, b := voiceTestPair(t)

	audio := make([]byte, 5000)
	for i := range audio {
		audio[i] = byte(i % 251)
	}

	got := make(chan *VoiceNote, 1)
	b.SetVoiceNoteHandler(func(src string, note *VoiceNote) { got <- note })

	xfer, checksum, err := a.SendVoiceNote("vb", "opus", audio, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if xfer == "" || checksum == "" {
		t.Fatal("sender returned empty transfer id or checksum")
	}
	sum := sha256.Sum256(audio)
	if checksum != hex.EncodeToString(sum[:]) {
		t.Fatal("sender checksum does not match the audio")
	}

	select {
	case note := <-got:
		if note.Codec != "opus" {
			t.Fatalf("codec = %q, want opus", note.Codec)
		}
		if string(note.Audio) != string(audio) {
			t.Fatalf("reassembled audio mismatch: got %d bytes, want %d", note.Size, len(audio))
		}
		if note.Size != len(audio) {
			t.Fatalf("size = %d, want %d", note.Size, len(audio))
		}
		if note.Checksum != checksum {
			t.Fatal("receiver checksum mismatch")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("voice note delivery timed out")
	}
}

func TestVoiceNoteCorruptChunkNeverDelivered(t *testing.T) {
	s := NewVoiceNoteStore()
	audio := []byte("0123456789abcdef")
	sum := sha256.Sum256(audio)
	checksum := hex.EncodeToString(sum[:])

	good := s.Offer("src-a", &VoiceChunk{Xfer: "t1", Index: 0, Total: 1, Codec: "opus", Checksum: checksum, Data: audio})
	if good == nil {
		t.Fatal("intact note was not delivered")
	}
	// A flipped bit must fail verification and be discarded, not delivered.
	bad := append([]byte(nil), audio...)
	bad[3] ^= 0xff
	if n := s.Offer("src-b", &VoiceChunk{Xfer: "t2", Index: 0, Total: 1, Codec: "opus", Checksum: checksum, Data: bad}); n != nil {
		t.Fatal("corrupt assembly was delivered")
	}
	// The corrupted assembly was discarded, so a clean re-send completes.
	if n := s.Offer("src-b", &VoiceChunk{Xfer: "t3", Index: 0, Total: 1, Codec: "opus", Checksum: checksum, Data: audio}); n == nil {
		t.Fatal("clean re-send after corruption was not delivered")
	}
}

func TestVoiceNoteOutOfOrderAndDuplicateChunks(t *testing.T) {
	s := NewVoiceNoteStore()
	audio := []byte("abcdefghij")
	sum := sha256.Sum256(audio)
	checksum := hex.EncodeToString(sum[:])
	chunk := func(i int, data string) *VoiceChunk {
		return &VoiceChunk{Xfer: "t1", Index: i, Total: 2, Codec: "aac", Checksum: checksum, Data: []byte(data)}
	}
	if s.Offer("src-a", chunk(1, "fghij")) != nil {
		t.Fatal("single trailing chunk completed the note")
	}
	if s.Offer("src-a", chunk(1, "fghij")) != nil {
		t.Fatal("duplicate chunk completed the note")
	}
	n := s.Offer("src-a", chunk(0, "abcde"))
	if n == nil {
		t.Fatal("note did not complete once all chunks arrived")
	}
	if string(n.Audio) != "abcdefghij" {
		t.Fatalf("reassembled = %q", n.Audio)
	}
	// Full duplicates after completion never re-deliver.
	if s.Offer("src-a", chunk(0, "abcde")) != nil {
		t.Fatal("completed note re-delivered on duplicate chunk")
	}
}

func TestVoiceNoteMissingReportsGaps(t *testing.T) {
	s := NewVoiceNoteStore()
	sum := sha256.Sum256([]byte("xyz"))
	checksum := hex.EncodeToString(sum[:])
	if _, ok := s.Missing("src-a", checksum); ok {
		t.Fatal("missing reported ok before any chunk arrived")
	}
	s.Offer("src-a", &VoiceChunk{Xfer: "t1", Index: 2, Total: 3, Codec: "opus", Checksum: checksum, Data: []byte("zz")})
	missing, ok := s.Missing("src-a", checksum)
	if !ok {
		t.Fatal("missing reported nothing in flight")
	}
	if len(missing) != 2 || missing[0] != 0 || missing[1] != 1 {
		t.Fatalf("missing = %v, want [0 1]", missing)
	}
}

func TestVoiceNoteValidation(t *testing.T) {
	_, a, b := voiceTestPair(t)
	if _, _, err := a.SendVoiceNote("", "opus", []byte("x"), 0); err == nil {
		t.Fatal("empty destination accepted")
	}
	if _, _, err := a.SendVoiceNote("vb", "", []byte("x"), 0); err == nil {
		t.Fatal("empty codec accepted")
	}
	if _, _, err := a.SendVoiceNote("vb", "opus", nil, 0); err == nil {
		t.Fatal("empty audio accepted")
	}
	if _, _, err := a.SendVoiceNote("vb", "opus", []byte("x"), 1<<30); err == nil {
		t.Fatal("oversized chunk size accepted")
	}
	// decodeVoiceChunk rejects malformed payloads.
	if _, ok := decodeVoiceChunk([]byte("not json")); ok {
		t.Fatal("garbage plaintext decoded as a voice chunk")
	}
	if _, ok := decodeVoiceChunk([]byte(`{"xfer":"","index":0,"total":1,"codec":"opus","checksum":"ab"}`)); ok {
		t.Fatal("chunk with empty xfer id decoded")
	}
	if _, ok := decodeVoiceChunk([]byte(`{"xfer":"t","index":5,"total":2,"codec":"opus","checksum":"ab"}`)); ok {
		t.Fatal("chunk index beyond total decoded")
	}
	_ = b
}

func TestVoiceNoteResumeSendsOnlyMissing(t *testing.T) {
	_, a, b := voiceTestPair(t)
	audio := make([]byte, 4000)
	for i := range audio {
		audio[i] = byte(i & 0xff)
	}
	xfer, checksum, err := a.SendVoiceNote("vb", "opus", audio, 1000)
	if err != nil {
		t.Fatal(err)
	}
	_ = xfer
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := b.VoiceNoteProgress("va", checksum); !ok {
			// No assembly in flight: the note already completed.
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	// Nothing is missing after full delivery, so resume must re-send 0.
	missing, inFlight := b.VoiceNoteProgress("va", checksum)
	if inFlight {
		t.Fatalf("assembly still in flight after completion: missing=%v", missing)
	}
	sent, err := a.SendVoiceNoteResume("vb", "opus", checksum, audio, 1000, nil)
	if err != nil {
		t.Fatal(err)
	}
	if sent != 0 {
		t.Fatalf("resume re-sent %d chunks after complete delivery, want 0", sent)
	}
}
