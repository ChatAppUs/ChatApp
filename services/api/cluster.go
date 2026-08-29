package main

import (
	"context"
	"encoding/json"
	"hash/fnv"
	"log"
	"net/http"
	"sort"
	"time"
)

// Cluster engine: node registry, health tracking, region routing, and
// rendezvous (HRW) sharding so the platform scales horizontally across
// regions. Single-node deployments behave exactly as before — the engine
// activates when CLUSTER_NODE_ID/CLUSTER_REGION are set or when sibling
// nodes heartbeat in.

type ClusterNode struct {
	NodeID    string    `json:"node_id"`
	Region    string    `json:"region"`
	APIURL    string    `json:"api_url"`
	MediaURL  string    `json:"media_url"`
	Weight    int       `json:"weight"`
	Capacity  int       `json:"capacity"`
	Load      int       `json:"load"`
	Status    string    `json:"status"`
	LastSeen  time.Time `json:"last_seen_at"`
	CreatedAt time.Time `json:"created_at"`
}

const clusterNodeTTL = 30 * time.Second

// startCluster launches self-registration heartbeat and the dead-node reaper.
func (a *App) startCluster() {
	if a.cfg.ClusterNodeID == "" {
		return
	}
	go func() {
		a.selfHeartbeat()
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			a.selfHeartbeat()
			a.reapDeadNodes()
		}
	}()
	log.Printf("cluster: node %s in region %s", a.cfg.ClusterNodeID, a.cfg.ClusterRegion)
}

func (a *App) selfHeartbeat() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := a.db.Exec(ctx,
		`INSERT INTO cluster_nodes (node_id, region, api_url, media_url, load, status, last_seen_at)
		 VALUES ($1,$2,$3,$4,$5,'active', now())
		 ON CONFLICT (node_id) DO UPDATE
		 SET region=$2, api_url=$3, media_url=$4, load=$5, status='active', last_seen_at=now()`,
		a.cfg.ClusterNodeID, a.cfg.ClusterRegion, a.cfg.ClusterAPIURL,
		a.cfg.ClusterMediaURL, a.hub.connCount())
	if err != nil {
		log.Printf("cluster heartbeat: %v", err)
	}
}

func (a *App) reapDeadNodes() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _ = a.db.Exec(ctx,
		`UPDATE cluster_nodes SET status='down'
		 WHERE status <> 'down' AND last_seen_at < now() - interval '30 seconds'`)
}

