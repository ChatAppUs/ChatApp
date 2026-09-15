package mesh

// reliability_test.go — executable coverage for the reliable-delivery layer:
// acknowledgements, bounded retries with backoff, alternate-path retry,
// exactly-once application delivery, terminal states, the priority-ordered
// forwarding buffer, and the live end-to-end ACK round trip over a real
// transport.
//
// Where timing matters the tests drive the tracker's clock directly instead of
// sleeping, so the state machine is asserted deterministically and the suite
// stays fast.

import (
	"sync"
	"testing"
	"time"
)

// TestTransferAckSettles verifies a live transfer stops retrying once the
// destination acknowledges it.
func TestTransferAckSettles(t *testing.T) {
	tr := NewTransferTracker(time.Second, 5, time.Hour)
	tr.SetNow(func() time.Time { return time.Unix(0, 0) })

	x := tr.Create("dev-b", KindMessage, PriorityText, []byte("hi"))

	// First tick transmits attempt 1.
	due := tr.Tick()
	if len(due) != 1 || due[0].Attempts != 1 {
		t.Fatalf("expected one attempt, got %d", len(due))
	}
	if due[0].State != StateRelaying {
		t.Fatalf("state = %q, want relaying", due[0].State)
	}

	// The acknowledgement settles it, and it is never retried again.
	if !tr.Ack(x.ID) {
		t.Fatal("first acknowledgement should report settled")
	}
	if again := tr.Tick(); len(again) != 0 {
		t.Fatalf("acknowledged transfer was retried %d times", len(again))
	}
	view, ok := tr.Get(x.ID)
	if !ok || view.State != StateAcked {
		t.Fatalf("state = %+v, want acked", view)
	}
	if tr.Pending() != 0 {
		t.Fatalf("expected no pending transfers, got %d", tr.Pending())
	}
}

// TestTransferRetriesWithBackoff verifies a transfer is retried a bounded
// number of times with a growing interval, then dead-lettered.
func TestTransferRetriesWithBackoff(t *testing.T) {
	tr := NewTransferTracker(time.Second, 3, 24*time.Hour)
	now := time.Unix(0, 0)
	tr.SetNow(func() time.Time { return now })

	x := tr.Create("dev-b", KindMessage, PriorityText, []byte("hi"))

	// Attempt 1 fires immediately.
	if due := tr.Tick(); len(due) != 1 {
		t.Fatalf("expected attempt 1, got %d", len(due))
	}
	// Nothing is due before the backoff elapses.
	if due := tr.Tick(); len(due) != 0 {
		t.Fatalf("expected no attempt before backoff, got %d", len(due))
	}
	// After the first backoff, attempt 2 fires and the interval doubled.
	now = now.Add(1500 * time.Millisecond)
	if due := tr.Tick(); len(due) != 1 || due[0].Attempts != 2 {
		t.Fatalf("expected attempt 2, got %+v", due)
	}
	// Advance past the capped backoff so attempt 3 fires. The step stays well
	// inside the payload lifetime, so this exercises the RETRY BUDGET rather
	// than expiry — the two terminal states must stay distinguishable.
	now = now.Add(5 * time.Minute)
	if due := tr.Tick(); len(due) != 1 || due[0].Attempts != 3 {
		t.Fatalf("expected attempt 3, got %+v", due)
	}
	// The retry budget is exhausted: the next tick dead-letters rather than
	// transmitting a fourth attempt.
	now = now.Add(5 * time.Minute)
	if due := tr.Tick(); len(due) != 0 {
		t.Fatalf("expected no attempt past the retry budget, got %d", len(due))
	}
	view, _ := tr.Get(x.ID)
	if view.State != StateDeadLetter {
		t.Fatalf("state = %q, want dead_letter", view.State)
	}
	if view.Attempts != 3 {
		t.Fatalf("attempts = %d, want 3", view.Attempts)
	}
	// Dead letter is terminal and distinct from expiry.
	if !view.State.IsTerminal() {
		t.Fatal("dead_letter must be terminal")
	}
	if view.State == StateExpired {
		t.Fatal("dead_letter must be distinguishable from expired")
	}
}

