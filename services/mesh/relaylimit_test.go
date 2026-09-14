package mesh

import (
	"testing"
	"time"
)

func TestRelayLimiterBurstAndRefill(t *testing.T) {
	l := NewRelayLimiter(RelayQuota{Every: time.Second, Burst: 3})
	now := time.Now()
	for i := 0; i < 3; i++ {
		if !l.Allow("src", now) {
			t.Fatalf("burst packet %d denied", i)
		}
	}
	if l.Allow("src", now) {
		t.Fatal("flood packet allowed beyond burst")
	}
	if !l.Allow("other", now) {
		t.Fatal("second source wrongly limited by first source's bucket")
	}
	// One token refills after Every.
	if !l.Allow("src", now.Add(time.Second)) {
		t.Fatal("refilled token denied")
	}
	if l.Allow("src", now.Add(time.Second)) {
		t.Fatal("more than one token refilled")
	}
}

func TestRelayQuotaBlocksFloodForwarding(t *testing.T) {
	bus := NewSimBus()
	addrs := simAddrs(3)
	a := newSimNode(t, bus, "d0", "member", nil, 8)
	m := newSimNode(t, bus, "d1", "relay", nil, 8)
	b := newSimNode(t, bus, "d2", "member", nil, 8)
	wireChain(bus, addrs)
	a.routes.Upsert(&Beacon{DeviceID: m.DeviceID, Kind: "relay", Transport: "local_wifi", Addr: addrs[1]})
	m.routes.Upsert(&Beacon{DeviceID: a.DeviceID, Kind: "member", Transport: "local_wifi", Addr: addrs[0]})
	m.routes.Upsert(&Beacon{DeviceID: b.DeviceID, Kind: "member", Transport: "local_wifi", Addr: addrs[2]})
	b.routes.Upsert(&Beacon{DeviceID: m.DeviceID, Kind: "relay", Transport: "local_wifi", Addr: addrs[1]})

	// Tight quota on the relay: burst 5, refill 1/s.
	m.limiter = NewRelayLimiter(RelayQuota{Every: time.Second, Burst: 5})
	delivered := 0
	var mu = b.handler
	_ = mu
	b.handler = func(p *Packet, pt []byte) { delivered++ }
	// Queue 20 floods from a to b.
	for i := 0; i < 20; i++ {
		if _, err := a.Send(KindMessage, b.DeviceID, []byte("flood")); err != nil {
			t.Fatal(err)
		}
	}
	if delivered > 5+1 { // burst + at most one refill tick during flush
		t.Fatalf("relay quota not enforced: delivered %d of 20 floods", delivered)
	}
	for _, n := range []*Node{a, m, b} {
		n.Stop()
	}
}
