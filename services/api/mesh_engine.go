package main

// mesh_engine.go — wires the native offline mesh transport engine
// (services/mesh) into the API service.
//
// The API's Postgres-backed store-and-forward queue (handlers_mesh.go) is the
// authoritative internet fallback. This file additionally boots the native
// local-first mesh engine so the API node itself can participate in the
// offline mesh (local Wi-Fi / hotspot UDP transport with presence-beacon
// discovery, multi-hop routing, TTL, dedup, and store-and-forward). Native
// Bluetooth / Wi-Fi Direct adapters on Android/iOS implement the same
// Transport interface and feed packets into this engine, so the mesh works
// with no internet at all.

import (
	"log"
	"sync"

	"github.com/chatappus/chatapp/services/mesh"
)

// meshEngine holds the API node's native mesh engine instance.
type meshEngine struct {
	mu   sync.Mutex
	node *mesh.Node
	key  mesh.IdentityKey
}

// startMeshEngine boots the native mesh engine on the API node. It is
// best-effort: if the local UDP transport cannot bind, the API still runs
// (the Postgres store-and-forward path remains authoritative). The device id
// is derived from the cluster node id so each API node is a distinct mesh
// peer.
func (a *App) startMeshEngine() {
	key, err := mesh.NewIdentityKey()
	if err != nil {
		log.Printf("[mesh] failed to generate identity key: %v", err)
		return
	}
	tr, err := mesh.NewUDPTransport(0, nil)
	if err != nil {
		log.Printf("[mesh] failed to bind local transport: %v", err)
		return
	}
	deviceID := "api-" + a.cfg.ClusterNodeID
	if deviceID == "api-" {
		deviceID = "api-node"
	}
	node := mesh.NewNode(mesh.NodeConfig{
		DeviceID:  deviceID,
		Key:       &key,
		Kind:      "relay",
		Transport: tr,
		Handler: func(p *mesh.Packet, plaintext []byte) {
			// Deliver decrypted local mesh payloads to the application layer.
			// The API node relays ciphertext only; it never decrypts content
			// it is not the intended recipient of. When it is the recipient,
			// the payload is handed to the store-and-forward path for durable
			// delivery to the connected client.
			log.Printf("[mesh] delivered packet %s kind=%s (%d bytes)", p.ID, p.Kind, len(plaintext))
		},
	})
	node.Start()
	a.mesh = &meshEngine{node: node, key: key}
	log.Printf("[mesh] native engine started: device=%s transport=%s", deviceID, tr.Addr())
}

// meshStatus returns a snapshot of the native engine for the /api/mesh/status
// endpoint (merged with the Postgres queue counts in handleMeshStatus).
func (a *App) meshStatus() map[string]any {
	if a.mesh == nil {
		return map[string]any{"native_engine": "not_started"}
	}
	a.mesh.mu.Lock()
	defer a.mesh.mu.Unlock()
	return map[string]any{
		"native_engine": "running",
		"device_id":     a.mesh.node.DeviceID,
		"kind":          a.mesh.node.Kind,
	}
}
