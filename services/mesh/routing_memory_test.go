package mesh

import "testing"

func TestRouteTableDedupMetadataIsBounded(t *testing.T) {
	rt := NewRouteTable()
	for i := 0; i < rt.maxSeen+250; i++ {
		id := newPacketID()
		if duplicate := rt.Seen(id); duplicate {
			t.Fatalf("fresh packet %q was treated as duplicate", id)
		}
	}
	if len(rt.seen) > rt.maxSeen {
		t.Fatalf("seen cache grew beyond bound: %d > %d", len(rt.seen), rt.maxSeen)
	}
	if len(rt.bestTTL) > rt.maxSeen {
		t.Fatalf("bestTTL cache grew beyond bound: %d > %d", len(rt.bestTTL), rt.maxSeen)
	}
}

func TestRouteTableSeenBetterKeepsBothCachesBounded(t *testing.T) {
	rt := NewRouteTable()
	for i := 0; i < rt.maxSeen+250; i++ {
		id := newPacketID()
		improve, first := rt.SeenBetter(id, 8)
		if !improve || !first {
			t.Fatalf("fresh packet was not accepted: improve=%v first=%v", improve, first)
		}
	}
	if len(rt.seen) > rt.maxSeen || len(rt.bestTTL) > rt.maxSeen {
		t.Fatalf("dedup metadata exceeded bound: seen=%d bestTTL=%d max=%d", len(rt.seen), len(rt.bestTTL), rt.maxSeen)
	}
}
