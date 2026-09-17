package mesh

import (
	"testing"
	"time"
)

// --- groupkey.go ---

func TestGroupKeyManagerCreatesAndRotates(t *testing.T) {
	m := NewGroupKeyManager(0)
	k1, err := m.KeyFor("g1")
	if err != nil {
		t.Fatal(err)
	}
	if k1.Epoch != 1 {
		t.Fatalf("first key epoch = %d, want 1", k1.Epoch)
	}
	// Same group, same epoch -> same key.
	k2, err := m.KeyFor("g1")
	if err != nil {
		t.Fatal(err)
	}
	if k2.Epoch != k1.Epoch || k2.Key != k1.Key {
		t.Fatal("same group returned a different key before rotation")
	}
	// Forced rotation bumps the epoch and changes the key.
	k3, err := m.RotateGroup("g1")
	if err != nil {
		t.Fatal(err)
	}
	if k3.Epoch != k1.Epoch+1 {
		t.Fatalf("rotated epoch = %d, want %d", k3.Epoch, k1.Epoch+1)
	}
	if k3.Key == k1.Key {
		t.Fatal("rotation did not change the key")
	}
}

func TestGroupKeyManagerAdoptNewerEpoch(t *testing.T) {
	m := NewGroupKeyManager(0)
	k1, _ := m.KeyFor("g1")
	// Adopting an older/equal epoch is ignored.
	if err := m.AdoptKey("g1", k1); err != nil {
		t.Fatal(err)
	}
	if m.Epoch("g1") != k1.Epoch {
		t.Fatal("equal epoch was not ignored")
	}
	// Adopting a newer epoch replaces the key.
	var newerKey IdentityKey
	copy(newerKey[:], MustKey()[:])
	newer := GroupKey{Key: newerKey, Epoch: k1.Epoch + 5}
	if err := m.AdoptKey("g1", newer); err != nil {
		t.Fatal(err)
	}
	if m.Epoch("g1") != newer.Epoch {
		t.Fatal("newer epoch was not adopted")
	}
}

func TestGroupKeyManagerScheduledRotation(t *testing.T) {
	m := NewGroupKeyManager(time.Hour)
	now := time.Unix(1000, 0)
	m.SetNow(func() time.Time { return now })
	k1, _ := m.KeyFor("g1")
	// Advance past the rotation interval.
	now = now.Add(2 * time.Hour)
	k2, _ := m.KeyFor("g1")
	if k2.Epoch != k1.Epoch+1 {
		t.Fatalf("scheduled rotation did not bump epoch: %d -> %d", k1.Epoch, k2.Epoch)
	}
}

// --- groupack.go ---

func TestGroupAckTrackerAggregates(t *testing.T) {
	tr := NewGroupAckTracker()
	g := tr.Create("xfer-1", "g1", []string{"a", "b", "c"})
	if g.State != GroupAckPending {
		t.Fatalf("initial state = %s, want pending", g.State)
	}
	if !tr.Ack("xfer-1", "a") {
		t.Fatal("first ack did not report a change")
	}
	got, _ := tr.Get("xfer-1")
	if got.State != GroupAckPartial {
		t.Fatalf("state after 1/3 = %s, want partial", got.State)
	}
	tr.Ack("xfer-1", "b")
	tr.Ack("xfer-1", "c")
	got, _ = tr.Get("xfer-1")
	if got.State != GroupAckComplete {
		t.Fatalf("state after 3/3 = %s, want complete", got.State)
	}
	// Duplicate ack does not change state.
	if tr.Ack("xfer-1", "a") {
		t.Fatal("duplicate ack reported a change")
	}
}

func TestGroupAckTrackerUnknown(t *testing.T) {
	tr := NewGroupAckTracker()
	if tr.Ack("nope", "a") {
		t.Fatal("ack for unknown transfer reported a change")
	}
	if _, ok := tr.Get("nope"); ok {
		t.Fatal("unknown transfer was found")
	}
}

// --- routerepair.go ---

func TestRepairCandidatesExcludesFailed(t *testing.T) {
	rt := NewRouteTable()
	rt.Upsert(&Beacon{DeviceID: "r1", Kind: "relay", Addr: "a:1", Transport: "local_wifi"})
	rt.Upsert(&Beacon{DeviceID: "r2", Kind: "relay", Addr: "b:1", Transport: "local_wifi"})
	rt.Upsert(&Beacon{DeviceID: "r3", Kind: "relay", Addr: "c:1", Transport: "local_wifi"})
	cands := rt.repairCandidates("dst", "r1", 2)
	if len(cands) != 2 {
		t.Fatalf("repair candidates = %d, want 2", len(cands))
	}
	for _, c := range cands {
		if c.DeviceID == "r1" {
			t.Fatal("repair candidates included the failed neighbour")
		}
	}
}

// --- multipath.go ---

