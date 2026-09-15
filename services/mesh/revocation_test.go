package mesh

import "testing"

func TestPeerRevocationRejectsSignedAndLegacyBeacons(t *testing.T) {
	localKey := MustKey()
	peerKey := MustKey()
	local := NewNode(NodeConfig{DeviceID: "local", Key: localKey})
	peer := NewNode(NodeConfig{DeviceID: "peer", Key: peerKey})

	beacon := &Beacon{DeviceID: peer.DeviceID, Kind: "relay", Addr: "127.0.0.1:1234", Transport: "local_wifi"}
	signed, err := peer.signer.SignBeacon(beacon, peer.kem.Public(), peer.kem.Epoch)
	if err != nil {
		t.Fatal(err)
	}
	wire, err := MarshalSignedBeacon(signed)
	if err != nil {
		t.Fatal(err)
	}
	local.HandleInbound("", wire)
	if len(local.Routes().Neighbors()) != 1 {
		t.Fatal("signed beacon was not admitted")
	}
	if err := local.RevokePeer(peer.DeviceID, signed.PubKey); err != nil {
		t.Fatal(err)
	}
	if !local.IsPeerRevoked(peer.DeviceID) || len(local.Routes().Neighbors()) != 0 {
		t.Fatal("revocation did not withdraw the peer")
	}
	local.HandleInbound("", wire)
	if len(local.Routes().Neighbors()) != 0 {
		t.Fatal("revoked signed beacon was accepted")
	}
	legacy, err := MarshalBeacon(beacon)
	if err != nil {
		t.Fatal(err)
	}
	local.HandleInbound("", legacy)
	if len(local.Routes().Neighbors()) != 0 {
		t.Fatal("revoked identity re-entered through unsigned beacon")
	}
	if err := local.RevokePeer(peer.DeviceID, []byte("wrong-key")); err == nil {
		t.Fatal("revocation accepted a mismatched public key")
	}
}