// TestTransferGoesQueuedThenRelaying verifies the state machine exposes the
// queued -> relaying transition and then a terminal dead_letter once the retry
// budget is exhausted without an acknowledgement.
func TestTransferGoesQueuedThenRelaying(t *testing.T) {
	tr := NewTransferTracker(time.Second, 2, 24*time.Hour)
	now := time.Unix(0, 0)
	tr.SetNow(func() time.Time { return now })

	x := tr.Create("dev-b", KindMessage, PriorityText, []byte("hi"))
	if view, _ := tr.Get(x.ID); view.State != StateQueued {
		t.Fatalf("fresh transfer state = %q, want queued", view.State)
	}
	if due := tr.Tick(); len(due) != 1 {
		t.Fatalf("expected attempt 1, got %d", len(due))
	}
	if view, _ := tr.Get(x.ID); view.State != StateRelaying {
		t.Fatalf("state after attempt 1 = %q, want relaying", view.State)
	}
	if due := tr.Tick(); len(due) != 0 {
		t.Fatalf("expected no attempt before the backoff elapsed, got %d", len(due))
	}
	now = now.Add(5 * time.Minute)
	if due := tr.Tick(); len(due) != 1 || due[0].Attempts != 2 {
		t.Fatalf("expected attempt 2, got %+v", due)
	}
	// Retry budget (2) exhausted: the next tick dead-letters.
	now = now.Add(5 * time.Minute)
	if due := tr.Tick(); len(due) != 0 {
		t.Fatalf("expected no attempt past the retry budget, got %d", len(due))
	}
	view, _ := tr.Get(x.ID)
	if view.State != StateDeadLetter {
		t.Fatalf("state = %q, want dead_letter", view.State)
	}
	if view.Attempts != 2 {
		t.Fatalf("attempts = %d, want 2", view.Attempts)
	}
	// Dead letter is terminal and distinct from expiry.
	if !view.State.IsTerminal() {
		t.Fatal("dead_letter must be terminal")
	}
	if view.State == StateExpired {
		t.Fatal("dead_letter must be distinguishable from expired")
	}
}

// TestTransferExpiresBeforeRetryBudget verifies the payload lifetime wins over
// the retry budget: a transfer that outlives its TTL expires, not dead-letters.
func TestTransferExpiresBeforeRetryBudget(t *testing.T) {
	tr := NewTransferTracker(time.Second, 10, 5*time.Second)
	now := time.Unix(0, 0)
	tr.SetNow(func() time.Time { return now })

	x := tr.Create("dev-b", KindMessage, PriorityText, []byte("hi"))
	if due := tr.Tick(); len(due) != 1 {
		t.Fatalf("expected attempt 1, got %d", len(due))
	}
	now = now.Add(6 * time.Second) // past the 5s lifetime
	if due := tr.Tick(); len(due) != 0 {
		t.Fatalf("expected no attempt after expiry, got %d", len(due))
	}
	view, _ := tr.Get(x.ID)
	if view.State != StateExpired {
		t.Fatalf("state = %q, want expired", view.State)
	}
	if view.State == StateDeadLetter {
		t.Fatal("expiry must be distinguishable from dead_letter")
	}
}

// TestReceiverExactlyOnceDelivery verifies that retried copies of one transfer
// are collapsed to a single application delivery while every copy is still
// acknowledged, so a lost acknowledgement converges.
func TestReceiverExactlyOnceDelivery(t *testing.T) {
	tr := NewTransferTracker(time.Second, 5, time.Hour)
	if !tr.MarkDelivered("xfer-1") {
		t.Fatal("first copy must be delivered")
	}
	for i := 0; i < 5; i++ {
		if tr.MarkDelivered("xfer-1") {
			t.Fatalf("duplicate copy %d was re-delivered to the application", i)
		}
	}
	// A different transfer is unaffected.
	if !tr.MarkDelivered("xfer-2") {
		t.Fatal("a distinct transfer must still be delivered")
	}
}

// TestTransferRetentionBounded verifies the tracker cannot grow without bound:
// once the cap is exceeded the oldest terminal transfers are evicted.
func TestTransferRetentionBounded(t *testing.T) {
	tr := NewTransferTracker(time.Second, 5, time.Hour)
	tr.maxActive = 8
	for i := 0; i < 40; i++ {
		x := tr.Create("dev-b", KindMessage, PriorityText, []byte("hi"))
		tr.Ack(x.ID)
	}
	if n := len(tr.Snapshot()); n > 8 {
		t.Fatalf("retained %d transfers, want at most 8", n)
	}
}

