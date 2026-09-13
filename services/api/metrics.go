package main

import (
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

var (
	wsConnections atomic.Int64
	httpRequests  atomic.Int64
	httpErrors    atomic.Int64
	processStart  = time.Now()
)

// handleMetrics serves Prometheus text-format process and runtime metrics.
// It exposes only aggregates — no user identifiers, tokens, or message data.
func (a *App) handleMetrics(w http.ResponseWriter, r *http.Request) {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	var b strings.Builder
	f := func(name, help, typ string, val string) {
		b.WriteString("# HELP " + name + " " + help + "\n# TYPE " + name + " " + typ + "\n")
		b.WriteString(name + " " + val + "\n")
	}
	f("chatapp_uptime_seconds", "Seconds since process start", "gauge",
		strconv.FormatFloat(time.Since(processStart).Seconds(), 'f', -1, 64))
	f("chatapp_ws_connections", "Active websocket connections", "gauge",
		strconv.FormatInt(wsConnections.Load(), 10))
	f("chatapp_http_requests_total", "Total HTTP requests served", "counter",
		strconv.FormatInt(httpRequests.Load(), 10))
	f("chatapp_http_errors_total", "Total 5xx responses", "counter",
		strconv.FormatInt(httpErrors.Load(), 10))
	f("chatapp_go_goroutines", "Number of goroutines", "gauge",
		strconv.Itoa(runtime.NumGoroutine()))
	f("chatapp_go_heap_alloc_bytes", "Heap bytes allocated and in use", "gauge",
		strconv.FormatUint(ms.HeapAlloc, 10))
	f("chatapp_go_gc_count_total", "Completed GC cycles", "counter",
		strconv.FormatUint(uint64(ms.NumGC), 10))
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	_, _ = w.Write([]byte(b.String()))
}

// withMetrics counts HTTP requests and 5xx responses for GET /metrics, and
// tracks live websocket connections for the hub.
func withMetrics(prefix string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sw := &metricsWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r)
		httpRequests.Add(1)
		if sw.status >= 500 {
			httpErrors.Add(1)
		}
	})
}

// metricsWriter captures the response status for the error counter.
type metricsWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (m *metricsWriter) WriteHeader(code int) {
	if !m.wroteHeader {
		m.status = code
		m.wroteHeader = true
	}
	m.ResponseWriter.WriteHeader(code)
}
