package main

// Push notifications: self-built Web Push delivery (RFC 8291 message
// encryption + RFC 8292 VAPID) plus native device-token routing
// (FCM/APNs gateways). Subscriptions are stored in push_subscriptions,
// outbound work flows through push_queue and is drained by a SKIP LOCKED
// worker so multiple API nodes can share delivery.

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ---- VAPID key management (RFC 8292) ----

func b64urlRaw(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

// vapidKeypair returns the configured VAPID P-256 keypair. In development a
// fresh keypair is generated at boot when VAPID_PRIVATE_KEY is unset;
// production deployments must provide a stable key (config enforces it).
func (a *App) vapidKeypair() (*ecdsa.PrivateKey, error) {
	if a.vapidKey != nil {
		return a.vapidKey, nil
	}
	if a.cfg.VAPIDPrivateKey == "" {
		if a.cfg.AppEnv == "production" {
			return nil, errors.New("VAPID_PRIVATE_KEY is required in production")
		}
		k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			return nil, err
		}
		a.vapidKey = k
		return k, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(a.cfg.VAPIDPrivateKey)
	if err != nil || len(raw) != 32 {
		return nil, errors.New("VAPID_PRIVATE_KEY must be base64url 32-byte P-256 scalar")
	}
	d := new(big.Int).SetBytes(raw)
	priv := &ecdsa.PrivateKey{PublicKey: ecdsa.PublicKey{Curve: elliptic.P256()}, D: d}
	priv.PublicKey.X, priv.PublicKey.Y = elliptic.P256().ScalarBaseMult(raw)
	a.vapidKey = priv
	return priv, nil
}

func (a *App) vapidPublicKeyB64() (string, error) {
	k, err := a.vapidKeypair()
	if err != nil {
		return "", err
	}
	pub := elliptic.Marshal(elliptic.P256(), k.PublicKey.X, k.PublicKey.Y)
	return b64urlRaw(pub), nil
}

// vapidJWT signs a VAPID token (ES256) for the push endpoint's origin.
func (a *App) vapidJWT(endpoint string) (string, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return "", err
	}
	aud := u.Scheme + "://" + u.Host
	now := time.Now()
	header := b64urlRaw([]byte(`{"typ":"JWT","alg":"ES256"}`))
	claims, _ := json.Marshal(map[string]any{
		"aud": aud,
		"exp": now.Add(12 * time.Hour).Unix(),
		"sub": a.cfg.VAPIDSubject,
	})
	body := header + "." + b64urlRaw(claims)
	digest := sha256.Sum256([]byte(body))
	k, err := a.vapidKeypair()
	if err != nil {
		return "", err
	}
	rSig, sSig, err := ecdsa.Sign(rand.Reader, k, digest[:])
	if err != nil {
		return "", err
	}
	sig := make([]byte, 64)
	rSig.FillBytes(sig[:32])
	sSig.FillBytes(sig[32:])
	return body + "." + b64urlRaw(sig), nil
}

// ---- RFC 8291 aes128gcm content encryption ----

func hkdfExpand(prk []byte, info string, n int) ([]byte, error) {
	return hkdf.Expand(sha256.New, prk, info, n)
}

func hkdfExtract(salt, ikm []byte) ([]byte, error) {
	return hkdf.Extract(sha256.New, ikm, salt)
}

