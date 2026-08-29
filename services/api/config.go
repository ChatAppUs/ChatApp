package main

import (
	"os"
	"time"
)

type Config struct {
	Port             string
	DatabaseURL      string
	JWTSecret        []byte
	AccessTokenTTL   time.Duration
	RefreshTokenTTL  time.Duration
	AppEnv           string
	MediaServiceURL  string
	MLServiceURL     string
	SecuritySvcURL   string
	TwilioSID        string
	TwilioToken      string
	TwilioVerifySID  string
	SMTPHost         string
	SMTPPort         string
	SMTPUser         string
	SMTPPass         string
	StripeSecret     string
	SumsubAppToken   string
	SumsubSecretKey  string
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func loadConfig() Config {
	return Config{
		Port:            getenv("API_PORT", "8080"),
		DatabaseURL:     getenv("DATABASE_URL", "postgres://chatapp:chatapp@localhost:5432/chatapp?sslmode=disable"),
		JWTSecret:       []byte(getenv("JWT_SECRET", "change-me-in-production")),
		AccessTokenTTL:  15 * time.Minute,
		RefreshTokenTTL: 30 * 24 * time.Hour,
		AppEnv:          getenv("APP_ENV", "development"),
		MediaServiceURL: getenv("MEDIA_SERVICE_URL", "http://localhost:8100"),
		MLServiceURL:    getenv("ML_SERVICE_URL", "http://localhost:8200"),
		SecuritySvcURL:  getenv("SECURITY_SERVICE_URL", "http://localhost:8090"),
		TwilioSID:       os.Getenv("TWILIO_ACCOUNT_SID"),
		TwilioToken:     os.Getenv("TWILIO_AUTH_TOKEN"),
		TwilioVerifySID: os.Getenv("TWILIO_VERIFY_SERVICE_SID"),
		SMTPHost:        os.Getenv("SMTP_HOST"),
		SMTPPort:        getenv("SMTP_PORT", "587"),
		SMTPUser:        os.Getenv("SMTP_USER"),
		SMTPPass:        os.Getenv("SMTP_PASS"),
		StripeSecret:    os.Getenv("STRIPE_SECRET_KEY"),
		SumsubAppToken:  os.Getenv("SUMSUB_APP_TOKEN"),
		SumsubSecretKey: os.Getenv("SUMSUB_SECRET_KEY"),
	}
}
