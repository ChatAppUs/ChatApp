package main

import (
	"crypto/tls"
	"errors"
	"net/smtp"
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
