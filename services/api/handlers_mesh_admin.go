package main

// Admin control of mesh features (§110).
//
// Administrators may configure operational policy for the offline mesh but
// must never gain the ability to decrypt private mesh messages. This handler
// exposes a singleton operational-policy row (mesh_admin_policy) that gates
// admission, sizing and transport eligibility only:
//   - feature enablement (mesh_enabled)
//   - version requirements (min_protocol_version)
//   - abuse limits (max_payload_bytes, max_ttl, relay_quota_bytes)
//   - network protocol deprecation (deprecated_transports)
//
// Every change is audit-logged. The mesh engine itself never exposes keys or
// plaintext to administrators; this table only bounds what the mesh accepts.

import (
	"context"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
)

// meshPolicy is the operational policy row as served to admins.
type meshPolicy struct {
	MeshEnabled          bool     `json:"mesh_enabled"`
	MinProtocolVersion   int      `json:"min_protocol_version"`
	MaxPayloadBytes      int64    `json:"max_payload_bytes"`
	MaxTTL               int      `json:"max_ttl"`
	RelayQuotaBytes      int64    `json:"relay_quota_bytes"`
	DeprecatedTransports []string `json:"deprecated_transports"`
	UpdatedAt            string   `json:"updated_at"`
}

// loadMeshPolicy reads the singleton operational policy row.
func (a *App) loadMeshPolicy(ctx context.Context) (meshPolicy, error) {
	var p meshPolicy
	var updated time.Time
	err := a.db.QueryRow(ctx,
		`SELECT mesh_enabled, min_protocol_version, max_payload_bytes, max_ttl,
		        relay_quota_bytes, deprecated_transports, updated_at
		 FROM mesh_admin_policy WHERE id=1`).Scan(
		&p.MeshEnabled, &p.MinProtocolVersion, &p.MaxPayloadBytes, &p.MaxTTL,
		&p.RelayQuotaBytes, &p.DeprecatedTransports, &updated)
	if err != nil {
		return p, err
	}
	p.UpdatedAt = updated.UTC().Format(time.RFC3339)
	return p, nil
}

// handleAdminGetMeshPolicy returns the current mesh operational policy.
func (a *App) handleAdminGetMeshPolicy(w http.ResponseWriter, r *http.Request) {
	p, err := a.loadMeshPolicy(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to read mesh policy")
		return
	}
	writeJSON(w, http.StatusOK, p)
}

// handleAdminSetMeshPolicy updates the mesh operational policy. Only
// operational controls are accepted; there is no key or plaintext surface.
func (a *App) handleAdminSetMeshPolicy(w http.ResponseWriter, r *http.Request) {
	var req struct {
		MeshEnabled          *bool    `json:"mesh_enabled"`
		MinProtocolVersion   *int     `json:"min_protocol_version"`
		MaxPayloadBytes      *int64   `json:"max_payload_bytes"`
		MaxTTL               *int     `json:"max_ttl"`
		RelayQuotaBytes      *int64   `json:"relay_quota_bytes"`
		DeprecatedTransports []string `json:"deprecated_transports"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	// Load current values so partial updates merge cleanly.
	cur, err := a.loadMeshPolicy(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to read mesh policy")
		return
	}
	enabled := cur.MeshEnabled
	if req.MeshEnabled != nil {
		enabled = *req.MeshEnabled
	}
	minVer := cur.MinProtocolVersion
	if req.MinProtocolVersion != nil {
		if *req.MinProtocolVersion < 1 || *req.MinProtocolVersion > 16 {
			writeErr(w, http.StatusBadRequest, "min_protocol_version must be between 1 and 16")
			return
		}
		minVer = *req.MinProtocolVersion
	}
	maxPayload := cur.MaxPayloadBytes
	if req.MaxPayloadBytes != nil {
		if *req.MaxPayloadBytes < 1<<10 || *req.MaxPayloadBytes > 1<<30 {
			writeErr(w, http.StatusBadRequest, "max_payload_bytes must be between 1 KiB and 1 GiB")
			return
		}
		maxPayload = *req.MaxPayloadBytes
	}
	maxTTL := cur.MaxTTL
	if req.MaxTTL != nil {
		if *req.MaxTTL < 1 || *req.MaxTTL > 16 {
			writeErr(w, http.StatusBadRequest, "max_ttl must be between 1 and 16")
			return
		}
		maxTTL = *req.MaxTTL
	}
	relayQuota := cur.RelayQuotaBytes
	if req.RelayQuotaBytes != nil {
		if *req.RelayQuotaBytes < 1<<20 || *req.RelayQuotaBytes > 1<<30 {
			writeErr(w, http.StatusBadRequest, "relay_quota_bytes must be between 1 MiB and 1 GiB")
			return
		}
		relayQuota = *req.RelayQuotaBytes
	}
	deprecated := req.DeprecatedTransports
	if deprecated == nil {
		deprecated = cur.DeprecatedTransports
	}
	for _, t := range deprecated {
		if !validMeshTransport(t) {
			writeErr(w, http.StatusBadRequest, "invalid deprecated transport: "+t)
			return
		}
	}
	adminID := userIDFrom(r)
	if _, err := a.db.Exec(r.Context(),
		`UPDATE mesh_admin_policy SET
		   mesh_enabled=$1, min_protocol_version=$2, max_payload_bytes=$3,
		   max_ttl=$4, relay_quota_bytes=$5, deprecated_transports=$6,
		   updated_at=now(), updated_by=$7
		 WHERE id=1`,
		enabled, minVer, maxPayload, maxTTL, relayQuota, deprecated, adminID); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to update mesh policy")
		return
	}
	a.audit(r.Context(), adminID, "mesh.policy_update", "mesh_admin_policy", map[string]any{
		"mesh_enabled": enabled, "min_protocol_version": minVer,
		"max_payload_bytes": maxPayload, "max_ttl": maxTTL,
		"relay_quota_bytes": relayQuota, "deprecated_transports": deprecated,
	})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// meshPolicyGate returns an error string if the current operational policy
// rejects a mesh operation, or "" if it is allowed. It is called at the top
// of the mesh admission paths (register/send/relay) so a disabled mesh or a
// deprecated transport is refused consistently.
func (a *App) meshPolicyGate(r *http.Request, transport string) string {
	p, err := a.loadMeshPolicy(r.Context())
	if err != nil {
		return "mesh policy unavailable"
	}
	if !p.MeshEnabled {
		return "mesh is disabled by policy"
	}
	if transport != "" {
		for _, t := range p.DeprecatedTransports {
			if t == transport {
				return "transport is deprecated by policy: " + transport
			}
		}
	}
	return ""
}

// meshPolicyLimits returns the policy-bounded payload and TTL ceilings so the
// send path can reject oversized or over-long packets consistently.
func (a *App) meshPolicyLimits(r *http.Request) (int64, int, error) {
	p, err := a.loadMeshPolicy(r.Context())
	if err != nil {
		return 0, 0, err
	}
	return p.MaxPayloadBytes, p.MaxTTL, nil
}

// ensure pgx import is used (kept for parity with other handlers).
var _ = pgx.ErrNoRows
