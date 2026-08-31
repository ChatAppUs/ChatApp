package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/smtp"
	"strings"
	"time"
)

// ---- Email ----

type Mailer struct {
	host, port, user, pass string
}

func (m *Mailer) Configured() bool { return m != nil && m.host != "" }

func (m *Mailer) Send(to, subject, body string) error {
	if !m.Configured() {
		return errors.New("smtp not configured")
	}
	addr := m.host + ":" + m.port
	msg := []byte("From: " + m.user + "\r\nTo: " + to + "\r\nSubject: " + subject + "\r\n\r\n" + body + "\r\n")
	auth := smtp.PlainAuth("", m.user, m.pass, m.host)
	if m.port == "465" {
		return sendMailTLS(addr, m.host, auth, m.user, []string{to}, msg)
	}
	return smtp.SendMail(addr, auth, m.user, []string{to}, msg)
}

func sendMailTLS(addr, host string, auth smtp.Auth, from string, to []string, msg []byte) error {
	conn, err := tls.Dial("tcp", addr, &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12})
	if err != nil {
		return err
	}
	c, err := smtp.NewClient(conn, host)
	if err != nil {
		return err
	}
	defer c.Close()
	if err := c.Auth(auth); err != nil {
		return err
	}
	if err := c.Mail(from); err != nil {
		return err
	}
	for _, rcpt := range to {
		if err := c.Rcpt(rcpt); err != nil {
			return err
		}
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(msg); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}

// ---- Media upload signing (Rust security service) ----

var securityClient = &http.Client{Timeout: 3 * time.Second}

type uploadTicket struct {
	Expires   int64  `json:"expires"`
	Signature string `json:"signature"`
}

// signPayload asks the Rust security service for an HMAC-signed grant over
// an arbitrary payload (media path, chunk-upload session, ...).
func (a *App) signPayload(ctx context.Context, payload string, expiresIn int64) (*uploadTicket, error) {
	if a.cfg.SecuritySvcURL == "" {
		return nil, errors.New("security service not configured")
	}
	req, err := http.NewRequestWithContext(ctx, "POST",
		a.cfg.SecuritySvcURL+"/sign",
		strings.NewReader(fmt.Sprintf(`{"payload":%q,"expires_in":%d}`, payload, expiresIn)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := securityClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("security service status %d", resp.StatusCode)
	}
	var out struct {
		Expires   int64  `json:"expires"`
		Signature string `json:"signature"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return &uploadTicket{Expires: out.Expires, Signature: out.Signature}, nil
}

// handleMediaUploadToken mints a short-lived signed upload grant. Clients
// append exp+sig to the media-edge /upload URL; the C++ edge verifies them
// against the security service before accepting bytes.
func (a *App) handleMediaUploadToken(w http.ResponseWriter, r *http.Request) {
	t, err := a.signPayload(r.Context(), "/upload", 300)
	if err != nil {
		writeErr(w, http.StatusServiceUnavailable, "upload signing unavailable")
		return
	}
	writeJSON(w, http.StatusOK, t)
}
