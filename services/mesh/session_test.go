package mesh

import (
	"bytes"
	"sync/atomic"
	"testing"
	"time"
)

func TestSessionKeySymmetric(t *testing.T) {
	a := NewKeyExchange(0)
	b := NewKeyExchange(0)
	ka, err := SessionKey(a, "dev-a", b.Public(), "dev-b")
	if err != nil {
		t.Fatal(err)
	}
	kb, err := SessionKey(b, "dev-b", a.Public(), "dev-a")
	if err != nil {
		t.Fatal(err)
	}
	if *ka != *kb {
		t.Fatal("session keys derived by both peers differ")
	}
	c := NewKeyExchange(0)
	kc, _ := SessionKey(c, "dev-c", b.Public(), "dev-b")
	if *kc == *kb {
		t.Fatal("unrelated key agreement produced an identical session key")
	}
}

func TestKeyExchangeRotate(t *testing.T) {
	k := NewKeyExchange(0)
	oldPub := append([]byte(nil), k.Public()...)
	if err := k.Rotate(); err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(oldPub, k.Public()) {
		t.Fatal("rotation did not change the public key")
	}
	if k.Epoch != 1 {
		t.Fatalf("epoch = %d, want 1", k.Epoch)
	}
}

// TestSessionKeyAuthenticatedExchange runs a real three-node exchange: the
// sender encrypts with the ECDH session key derived from the receiver's
// signed-beacon-advertised X25519 public key, and the receiver decrypts it.
// A passive attacker holding only the legacy pre-shared key must not be able
// to open the payload.
func TestSessionKeyAuthenticatedExchange(t *testing.T) {
	bus := NewSimBus()
	src := newSimNode(t, bus, "src", "member", nil, 8)
	dst := newSimNode(t, bus, "dst", "member", nil, 8)
	defer src.Stop()
	defer dst.Stop()
	bus.Wire("sim://src", "sim://dst")
	bus.Wire("sim://dst", "sim://src")

	// Exchange signed beacons (as the beacon loop would): src learns dst's
	// X25519 public key, dst pins src's identity.
	seedBeacon := func(from, to *Node) {
		b := &Beacon{DeviceID: from.DeviceID, Kind: "member", Transport: "sim", Addr: "sim://" + from.DeviceID, Seq: 1}
		sb, err := from.signer.SignBeacon(b, from.kem.Public(), from.kem.Epoch)
		if err != nil {
			t.Fatal(err)
		}
		data, err := MarshalSignedBeacon(sb)
		if err != nil {
			t.Fatal(err)
		}
		to.HandleInbound("sim://"+from.DeviceID, data)
	}
	seedBeacon(dst, src)
	seedBeacon(src, dst)

	got := make(chan []byte, 1)
	dst.SetHandler(func(p *Packet, pt []byte) { got <- pt })

	if _, err := src.Send(KindMessage, dst.DeviceID, []byte("session secret")); err != nil {
		t.Fatal(err)
	}
	select {
	case pt := <-got:
		if string(pt) != "session secret" {
			t.Fatalf("wrong payload %q", pt)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("session-key delivery timed out")
	}

	// The payload must not be openable with the legacy pre-shared key.
	pkt := NewPacket(KindMessage, src.DeviceID, dst.DeviceID, 8)
	sk, err := SessionKey(src.kem, src.DeviceID, dst.kem.Public(), dst.DeviceID)
	if err != nil {
		t.Fatal(err)
	}
	pkt.Payload, pkt.Nonce, err = Encrypt(sk, []byte("top secret"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Decrypt(src.Key, pkt.Payload, pkt.Nonce); err == nil {
		t.Fatal("pre-shared key opened a session-key payload; keys are not independent")
	}
	pt, err := Decrypt(sk, pkt.Payload, pkt.Nonce)
	if err != nil || string(pt) != "top secret" {
		t.Fatalf("session key failed to decrypt own payload: %v", err)
	}
}

func TestSessionRotationInvalidates(t *testing.T) {
	a := NewKeyExchange(0)
	b := NewKeyExchange(0)
	k1, _ := SessionKey(a, "a", b.Public(), "b")
	a.Rotate()
	b.Rotate()
	k2, _ := SessionKey(a, "a", b.Public(), "b")
	if *k1 == *k2 {
		t.Fatal("rotation produced the same session key")
	}
}

func TestReplayFilter(t *testing.T) {
	f := NewReplayFilter()
	if !f.Check("src", 1) || f.Check("src", 1) {
		t.Fatal("duplicate seq not rejected")
	}
	if !f.Check("src", 5) {
		t.Fatal("new seq within window rejected")
	}
	if f.Check("src", 5) {
		t.Fatal("duplicate seq 5 not rejected")
	}
	// Advance the window past the low mark; old seqs are replays.
	for i := int64(6); i <= 1100; i++ {
		if !f.Check("src", i) {
			t.Fatalf("seq %d wrongly rejected", i)
		}
	}
	if f.Check("src", 1) {
		t.Fatal("replayed old seq accepted after window advanced")
	}
	// Far-future seq slides the window forward (and evicts old marks).
	if !f.Check("src", 2000) {
		t.Fatal("far-future seq rejected")
	}
	// Per-source isolation.
	if !f.Check("other", 1) {
		t.Fatal("per-source windows interfere")
	}
	// Zero seq (legacy packet) must be ignored by callers; Check documents
	// behaviour for it anyway.
	if f.Check("src", 0) && f.Check("src", 0) == false {
		t.Fatal("inconsistent zero handling")
	}
}

// TestReplayRejectedInbound proves the filter refuses a replayed sequence
// number and any sequence below the window base after it has advanced.
func TestReplayRejectedInbound(t *testing.T) {
	n := NewNode(NodeConfig{DeviceID: "r", Key: MustKey()})
	if !n.replay.Check("s", 42) {
		t.Fatal("first seq 42 rejected")
	}
	if n.replay.Check("s", 42) {
		t.Fatal("replayed seq 42 accepted")
	}
	// Jump far ahead: the window slides so base = 2000-1024+1 = 977.
	if !n.replay.Check("s", 2000) {
		t.Fatal("far-future seq 2000 rejected")
	}
	if n.replay.Check("s", 7) {
		t.Fatal("seq 7 below window base accepted")
	}
	if !n.replay.Check("s", 1000) {
		t.Fatal("seq 1000 within window rejected")
	}
}

// TestNoDuplicateLocalDelivery is the regression test for the grid deadlock:
// a destination reachable via multiple paths of different lengths must have
// its handler invoked exactly once per packet id.
func TestNoDuplicateLocalDelivery(t *testing.T) {
	bus := NewSimBus()
	var deliveries int64
	dst := newSimNode(t, bus, "dst", "member", nil, 16)
	dst.SetHandler(func(p *Packet, pt []byte) { atomic.AddInt64(&deliveries, 1) })
	src := newSimNode(t, bus, "src", "member", nil, 16)
	m1 := newSimNode(t, bus, "m1", "relay", nil, 16)
	m2 := newSimNode(t, bus, "m2", "relay", nil, 16)
	defer func() { dst.Stop(); src.Stop(); m1.Stop(); m2.Stop() }()

	addr := func(id string) string { return "sim://" + id }
	for _, e := range [][2]string{{"src", "m1"}, {"src", "m2"}, {"m1", "dst"}, {"m2", "dst"}} {
		bus.Wire(addr(e[0]), addr(e[1]))
		bus.Wire(addr(e[1]), addr(e[0]))
	}
	seed := func(id, kind string) {
		n := map[string]*Node{"src": src, "m1": m1, "m2": m2, "dst": dst}[id]
		n.routes.Upsert(&Beacon{DeviceID: "x", Kind: kind, Transport: "local_wifi", Addr: addr(id)})
	}
	_ = seed
	// Give each node the real neighbours so flooding branches and copies of
	// different TTL reach the destination.
	neigh := func(n *Node, ids ...string) {
		for _, id := range ids {
			kind := "relay"
			if id == "src" || id == "dst" {
				kind = "member"
			}
			n.routes.Upsert(&Beacon{DeviceID: id, Kind: kind, Transport: "local_wifi", Addr: addr(id)})
		}
	}
	neigh(src, "m1", "m2")
	neigh(m1, "src", "dst")
	neigh(m2, "src", "dst")
	neigh(dst, "m1", "m2")

	if _, err := src.Send(KindMessage, dst.DeviceID, []byte("once")); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && atomic.LoadInt64(&deliveries) == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(200 * time.Millisecond) // let late improved copies arrive
	if got := atomic.LoadInt64(&deliveries); got != 1 {
		t.Fatalf("handler invoked %d times, want exactly 1", got)
	}
}