func TestSelectMultipathPrefersDiverseTransports(t *testing.T) {
	rt := NewRouteTable()
	rt.Upsert(&Beacon{DeviceID: "dst", Kind: "member", Addr: "d:1", Transport: "wifi_direct"})
	rt.Upsert(&Beacon{DeviceID: "r1", Kind: "relay", Addr: "a:1", Transport: "local_wifi"})
	rt.Upsert(&Beacon{DeviceID: "r2", Kind: "relay", Addr: "b:1", Transport: "bluetooth"})
	rt.Upsert(&Beacon{DeviceID: "r3", Kind: "relay", Addr: "c:1", Transport: "wifi_direct"})
	cands := rt.selectMultipath("dst", 3)
	if len(cands) != 3 {
		t.Fatalf("multipath candidates = %d, want 3", len(cands))
	}
	// The direct destination must be first.
	if cands[0].DeviceID != "dst" {
		t.Fatalf("direct destination not first: %s", cands[0].DeviceID)
	}
	// The three candidates should be on three different transports.
	transports := map[string]bool{}
	for _, c := range cands {
		transports[c.Transport] = true
	}
	if len(transports) != 3 {
		t.Fatalf("expected 3 distinct transports, got %v", transports)
	}
}

// --- revocdist.go ---

func TestRevocationSignVerify(t *testing.T) {
	sk, err := NewSigningKey()
	if err != nil {
		t.Fatal(err)
	}
	known := map[string][]byte{
		"revoker": sk.Public,
		"bad":     MustKey()[:], // placeholder, replaced below
	}
	// Build a real pinned key for the revoked device.
	revokedKey := MustKey()[:]
	known["bad"] = revokedKey[:]

	r, err := SignRevocation(sk, "revoker", "bad", revokedKey[:])
	if err != nil {
		t.Fatal(err)
	}
	verified, err := VerifyRevocation(r, known)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if verified.DeviceID != "bad" {
		t.Fatal("wrong device id")
	}
}

func TestRevocationRejectsMismatchedKey(t *testing.T) {
	sk, _ := NewSigningKey()
	known := map[string][]byte{"revoker": sk.Public}
	revokedKey := MustKey()[:]
	known["bad"] = revokedKey[:]
	// Sign a notice naming a DIFFERENT key than the pinned one.
	r, err := SignRevocation(sk, "revoker", "bad", MustKey()[:])
	if err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyRevocation(r, known); err == nil {
		t.Fatal("revocation naming a mismatched key was accepted")
	}
}

func TestRevocationRejectsUnknownRevoker(t *testing.T) {
	sk, _ := NewSigningKey()
	known := map[string][]byte{} // revoker not pinned
	revokedKey := MustKey()[:]
	r, err := SignRevocation(sk, "stranger", "bad", revokedKey[:])
	if err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyRevocation(r, known); err == nil {
		t.Fatal("revocation from an unknown revoker was accepted")
	}
}

func TestRevocationStoreAppliesOnce(t *testing.T) {
	s := newRevocationStore()
	r := &RevocationNotice{Revoker: "r", DeviceID: "d", PublicKey: []byte("k"), IssuedAt: 1}
	if !s.Apply(r) {
		t.Fatal("first apply did not report a change")
	}
	if s.Apply(r) {
		t.Fatal("second apply reported a change")
	}
	if !s.IsRevoked("d") {
		t.Fatal("device not marked revoked")
	}
}

// --- end-to-end: group key + group ack + revocation distribution ---

