package main

// Bridge from the Go control plane to the C++ realtime relay
// (services/realtime). When REALTIME_RELAY_URL is set, every fanout payload
// is also POSTed to the relay's control plane so clients attached to the C++
// edge receive the same events as clients on the Go hub. Delivery is
// fire-and-forget with a bounded queue: the Go hub remains authoritative for
// local connections and message persistence is unaffected by relay health.

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"time"
)

type relayPublish struct {
	UserIDs []string        `json:"user_ids"`
	Payload json.RawMessage `json:"payload"`
}

var relayQueue = make(chan relayPublish, 4096)

// startRelayBridge drains relayQueue and posts to the relay control plane.
func (a *App) startRelayBridge() {
	if a.cfg.RelayURL == "" || a.cfg.ClusterSecret == "" {
		return
	}
	go func() {
		client := &http.Client{Timeout: 3 * time.Second}
		for pub := range relayQueue {
			body, err := json.Marshal(pub)
			if err != nil {
				continue
			}
			req, err := http.NewRequest(http.MethodPost, a.cfg.RelayURL+"/publish", bytes.NewReader(body))
			if err != nil {
				continue
			}
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+a.cfg.ClusterSecret)
			resp, err := client.Do(req)
			if err != nil {
				continue
			}
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 256))
			resp.Body.Close()
		}
	}()
}

// publishRelay queues a fanout for the C++ edge (non-blocking).
func publishRelay(userIDs []string, payload []byte) {
	select {
	case relayQueue <- relayPublish{UserIDs: userIDs, Payload: json.RawMessage(payload)}:
	default: // relay backpressure: local hub delivery already happened
	}
}

// fanoutUsers delivers a payload to a set of users via the local hub and the
// C++ relay edge.
func (a *App) fanoutUsers(userIDs []string, payload []byte) {
	for _, uid := range userIDs {
		a.hub.sendTo(uid, payload)
	}
	publishRelay(userIDs, payload)
}
