package mesh

// native_transport_test.go — real-socket tests for the Bluetooth / Wi-Fi
// Direct bridge transports and the Anonymous.md §5.3 automatic transport
// selection. These exercise actual TCP loopback sockets and the real Node
// routing path, not mocks.

import (
	"testing"
	"time"
)

// TestStreamFrameRoundTrip verifies the length-prefixed framing used over the
// Bluetooth RFCOMM and Wi-Fi Direct bridges survives a real socket round trip.
func TestStreamFrameRoundTrip(t *testing.T) {
	got := make(chan []byte, 1)
	tr, err := NewTCPTransport(KindBluetooth, "127.0.0.1:0", func(_ string, data []byte) {
		got <- data
	})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer tr.Close()

	sender, err := NewTCPTransport(KindWifiDirect, "127.0.0.1:0", nil)
	if err != nil {
		t.Fatalf("listen sender: %v", err)
	}
	defer sender.Close()

	payload := []byte(`{"id":"pkt-1","kind":"message","payload":"aGk="}`)
	if err := sender.Send(tr.Addr(), payload); err != nil {
		t.Fatalf("send: %v", err)
	}

	select {
	case data := <-got:
		if string(data) != string(payload) {
			t.Fatalf("payload mismatch: got %q want %q", data, payload)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for framed payload")
	}
}

// TestStreamTransportKindNames verifies each bridge reports the transport name
// used in beacons and by the selection order.
func TestStreamTransportKindNames(t *testing.T) {
	bt, err := NewTCPTransport(KindBluetooth, "127.0.0.1:0", nil)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer bt.Close()
	if bt.Transport() != "bluetooth" {
		t.Fatalf("bluetooth transport name = %q", bt.Transport())
	}

	p2p, err := NewTCPTransport(KindWifiDirect, "127.0.0.1:0", nil)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer p2p.Close()
	if p2p.Transport() != "wifi_direct" {
		t.Fatalf("wifi_direct transport name = %q", p2p.Transport())
	}
}

// TestAutoTransportPreferenceOrder verifies §5.3 ordering: local Wi-Fi wins
// when present, then Wi-Fi Direct, then Bluetooth.
func TestAutoTransportPreferenceOrder(t *testing.T) {
	auto := NewAutoTransport(nil)
	if auto.Active() != "none" {
		t.Fatalf("empty auto transport should report none, got %q", auto.Active())
	}

	bt, _ := NewTCPTransport(KindBluetooth, "127.0.0.1:0", nil)
	defer bt.Close()
	auto.Add("bluetooth", bt)
	if auto.Active() != "bluetooth" {
		t.Fatalf("expected bluetooth, got %q", auto.Active())
	}

	p2p, _ := NewTCPTransport(KindWifiDirect, "127.0.0.1:0", nil)
	defer p2p.Close()
	auto.Add("wifi_direct", p2p)
	if auto.Active() != "wifi_direct" {
		t.Fatalf("Wi-Fi Direct must outrank Bluetooth, got %q", auto.Active())
	}

	wifi, err := NewUDPTransport(0, nil)
	if err != nil {
		t.Fatalf("udp: %v", err)
	}
	defer wifi.Close()
	auto.Add("local_wifi", wifi)
	if auto.Active() != "local_wifi" {
		t.Fatalf("local Wi-Fi must outrank Wi-Fi Direct, got %q", auto.Active())
	}

	// Losing the preferred link falls back down the chain automatically.
	auto.Remove("local_wifi")
	if auto.Active() != "wifi_direct" {
		t.Fatalf("expected fallback to wifi_direct, got %q", auto.Active())
	}
	auto.Remove("wifi_direct")
	if auto.Active() != "bluetooth" {
		t.Fatalf("expected fallback to bluetooth, got %q", auto.Active())
	}
}

// TestAutoTransportStoreAndForwardWhenNoLink verifies that with no link the
// send fails so the caller keeps the packet queued for store-and-forward,
// rather than silently dropping it.
func TestAutoTransportStoreAndForwardWhenNoLink(t *testing.T) {
	auto := NewAutoTransport(nil)
	if err := auto.Send("127.0.0.1:1", []byte("x")); err == nil {
		t.Fatal("expected an error with no transport available")
	}
	if auto.Addr() != "" {
		t.Fatalf("expected empty addr with no transport, got %q", auto.Addr())
	}

	// A real node keeps the packet queued and reports pending work.
	key, _ := NewIdentityKey()
	node := NewNode(NodeConfig{DeviceID: "dev-offline", Key: &key, Transport: auto})
	defer node.Stop()
	if _, err := node.Send(KindMessage, "dev-peer", []byte("queued while offline")); err != nil {
		t.Fatalf("send: %v", err)
	}
	if node.pfifo.Len() != 1 {
		t.Fatalf("expected 1 packet held for store-and-forward, got %d", node.pfifo.Len())
	}
}

// TestNodeOverStreamTransport verifies a packet really traverses a stream
// (Bluetooth-style) bridge between two nodes and is decrypted by the peer.
func TestNodeOverStreamTransport(t *testing.T) {
	key, _ := NewIdentityKey()
	received := make(chan string, 1)

	peer, err := NewTCPTransport(KindBluetooth, "127.0.0.1:0", nil)
	if err != nil {
		t.Fatalf("peer listen: %v", err)
	}
	defer peer.Close()

	peerNode := NewNode(NodeConfig{
		DeviceID:  "peer-device",
		Key:       &key,
		Transport: peer,
		Handler: func(_ *Packet, plaintext []byte) {
			received <- string(plaintext)
		},
	})

	sender, err := NewTCPTransport(KindBluetooth, "127.0.0.1:0", nil)
	if err != nil {
		t.Fatalf("sender listen: %v", err)
	}
	defer sender.Close()
	senderNode := NewNode(NodeConfig{DeviceID: "sender-device", Key: &key, Transport: sender})

	// Learn the peer, then send an encrypted packet across the bridge.
	senderNode.routes.Upsert(&Beacon{DeviceID: "peer-device", Transport: "bluetooth", Addr: peer.Addr()})
	if _, err := senderNode.Send(KindMessage, "peer-device", []byte("hello over bluetooth bridge")); err != nil {
		t.Fatalf("send: %v", err)
	}

	select {
	case msg := <-received:
		if msg != "hello over bluetooth bridge" {
			t.Fatalf("peer received %q", msg)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for the peer to receive the packet")
	}
	_ = peerNode
}

// TestBeaconReportsActiveTransport verifies the discovery beacon advertises the
// transport actually in use, so peers route over the right link.
func TestBeaconReportsActiveTransport(t *testing.T) {
	key, _ := NewIdentityKey()
	auto := NewAutoTransport(nil)
	p2p, _ := NewTCPTransport(KindWifiDirect, "127.0.0.1:0", nil)
	defer p2p.Close()
	auto.Add("wifi_direct", p2p)

	node := NewNode(NodeConfig{DeviceID: "dev-a", Key: &key, Transport: auto})
	defer node.Stop()
	if got := node.transportName(); got != "wifi_direct" {
		t.Fatalf("transportName = %q, want wifi_direct", got)
	}

	// A plain UDP node still reports the local-Wi-Fi link.
	udp, _ := NewUDPTransport(0, nil)
	defer udp.Close()
	udpNode := NewNode(NodeConfig{DeviceID: "dev-b", Key: &key, Transport: udp})
	defer udpNode.Stop()
	if got := udpNode.transportName(); got != "local_wifi" {
		t.Fatalf("transportName = %q, want local_wifi", got)
	}
}
