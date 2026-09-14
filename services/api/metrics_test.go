package main

import (
	"bufio"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

// hijackableWriter mimics what net/http hands down the middleware chain: a
// concrete writer that implements http.Hijacker. The real server passes its own
// *http.response, which does the same.
type hijackableWriter struct {
	hdr      http.Header
	hijacked bool
}

func newHijackable() *hijackableWriter { return &hijackableWriter{hdr: http.Header{}} }

func (h *hijackableWriter) Header() http.Header         { return h.hdr }
func (h *hijackableWriter) Write(b []byte) (int, error) { return len(b), nil }
func (h *hijackableWriter) WriteHeader(int)             {}
func (h *hijackableWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h.hijacked = true
	return nil, nil, nil
}

// A plain writer with no Hijack support, like a bare httptest recorder.
type plainWriter struct{ hdr http.Header }

func newPlain() *plainWriter                       { return &plainWriter{hdr: http.Header{}} }
func (p *plainWriter) Header() http.Header         { return p.hdr }
func (p *plainWriter) Write(b []byte) (int, error) { return len(b), nil }
func (p *plainWriter) WriteHeader(int)             {}

// TestMetricsWriterHijacksUnderlyingConnection is the regression guard for the
// defect where wrapping the ResponseWriter in the metrics middleware stripped
// the http.Hijacker interface. gorilla/websocket type-asserts `w.(http.Hijacker)`
// during Upgrade; when the assertion fails it answers HTTP 500, so every
// WebSocket upgrade on the production middleware chain (chat, presence, typing,
// call signalling) died while builds and unit tests stayed green.
func TestMetricsWriterHijacksUnderlyingConnection(t *testing.T) {
	inner := newHijackable()
	mw := &metricsWriter{ResponseWriter: inner, status: http.StatusOK}

	hj, ok := http.ResponseWriter(mw).(http.Hijacker)
	if !ok {
		t.Fatal("metricsWriter must implement http.Hijacker so WebSocket upgrades can hijack the connection")
	}
	if _, _, err := hj.Hijack(); err != nil {
		t.Fatalf("Hijack() via wrapper returned error: %v", err)
	}
	if !inner.hijacked {
		t.Fatal("Hijack() must be forwarded to the underlying ResponseWriter")
	}
}

// A writer that genuinely cannot be hijacked must surface http.ErrNotSupported
// rather than panicking, so non-hijackable contexts fail predictably.
func TestMetricsWriterHijackUnsupported(t *testing.T) {
	mw := &metricsWriter{ResponseWriter: newPlain(), status: http.StatusOK}
	_, _, err := mw.Hijack()
	if !errors.Is(err, http.ErrNotSupported) {
		t.Fatalf("expected http.ErrNotSupported for a non-hijackable writer, got %v", err)
	}
}

// The wrapper must still pass status codes through and record them for the
// error counter, and must expose the original writer via Unwrap.
func TestMetricsWriterRecordsStatusAndUnwraps(t *testing.T) {
	rec := httptest.NewRecorder()
	mw := &metricsWriter{ResponseWriter: rec, status: http.StatusOK}

	mw.WriteHeader(http.StatusTeapot)
	if mw.status != http.StatusTeapot {
		t.Fatalf("status not recorded: got %d want %d", mw.status, http.StatusTeapot)
	}
	if rec.Code != http.StatusTeapot {
		t.Fatalf("status not forwarded: got %d want %d", rec.Code, http.StatusTeapot)
	}
	if mw.Unwrap() == nil {
		t.Fatal("Unwrap must expose the underlying ResponseWriter")
	}
}

// withMetrics must return a writer that is still hijackable end to end.
func TestWithMetricsPreservesHijacker(t *testing.T) {
	var sawHijacker bool
	h := withMetrics("", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, sawHijacker = w.(http.Hijacker)
	}))
	h.ServeHTTP(newHijackable(), httptest.NewRequest(http.MethodGet, "/ws", nil))

	if !sawHijacker {
		t.Fatal("handler behind withMetrics() cannot hijack: WebSocket upgrades would fail with HTTP 500")
	}
}
