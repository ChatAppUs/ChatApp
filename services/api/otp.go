package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
)

// ---- Self-built phone OTP engine ----
//
// ChatApp's own verification engine: unbiased crypto/rand code generation,
// salted SHA-256 storage, 10-minute expiry, max 5 attempts, 60-second resend
// cooldown. No third-party verification service is involved anywhere in the
// pipeline. Message delivery goes through the SMSGateway interface; the
// built-in OutboxGateway enqueues messages into our own sms_outbox table,
// which is drained by our own carrier-gateway daemon (SMPP interconnect is a
// protocol endpoint we operate, not a software dependency).

var errOTPThrottled = errors.New("please wait before requesting another code")

type SMSGateway interface {
	Deliver(ctx context.Context, phoneE164, message string) error
}

// OutboxGateway is the built-in gateway: messages are queued in our own
// sms_outbox table for the carrier-gateway daemon.
type OutboxGateway struct {
	app *App
}

func (g *OutboxGateway) Deliver(ctx context.Context, phone, message string) error {
	_, err := g.app.db.Exec(ctx,
		`INSERT INTO sms_outbox (phone_e164, message) VALUES ($1,$2)`, phone, message)
	return err
}

type OTPService struct {
	app     *App
	gateway SMSGateway
	devMode bool
}

func NewOTPService(app *App, devMode bool) *OTPService {
	return &OTPService{app: app, gateway: &OutboxGateway{app: app}, devMode: devMode}
}

func generateOTP() (string, error) {
	// Unbiased 6-digit code via rejection sampling (crypto/rand).
	max := big.NewInt(1000000)
	n, err := rand.Int(rand.Reader, max)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}

func hashOTP(salt, code string) string {
	sum := sha256.Sum256([]byte(salt + ":" + code))
	return hex.EncodeToString(sum[:])
}

// otpMake is the delegated generator: when the Rust authn service is
// configured it owns the code-generation RNG + hash (same distribution
// contract); local implementations are the fail-open fallback.
func (a *App) otpMake() (code, salt, hash string, err error) {
	if a.authn != nil {
		if c, s, h, ok := a.authn.otpGenerate(); ok {
			return c, s, h, nil
		}
	}
	code, err = generateOTP()
	if err != nil {
		return "", "", "", err
	}
	salt, err = randomToken(8)
	if err != nil {
		return "", "", "", err
	}
	return code, salt, hashOTP(salt, code), nil
}

// otpHashOf computes the verifier hash, delegating to the Rust service
// when one is configured so both halves hash in the same boundary.
func (a *App) otpHashOf(salt, code string) string {
	if a.authn != nil {
		if h, ok := a.authn.otpHash(salt, code); ok {
			return h
		}
	}
	return hashOTP(salt, code)
}

// SendCode generates, stores and queues a code. Returns the plaintext code
// only in development mode so the flow is testable end-to-end without a
// carrier link.
func (s *OTPService) SendCode(phoneE164 string) (devCode string, err error) {
	ctx := context.Background()
	// Resend cooldown: at most one code per 60s per phone.
	var recent bool
	_ = s.app.db.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM phone_verifications
		 WHERE phone_e164=$1 AND created_at > now() - interval '60 seconds')`,
		phoneE164).Scan(&recent)
	if recent {
		return "", errOTPThrottled
	}
	code, salt, hash, err := s.app.otpMake()
	if err != nil {
		return "", err
	}
	if _, err := s.app.db.Exec(ctx,
		`INSERT INTO phone_verifications (phone_e164, code_hash, salt, expires_at)
		 VALUES ($1,$2,$3, now() + interval '10 minutes')`,
		phoneE164, hash, salt); err != nil {
		return "", err
	}
	if err := s.gateway.Deliver(ctx, phoneE164,
		fmt.Sprintf("Your ChatApp verification code is: %s", code)); err != nil {
		return "", err
	}
	if s.devMode {
		return code, nil
	}
	return "", nil
}

func (s *OTPService) CheckCode(phoneE164, code string) (bool, error) {
	ctx := context.Background()
	var id, wantHash, salt string
	err := s.app.db.QueryRow(ctx,
		`SELECT id, code_hash, COALESCE(salt,'') FROM phone_verifications
		 WHERE phone_e164=$1 AND verified_at IS NULL AND expires_at > now() AND attempts < 5
		 ORDER BY created_at DESC LIMIT 1`, phoneE164).Scan(&id, &wantHash, &salt)
	if err != nil {
		_, _ = s.app.db.Exec(ctx,
			`UPDATE phone_verifications SET attempts = attempts + 1
			 WHERE phone_e164=$1 AND verified_at IS NULL`, phoneE164)
		return false, nil
	}
	if subtle.ConstantTimeCompare([]byte(s.app.otpHashOf(salt, code)), []byte(wantHash)) != 1 {
		_, _ = s.app.db.Exec(ctx,
			`UPDATE phone_verifications SET attempts = attempts + 1 WHERE id=$1`, id)
		return false, nil
	}
	_, err = s.app.db.Exec(ctx,
		`UPDATE phone_verifications SET verified_at=now() WHERE id=$1`, id)
	return err == nil, err
}