// TestDeliveryCounts verifies the aggregate the status endpoint exposes.
func TestDeliveryCounts(t *testing.T) {
	tr := NewTransferTracker(time.Second, 5, time.Hour)
	a := tr.Create("dev-b", KindMessage, PriorityText, []byte("a"))
	b := tr.Create("dev-c", KindMessage, PriorityText, []byte("b"))
	tr.Ack(a.ID)
	_ = b

	counts := tr.Counts()
	if counts[string(StateQueued)] != 1 {
		t.Fatalf("queued = %d, want 1", counts[string(StateQueued)])
	}
	if counts[string(StateAcked)] != 1 {
		t.Fatalf("acked = %d, want 1", counts[string(StateAcked)])
	}
	for _, s := range []DeliveryState{StateRelaying, StateExpired, StateDeadLetter} {
		if _, ok := counts[string(s)]; !ok {
			t.Fatalf("counts missing the %q state", s)
		}
	}
}

// TestAckPacketValidation verifies a nameless acknowledgement is rejected
// before it can consume relay quota.
func TestAckPacketValidation(t *testing.T) {
	bad := NewPacket(KindAck, "dev-a", "dev-b", 8)
	if err := bad.Validate(); err == nil {
		t.Fatal("expected an acknowledgement with no transfer id to be rejected")
	}
	good := NewPacket(KindAck, "dev-a", "dev-b", 8)
	good.AckFor = "xfer-1"
	if err := good.Validate(); err != nil {
		t.Fatalf("valid acknowledgement rejected: %v", err)
	}
}

// TestPriorityOrdering verifies the forwarding buffer drains control traffic
// ahead of text, voice and media, and preserves FIFO inside a class.
func TestPriorityOrdering(t *testing.T) {
	q := NewPriorityQueue(100, 1<<20, time.Hour)
	mk := func(kind PacketKind, n int) *Packet {
		return NewPacket(kind, "src", "dst", 8)
	}
	// Enqueue in reverse priority order.
	media := mk(KindVoiceMessage, 0)
	media.Kind = "media"
	q.Enqueue(media)
	q.Enqueue(mk(KindVoiceMessage, 0))
	q.Enqueue(mk(KindGroupMessage, 0))
	ackA := NewPacket(KindAck, "src", "dst", 8)
	ackA.AckFor = "x1"
	ackB := NewPacket(KindAck, "src", "dst", 8)
	ackB.AckFor = "x2"
	q.Enqueue(ackA)
	q.Enqueue(ackB)

	drained := q.Drain(0)
	if len(drained) != 5 {
		t.Fatalf("drained %d packets, want 5", len(drained))
	}
	if drained[0].Kind != KindAck || drained[1].Kind != KindAck {
		t.Fatalf("control traffic must drain first, got %q then %q", drained[0].Kind, drained[1].Kind)
	}
	// FIFO inside the control class.
	if drained[0].AckFor != "x1" || drained[1].AckFor != "x2" {
		t.Fatalf("control class lost FIFO order: %q then %q", drained[0].AckFor, drained[1].AckFor)
	}
	if drained[2].Kind != KindGroupMessage {
		t.Fatalf("text must drain before voice, got %q", drained[2].Kind)
	}
	if drained[3].Kind != KindVoiceMessage {
		t.Fatalf("voice must drain before media, got %q", drained[3].Kind)
	}
	if drained[4].Kind != PacketKind("media") {
		t.Fatalf("media must drain last, got %q", drained[4].Kind)
	}
}

// TestPriorityEvictionProtectsControl verifies that under pressure the buffer
// drops the lowest-priority class first and never evicts control traffic.
func TestPriorityEvictionProtectsControl(t *testing.T) {
	// Room for roughly four small packets.
	q := NewPriorityQueue(4, 1<<20, time.Hour)
	ack := NewPacket(KindAck, "src", "dst", 8)
	ack.AckFor = "keep"
	if err := q.Enqueue(ack); err != nil {
		t.Fatalf("enqueue control: %v", err)
	}
	// Fill the rest with bulk media, then push more media: the media must be
	// evicted, never the acknowledgement.
	for i := 0; i < 3; i++ {
		m := NewPacket(KindMessage, "src", "dst", 8)
		m.Kind = "media"
		if err := q.Enqueue(m); err != nil {
			t.Fatalf("enqueue media %d: %v", i, err)
		}
	}
	for i := 0; i < 20; i++ {
		m := NewPacket(KindMessage, "src", "dst", 8)
		m.Kind = "media"
		if err := q.Enqueue(m); err != nil {
			t.Fatalf("enqueue media %d: %v", i, err)
		}
	}
	if q.Len() > 4 {
		t.Fatalf("buffer exceeded its packet bound: %d", q.Len())
	}
	found := false
	for _, p := range q.Peek() {
		if p.Kind == KindAck && p.AckFor == "keep" {
			found = true
		}
	}
	if !found {
		t.Fatal("control traffic was evicted to make room for bulk media")
	}
	if q.Dropped() == 0 {
		t.Fatal("expected media to be dropped under pressure")
	}
}

