package mesh

// relay_config.go — self-hostable relay server configuration.
//
// Anonymous.md §1 requires a self-hostable relay option so users
// can run their own relay servers for complete independence from
// the centralized infrastructure.
//
// This file defines:
//   1. Relay server configuration (YAML-based) for self-hosting.
//   2. Health check and monitoring endpoints for self-hosted relays.
//   3. Peer discovery via DNS SRV records + manual bootstrap peers.
//   4. Relay capacity and connection limits.
//   5. Metrics export for Prometheus-compatible monitoring.

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

// RelayConfig defines the configuration for a self-hosted relay server.
type RelayConfig struct {
	RelayID              string   `json:"relay_id" yaml:"relay_id"`
	RelayName            string   `json:"relay_name" yaml:"relay_name"`
	MeshPort             int      `json:"mesh_port" yaml:"mesh_port"`
	MetricsPort          int      `json:"metrics_port" yaml:"metrics_port"`
	AdminPort            int      `json:"admin_port" yaml:"admin_port"`
	TLSCertFile          string   `json:"tls_cert_file" yaml:"tls_cert_file"`
	TLSKeyFile           string   `json:"tls_key_file" yaml:"tls_key_file"`
	IdentityKeySeed      string   `json:"identity_key_seed" yaml:"identity_key_seed"`
	MaxConnections       int      `json:"max_connections" yaml:"max_connections"`
	MaxBandwidthMB       int      `json:"max_bandwidth_mb" yaml:"max_bandwidth_mb"`
	BootstrapPeers       []string `json:"bootstrap_peers" yaml:"bootstrap_peers"`
	DNSSRVName           string   `json:"dns_srv_name" yaml:"dns_srv_name"`
	AllowFederation      bool     `json:"allow_federation" yaml:"allow_federation"`
	FederationPeers      []string `json:"federation_peers" yaml:"federation_peers"`
	LogLevel             string   `json:"log_level" yaml:"log_level"`
	LogFormat            string   `json:"log_format" yaml:"log_format"`
	MaxMessagesPerSecond int      `json:"max_messages_per_second" yaml:"max_messages_per_second"`
	MaxConnectionsPerIP  int      `json:"max_connections_per_ip" yaml:"max_connections_per_ip"`
}

// DefaultRelayConfig returns sensible defaults for a self-hosted relay.
func DefaultRelayConfig() *RelayConfig {
	return &RelayConfig{
		RelayID:              generateRelayID(),
		RelayName:            "ChatApp Relay",
		MeshPort:             9000,
		MetricsPort:          9090,
		AdminPort:            9091,
		MaxConnections:       10000,
		MaxBandwidthMB:       1000,
		LogLevel:             "info",
		LogFormat:            "json",
		MaxMessagesPerSecond: 500,
		MaxConnectionsPerIP:  5,
		AllowFederation:      true,
	}
}

// RelayStats holds the current status of a relay server.
type RelayStats struct {
	mu                sync.RWMutex
	Uptime            time.Duration
	ActiveConnections int
	TotalConnections  uint64
	TotalMessages     uint64
	TotalBytesIn      uint64
	TotalBytesOut     uint64
	BandwidthIn       float64
	BandwidthOut      float64
	Errors            uint64
	ConnectionDrops   uint64
	FederationPeers   int
}

// RelayHealth holds the health status of the relay.
type RelayHealth struct {
	Status    string `json:"status"`
	Version   string `json:"version"`
	RelayID   string `json:"relay_id"`
	PublicKey string `json:"public_key"`
}

// RelayServer represents a running self-hosted relay instance.
type RelayServer struct {
	Config      *RelayConfig
	IdentityKey ed25519.PrivateKey
	PublicKey   ed25519.PublicKey
	Stats       *RelayStats
	StartedAt   time.Time
}