func TestGroupMessageUsesPerGroupKey(t *testing.T) {
	// Two nodes in the same group must be able to exchange a group message
	// sealed under the per-group key. The group key is distributed to members
	// (here, B adopts A's key, mirroring a group-key advertisement).
	bus := NewSimBus()
	a := newSimNode(t, bus, "a", "member", nil, 8)
	b := newSimNode(t, bus, "b", "member", nil, 8)
	defer a.Stop()
	defer b.Stop()
	bus.Wire("sim://a", "sim://b")
	bus.Wire("sim://b", "sim://a")

	got := make(chan []byte, 1)
	b.SetHandler(func(p *Packet, pt []byte) { got <- pt })

	// Seed beacons so the group message can route.
	seed := func(from, to *Node) {
		beacon := &Beacon{DeviceID: from.DeviceID, Kind: "member", Transport: "sim", Addr: "sim://" + from.DeviceID}
		sb, _ := from.signer.SignBeacon(beacon, from.kem.Public(), from.kem.Epoch)
		data, _ := MarshalSignedBeacon(sb)
		to.HandleInbound("sim://"+from.DeviceID, data)
	}
	seed(a, b)
	seed(b, a)

	// Distribute the group key: A's key for g1 is adopted by B.
	gk, err := a.groupKeys.KeyFor("g1")
	if err != nil {
		t.Fatal(err)
	}
	if err := b.groupKeys.AdoptKey("g1", gk); err != nil {
		t.Fatal(err)
	}

	if _, err := a.SendGroup(KindGroupMessage, "g1", []byte("hello group")); err != nil {
		t.Fatal(err)
	}
	select {
	case pt := <-got:
		if string(pt) != "hello group" {
			t.Fatalf("wrong group payload %q", pt)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("group message delivery timed out")
	}
}

func TestRevocationDistributionEndToEnd(t *testing.T) {
	// A revokes B and floods the notice; C (which pinned B) applies it.
	key := MustKey()
	a := NewNode(NodeConfig{DeviceID: "a", Key: key})
	b := NewNode(NodeConfig{DeviceID: "b", Key: key})
	c := NewNode(NodeConfig{DeviceID: "c", Key: key})

	// C pins B via a signed beacon.
	beacon := &Beacon{DeviceID: b.DeviceID, Kind: "member", Transport: "sim", Addr: "sim://" + b.DeviceID}
	sb, _ := b.signer.SignBeacon(beacon, b.kem.Public(), b.kem.Epoch)
	wire, _ := MarshalSignedBeacon(sb)
	c.HandleInbound("sim://"+b.DeviceID, wire)
	if len(c.Routes().Neighbors()) != 1 {
		t.Fatal("C did not pin B")
	}

	// A must also pin B (it can only revoke an identity it has authenticated).
	a.HandleInbound("sim://"+b.DeviceID, wire)
	if len(a.Routes().Neighbors()) != 1 {
		t.Fatal("A did not pin B")
	}

	// C must pin the revoker A so it can verify A's revocation notice.
	aBeacon := &Beacon{DeviceID: a.DeviceID, Kind: "member", Transport: "sim", Addr: "sim://" + a.DeviceID}
	aSB, _ := a.signer.SignBeacon(aBeacon, a.kem.Public(), a.kem.Epoch)
	aWire, _ := MarshalSignedBeacon(aSB)
	c.HandleInbound("sim://"+a.DeviceID, aWire)

	// A issues a revocation for B and floods it to C.
	notice, err := a.BroadcastRevocation(b.DeviceID, b.signer.Public)
	if err != nil {
		t.Fatal(err)
	}
	// Deliver the notice to C directly (simulating the flood).
	c.HandleInbound("sim://"+a.DeviceID, notice)
	if !c.IsPeerRevoked(b.DeviceID) {
		t.Fatal("C did not apply the network-wide revocation")
	}
	// B must be gone from C's route table (A, the revoker, remains).
	for _, nb := range c.Routes().Neighbors() {
		if nb.DeviceID == b.DeviceID {
			t.Fatal("C still routes to the revoked peer")
		}
	}
	// A revoked B's beacon must now be rejected by C.
	c.HandleInbound("sim://"+b.DeviceID, wire)
	for _, nb := range c.Routes().Neighbors() {
		if nb.DeviceID == b.DeviceID {
			t.Fatal("revoked peer re-entered through a signed beacon")
		}
	}
}

func TestGroupAckEndToEnd(t *testing.T) {
	// A sends a group message to B; B returns a per-member group ACK that A
	// can attribute.
	bus := NewSimBus()
	a := newSimNode(t, bus, "a", "member", nil, 8)
	b := newSimNode(t, bus, "b", "member", nil, 8)
	defer a.Stop()
	defer b.Stop()
	bus.Wire("sim://a", "sim://b")
	bus.Wire("sim://b", "sim://a")

	got := make(chan []byte, 1)
	b.SetHandler(func(p *Packet, pt []byte) { got <- pt })

	seed := func(from, to *Node) {
		beacon := &Beacon{DeviceID: from.DeviceID, Kind: "member", Transport: "sim", Addr: "sim://" + from.DeviceID}
		sb, _ := from.signer.SignBeacon(beacon, from.kem.Public(), from.kem.Epoch)
		data, _ := MarshalSignedBeacon(sb)
		to.HandleInbound("sim://"+from.DeviceID, data)
	}
	seed(a, b)
	seed(b, a)

	// Distribute the group key to B so it can decrypt the group message.
	gk, err := a.groupKeys.KeyFor("g1")
	if err != nil {
		t.Fatal(err)
	}
	if err := b.groupKeys.AdoptKey("g1", gk); err != nil {
		t.Fatal(err)
	}

	// Register the member list so the sender-side tracker can aggregate
	// per-member acknowledgements (see NoteGroupMembers).
	a.NoteGroupMembers("g1", []string{"b"})
	if _, err := a.SendGroup(KindGroupMessage, "g1", []byte("ack me")); err != nil {
		t.Fatal(err)
	}
	select {
	case <-got:
	case <-time.After(5 * time.Second):
		t.Fatal("group message delivery timed out")
	}
	// Give the group ACK a moment to return to A.
	time.Sleep(200 * time.Millisecond)
	// The returned group ACK must have settled the sender-side per-member
	// tracker: the only known member (b) acknowledged, so the aggregate
	// state is complete.
	snap := a.GroupTransfers()
	if len(snap) != 1 {
		t.Fatalf("expected 1 tracked group transfer, got %d", len(snap))
	}
	if snap[0].State != GroupAckComplete {
		t.Fatalf("group ack did not settle: state=%v acked=%v", snap[0].State, snap[0].Acked)
	}
	if !snap[0].Acked["b"] {
		t.Fatal("member b was not credited with the acknowledgement")
	}
}