// healthyNodes returns active nodes, freshest first.
func (a *App) healthyNodes(ctx context.Context) ([]ClusterNode, error) {
	rows, err := a.db.Query(ctx,
		`SELECT node_id, region, api_url, media_url, weight, capacity, load, status, last_seen_at, created_at
		 FROM cluster_nodes
		 WHERE status='active' AND last_seen_at > now() - interval '30 seconds'
		 ORDER BY last_seen_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ClusterNode
	for rows.Next() {
		var n ClusterNode
		if err := rows.Scan(&n.NodeID, &n.Region, &n.APIURL, &n.MediaURL,
			&n.Weight, &n.Capacity, &n.Load, &n.Status, &n.LastSeen, &n.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// rendezvousPick assigns key to a node via highest-random-weight hashing,
// weighted by node weight × spare capacity. Adding/removing a node remaps
// only ~1/N of keys.
func rendezvousPick(nodes []ClusterNode, key string) *ClusterNode {
	var best *ClusterNode
	var bestScore uint64
	for i := range nodes {
		n := &nodes[i]
		h := fnv.New64a()
		_, _ = h.Write([]byte(n.NodeID + "\x00" + key))
		score := h.Sum64()
		// scale by weight and spare capacity fraction
		spare := uint64(n.Capacity - n.Load)
		if spare == 0 {
			continue
		}
		score = score / 2 * uint64(n.Weight) / 100 * spare / uint64(n.Capacity)
		if best == nil || score > bestScore {
			best, bestScore = n, score
		}
	}
	return best
}

// pickRegionNode returns the least-loaded healthy node in region, falling
// back to the globally least-loaded node.
func pickRegionNode(nodes []ClusterNode, region string) *ClusterNode {
	pool := []ClusterNode{}
	for _, n := range nodes {
		if n.Region == region {
			pool = append(pool, n)
		}
	}
	if len(pool) == 0 {
		pool = nodes
	}
	if len(pool) == 0 {
		return nil
	}
	sort.Slice(pool, func(i, j int) bool {
		li := float64(pool[i].Load) / float64(max(pool[i].Capacity, 1))
		lj := float64(pool[j].Load) / float64(max(pool[j].Capacity, 1))
		return li < lj
	})
	return &pool[0]
}

// ---------- HTTP handlers ----------

// POST /api/cluster/heartbeat — sibling nodes register/update themselves.
// Authenticated by a shared cluster secret, not user JWT.
func (a *App) handleClusterHeartbeat(w http.ResponseWriter, r *http.Request) {
	if a.cfg.ClusterSecret == "" || r.Header.Get("X-Cluster-Secret") != a.cfg.ClusterSecret {
		writeErr(w, http.StatusForbidden, "invalid cluster secret")
		return
	}
	var req struct {
		NodeID   string `json:"node_id"`
		Region   string `json:"region"`
		APIURL   string `json:"api_url"`
		MediaURL string `json:"media_url"`
		Weight   int    `json:"weight"`
		Capacity int    `json:"capacity"`
		Load     int    `json:"load"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil ||
		req.NodeID == "" || req.Region == "" || req.APIURL == "" {
		writeErr(w, http.StatusBadRequest, "node_id, region, api_url required")
		return
	}
	if req.Weight <= 0 {
		req.Weight = 100
	}
	if req.Capacity <= 0 {
		req.Capacity = 10000
	}
	if _, err := a.db.Exec(r.Context(),
		`INSERT INTO cluster_nodes (node_id, region, api_url, media_url, weight, capacity, load, status, last_seen_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,'active', now())
		 ON CONFLICT (node_id) DO UPDATE
		 SET region=$2, api_url=$3, media_url=$4, weight=$5, capacity=$6, load=$7,
		     status='active', last_seen_at=now()`,
		req.NodeID, req.Region, req.APIURL, req.MediaURL, req.Weight, req.Capacity, req.Load); err != nil {
		writeErr(w, http.StatusInternalServerError, "heartbeat failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// GET /api/cluster/route?region=xx&key=conversation-id — client bootstrap:
// which node should this client talk to.
func (a *App) handleClusterRoute(w http.ResponseWriter, r *http.Request) {
	nodes, err := a.healthyNodes(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "cluster unavailable")
		return
	}
	if len(nodes) == 0 {
		// single-node deployment: route to self
		writeJSON(w, http.StatusOK, map[string]any{
			"node_id": a.cfg.ClusterNodeID, "region": a.cfg.ClusterRegion,
			"api_url": a.cfg.ClusterAPIURL, "media_url": a.cfg.ClusterMediaURL,
			"shard_of": "", "nodes": 0,
		})
		return
	}
	region := r.URL.Query().Get("region")
	key := r.URL.Query().Get("key")
	var node *ClusterNode
	if key != "" {
		node = rendezvousPick(nodes, key)
	} else {
		node = pickRegionNode(nodes, region)
	}
	if node == nil {
		writeErr(w, http.StatusServiceUnavailable, "no capacity")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"node_id": node.NodeID, "region": node.Region,
		"api_url": node.APIURL, "media_url": node.MediaURL,
		"shard_of": key, "nodes": len(nodes),
	})
}

// GET /api/cluster/nodes — admin view of the fleet.
func (a *App) handleClusterNodes(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Query(r.Context(),
		`SELECT node_id, region, api_url, media_url, weight, capacity, load, status, last_seen_at, created_at
		 FROM cluster_nodes ORDER BY region, node_id`)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "query failed")
		return
	}
	defer rows.Close()
	nodes := []ClusterNode{}
	for rows.Next() {
		var n ClusterNode
		if err := rows.Scan(&n.NodeID, &n.Region, &n.APIURL, &n.MediaURL,
			&n.Weight, &n.Capacity, &n.Load, &n.Status, &n.LastSeen, &n.CreatedAt); err != nil {
			writeErr(w, http.StatusInternalServerError, "scan failed")
			return
		}
		nodes = append(nodes, n)
	}
	writeJSON(w, http.StatusOK, map[string]any{"nodes": nodes})
}

// POST /api/cluster/nodes/{id}/drain — stop routing new traffic to a node.
func (a *App) handleClusterDrain(w http.ResponseWriter, r *http.Request) {
	res, err := a.db.Exec(r.Context(),
		`UPDATE cluster_nodes SET status='draining' WHERE node_id=$1`, r.PathValue("id"))
	if err != nil || res.RowsAffected() == 0 {
		writeErr(w, http.StatusNotFound, "no such node")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// DELETE /api/cluster/nodes/{id} — remove a node from the registry.
func (a *App) handleClusterRemove(w http.ResponseWriter, r *http.Request) {
	res, err := a.db.Exec(r.Context(),
		`DELETE FROM cluster_nodes WHERE node_id=$1`, r.PathValue("id"))
	if err != nil || res.RowsAffected() == 0 {
		writeErr(w, http.StatusNotFound, "no such node")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