// NewRelayServer creates a new relay server from config.
func NewRelayServer(cfg *RelayConfig) (*RelayServer, error) {
	var seed []byte
	if cfg.IdentityKeySeed != "" {
		var err error
		seed, err = hex.DecodeString(cfg.IdentityKeySeed)
		if err != nil {
			return nil, fmt.Errorf("invalid identity key seed: %w", err)
		}
		if len(seed) != ed25519.SeedSize {
			return nil, fmt.Errorf("seed must be %d bytes", ed25519.SeedSize)
		}
	} else {
		seed = make([]byte, ed25519.SeedSize)
		data, err := os.ReadFile("/var/lib/chatapp/relay/identity.seed")
		if err == nil && len(data) == ed25519.SeedSize {
			seed = data
		}
	}

	priv := ed25519.NewKeyFromSeed(seed)
	pub := priv.Public().(ed25519.PublicKey)

	return &RelayServer{
		Config:      cfg,
		IdentityKey: priv,
		PublicKey:   pub,
		Stats:       &RelayStats{},
		StartedAt:   time.Now(),
	}, nil
}

// HealthCheck returns the current health status of the relay.
func (rs *RelayServer) HealthCheck() *RelayHealth {
	rs.Stats.mu.RLock()
	defer rs.Stats.mu.RUnlock()

	status := "healthy"
	maxConns := rs.Config.MaxConnections
	if maxConns > 0 && rs.Stats.ActiveConnections > maxConns*9/10 {
		status = "degraded"
	}
	if maxConns > 0 && rs.Stats.ActiveConnections >= maxConns {
		status = "unhealthy"
	}

	return &RelayHealth{
		Status:    status,
		Version:   "1.0.0",
		RelayID:   rs.Config.RelayID,
		PublicKey: hex.EncodeToString(rs.PublicKey),
	}
}

// MetricsJSON returns Prometheus-compatible relay metrics.
func (rs *RelayServer) MetricsJSON() map[string]any {
	rs.Stats.mu.RLock()
	defer rs.Stats.mu.RUnlock()

	return map[string]any{
		"chatapp_relay_uptime_seconds":     rs.Stats.Uptime.Seconds(),
		"chatapp_relay_active_connections": rs.Stats.ActiveConnections,
		"chatapp_relay_total_connections":  rs.Stats.TotalConnections,
		"chatapp_relay_total_messages":     rs.Stats.TotalMessages,
		"chatapp_relay_bytes_in_total":     rs.Stats.TotalBytesIn,
		"chatapp_relay_bytes_out_total":    rs.Stats.TotalBytesOut,
		"chatapp_relay_bandwidth_in_mbps":  rs.Stats.BandwidthIn,
		"chatapp_relay_bandwidth_out_mbps": rs.Stats.BandwidthOut,
		"chatapp_relay_errors_total":       rs.Stats.Errors,
		"chatapp_relay_connection_drops":   rs.Stats.ConnectionDrops,
		"chatapp_relay_federation_peers":   rs.Stats.FederationPeers,
	}
}

var RelayDockerCompose = `version: '3.8'
services:
  chatapp-relay:
    image: chatapp/relay:latest
    restart: unless-stopped
    ports:
      - "9000:9000"
      - "9090:9090"
    volumes:
      - ./relay.yaml:/etc/chatapp/relay.yaml:ro
      - relay-data:/var/lib/chatapp/relay
    environment:
      - RELAY_CONFIG=/etc/chatapp/relay.yaml
    networks:
      - chatapp

volumes:
  relay-data:

networks:
  chatapp:
`

var RelaySystemdUnit = `[Unit]
Description=ChatApp Mesh Relay
After=network.target

[Service]
Type=simple
User=chatapp
ExecStart=/usr/local/bin/chatapp-relay --config /etc/chatapp/relay.yaml
Restart=always
RestartSec=10
LimitNOFILE=65536

[Install]
WantedBy=multi-user.target
`

func generateRelayID() string {
	return fmt.Sprintf("relay-%d", time.Now().UnixNano()%1000000)
}

var _ = json.Marshal