// TestPriorityByteBound verifies the buffer is bounded by bytes, not only by
// packet count, so a few large packets cannot exhaust memory.
func TestPriorityByteBound(t *testing.T) {
	q := NewPriorityQueue(1000, 4096, time.Hour)
	big := make([]byte, 3000)
	// Three 3 KiB packets exceed the 4 KiB byte budget.
	for i := 0; i < 3; i++ {
		p := NewPacket(KindMessage, "src", "dst", 8)
		p.Payload = append([]byte(nil), big...)
		_ = q.Enqueue(p)
	}
	if q.Bytes() > 4096 {
		t.Fatalf("buffer holds %d bytes, over the 4096 byte bound", q.Bytes())
	}
	if q.Len() >= 3 {
		t.Fatalf("expected byte pressure to evict, buffer holds %d packets", q.Len())
	}
	// A control packet is still admitted and never evicted.
	ack := NewPacket(KindAck, "src", "dst", 8)
	ack.AckFor = "x"
	if err := q.Enqueue(ack); err != nil {
		t.Fatalf("control packet refused under byte pressure: %v", err)
	}
}

// TestPriorityDepthByClass verifies the per-class depth report used by status
// output.
func TestPriorityDepthByClass(t *testing.T) {
	q := NewPriorityQueue(100, 1<<20, time.Hour)
	q.Enqueue(NewPacket(KindMessage, "s", "d", 8))
	q.Enqueue(NewPacket(KindMessage, "s", "d", 8))
	ack := NewPacket(KindAck, "s", "d", 8)
	ack.AckFor = "x"
	q.Enqueue(ack)

	depth := q.DepthByPriority()
	if depth[PriorityText.String()] != 2 {
		t.Fatalf("text depth = %d, want 2", depth[PriorityText.String()])
	}
	if depth[PriorityControl.String()] != 1 {
		t.Fatalf("control depth = %d, want 1", depth[PriorityControl.String()])
	}
	if depth[PriorityMedia.String()] != 0 {
		t.Fatalf("media depth = %d, want 0", depth[PriorityMedia.String()])
	}
}

// TestPriorityForKind verifies the traffic-class mapping.
func TestPriorityForKind(t *testing.T) {
	cases := []struct {
		kind PacketKind
		want Priority
	}{
		{KindAck, PriorityControl},
		{KindCallSignal, PriorityControl},
		{KindMessage, PriorityText},
		{KindGroupMessage, PriorityText},
		{KindVoiceMessage, PriorityVoice},
		{PacketKind("media"), PriorityMedia},
	}
	for _, c := range cases {
		if got := PriorityForKind(c.kind); got != c.want {
			t.Fatalf("PriorityForKind(%q) = %v, want %v", c.kind, got, c.want)
		}
	}
}

// TestQueueBackpressureRefusal verifies Enqueue refuses a packet when the
// buffer is full of traffic at least as important, rather than evicting
// something more important.
func TestQueueBackpressureRefusal(t *testing.T) {
	q := NewPriorityQueue(2, 1<<20, time.Hour)
	for i := 0; i < 2; i++ {
		ack := NewPacket(KindAck, "s", "d", 8)
		ack.AckFor = "x"
		if err := q.Enqueue(ack); err != nil {
			t.Fatalf("enqueue control %d: %v", i, err)
		}
	}
	// A third control packet cannot displace the two already held.
	ack := NewPacket(KindAck, "s", "d", 8)
	ack.AckFor = "x"
	if err := q.Enqueue(ack); err == nil {
		t.Fatal("expected the buffer to refuse rather than evict control traffic")
	}
	if q.Len() != 2 {
		t.Fatalf("buffer holds %d packets, want 2", q.Len())
	}
}

