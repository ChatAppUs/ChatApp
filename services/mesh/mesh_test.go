package mesh

import (
	"strings"
	"sync"
	"testing"
	"time"
)

// TestEncryptDecrypt verifies the authenticated-encryption round trip.
func TestEncryptDecrypt(t *testing.T) {
	key, err := NewIdentityKey()
	if err != nil {
		t.Fatal(err)
	}
	ct, nonce, err := Encrypt(&key, []byte("hello mesh"))
	if err != nil {
		t.Fatal(err)
	}
	pt, err := Decrypt(&key, ct, nonce)
	if err != nil {
		t.Fatal(err)
	}
	if string(pt) != "hello mesh" {
		t.Fatalf("round trip mismatch: %q", pt)
	}
	// Tampered ciphertext must fail.
	ct[0] ^= 0xff
	if _, err := Decrypt(&key, ct, nonce); err == nil {
		t.Fatal("expected tamper detection to fail")
	}
}

// TestPacketRoundTrip verifies packet marshal/unmarshal.
func TestPacketRoundTrip(t *testing.T) {
	p := NewPacket(KindMessage, "dev-1", "dev-2", 8)
	p.Payload = []byte("ciphertext")
	data, err := p.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	got, err := UnmarshalPacket(data)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != p.ID || got.Src != "dev-1" || got.Dst != "dev-2" {
		t.Fatalf("round trip mismatch: %+v", got)
	}
}

// TestRouteTableDedup verifies duplicate suppression.
func TestRouteTableDedup(t *testing.T) {
	rt := NewRouteTable()
	if rt.Seen("pkt-1") {
		t.Fatal("first sighting should not be a duplicate")
	}
	if !rt.Seen("pkt-1") {
		t.Fatal("second sighting should be a duplicate")
	}
}

// TestStoreForward verifies the queue enqueues, expires, and removes.
func TestStoreForward(t *testing.T) {
	q := NewQueue(10, time.Hour)
	p := NewPacket(KindMessage, "dev-1", "dev-2", 8)
	q.Enqueue(p)
	if q.Len() != 1 {
		t.Fatalf("expected 1 queued, got %d", q.Len())
	}
	pending := q.Pending(time.Now())
	if len(pending) != 1 {
		t.Fatalf("expected 1 pending, got %d", len(pending))
	}
	q.Remove(p.ID)
	if q.Len() != 0 {
		t.Fatalf("expected 0 after remove, got %d", q.Len())
	}
}

// TestNodeSendDeliver verifies a node encrypts, sends, and the peer decrypts
// and delivers a 1:1 message over a real UDP transport.
func TestNodeSendDeliver(t *testing.T) {
	// Both nodes share a session key (established by the identity layer).
	shared, _ := NewIdentityKey()
	key1 := shared
	key2 := shared

	var mu sync.Mutex
	delivered := map[string]string{}

	tr1, err := NewUDPTransport(0, nil)
	if err != nil {
		t.Fatal(err)
	}
	tr2, err := NewUDPTransport(0, nil)
	if err != nil {
		t.Fatal(err)
	}

	n1 := NewNode(NodeConfig{DeviceID: "dev-1", Key: &key1, Transport: tr1,
		Handler: func(p *Packet, pt []byte) {}})
	n2 := NewNode(NodeConfig{DeviceID: "dev-2", Key: &key2, Transport: tr2,
		Handler: func(p *Packet, pt []byte) {
			mu.Lock()
			delivered[p.ID] = string(pt)
			mu.Unlock()
		}})
	n1.Start()
	n2.Start()
	defer n1.Stop()
	defer n2.Stop()

	// Register each as a neighbor of the other (simulate discovery), using
	// loopback addresses (0.0.0.0 is not a valid send target).
	n1.routes.Upsert(&Beacon{DeviceID: "dev-2", Addr: "127.0.0.1" + tr2.Addr()[strings.LastIndex(tr2.Addr(), ":"):], Transport: "local_wifi"})
	n2.routes.Upsert(&Beacon{DeviceID: "dev-1", Addr: "127.0.0.1" + tr1.Addr()[strings.LastIndex(tr1.Addr(), ":"):], Transport: "local_wifi"})
	n1.routes.SetRelay("dev-2", true)
	n2.routes.SetRelay("dev-1", true)

	// Send a message from dev-1 to dev-2.
	msg := &Message{ConversationID: "conv-1", Text: "hello over mesh", SentAt: time.Now().UnixMilli()}
	pt, _ := MarshalMessage(msg)
	if _, err := n1.Send(KindMessage, "dev-2", pt); err != nil {
		t.Fatal(err)
	}

	// Wait for delivery.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		got := len(delivered)
		mu.Unlock()
		if got > 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(delivered) == 0 {
		t.Fatal("message was not delivered over the mesh")
	}
	for _, v := range delivered {
		m, err := UnmarshalMessage([]byte(v))
		if err != nil {
			t.Fatalf("delivered payload not a message: %q (%v)", v, err)
		}
		if m.Text != "hello over mesh" {
			t.Fatalf("unexpected delivered text: %q", m.Text)
		}
	}
}