// encryptWebPush encrypts payload for a subscription per RFC 8291/8188
// (single record, aes128gcm content coding).
func encryptWebPush(p256dhB64, authB64 string, payload []byte) ([]byte, error) {
	uaPubRaw, err := base64.RawURLEncoding.DecodeString(p256dhB64)
	if err != nil || len(uaPubRaw) != 65 {
		return nil, errors.New("invalid p256dh key")
	}
	auth, err := base64.RawURLEncoding.DecodeString(authB64)
	if err != nil || len(auth) < 16 {
		return nil, errors.New("invalid auth secret")
	}
	uaPub, err := ecdh.P256().NewPublicKey(uaPubRaw)
	if err != nil {
		return nil, err
	}
	asPriv, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	shared, err := asPriv.ECDH(uaPub)
	if err != nil {
		return nil, err
	}
	asPubRaw := asPriv.PublicKey().Bytes()

	// RFC 8291 §3.3: key derivation combining auth secret and ECDH secret.
	info := append([]byte("WebPush: info\x00"), uaPubRaw...)
	info = append(info, asPubRaw...)
	prkAuth, err := hkdfExtract(auth, shared)
	if err != nil {
		return nil, err
	}
	ikm, err := hkdfExpand(prkAuth, string(info), 32)
	if err != nil {
		return nil, err
	}

	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	prk, err := hkdfExtract(salt, ikm)
	if err != nil {
		return nil, err
	}
	cek, err := hkdfExpand(prk, "Content-Encoding: aes128gcm\x00", 16)
	if err != nil {
		return nil, err
	}
	nonce, err := hkdfExpand(prk, "Content-Encoding: nonce\x00", 12)
	if err != nil {
		return nil, err
	}

	block, err := aes.NewCipher(cek)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	// Final record: payload || 0x02 delimiter.
	plain := append(append([]byte{}, payload...), 0x02)
	ciphertext := gcm.Seal(nil, nonce, plain, nil)

	// aes128gcm header: salt(16) || rs(4) || idlen(1) || keyid(65)
	var out bytes.Buffer
	out.Write(salt)
	_ = binary.Write(&out, binary.BigEndian, uint32(4096))
	out.WriteByte(65)
	out.Write(asPubRaw)
	out.Write(ciphertext)
	return out.Bytes(), nil
}

// deliverWebPush sends one encrypted push to a browser subscription.
func (a *App) deliverWebPush(ctx context.Context, endpoint, p256dh, auth string, payload []byte) (int, error) {
	body, err := encryptWebPush(p256dh, auth, payload)
	if err != nil {
		return 0, err
	}
	jwt, err := a.vapidJWT(endpoint)
	if err != nil {
		return 0, err
	}
	pub, err := a.vapidPublicKeyB64()
	if err != nil {
		return 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Encoding", "aes128gcm")
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("TTL", "86400")
	req.Header.Set("Authorization", "vapid t="+jwt+", k="+pub)
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1024))
	if resp.StatusCode >= 400 {
		return resp.StatusCode, fmt.Errorf("push service returned %d", resp.StatusCode)
	}
	return resp.StatusCode, nil
}

// ---- subscription endpoints ----

