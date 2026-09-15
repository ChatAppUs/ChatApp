package main

// Offline multi-hop device mesh (Briar-style store-and-forward).
//
// A mesh packet is an encrypted, authenticated payload that may be relayed
// through intermediate devices before reaching its destination. Relays carry
// ciphertext only and never decrypt message content. Delivery is best-effort
// and delay-tolerant: packets persist in a bounded queue until a route to the
// destination becomes available, then expire (TTL bounds lifetime, seen-ids
// deduplicate against loops). This is a real store-and-forward transport, not
// a demo: state is authoritative in Postgres and every transition is audited.

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// ---- Device registration ----

type meshDevice struct {
	ID        string `json:"id"`
	DeviceKey string `json:"device_key"`
	Transport string `json:"transport"`
	LastSeen  string `json:"last_seen_at"`
}

const (
	meshMaxPayloadBytes = 2 << 20
	meshMaxDeviceKeyLen = 256
	meshDefaultQuota    = 10 << 20
	meshMaxQuota        = 1 << 30
)

func meshSubject(r *http.Request) string {
	return strings.TrimSpace(userIDFrom(r))
}

func validMeshDeviceKey(key string) bool {
	return key != "" && len(key) <= meshMaxDeviceKeyLen && !strings.ContainsAny(key, "\r\n\t")
}

func validMeshTransport(transport string) bool {
	switch transport {
	case "bluetooth", "ble", "wifi_direct", "local_wifi", "internet":
		return true
	default:
		return false
	}
}

func meshDeviceHeader(r *http.Request) string {
	return strings.TrimSpace(r.Header.Get("X-Mesh-Device"))
}

func meshOwnedDeviceID(ctx context.Context, db interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, subject, deviceKey string) (string, error) {
	var id string
	err := db.QueryRow(ctx,
		`SELECT id FROM mesh_devices WHERE device_key=$1 AND owner_subject=$2`,
		deviceKey, subject).Scan(&id)
	return id, err
}

