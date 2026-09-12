package mesh

// main.go — package doc and exported API surface for the offline multi-hop
// device mesh transport engine.
//
// This package is a self-contained, dependency-light Go engine that powers
// the offline mesh on every client. It provides:
//
//   - Authenticated encryption (NaCl secretbox / XChaCha20-Poly1305)
//   - Local Wi-Fi / hotspot UDP transport with presence-beacon discovery
//   - Multi-hop routing with TTL and duplicate suppression
//   - Delay-tolerant store-and-forward queue
//   - 1:1 messages, group messages, voice messages, and call signaling
//
// Native Bluetooth and Wi-Fi Direct adapters (Android/iOS) implement the
// Transport interface and feed packets into this same engine, so the mesh
// works with no internet at all. The API service's Postgres-backed
// store-and-forward queue (handlers_mesh.go) remains the authoritative
// internet fallback; this engine is the local, offline-first path.
//
// Example:
//
//	key, _ := mesh.NewIdentityKey()
//	tr, _ := mesh.NewUDPTransport(0, nil)
//	node := mesh.NewNode(mesh.NodeConfig{
//	    DeviceID: "dev-1", Key: &key, Transport: tr,
//	    Handler: func(p *mesh.Packet, pt []byte) { /* deliver */ },
//	})
//	node.Start()
//	defer node.Stop()
//	node.Send(mesh.KindMessage, "dev-2", []byte("hello"))