func (a *App) handlePushSubscribe(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Platform string `json:"platform"`
		Endpoint string `json:"endpoint"`
		P256DH   string `json:"p256dh"`
		Auth     string `json:"auth"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	req.Platform = strings.ToLower(strings.TrimSpace(req.Platform))
	switch req.Platform {
	case "web":
		if req.Endpoint == "" || req.P256DH == "" || req.Auth == "" {
			writeErr(w, http.StatusBadRequest, "web push requires endpoint, p256dh and auth keys")
			return
		}
		if !strings.HasPrefix(req.Endpoint, "https://") && a.cfg.AppEnv == "production" {
			writeErr(w, http.StatusBadRequest, "push endpoint must be https")
			return
		}
	case "android", "ios", "desktop":
		if len(req.Endpoint) < 16 {
			writeErr(w, http.StatusBadRequest, "invalid device token")
			return
		}
	default:
		writeErr(w, http.StatusBadRequest, "platform must be web, android, ios or desktop")
		return
	}
	if len(req.Endpoint) > 2048 {
		writeErr(w, http.StatusBadRequest, "endpoint too long")
		return
	}
	uid := userIDFrom(r)
	_, err := a.db.Exec(r.Context(),
		`INSERT INTO push_subscriptions (user_id, platform, endpoint, p256dh, auth_secret, user_agent)
		 VALUES ($1,$2,$3,$4,$5,$6)
		 ON CONFLICT (user_id, endpoint) DO UPDATE
		 SET platform=$2, p256dh=$4, auth_secret=$5, last_used_at=now()`,
		uid, req.Platform, req.Endpoint, req.P256DH, req.Auth, r.UserAgent())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "subscription failed")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"status": "subscribed"})
}

func (a *App) handlePushUnsubscribe(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Endpoint string `json:"endpoint"`
	}
	if !decodeJSON(w, r, &req) || req.Endpoint == "" {
		writeErr(w, http.StatusBadRequest, "endpoint required")
		return
	}
	_, _ = a.db.Exec(r.Context(),
		`DELETE FROM push_subscriptions WHERE user_id=$1 AND endpoint=$2`,
		userIDFrom(r), req.Endpoint)
	writeJSON(w, http.StatusOK, map[string]string{"status": "unsubscribed"})
}

// handlePushPublicKey exposes the VAPID application server key so browsers
// can subscribe with applicationServerKey.
func (a *App) handlePushPublicKey(w http.ResponseWriter, r *http.Request) {
	pub, err := a.vapidPublicKeyB64()
	if err != nil {
		writeErr(w, http.StatusServiceUnavailable, "push not configured")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"vapid_public_key": pub})
}

// ---- enqueue + worker ----

// queuePush enqueues a push delivery for every active subscription of the user.
func (a *App) queuePush(ctx context.Context, userID, kind, title, body string, data map[string]any) {
	_, _ = a.db.Exec(ctx,
		`INSERT INTO push_queue (user_id, kind, title, body, data)
		 SELECT $1, $2, $3, $4, $5
		 WHERE EXISTS(SELECT 1 FROM push_subscriptions WHERE user_id=$1)`,
		userID, kind, title, body, data)
}

// notify is the single funnel for user notifications: in-app row, realtime
// WS event, and a queued push for offline devices.
func (a *App) notify(ctx context.Context, userID, kind, title, body string, payload map[string]any) {
	_, _ = a.db.Exec(ctx,
		`INSERT INTO notifications (user_id, kind, payload) VALUES ($1,$2,$3)`, userID, kind, payload)
	wsPayload, _ := json.Marshal(map[string]any{
		"type": "notification", "kind": kind, "payload": payload, "created_at": time.Now(),
	})
	a.hub.sendTo(userID, wsPayload)
	a.queuePush(ctx, userID, kind, title, body, payload)
}

func (a *App) startPushWorker() {
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			a.drainPushQueue()
		}
	}()
}

// drainPushQueue claims queued pushes (SKIP LOCKED, safe across API nodes)
// and delivers them. Web pushes are delivered directly via RFC 8291/8292;
// native tokens are handed to the OS gateways via deliverNative.
func (a *App) drainPushQueue() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx,
		`SELECT id, user_id, kind, title, body, data FROM push_queue
		 WHERE status='queued' AND attempts < 8
		 ORDER BY id LIMIT 50 FOR UPDATE SKIP LOCKED`)
	if err != nil {
		return
	}
	type job struct {
		id                        int64
		userID, kind, title, body string
		data                      map[string]any
	}
	var jobs []job
	for rows.Next() {
		var j job
		if err := rows.Scan(&j.id, &j.userID, &j.kind, &j.title, &j.body, &j.data); err == nil {
			jobs = append(jobs, j)
		}
	}
	rows.Close()
	if len(jobs) == 0 {
		return
	}
	for _, j := range jobs {
		_, _ = tx.Exec(ctx, `UPDATE push_queue SET attempts = attempts + 1 WHERE id=$1`, j.id)
	}
	if err := tx.Commit(ctx); err != nil {
		return
	}

	for _, j := range jobs {
		subs, err := a.db.Query(ctx,
			`SELECT id, platform, endpoint, p256dh, auth_secret FROM push_subscriptions WHERE user_id=$1`, j.userID)
		if err != nil {
			continue
		}
		payload, _ := json.Marshal(map[string]any{
			"kind": j.kind, "title": j.title, "body": j.body, "data": j.data,
		})
		allOK := true
		var stale []string
		for subs.Next() {
			var subID, platform, endpoint, p256dh, auth string
			if err := subs.Scan(&subID, &platform, &endpoint, &p256dh, &auth); err != nil {
				continue
			}
			var derr error
			var code int
			if platform == "web" {
				code, derr = a.deliverWebPush(ctx, endpoint, p256dh, auth, payload)
				if code == 404 || code == 410 { // subscription expired/gone
					stale = append(stale, subID)
					continue
				}
			} else {
				derr = a.deliverNative(ctx, platform, endpoint, payload)
			}
			if derr != nil {
				allOK = false
			}
		}
		subs.Close()
		for _, subID := range stale {
			_, _ = a.db.Exec(ctx, `DELETE FROM push_subscriptions WHERE id=$1`, subID)
		}
		status := "sent"
		if !allOK {
			status = "queued" // retry on next sweep until attempts cap
		}
		_, _ = a.db.Exec(ctx,
			`UPDATE push_queue SET status=$2, sent_at=CASE WHEN $2='sent' THEN now() END WHERE id=$1`,
			j.id, status)
	}
}
