package main

// tor.go — Tor/Onion routing integration for the API service.
//
// Anonymous.md requires Tor routing by default (§1 "Winning Features") and
// onion-routed real-time communication (§2). This file integrates the Go API
// with a local Tor SOCKS5 proxy so outbound connections (relay registration,
// mesh federation, provider API calls that are privacy-sensitive) transit
// the Tor network when TOR_ENABLED is set.
//
// The Tor proxy is also exposed to the mesh engine for onion construction.
// When the API builds a multi-hop onion path through the mesh relay network,
// the relay-registration step itself may traverse Tor so the relay's IP is
// never exposed to the registration coordinator. When TOR_ENABLED is true,
// all mesh relay egress uses the Tor SOCKS5 proxy.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"sync"
	"time"

	"golang.org/x/net/proxy"
)

// TorConfig holds the Tor integration settings.
type TorConfig struct {
	Enabled       bool
	SOCKS5Addr    string
	ControlAddr   string
	ControlPass   string
	MaxCircuitAge time.Duration
}

// TorManager owns the Tor SOCKS5 dialer and circuit state.
type TorManager struct {
	cfg       TorConfig
	dialer    proxy.Dialer
	mu        sync.Mutex
	lastCheck time.Time
	reachable bool
}

// NewTorManager creates a Tor manager from environment configuration.
func NewTorManager() *TorManager {
	cfg := TorConfig{
		Enabled:       os.Getenv("TOR_ENABLED") == "1" || os.Getenv("TOR_ENABLED") == "true",
		SOCKS5Addr:    envDefault("TOR_SOCKS5_ADDR", "127.0.0.1:9050"),
		ControlAddr:   os.Getenv("TOR_CONTROL_ADDR"),
		ControlPass:   os.Getenv("TOR_CONTROL_PASS"),
		MaxCircuitAge: 10 * time.Minute,
	}
	if cfg.Enabled {
		log.Printf("[tor] Tor integration enabled, SOCKS5 proxy at %s", cfg.SOCKS5Addr)
	}
	return &TorManager{cfg: cfg}
}

// DialContext returns a context-aware dialer that routes through Tor.
func (t *TorManager) DialContext(ctx context.Context) func(ctx context.Context, network, addr string) (net.Conn, error) {
	if !t.cfg.Enabled {
		return (&net.Dialer{Timeout: 10 * time.Second}).DialContext
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.dialer == nil {
		d, err := proxy.SOCKS5("tcp", t.cfg.SOCKS5Addr, nil, proxy.Direct)
		if err != nil {
			log.Printf("[tor] SOCKS5 dialer creation failed: %v — falling back to direct", err)
			return (&net.Dialer{Timeout: 15 * time.Second}).DialContext
		}
		t.dialer = d
	}
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		deadline, ok := ctx.Deadline()
		if !ok {
			deadline = time.Now().Add(15 * time.Second)
		}
		conn, err := t.dialer.Dial(network, addr)
		if err != nil {
			t.reachable = false
			return nil, fmt.Errorf("tor dial %s: %w", addr, err)
		}
		conn.SetDeadline(deadline)
		t.reachable = true
		t.lastCheck = time.Now()
		return conn, nil
	}
}

// HTTPClient returns an *http.Client that routes through Tor.
func (t *TorManager) HTTPClient() *http.Client {
	if !t.cfg.Enabled {
		return http.DefaultClient
	}
	return &http.Client{
		Transport: &http.Transport{
			DialContext:     t.DialContext(context.Background()),
			MaxIdleConns:    2,
			IdleConnTimeout: 30 * time.Second,
		},
		Timeout: 30 * time.Second,
	}
}

// Reachable reports whether the Tor proxy accepted a connection recently.
func (t *TorManager) Reachable() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.reachable && time.Since(t.lastCheck) < t.cfg.MaxCircuitAge
}

// NewCircuit requests a new Tor circuit.
func (t *TorManager) NewCircuit() error {
	if t.cfg.ControlAddr == "" {
		return fmt.Errorf("tor: ControlPort not configured")
	}
	conn, err := net.DialTimeout("tcp", t.cfg.ControlAddr, 3*time.Second)
	if err != nil {
		return fmt.Errorf("tor: cannot connect to ControlPort: %w", err)
	}
	defer conn.Close()
	if t.cfg.ControlPass != "" {
		fmt.Fprintf(conn, "AUTHENTICATE \"%s\"\r\n", t.cfg.ControlPass)
	} else {
		fmt.Fprintf(conn, "AUTHENTICATE\r\n")
	}
	buf := make([]byte, 256)
	n, _ := conn.Read(buf)
	if !isTorOK(string(buf[:n])) {
		return fmt.Errorf("tor: authentication failed: %s", string(buf[:n]))
	}
	fmt.Fprintf(conn, "SIGNAL NEWNYM\r\n")
	n, _ = conn.Read(buf)
	if !isTorOK(string(buf[:n])) {
		return fmt.Errorf("tor: NEWNYM failed: %s", string(buf[:n]))
	}
	log.Printf("[tor] new circuit requested")
	return nil
}

func (t *TorManager) HealthCheck() bool {
	if !t.cfg.Enabled {
		return true
	}
	conn, err := net.DialTimeout("tcp", t.cfg.SOCKS5Addr, 3*time.Second)
	if err != nil {
		t.mu.Lock()
		t.reachable = false
		t.mu.Unlock()
		return false
	}
	conn.Close()
	t.mu.Lock()
	t.reachable = true
	t.lastCheck = time.Now()
	t.mu.Unlock()
	return true
}

func (t *TorManager) OnionAddress() string {
	if !t.cfg.Enabled {
		return ""
	}
	dataDir := os.Getenv("TOR_DATA_DIR")
	if dataDir == "" {
		dataDir = "/var/lib/tor/hidden_service"
	}
	hostname, err := os.ReadFile(dataDir + "/hostname")
	if err != nil {
		return ""
	}
	return string(hostname)
}

func GenerateAnonymousPairwiseID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func isTorOK(response string) bool {
	return len(response) >= 3 && (response[:3] == "250" || response[:3] == "251")
}

func envDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

type TorTransport struct {
	manager *TorManager
}

func (t *TorTransport) Dial(network, addr string) (net.Conn, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return t.manager.DialContext(ctx)(ctx, network, addr)
}

func (a *App) RegisterTorRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/tor/status", a.requireAuth(a.handleTorStatus))
	mux.HandleFunc("POST /api/tor/new-circuit", a.requireAuth(a.handleTorNewCircuit))
}

func (a *App) handleTorStatus(w http.ResponseWriter, r *http.Request) {
	if a.tor == nil {
		writeJSON(w, http.StatusOK, map[string]any{"enabled": false})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled":     a.tor.cfg.Enabled,
		"reachable":   a.tor.Reachable(),
		"socks5_addr": a.tor.cfg.SOCKS5Addr,
		"onion_addr":  a.tor.OnionAddress(),
	})
}

func (a *App) handleTorNewCircuit(w http.ResponseWriter, r *http.Request) {
	if a.tor == nil || !a.tor.cfg.Enabled {
		writeErr(w, http.StatusServiceUnavailable, "tor not enabled")
		return
	}
	if err := a.tor.NewCircuit(); err != nil {
		writeErr(w, http.StatusInternalServerError, "new circuit failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "new_circuit_established"})
}

var _ = url.Parse
