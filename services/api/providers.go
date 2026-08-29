package main

import (
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/smtp"
	"net/url"
	"strings"
)

// ---- SMS verification ----
// Twilio Verify is used when credentials are configured. In development
// (APP_ENV=development) without credentials, codes are stored server-side
// and surfaced via the API response so the flow remains fully testable.

type SMSProvider interface {
	SendCode(phoneE164 string) (devCode string, err error)
	CheckCode(phoneE164, code string) (bool, error)
}

type TwilioVerify struct {
	sid, token, serviceSID string
}

func (t *TwilioVerify) SendCode(phone string) (string, error) {
	form := url.Values{"To": {phone}, "Channel": {"sms"}}
	req, _ := http.NewRequest("POST",
		fmt.Sprintf("https://verify.twilio.com/v2/Services/%s/Verifications", t.serviceSID),
		strings.NewReader(form.Encode()))
	req.SetBasicAuth(t.sid, t.token)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("twilio verify send failed: status %d", resp.StatusCode)
	}
	return "", nil
}

func (t *TwilioVerify) CheckCode(phone, code string) (bool, error) {
	form := url.Values{"To": {phone}, "Code": {code}}
	req, _ := http.NewRequest("POST",
		fmt.Sprintf("https://verify.twilio.com/v2/Services/%s/VerificationCheck", t.serviceSID),
		strings.NewReader(form.Encode()))
	req.SetBasicAuth(t.sid, t.token)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	var out struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return false, err
	}
	return out.Status == "approved", nil
}

// DevSMS stores codes in the phone_verifications table via callback hooks.
type DevSMS struct {
	store func(phone, codeHash string) error
	check func(phone, code string) (bool, error)
}

func (d *DevSMS) SendCode(phone string) (string, error) {
	code, err := randomDigits(6)
	if err != nil {
		return "", err
	}
	if err := d.store(phone, sha256hex(code)); err != nil {
		return "", err
	}
	return code, nil
}

func (d *DevSMS) CheckCode(phone, code string) (bool, error) {
	return d.check(phone, code)
}

func randomDigits(n int) (string, error) {
	tok, err := randomToken(n)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for i := 0; i < n; i++ {
		b.WriteByte('0' + (tok[i] % 10))
	}
	return b.String(), nil
}

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
