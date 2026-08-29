package main

import (
	"fmt"
	"os"
	"time"
)

type Config struct {
	Port            string
	DatabaseURL     string
	JWTSecret       []byte
	AccessTokenTTL  time.Duration
	RefreshTokenTTL time.Duration
	AppEnv          string
	MediaServiceURL string
	MLServiceURL    string
	SecuritySvcURL  string
	TwilioSID       string
	TwilioToken     string
	TwilioVerifySID string
	SMTPHost        string
	SMTPPort        string
	SMTPUser        string
	SMTPPass        string
	StripeSecret    string
	SumsubAppToken  string
	SumsubSecretKey string
	CreatorRPM      float64 // creator revenue per 1000 views, USD
	GoogleClientID  string
	WebAuthnRPID    string
	WebAuthnRPName  string
	WebAuthnOrigins string // comma-separated allowed origins
	ClusterNodeID   string
	ClusterRegion   string
	ClusterAPIURL   string
	ClusterMediaURL string
	ClusterSecret   string
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
		ClusterNodeID:   os.Getenv("CLUSTER_NODE_ID"),
		ClusterRegion:   getenv("CLUSTER_REGION", "us-east"),
		ClusterAPIURL:   os.Getenv("CLUSTER_API_URL"),
		ClusterMediaURL: os.Getenv("CLUSTER_MEDIA_URL"),
		ClusterSecret:   os.Getenv("CLUSTER_SECRET"),
		CreatorRPM:      atof(getenv("CREATOR_RPM", "0.50")),
		GoogleClientID:  os.Getenv("GOOGLE_CLIENT_ID"),
		WebAuthnRPID:    getenv("WEBAUTHN_RP_ID", "localhost"),
		WebAuthnRPName:  getenv("WEBAUTHN_RP_NAME", "ChatApp"),
		WebAuthnOrigins: getenv("WEBAUTHN_ORIGINS", "http://localhost:3000"),
	}
}

func atof(s string) float64 {
	var f float64
	_, _ = fmt.Sscanf(s, "%g", &f)
	return f
}
