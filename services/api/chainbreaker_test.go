package main

import (
	"errors"
	"testing"
	"time"
)

func TestRPCBreakerOpensAndRecovers(t *testing.T) {
	b := &rpcBreaker{}
	for i := 0; i < breakerMaxFails; i++ {
		if !b.allow() {
			t.Fatalf("call %d should be allowed while closed", i+1)
		}
		b.record(errors.New("connection refused"))
	}
	if b.allow() {
		t.Fatal("breaker should be open after 3 consecutive failures")
	}
	// Force the cooldown to elapse.
	b.mu.Lock()
	b.openUntil = time.Now().Add(-time.Second)
	b.mu.Unlock()
	if !b.allow() {
		t.Fatal("breaker should half-open after the cooldown")
	}
	b.record(nil)
	if !b.allow() {
		t.Fatal("breaker should be closed after a success")
	}
	st := b.status()
	if st["open"].(bool) {
		t.Fatal("status should report closed after a success")
	}
}

func TestRPCBreakerStatusCounts(t *testing.T) {
	b := &rpcBreaker{}
	b.record(nil)
	b.record(errors.New("boom"))
	b.record(errors.New("boom"))
	st := b.status()
	if st["total_success"] != 1 || st["total_failures"] != 2 {
		t.Fatalf("status counters wrong: %v", st)
	}
	if st["last_error"] != "boom" {
		t.Fatalf("last_error should be retained, got %v", st["last_error"])
	}
}

func TestReconcileGapRange(t *testing.T) {
	// Cursor far behind the tip: the reconciler must detect the gap and
	// bound it to the configured catch-up window.
	gap := reconcileGap(100, 10_000, 500)
	if gap != 500 {
		t.Fatalf("gap = %d, want bounded catch-up of 500", gap)
	}
	if reconcileGap(9_950, 10_000, 500) != 50 {
		t.Fatalf("small gap should not be padded")
	}
	if reconcileGap(10_000, 10_000, 500) != 0 {
		t.Fatalf("no gap should be reported when caught up")
	}
	if reconcileGap(10_100, 10_000, 500) != 0 {
		t.Fatalf("cursor ahead of tip must not go negative")
	}
}

func TestScanCatchUpBound(t *testing.T) {
	// A watcher left behind by an outage catches up in bounded batches and
	// never ahead of the confirmed tip.
	from, latest := int64(100), int64(10_000)
	target := from + reconcileGap(from, latest, evmCatchUpBlocks)
	if target != from+evmCatchUpBlocks {
		t.Fatalf("catch-up target = %d, want cursor + %d", target, evmCatchUpBlocks)
	}
	if target > latest {
		t.Fatal("catch-up must never pass the tip")
	}
}
