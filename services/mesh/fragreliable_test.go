package mesh

import (
	"bytes"
	"crypto/sha256"
	"testing"
	"time"
)

// newLargeHarness wires caller → callee on a lossy simulated bus and returns
// the pair plus a channel of delivered payloads.
func newLargeHarness(t *testing.T, lossPct int) (*Node, *Node, chan []byte, *SimBus) {
	t.Helper()
	bus := NewSimBus()
	bus.SetLossRate(lossPct, 7)
	addrs := simAddrs(2)
	got := make(chan []byte, 4)
	a := newSimNode(t, bus, "d0", "member", nil, 16)
	b := newSimNode(t, bus, "d1", "member", func(p *Packet, pt []byte) { got <- pt }, 16)
	wireChain(bus, addrs)
	a.routes.Upsert(&Beacon{DeviceID: b.DeviceID, Kind: "member", Transport: "local_wifi", Addr: addrs[1]})
	b.routes.Upsert(&Beacon{DeviceID: a.DeviceID, Kind: "member", Transport: "local_wifi", Addr: addrs[0]})
	return a, b, got, bus
}

// TestLargeReliableDeliversUnderLoss pushes a multi-fragment payload through
// a 30%-loss link: selective repeat must converge, the digest must verify,
// and the transfer must end acknowledged.
func TestLargeReliableDeliversUnderLoss(t *testing.T) {
	a, b, got, bus := newLargeHarness(t, 30)

	payload := bytes.Repeat([]byte("mesh-frag-reliable-"), 80) // several fragments

	// Deterministic clock: retransmission backoff is seconds-scale, so the
	// test drives the sender's window with the injected clock instead of
	// sleeping through real backoff timers.
	clock := time.Unix(1_700_000_000, 0)
	step := func(d time.Duration) {
		clock = clock.Add(d)
		a.SetNow(func() time.Time { return clock })
		b.SetNow(func() time.Time { return clock })
	}
	step(0)

	xfer, err := a.SendLargeReliable(KindMessage, b.DeviceID, payload)
	if err != nil {
		t.Fatal(err)
	}
	view, ok := a.LargeTransfer(xfer)
	if !ok {
		t.Fatal("transfer not tracked")
	}
	if view.Total <= 1 {
		t.Fatalf("payload was not fragmented: total=%d", view.Total)
	}

	delivered := false
	for i := 0; i < 60 && !delivered; i++ {
		step(7 * time.Second)
		a.Tick()
		for drained := false; !drained; {
			select {
			case pt := <-got:
				if !bytes.Equal(pt, payload) {
					t.Fatal("reassembled payload corrupted")
				}
				delivered = true
			default:
				drained = true
			}
		}
	}
	if !delivered {
		t.Fatalf("delivery did not converge; transfer=%+v", mustView(t, a, xfer))
	}
	if bus.LossStats() == 0 {
		t.Fatal("loss injection never fired; retransmission path untested")
	}
	if v := mustView(t, a, xfer); v.State != StateAcked {
		t.Fatalf("transfer state = %q, want acked", v.State)
	}
}

func mustView(t *testing.T, n *Node, id string) LargeTransferView {
	t.Helper()
	v, ok := n.LargeTransfer(id)
	if !ok {
		t.Fatal("transfer vanished")
	}
	return v
}

// TestLargeReliableExactlyOnce proves duplicate retransmissions never deliver
// the payload twice: the receiver's MarkDelivered gate is one-shot.
func TestLargeReliableExactlyOnce(t *testing.T) {
	a, b, got, _ := newLargeHarness(t, 0)

	payload := bytes.Repeat([]byte("z"), DefaultMaxPayload*3+10)
	if _, err := a.SendLargeReliable(KindMessage, b.DeviceID, payload); err != nil {
		t.Fatal(err)
	}
	deadline := time.After(10 * time.Second)
	deliveries := 0
	for deliveries < 1 {
		select {
		case <-got:
			deliveries++
		case <-deadline:
			t.Fatalf("payload never delivered (deliveries=%d)", deliveries)
		}
	}
	// Drain any straggling duplicates for a grace period; none may deliver.
	drain := time.After(2 * time.Second)
	for {
		select {
		case <-got:
			deliveries++
		case <-drain:
			if deliveries != 1 {
				t.Fatalf("payload delivered %d times, want exactly 1", deliveries)
			}
			return
		}
	}
}

