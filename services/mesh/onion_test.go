package mesh

import (
	"bytes"
	"testing"
)

func linkKEM(a, b *Node) {
	a.peerKEM[b.DeviceID] = AdvertisedKEM{Public: b.kem.Public(), Epoch: b.kem.Epoch}
}

func TestOnionEnvelopePeelsOneAuthenticatedHopAtATime(t *testing.T) {
	ka, kb, kc := MustKey(), MustKey(), MustKey()
	a := NewNode(NodeConfig{DeviceID: "onion-a", Key: ka})
	b := NewNode(NodeConfig{DeviceID: "onion-b", Key: kb})
	c := NewNode(NodeConfig{DeviceID: "onion-c", Key: kc})
	linkKEM(a, b)
	linkKEM(a, c)
	linkKEM(b, a)
	linkKEM(b, c)
	linkKEM(c, b)
	linkKEM(c, a)

	plain := []byte("layered secret")
	payload, nonce, err := Encrypt(a.sessionKeyFor(c.DeviceID), plain)
	if err != nil {
		t.Fatal(err)
	}
	onion, err := a.buildOnion(KindMessage, a.DeviceID, []string{a.DeviceID, b.DeviceID, c.DeviceID}, payload, nonce, "")
	if err != nil {
		t.Fatal(err)
	}
	p := NewPacket(KindMessage, a.DeviceID, b.DeviceID, 4)
	p.HopSrc = a.DeviceID
	p.Onion = onion

	// The outer layer is for B and must not expose C before authenticated peel.
	if bytes.Contains(p.Onion, []byte(c.DeviceID)) {
		t.Fatal("outer onion layer exposed the final destination")
	}
	if !b.peelOnion(p) {
		t.Fatal("first onion layer did not authenticate")
	}
	if p.Dst != c.DeviceID || p.HopSrc != b.DeviceID || p.OnionFinal {
		t.Fatalf("unexpected relay state: dst=%q hop_src=%q final=%v", p.Dst, p.HopSrc, p.OnionFinal)
	}
	if !c.peelOnion(p) {
		t.Fatal("final onion layer did not authenticate")
	}
	got, err := c.decryptPayload(p)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, plain) || !p.OnionFinal || p.Dst != c.DeviceID {
		t.Fatalf("destination did not recover plaintext: got=%q final=%v dst=%q", got, p.OnionFinal, p.Dst)
	}

	// Tampering with a layer must fail closed.
	p2 := *p
	p2.Onion = append([]byte(nil), onion...)
	p2.Onion[0] ^= 1
	p2.HopSrc = a.DeviceID
	if b.peelOnion(&p2) {
		t.Fatal("tampered onion layer accepted")
	}
}
