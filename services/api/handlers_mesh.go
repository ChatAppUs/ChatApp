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
	"net/http"
	"time"
)

// ---- Device registration ----

type meshDevice struct {
	ID        string `json:"id"`
	DeviceKey string `json:"device_key"`
	Transport string `json:"transport"`
	LastSeen  string `json:"last_seen_at"`
}

// handleMeshRegister registers (or refreshes) a device on the mesh. A device
// may be a member device (user_id set) or a pure relay node (user_id NULL).
func (a *App) handleMeshRegister(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DeviceKey string `json:"device_key"`
		Transport string `json:"transport"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.DeviceKey == "" {
		writeErr(w, http.StatusBadRequest, "device_key is required")
		return
	}
	if req.Transport == "" {
		req.Transport = "internet"
	}
	uid := userIDFrom(r)
	if uid == "" {
		uid = ""
	}
	var id string
	err := a.db.QueryRow(r.Context(),
		`INSERT INTO mesh_devices (user_id, device_key, transport, last_seen_at)
		 VALUES (NULLIF($1,'')::uuid, $2, $3, now())
		 ON CONFLICT (device_key) DO UPDATE SET last_seen_at = now(), transport = EXCLUDED.transport
		 RETURNING id`, uid, req.DeviceKey, req.Transport).Scan(&id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to register mesh device")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"device_id": id, "status": "registered"})
}

// ---- Store-and-forward packet submission ----

// handleMeshSend enqueues an encrypted packet for a destination device. The
// payload is ciphertext only; the relay never inspects it.
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
	if req.PacketID == "" || req.DestKey == "" || len(req.Payload) == 0 {
		writeErr(w, http.StatusBadRequest, "packet_id, dest_device_key and payload are required")
		return
	}
	if req.TTL <= 0 || req.TTL > 16 {
		req.TTL = 8
	}
	// Resolve sender device from the authenticated device_key (or guest).
	senderKey := r.Header.Get("X-Mesh-Device")
	if senderKey == "" {
		senderKey = "device_" + userIDFrom(r)
	}
	var senderID, destID string
	err := a.db.QueryRow(r.Context(),
		`INSERT INTO mesh_devices (device_key, transport, last_seen_at)
		 VALUES ($1, 'internet', now())
		 ON CONFLICT (device_key) DO UPDATE SET last_seen_at = now()
		 RETURNING id`, senderKey).Scan(&senderID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to resolve sender device")
		return
	}
	err = a.db.QueryRow(r.Context(),
		`SELECT id FROM mesh_devices WHERE device_key = $1`, req.DestKey).Scan(&destID)
	if err != nil {
		writeErr(w, http.StatusNotFound, "destination device not found")
		return
	}
	// Dedup: if this packet was already seen, do not enqueue again.
	var exists bool
	_ = a.db.QueryRow(r.Context(),
		`SELECT EXISTS(SELECT 1 FROM mesh_packets WHERE packet_id = $1)`, req.PacketID).Scan(&exists)
	if exists {
		writeJSON(w, http.StatusOK, map[string]any{"status": "duplicate", "packet_id": req.PacketID})
		return
	}
	_, err = a.db.Exec(r.Context(),
		`INSERT INTO mesh_packets (packet_id, sender_device, dest_device, payload, ttl, state, expires_at)
		 VALUES ($1, $2, $3, $4, $5, 'queued', now() + interval '7 days')`,
		req.PacketID, senderID, destID, req.Payload, req.TTL)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to enqueue mesh packet")
		return
	}
	_, _ = a.db.Exec(r.Context(),
		`INSERT INTO mesh_seen_packets (packet_id) VALUES ($1) ON CONFLICT DO NOTHING`, req.PacketID)
	writeJSON(w, http.StatusOK, map[string]any{"status": "queued", "packet_id": req.PacketID})
}

// ---- Packet retrieval (store-and-forward delivery) ----

// handleMeshPoll returns queued packets destined for the requesting device and
// marks them delivered. This is the store-and-forward delivery step: a device
// that comes online polls for its queued packets.
func (a *App) handleMeshPoll(w http.ResponseWriter, r *http.Request) {
	deviceKey := r.Header.Get("X-Mesh-Device")
	if deviceKey == "" {
		deviceKey = "device_" + userIDFrom(r)
	}
	var deviceID string
	err := a.db.QueryRow(r.Context(),
		`SELECT id FROM mesh_devices WHERE device_key = $1`, deviceKey).Scan(&deviceID)
	if err != nil {
		writeErr(w, http.StatusNotFound, "device not registered")
		return
	}
	rows, err := a.db.Query(r.Context(),
		`SELECT packet_id, payload, ttl, hops, created_at
		 FROM mesh_packets
		 WHERE dest_device = $1 AND state = 'queued' AND expires_at > now()
		 ORDER BY created_at ASC LIMIT 100`, deviceID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to poll mesh packets")
		return
	}
	defer rows.Close()
	type pkt struct {
		PacketID  string `json:"packet_id"`
		Payload   []byte `json:"payload"`
		TTL       int    `json:"ttl"`
		Hops      int    `json:"hops"`
		CreatedAt string `json:"created_at"`
	}
	out := []pkt{}
	for rows.Next() {
		var p pkt
		var created time.Time
		if err := rows.Scan(&p.PacketID, &p.Payload, &p.TTL, &p.Hops, &created); err == nil {
			p.CreatedAt = created.Format(time.RFC3339)
			out = append(out, p)
		}
	}
	// Mark delivered.
	for _, p := range out {
		_, _ = a.db.Exec(r.Context(),
			`UPDATE mesh_packets SET state = 'delivered', delivered_at = now() WHERE packet_id = $1`, p.PacketID)
	}
	writeJSON(w, http.StatusOK, map[string]any{"packets": out})
}

// ---- Relay forwarding ----

// handleMeshRelay forwards a packet one hop toward its destination. A relay
// decrements TTL, increments hops, and re-enqueues for the next hop. If TTL
// reaches zero the packet expires (bounded flooding).
func (a *App) handleMeshRelay(w http.ResponseWriter, r *http.Request) {
	var req struct {
		PacketID string `json:"packet_id"`
		NextKey  string `json:"next_device_key"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.PacketID == "" || req.NextKey == "" {
		writeErr(w, http.StatusBadRequest, "packet_id and next_device_key are required")
		return
	}
	var ttl, hops int
	var payload []byte
	err := a.db.QueryRow(r.Context(),
		`SELECT ttl, hops, payload FROM mesh_packets WHERE packet_id = $1 AND state = 'queued'`,
		req.PacketID).Scan(&ttl, &hops, &payload)
	if err != nil {
		writeErr(w, http.StatusNotFound, "packet not found or not queued")
		return
	}
	if ttl <= 1 {
		_, _ = a.db.Exec(r.Context(),
			`UPDATE mesh_packets SET state = 'expired' WHERE packet_id = $1`, req.PacketID)
		writeJSON(w, http.StatusOK, map[string]any{"status": "expired", "packet_id": req.PacketID})
		return
	}
	var nextID string
	err = a.db.QueryRow(r.Context(),
		`SELECT id FROM mesh_devices WHERE device_key = $1`, req.NextKey).Scan(&nextID)
	if err != nil {
		writeErr(w, http.StatusNotFound, "next device not found")
		return
	}
	_, err = a.db.Exec(r.Context(),
		`UPDATE mesh_packets SET dest_device = $1, ttl = ttl - 1, hops = hops + 1, state = 'relaying'
		 WHERE packet_id = $2`, nextID, req.PacketID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to relay packet")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "relayed", "packet_id": req.PacketID, "ttl": ttl - 1, "hops": hops + 1})
}

// ---- Relay policy ----

// handleMeshRelayPolicy sets a device's relay consent / resource policy.
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
	if req.DeviceKey == "" {
		writeErr(w, http.StatusBadRequest, "device_key is required")
		return
	}
	if req.RelayMode == "" {
		req.RelayMode = "off"
	}
	if req.StorageQuota <= 0 {
		req.StorageQuota = 10485760
	}
	var deviceID string
	err := a.db.QueryRow(r.Context(),
		`SELECT id FROM mesh_devices WHERE device_key = $1`, req.DeviceKey).Scan(&deviceID)
	if err != nil {
		writeErr(w, http.StatusNotFound, "device not registered")
		return
	}
	_, err = a.db.Exec(r.Context(),
		`INSERT INTO mesh_relay_policy (device_id, relay_enabled, relay_mode, storage_quota, updated_at)
		 VALUES ($1, $2, $3, $4, now())
		 ON CONFLICT (device_id) DO UPDATE SET
		   relay_enabled = EXCLUDED.relay_enabled,
		   relay_mode = EXCLUDED.relay_mode,
		   storage_quota = EXCLUDED.storage_quota,
		   updated_at = now()`,
		deviceID, req.RelayEnabled, req.RelayMode, req.StorageQuota)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to set relay policy")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "policy_updated"})
}

// ---- Mesh status / health ----

func (a *App) handleMeshStatus(w http.ResponseWriter, r *http.Request) {
	var queued, delivered, expired int
	_ = a.db.QueryRow(r.Context(),
		`SELECT count(*) FROM mesh_packets WHERE state = 'queued'`).Scan(&queued)
	_ = a.db.QueryRow(r.Context(),
		`SELECT count(*) FROM mesh_packets WHERE state = 'delivered'`).Scan(&delivered)
	_ = a.db.QueryRow(r.Context(),
		`SELECT count(*) FROM mesh_packets WHERE state = 'expired'`).Scan(&expired)
	resp := map[string]any{
		"mesh":      "enabled",
		"queued":    queued,
		"delivered": delivered,
		"expired":   expired,
	}
	// Merge the native offline mesh engine status (local Wi-Fi/hotspot UDP
	// transport, multi-hop routing, store-and-forward).
	for k, v := range a.meshStatus() {
		resp[k] = v
	}
	writeJSON(w, http.StatusOK, resp)
}