// TestReliableDeliveryOverRealTransport is the end-to-end assertion: two real
// nodes on real sockets exchange a payload, the destination delivers it exactly
// once and returns an acknowledgement, and the sender's transfer settles to
// acked. It also asserts the destination's retried copies do not re-deliver.
func TestReliableDeliveryOverRealTransport(t *testing.T) {
	shared, _ := NewIdentityKey()

	delivered := make(chan []byte, 4)
	peer, err := NewUDPTransport(0, nil)
	if err != nil {
		t.Fatalf("peer transport: %v", err)
	}
	defer peer.Close()
	peerNode := NewNode(NodeConfig{
		DeviceID:  "peer-device",
		Key:       &shared,
		Transport: peer,
		Handler: func(_ *Packet, plaintext []byte) {
			delivered <- append([]byte(nil), plaintext...)
		},
	})

	sender, err := NewUDPTransport(0, nil)
	if err != nil {
		t.Fatalf("sender transport: %v", err)
	}
	defer sender.Close()
	senderNode := NewNode(NodeConfig{DeviceID: "sender-device", Key: &shared, Transport: sender})

	// Both sides learn each other's address, as discovery beacons would do.
	senderNode.routes.Upsert(&Beacon{DeviceID: "peer-device", Kind: "member", Addr: peer.Addr()})
	peerNode.routes.Upsert(&Beacon{DeviceID: "sender-device", Kind: "member", Addr: sender.Addr()})

	id, err := senderNode.SendReliable(KindMessage, "peer-device", []byte("reliable hello"))
	if err != nil {
		t.Fatalf("SendReliable: %v", err)
	}

	select {
	case got := <-delivered:
		if string(got) != "reliable hello" {
			t.Fatalf("peer delivered %q", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for reliable delivery")
	}

	// The acknowledgement travels back on the same socket pair; wait for the
	// sender's transfer to settle.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		view, ok := senderNode.Transfer(id)
		if ok && view.State == StateAcked {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	view, ok := senderNode.Transfer(id)
	if !ok || view.State != StateAcked {
		t.Fatalf("transfer state = %+v, want acked", view)
	}
	if view.Attempts != 1 {
		t.Fatalf("attempts = %d, want 1 (no retry needed)", view.Attempts)
	}

	// A retransmitted copy must not be re-delivered to the application.
	pt := []byte("reliable hello")
	key := senderNode.sessionKeyFor("peer-device")
	ct, nonce, err := Encrypt(key, pt)
	if err != nil {
		t.Fatalf("re-encrypt: %v", err)
	}
	dup := NewPacket(KindMessage, "sender-device", "peer-device", 8)
	dup.Payload, dup.Nonce, dup.Xfer = ct, nonce, id
	dup.Seq = senderNode.nextSeq()
	senderNode.routes.Seen(dup.ID)
	if err := senderNode.pfifo.Enqueue(dup); err != nil {
		t.Fatalf("enqueue duplicate: %v", err)
	}
	senderNode.flush()

	select {
	case got := <-delivered:
		t.Fatalf("duplicate was re-delivered to the application: %q", got)
	case <-time.After(700 * time.Millisecond):
		// Correct: exactly-once delivery held.
	}
}

// TestSendReliableRejectsOversize verifies the reliable path refuses a payload
// it cannot retain for retry, directing the caller to the fragmentation path.
func TestSendReliableRejectsOversize(t *testing.T) {
	key, _ := NewIdentityKey()
	n := NewNode(NodeConfig{DeviceID: "dev", Key: &key})
	if _, err := n.SendReliable(KindMessage, "peer", make([]byte, maxReliablePayload+1)); err != ErrTransferTooLarge {
		t.Fatalf("err = %v, want ErrTransferTooLarge", err)
	}
	if _, err := n.SendReliable(KindMessage, "", []byte("x")); err == nil {
		t.Fatal("expected an error for a missing destination")
	}
	if _, err := n.SendReliable(KindAck, "peer", []byte("x")); err == nil {
		t.Fatal("expected an error for a caller-supplied acknowledgement")
	}
}

// TestNodeReliabilityCounters verifies the counters the status endpoint
// reports.
func TestNodeReliabilityCounters(t *testing.T) {
	key, _ := NewIdentityKey()
	n := NewNode(NodeConfig{DeviceID: "dev", Key: &key})
	before := n.PendingTransfers()
	if _, err := n.SendReliable(KindMessage, "peer", []byte("count me")); err != nil {
		t.Fatalf("SendReliable: %v", err)
	}
	if got := n.PendingTransfers(); got != before+1 {
		t.Fatalf("pending = %d, want %d", got, before+1)
	}
	counts := n.DeliveryCounts()
	if counts[string(StateQueued)]+counts[string(StateRelaying)] != 1 {
		t.Fatalf("delivery counts = %+v, want one live transfer", counts)
	}
	status := n.QueueStatus()
	for _, k := range []string{"packets", "bytes", "dropped", "depth_by_class", "pending_transfer", "delivery_counts"} {
		if _, ok := status[k]; !ok {
			t.Fatalf("queue status missing %q", k)
		}
	}
}

// TestRetryPrefersAlternatePath verifies the alternate-path requirement: the
// hop that failed to produce an acknowledgement is tried last on retry, while
// the remaining neighbours are still flooded.
func TestRetryPrefersAlternatePath(t *testing.T) {
	key, _ := NewIdentityKey()
	shared := key

	type sent struct {
		addr string
	}
	var mu sync.Mutex
	var sentTo []string
	tr := &recordingTransport{onSend: func(addr string) {
		mu.Lock()
		sentTo = append(sentTo, addr)
		mu.Unlock()
	}}
	node := NewNode(NodeConfig{DeviceID: "dev-a", Key: &shared, Transport: tr})

	// Two relay-consenting neighbours and a destination that is not directly
	// reachable, so the packet is flooded through the relays.
	node.routes.Upsert(&Beacon{DeviceID: "relay-1", Kind: "relay", Addr: "10.0.0.1:1"})
	node.routes.Upsert(&Beacon{DeviceID: "relay-2", Kind: "relay", Addr: "10.0.0.2:1"})

	id, err := node.SendReliable(KindMessage, "far-destination", []byte("route around the bad hop"))
	if err != nil {
		t.Fatalf("SendReliable: %v", err)
	}

	mu.Lock()
	first := append([]string(nil), sentTo...)
	sentTo = nil
	mu.Unlock()
	if len(first) < 2 {
		t.Fatalf("expected the first attempt to flood both relays, got %v", first)
	}
	// The first attempt recorded a hop; drive attempts forward so the retry
	// sees Attempts > 1 and avoids that hop.
	view, _ := node.Transfer(id)
	if view.LastHop == "" {
		t.Fatalf("first attempt recorded no hop: %+v", view)
	}
	expectedLast := addrFor(view.LastHop, node)

	// Force the retry immediately.
	node.transfers.mu.Lock()
	if x, ok := node.transfers.active[id]; ok {
		x.NextAttempt = time.Unix(0, 0)
	}
	node.transfers.mu.Unlock()
	if n := node.Tick(); n == 0 {
		t.Fatal("expected the retry to transmit")
	}

	mu.Lock()
	second := append([]string(nil), sentTo...)
	mu.Unlock()
	if len(second) != 2 {
		t.Fatalf("retry weakened the fan-out: sent to %v", second)
	}
	// The hop that failed to produce an acknowledgement is tried LAST, so the
	// first transmission on the retry leads with a different path.
	if second[len(second)-1] != expectedLast {
		t.Fatalf("retry did not defer the failed hop: order %v, want %q last", second, expectedLast)
	}
	if second[0] == expectedLast {
		t.Fatalf("retry led with the already-failed hop: %v", second)
	}
}

// addrFor resolves a device id to its recorded address.
func addrFor(deviceID string, n *Node) string {
	for _, nb := range n.routes.Neighbors() {
		if nb.DeviceID == deviceID {
			return nb.Addr
		}
	}
	return ""
}

// recordingTransport is a Transport that records addresses instead of sending,
// so routing decisions can be asserted without a real radio.
type recordingTransport struct {
	mu     sync.Mutex
	onSend func(addr string)
}

func (t *recordingTransport) Send(addr string, _ []byte) error {
	t.mu.Lock()
	fn := t.onSend
	t.mu.Unlock()
	if fn != nil {
		fn(addr)
	}
	return nil
}

func (t *recordingTransport) Addr() string { return "127.0.0.1:0" }
func (t *recordingTransport) Close() error { return nil }