// handleMeshRegister registers (or refreshes) a device on the mesh. Ownership
// is bound to the authenticated member or guest subject; a device key alone is
// not a bearer credential for another subject's queue.
func (a *App) handleMeshRegister(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DeviceKey string `json:"device_key"`
		Transport string `json:"transport"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	req.DeviceKey = strings.TrimSpace(req.DeviceKey)
	if !validMeshDeviceKey(req.DeviceKey) {
		writeErr(w, http.StatusBadRequest, "device_key is required and must be at most 256 characters")
		return
	}
	if req.Transport == "" {
		req.Transport = "internet"
	}
	if !validMeshTransport(req.Transport) {
		writeErr(w, http.StatusBadRequest, "unsupported mesh transport")
		return
	}
	// Enforce the admin operational policy (§110): a disabled mesh or a
	// deprecated transport is refused at admission.
	if gate := a.meshPolicyGate(r, req.Transport); gate != "" {
		writeErr(w, http.StatusForbidden, gate)
		return
	}
	subject := meshSubject(r)
	if subject == "" {
		writeErr(w, http.StatusUnauthorized, "mesh subject is missing")
		return
	}
	uid := subject
	if !isUUIDShape(uid) {
		uid = ""
	}
	var id string
	err := a.db.QueryRow(r.Context(),
		`INSERT INTO mesh_devices (user_id, owner_subject, device_key, transport, last_seen_at)
		 VALUES (NULLIF($1,'')::uuid, $2, $3, $4, now())
		 ON CONFLICT (device_key) DO UPDATE SET
		     owner_subject = EXCLUDED.owner_subject,
		     user_id = COALESCE(mesh_devices.user_id, EXCLUDED.user_id),
		     last_seen_at = now(),
		     transport = EXCLUDED.transport
		 WHERE mesh_devices.owner_subject = EXCLUDED.owner_subject
		 RETURNING id`, uid, subject, req.DeviceKey, req.Transport).Scan(&id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeErr(w, http.StatusForbidden, "device key belongs to another subject")
			return
		}
		writeErr(w, http.StatusInternalServerError, "failed to register mesh device")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"device_id": id, "status": "registered", "device_key": req.DeviceKey})
}

// ---- Store-and-forward packet submission ----

// handleMeshSend enqueues an encrypted packet for a destination device. The
// API never decrypts payload bytes; callers must submit an authenticated
// ciphertext envelope produced by the client mesh/E2E engine.
func (a *App) handleMeshSend(w http.ResponseWriter, r *http.Request) {
	var req struct {
		PacketID string `json:"packet_id"`
		DestKey  string `json:"dest_device_key"`
		Payload  []byte `json:"payload"`
		TTL      int    `json:"ttl"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	// Enforce the admin operational policy (§110): a disabled mesh or a
	// deprecated transport is refused, and payload/TTL ceilings are bounded
	// by policy rather than only by the hard-coded constants.
	if gate := a.meshPolicyGate(r, ""); gate != "" {
		writeErr(w, http.StatusForbidden, gate)
		return
	}
	maxPayload, maxTTL, err := a.meshPolicyLimits(r)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to read mesh policy")
		return
	}
	if len(req.PacketID) == 0 || len(req.PacketID) > 128 || strings.ContainsAny(req.PacketID, "\r\n\t") ||
		!validMeshDeviceKey(req.DestKey) || len(req.Payload) == 0 || int64(len(req.Payload)) > maxPayload {
		writeErr(w, http.StatusBadRequest, "invalid packet_id, destination or ciphertext payload")
		return
	}
	if req.TTL <= 0 || req.TTL > maxTTL {
		writeErr(w, http.StatusBadRequest, "ttl must be between 1 and the policy ceiling")
		return
	}
	subject := meshSubject(r)
	senderKey := meshDeviceHeader(r)
	if subject == "" || !validMeshDeviceKey(senderKey) {
		writeErr(w, http.StatusBadRequest, "X-Mesh-Device must identify a registered device")
		return
	}
	var senderID, destID string
	if err := a.db.QueryRow(r.Context(),
		`SELECT id FROM mesh_devices WHERE device_key=$1 AND owner_subject=$2`,
		senderKey, subject).Scan(&senderID); err != nil {
		writeErr(w, http.StatusForbidden, "sender device is not registered for this subject")
		return
	}
	destinationQuery := `SELECT id FROM mesh_devices WHERE device_key=$1 ORDER BY last_seen_at DESC LIMIT 1`
	if isUUIDShape(req.DestKey) {
		destinationQuery = `SELECT id FROM mesh_devices
		 WHERE device_key=$1 OR user_id=$1::uuid
		 ORDER BY last_seen_at DESC LIMIT 1`
	}
	if err := a.db.QueryRow(r.Context(), destinationQuery, req.DestKey).Scan(&destID); err != nil {
		writeErr(w, http.StatusNotFound, "destination device not found")
		return
	}
	var inserted string
	err = a.db.QueryRow(r.Context(),
		`INSERT INTO mesh_packets (packet_id, sender_device, dest_device, payload, ttl, state, expires_at)
		 VALUES ($1, $2, $3, $4, $5, 'queued', now() + interval '7 days')
		 ON CONFLICT (packet_id) DO NOTHING
		 RETURNING packet_id`, req.PacketID, senderID, destID, req.Payload, req.TTL).Scan(&inserted)
	if errors.Is(err, pgx.ErrNoRows) {
		writeJSON(w, http.StatusOK, map[string]any{"status": "duplicate", "packet_id": req.PacketID})
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to enqueue mesh packet")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "queued", "packet_id": inserted})
}

// ---- Packet retrieval (store-and-forward delivery) ----

