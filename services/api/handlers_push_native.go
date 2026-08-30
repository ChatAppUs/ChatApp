package main

// Native push delivery through the OS vendors' gateways. These are the only
// possible delivery paths to Android/iOS devices (the OS owns the radio);
// web/desktop push is fully self-built (see handlers_push.go).
// FCM: HTTP v1 is OAuth-based; we use the legacy server-key endpoint which
// is a plain authenticated POST. APNs: HTTP/2 with token (ES256 JWT) auth.

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"
)

var (
	apnsTokenMu  sync.Mutex
	apnsToken    string
	apnsTokenAt  time.Time
	apnsKeyCache *ecdsa.PrivateKey
)

// apnsJWT mints an APNs provider token (ES256 JWT, 1h validity, cached 50m).
func (a *App) apnsJWT() (string, error) {
	apnsTokenMu.Lock()
	defer apnsTokenMu.Unlock()
	if apnsToken != "" && time.Since(apnsTokenAt) < 50*time.Minute {
		return apnsToken, nil
	}
	if a.cfg.APNsKeyID == "" || a.cfg.APNsTeamID == "" || a.cfg.APNsPrivateKey == "" {
		return "", errors.New("APNs not configured")
	}
	if apnsKeyCache == nil {
		raw, err := base64.RawURLEncoding.DecodeString(a.cfg.APNsPrivateKey)
		if err != nil || len(raw) != 32 {
			return "", errors.New("APNS_PRIVATE_KEY must be base64url 32-byte P-256 scalar")
		}
		d := new(big.Int).SetBytes(raw)
		priv := &ecdsa.PrivateKey{PublicKey: ecdsa.PublicKey{Curve: elliptic.P256()}, D: d}
		priv.PublicKey.X, priv.PublicKey.Y = elliptic.P256().ScalarBaseMult(raw)
		apnsKeyCache = priv
	}
	header, _ := json.Marshal(map[string]string{"alg": "ES256", "kid": a.cfg.APNsKeyID})
	claims, _ := json.Marshal(map[string]any{"iss": a.cfg.APNsTeamID, "iat": time.Now().Unix()})
	body := b64urlRaw(header) + "." + b64urlRaw(claims)
	digest := sha256.Sum256([]byte(body))
	rSig, sSig, err := ecdsa.Sign(rand.Reader, apnsKeyCache, digest[:])
	if err != nil {
		return "", err
	}
	sig := make([]byte, 64)
	rSig.FillBytes(sig[:32])
	sSig.FillBytes(sig[32:])
	apnsToken = body + "." + b64urlRaw(sig)
	apnsTokenAt = time.Now()
	return apnsToken, nil
}

// deliverNative routes one payload to the platform gateway.
func (a *App) deliverNative(ctx context.Context, platform, deviceToken string, payload []byte) error {
	switch platform {
	case "android", "desktop":
		return a.deliverFCM(ctx, deviceToken, payload)
	case "ios":
		return a.deliverAPNs(ctx, deviceToken, payload)
	default:
		return fmt.Errorf("unknown push platform %q", platform)
	}
}

func (a *App) deliverFCM(ctx context.Context, deviceToken string, payload []byte) error {
	if a.cfg.FCMServerKey == "" {
		return errors.New("FCM not configured")
	}
	var inner map[string]any
	if err := json.Unmarshal(payload, &inner); err != nil {
		return err
	}
	body, _ := json.Marshal(map[string]any{
		"to":   deviceToken,
		"data": inner,
		"notification": map[string]string{
			"title": fmt.Sprint(inner["title"]),
			"body":  fmt.Sprint(inner["body"]),
		},
		"priority": "high",
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://fcm.googleapis.com/fcm/send", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "key="+a.cfg.FCMServerKey)
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode != 200 {
		return fmt.Errorf("fcm %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	return nil
}

func (a *App) deliverAPNs(ctx context.Context, deviceToken string, payload []byte) error {
	jwt, err := a.apnsJWT()
	if err != nil {
		return err
	}
	var inner map[string]any
	if err := json.Unmarshal(payload, &inner); err != nil {
		return err
	}
	body, _ := json.Marshal(map[string]any{
		"aps": map[string]any{
			"alert": map[string]string{
				"title": fmt.Sprint(inner["title"]),
				"body":  fmt.Sprint(inner["body"]),
			},
			"sound": "default",
		},
		"chatapp": inner["data"],
	})
	host := "https://api.push.apple.com"
	if a.cfg.AppEnv != "production" {
		host = "https://api.sandbox.push.apple.com"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		host+"/3/device/"+deviceToken, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("authorization", "bearer "+jwt)
	req.Header.Set("apns-push-type", "alert")
	req.Header.Set("apns-priority", "10")
	if a.cfg.APNsTopic != "" {
		req.Header.Set("apns-topic", a.cfg.APNsTopic)
	}
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1024))
	if resp.StatusCode != 200 {
		return fmt.Errorf("apns %d", resp.StatusCode)
	}
	return nil
}