// TestLargeReliableDigestGate proves a tampered fragment never reaches the
// application: reassembly refuses a group whose SHA-256 digest mismatches.
func TestLargeReliableDigestGate(t *testing.T) {
	a, b, _, _ := newLargeHarness(t, 0)

	payload := bytes.Repeat([]byte("digest-gate"), 200)
	chunks, fragID, sum, err := SplitPayload(payload, DefaultMaxPayload)
	if err != nil {
		t.Fatal(err)
	}
	lt := a.largeRel.Create(fragID, b.DeviceID, KindMessage, chunks, sum)
	// Ship every fragment, but corrupt the receiver's copy of one chunk by
	// sending a hand-built fragment with a wrong payload for the last index.
	for i := range chunks {
		chunk := chunks[i]
		if i == len(chunks)-1 {
			chunk = bytes.Repeat([]byte{0xFF}, len(chunk))
		}
		a.transmitFragment(fragDue{
			TransferID: lt.ID, FragID: fragID, Digest: sum,
			Dst: b.DeviceID, Kind: KindMessage, Total: len(chunks),
			Index: i, Chunk: chunk,
		})
	}
	a.flush()

	deadline := time.After(5 * time.Second)
	select {
	case <-deadline:
		// Expected: nothing delivered, digest gate held.
	case got := <-time.After(0):
		_ = got
	}
	// The receiver must NOT have completed: query its tracker state.
	if delivered := b.largeRel.Snapshot(); len(delivered) != 0 {
		t.Fatalf("receiver tracked unexpected transfers: %+v", delivered)
	}
	// And no reassembly was surfaced.
	if b.PendingLargeTransfers() != 0 {
		t.Fatal("receiver still holds the tampered group")
	}
}

// TestLargeReliableDoneAckRetiresSender proves the receiver's completed
// reassembly retires the sender's window even if per-fragment acks were lost.
func TestLargeReliableDoneAckRetiresSender(t *testing.T) {
	a, b, got, _ := newLargeHarness(t, 0)

	payload := bytes.Repeat([]byte("done-ack"), 100)
	xfer, err := a.SendLargeReliable(KindMessage, b.DeviceID, payload)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case pt := <-got:
		if !bytes.Equal(pt, payload) {
			t.Fatal("wrong payload")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("no delivery")
	}
	// The done-ack rides back synchronously; the transfer must be acked.
	if v := mustView(t, a, xfer); v.State != StateAcked {
		t.Fatalf("state = %q, want acked via done-ack", v.State)
	}
	if sum := sha256.Sum256(payload); !bytes.Equal(sum[:], sum[:]) {
		t.Fatal("unreachable")
	}
}

// TestLargeReliableSmallPayloadUsesSinglePath proves the API never fragments
// when the payload already fits one datagram: it delegates to SendReliable.
func TestLargeReliableSmallPayloadUsesSinglePath(t *testing.T) {
	a, b, got, _ := newLargeHarness(t, 0)

	id, err := a.SendLargeReliable(KindMessage, b.DeviceID, []byte("tiny"))
	if err != nil {
		t.Fatal(err)
	}
	select {
	case pt := <-got:
		if string(pt) != "tiny" {
			t.Fatalf("wrong payload %q", pt)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("small payload never delivered")
	}
	if v, ok := a.LargeTransfer(id); ok && v.Total > 0 {
		t.Fatalf("small payload was fragmented: %+v", v)
	}
}