// handleMeshPoll returns packets destined for the requesting device and marks
// them delivered in the same transaction. Row locks make concurrent polls
// consume each packet once rather than returning duplicate deliveries.
func (a *App) handleMeshPoll(w http.ResponseWriter, r *http.Request) {
	subject := meshSubject(r)
	deviceKey := meshDeviceHeader(r)
	if subject == "" || !validMeshDeviceKey(deviceKey) {
		writeErr(w, http.StatusBadRequest, "X-Mesh-Device must identify a registered device")
		return
	}
	var deviceID string
	if err := a.db.QueryRow(r.Context(),
		`SELECT id FROM mesh_devices WHERE device_key=$1 AND owner_subject=$2`,
		deviceKey, subject).Scan(&deviceID); err != nil {
		writeErr(w, http.StatusNotFound, "device not registered for this subject")
		return
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to start mesh poll")
		return
	}
	defer tx.Rollback(r.Context())
	type pkt struct {
		PacketID  string `json:"packet_id"`
		Payload   []byte `json:"payload"`
		TTL       int    `json:"ttl"`
		Hops      int    `json:"hops"`
		CreatedAt string `json:"created_at"`
	}
	rows, err := tx.Query(r.Context(),
		`SELECT packet_id, payload, ttl, hops, created_at
		 FROM mesh_packets
		 WHERE dest_device=$1 AND state IN ('queued','relaying') AND expires_at > now()
		 ORDER BY created_at ASC LIMIT 100 FOR UPDATE SKIP LOCKED`, deviceID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to poll mesh packets")
		return
	}
	out := []pkt{}
	for rows.Next() {
		var p pkt
		var created time.Time
		if err := rows.Scan(&p.PacketID, &p.Payload, &p.TTL, &p.Hops, &created); err != nil {
			rows.Close()
			writeErr(w, http.StatusInternalServerError, "failed to decode mesh packet")
			return
		}
		p.CreatedAt = created.Format(time.RFC3339)
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		writeErr(w, http.StatusInternalServerError, "failed to read mesh packets")
		return
	}
	rows.Close()
	for _, p := range out {
		if _, err := tx.Exec(r.Context(),
			`UPDATE mesh_packets SET state='delivered', relay_device=NULL, delivered_at=now()
			 WHERE packet_id=$1 AND dest_device=$2 AND state IN ('queued','relaying')`,
			p.PacketID, deviceID); err != nil {
			writeErr(w, http.StatusInternalServerError, "failed to commit mesh delivery")
			return
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to commit mesh poll")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"packets": out})
}

// ---- Relay forwarding ----

// handleMeshRelay forwards a packet one hop toward its destination. Only a
// registered device owned by the caller may perform the relay, and the relay
// must have explicitly enabled relay consent. The transition is transactional
// so two concurrent relays cannot both consume the same queued packet.
func (a *App) handleMeshRelay(w http.ResponseWriter, r *http.Request) {
	var req struct {
		PacketID string `json:"packet_id"`
		NextKey  string `json:"next_device_key"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if len(req.PacketID) == 0 || len(req.PacketID) > 128 || !validMeshDeviceKey(req.NextKey) {
		writeErr(w, http.StatusBadRequest, "packet_id and next_device_key are required")
		return
	}
	subject := meshSubject(r)
	relayKey := meshDeviceHeader(r)
	if subject == "" || !validMeshDeviceKey(relayKey) {
		writeErr(w, http.StatusBadRequest, "X-Mesh-Device must identify the relay device")
		return
	}
	// Enforce the admin operational policy (§110): a disabled mesh refuses
	// relay admission.
	if gate := a.meshPolicyGate(r, ""); gate != "" {
		writeErr(w, http.StatusForbidden, gate)
		return
	}
	var relayID string
	var relayQuota int64
	if err := a.db.QueryRow(r.Context(),
		`SELECT d.id, p.storage_quota FROM mesh_devices d
		 JOIN mesh_relay_policy p ON p.device_id=d.id
		 WHERE d.device_key=$1 AND d.owner_subject=$2 AND p.relay_enabled=true
		   AND p.relay_mode IN ('active','charging','approved')`, relayKey, subject).Scan(&relayID, &relayQuota); err != nil {
		writeErr(w, http.StatusForbidden, "relay consent is not enabled for this device")
		return
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to start mesh relay")
		return
	}
	defer tx.Rollback(r.Context())
	var ttl, hops int
	var payloadBytes int64
	if err := tx.QueryRow(r.Context(),
		`SELECT ttl, hops, octet_length(payload) FROM mesh_packets
		 WHERE packet_id=$1 AND state IN ('queued','relaying')
		 FOR UPDATE`, req.PacketID).Scan(&ttl, &hops, &payloadBytes); err != nil {
		writeErr(w, http.StatusNotFound, "packet not found or not queued")
		return
	}
	if ttl <= 1 {
		if _, err := tx.Exec(r.Context(),
			`UPDATE mesh_packets SET state='expired' WHERE packet_id=$1`, req.PacketID); err != nil {
			writeErr(w, http.StatusInternalServerError, "failed to expire mesh packet")
			return
		}
		if err := tx.Commit(r.Context()); err != nil {
			writeErr(w, http.StatusInternalServerError, "failed to commit mesh relay")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "expired", "packet_id": req.PacketID})
		return
	}
	var heldBytes int64
	if err := tx.QueryRow(r.Context(),
		`SELECT COALESCE(SUM(octet_length(payload)), 0) FROM mesh_packets
		 WHERE relay_device=$1 AND state IN ('queued','relaying') AND packet_id <> $2`,
		relayID, req.PacketID).Scan(&heldBytes); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to read relay quota")
		return
	}
	if heldBytes+payloadBytes > relayQuota {
		writeErr(w, http.StatusTooManyRequests, "relay storage quota exceeded")
		return
	}
	var nextID string
	if err := tx.QueryRow(r.Context(),
		`SELECT id FROM mesh_devices WHERE device_key=$1`, req.NextKey).Scan(&nextID); err != nil {
		writeErr(w, http.StatusNotFound, "next device not found")
		return
	}
	if nextID == relayID {
		writeErr(w, http.StatusBadRequest, "next device must differ from relay device")
		return
	}
	if _, err := tx.Exec(r.Context(),
		`UPDATE mesh_packets SET dest_device=$1, relay_device=$2, ttl=ttl-1, hops=hops+1, state='queued'
		 WHERE packet_id=$3`, nextID, relayID, req.PacketID); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to relay packet")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to commit mesh relay")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "relayed", "packet_id": req.PacketID, "ttl": ttl - 1, "hops": hops + 1, "relay_device_id": relayID})
}

// ---- Relay policy ----

// handleMeshRelayPolicy sets a device's relay consent / resource policy.
// Device ownership is checked against the authenticated subject; a caller
// cannot enable relaying or enlarge another device's storage quota.
func (a *App) handleMeshRelayPolicy(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DeviceKey    string `json:"device_key"`
		RelayEnabled bool   `json:"relay_enabled"`
		RelayMode    string `json:"relay_mode"`
		StorageQuota int64  `json:"storage_quota"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	subject := meshSubject(r)
	headerKey := meshDeviceHeader(r)
	if subject == "" || !validMeshDeviceKey(headerKey) {
		writeErr(w, http.StatusBadRequest, "X-Mesh-Device must identify the device")
		return
	}
	if req.DeviceKey == "" {
		req.DeviceKey = headerKey
	}
	if req.DeviceKey != headerKey || !validMeshDeviceKey(req.DeviceKey) {
		writeErr(w, http.StatusForbidden, "device key does not match the authenticated device")
		return
	}
	if req.RelayMode == "" {
		req.RelayMode = "off"
	}
	switch req.RelayMode {
	case "off", "active", "charging", "approved":
	default:
		writeErr(w, http.StatusBadRequest, "invalid relay_mode")
		return
	}
	if !req.RelayEnabled {
		req.RelayMode = "off"
	}
	if req.StorageQuota == 0 {
		req.StorageQuota = meshDefaultQuota
	}
	if req.StorageQuota < 1<<20 || req.StorageQuota > meshMaxQuota {
		writeErr(w, http.StatusBadRequest, "storage_quota must be between 1 MiB and 1 GiB")
		return
	}
	var deviceID string
	if err := a.db.QueryRow(r.Context(),
		`SELECT id FROM mesh_devices WHERE device_key=$1 AND owner_subject=$2`,
		req.DeviceKey, subject).Scan(&deviceID); err != nil {
		writeErr(w, http.StatusForbidden, "device not registered for this subject")
		return
	}
	if _, err := a.db.Exec(r.Context(),
		`INSERT INTO mesh_relay_policy (device_id, relay_enabled, relay_mode, storage_quota, updated_at)
		 VALUES ($1, $2, $3, $4, now())
		 ON CONFLICT (device_id) DO UPDATE SET
		   relay_enabled=EXCLUDED.relay_enabled,
		   relay_mode=EXCLUDED.relay_mode,
		   storage_quota=EXCLUDED.storage_quota,
		   updated_at=now()`,
		deviceID, req.RelayEnabled, req.RelayMode, req.StorageQuota); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to set relay policy")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "policy_updated", "device_id": deviceID})
}

// ---- Mesh status / health ----

func (a *App) handleMeshStatus(w http.ResponseWriter, r *http.Request) {
	subject := meshSubject(r)
	if subject == "" {
		writeErr(w, http.StatusUnauthorized, "mesh subject is missing")
		return
	}
	var queued, delivered, expired int
	queries := []struct {
		state string
		out   *int
	}{
		{"queued", &queued},
		{"delivered", &delivered},
		{"expired", &expired},
	}
	for _, q := range queries {
		if err := a.db.QueryRow(r.Context(),
			`SELECT count(*) FROM mesh_packets p
			 JOIN mesh_devices d ON d.id=p.dest_device
			 WHERE d.owner_subject=$1 AND p.state=$2`, subject, q.state).Scan(q.out); err != nil {
			writeErr(w, http.StatusInternalServerError, "failed to read mesh status")
			return
		}
	}
	resp := map[string]any{
		"mesh":      "enabled",
		"queued":    queued,
		"delivered": delivered,
		"expired":   expired,
	}
	for k, v := range a.meshStatus() {
		resp[k] = v
	}
	writeJSON(w, http.StatusOK, resp)
}
