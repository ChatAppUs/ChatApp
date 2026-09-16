package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"strings"
	"time"
)

type Config struct {
	Port                  string
	DatabaseURL           string
	JWTSecret             []byte
	AccessTokenTTL        time.Duration
	RefreshTokenTTL       time.Duration
	AppEnv                string
	AllowedOrigins        string // comma-separated CORS origins; empty = dev wildcard
	MediaServiceURL       string
	MLServiceURL          string
	ContactDiscoveryPepper string
	RedisURL              string
	SecuritySvcURL        string
	SecuritySecret        string
	SMTPHost              string
	SMTPPort              string
	SMTPUser              string
	SMTPPass              string
	CreatorRPM            float64 // creator revenue per 1000 views, USD
	PostEditWindowMinutes int     // post edit time window (X/Telegram parity); 0 = unlimited
	BTCRPCURL             string  // own bitcoind JSON-RPC (watch-only wallet)
	EVMRPCURL             string  // own geth/erigon JSON-RPC
	TronRPCURL            string  // own tron full-node HTTP
	SolanaRPCURL          string  // own solana JSON-RPC
	GoogleClientID        string
	WebAuthnRPID          string
	WebAuthnRPName        string
	WebAuthnOrigins       string // comma-separated allowed origins
	ClusterNodeID         string
	ClusterRegion         string
	ClusterAPIURL         string
	ClusterMediaURL       string
	ClusterSecret         string
	RelayURL              string // C++ realtime relay control plane
	CountersURL           string // C++ counters engine control plane
	CountersSecret        string
	AuthnURL              string // Rust authn service control plane
	AuthnSecret           string
	SFUInternalURL        string
	SFUPublicURL          string
	SFUHost               string
	SFUSecret             string
	TURNSecret            string
	TURNForwarder         string // host:port of the C++ TURN relay forwarder; empty = embedded pion/turn fallback
	VAPIDSubject          string // mailto: or https: contact for push services
	VAPIDPrivateKey       string // base64url P-256 scalar
	FCMServerKey          string
	APNsKeyID             string
	APNsTeamID            string
	APNsTopic             string
	APNsPrivateKey        string // base64url P-256 scalar

	// Finance plane. WalletMasterSeed derives self-custody deposit addresses;
	// WithdrawSigningKey is the superadmin authority key that signs every
	// withdrawal (auto-policy and manual approvals alike).
	WalletMasterSeed      string
	WithdrawSigningKey    string
	WithdrawAutoLimitUSD  float64 // auto-approve ceiling; above => manual sign
	WithdrawAutoThreshold int     // risk score ceiling for auto-approval
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func requiredSecret(key string, minLen int) string {
	value := strings.TrimSpace(os.Getenv(key))
	if len(value) < minLen {
		log.Fatalf("FATAL: %s must be set and contain at least %d random bytes", key, minLen)
	}
	return value
}

func loadConfig() Config {
	appEnv := getenv("APP_ENV", "development")
	jwtSecret := requiredSecret("JWT_SECRET", 32)
	masterSeed := requiredSecret("WALLET_MASTER_SEED", 32)
	// Production must carry an explicit, separate withdrawal authority key.
	// Non-production derives one from the required master seed (domain-separated,
	// deterministic) so local and CI runs exercise the real signing path without
	// a second operator secret; an explicitly configured key always wins.
	signingKey := strings.TrimSpace(os.Getenv("WITHDRAW_SIGNING_KEY"))
	if len(signingKey) < 32 {
		if appEnv == "production" {
			log.Fatalf("FATAL: WITHDRAW_SIGNING_KEY must be set and contain at least 32 random bytes in production")
		}
		d := hmac.New(sha256.New, []byte(masterSeed))
		d.Write([]byte("chatapp-withdraw-dev-v1"))
		signingKey = hex.EncodeToString(d.Sum(nil))
	}
	countersSecret := requiredSecret("COUNTERS_SECRET", 32)
	sfuSecret := requiredSecret("SFU_SECRET", 32)
	turnSecret := requiredSecret("TURN_SECRET", 32)
	securitySecret := requiredSecret("SIGNING_SECRET", 32)
	authnURL := os.Getenv("AUTHN_SERVICE_URL")
	authnSecret := os.Getenv("AUTHN_SECRET")
	if authnURL != "" {
		authnSecret = requiredSecret("AUTHN_SECRET", 32)
	}
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		log.Fatal("FATAL: DATABASE_URL must be configured")
	}
	if appEnv == "production" && strings.TrimSpace(os.Getenv("ALLOWED_ORIGINS")) == "" {
		log.Fatal("FATAL: ALLOWED_ORIGINS must be configured in production")
	}
	return Config{
		Port:            getenv("API_PORT", "8080"),
		DatabaseURL:     databaseURL,
		JWTSecret:       []byte(jwtSecret),
		AccessTokenTTL:  15 * time.Minute,
		RefreshTokenTTL: 30 * 24 * time.Hour,
		AppEnv:          appEnv,
		AllowedOrigins:  os.Getenv("ALLOWED_ORIGINS"),
		MediaServiceURL: getenv("MEDIA_SERVICE_URL", "http://localhost:8100"),
		MLServiceURL:    getenv("ML_SERVICE_URL", "http://localhost:8200"),
	ContactDiscoveryPepper: getenv("CONTACT_DISCOVERY_PEPPER", "chatapp-discovery-pepper-v1"),
		RedisURL:        os.Getenv("REDIS_URL"),
		SecuritySvcURL:  getenv("SECURITY_SERVICE_URL", "http://localhost:8090"),
		SecuritySecret:  securitySecret,
		SMTPHost:        os.Getenv("SMTP_HOST"),
		SMTPPort:        getenv("SMTP_PORT", "587"),
		SMTPUser:        os.Getenv("SMTP_USER"),
		SMTPPass:        os.Getenv("SMTP_PASS"),
		BTCRPCURL:       os.Getenv("BTC_RPC_URL"),
		EVMRPCURL:       os.Getenv("EVM_RPC_URL"),
		TronRPCURL:      os.Getenv("TRON_RPC_URL"),
		SolanaRPCURL:    os.Getenv("SOLANA_RPC_URL"),
		ClusterNodeID:   os.Getenv("CLUSTER_NODE_ID"),
		ClusterRegion:   getenv("CLUSTER_REGION", "us-east"),
		ClusterAPIURL:   os.Getenv("CLUSTER_API_URL"),
		ClusterMediaURL: os.Getenv("CLUSTER_MEDIA_URL"),
		ClusterSecret:   os.Getenv("CLUSTER_SECRET"),
		RelayURL:        os.Getenv("REALTIME_RELAY_URL"),
		CountersURL:     os.Getenv("COUNTERS_URL"),
		AuthnURL:        authnURL,
		AuthnSecret:     authnSecret,
		CountersSecret:  countersSecret,
		SFUInternalURL:  getenv("SFU_INTERNAL_URL", "http://localhost:8095"),
		SFUPublicURL:    getenv("SFU_PUBLIC_URL", "ws://localhost:8095/ws"),
		SFUHost:         getenv("SFU_HOST", "localhost"),
		SFUSecret:       sfuSecret,
		TURNSecret:      turnSecret,
		TURNForwarder:   os.Getenv("TURN_FORWARDER"),
		VAPIDSubject:    os.Getenv("VAPID_SUBJECT"),
		VAPIDPrivateKey: os.Getenv("VAPID_PRIVATE_KEY"),
		FCMServerKey:    os.Getenv("FCM_SERVER_KEY"),
		APNsKeyID:       os.Getenv("APNS_KEY_ID"),
		APNsTeamID:      os.Getenv("APNS_TEAM_ID"),
		APNsTopic:       os.Getenv("APNS_TOPIC"),
		APNsPrivateKey:  os.Getenv("APNS_PRIVATE_KEY"),
		CreatorRPM:      atof(getenv("CREATOR_RPM", "0.50")),
		// 48h default matches Telegram's edit window; X Premium is 1h.
		PostEditWindowMinutes: atoi(getenv("POST_EDIT_WINDOW_MINUTES", "2880")),
		GoogleClientID:        os.Getenv("GOOGLE_CLIENT_ID"),
		WebAuthnRPID:          getenv("WEBAUTHN_RP_ID", "localhost"),
		WebAuthnRPName:        getenv("WEBAUTHN_RP_NAME", "ChatApp"),
		WebAuthnOrigins:       getenv("WEBAUTHN_ORIGINS", "http://localhost:3000"),

		WalletMasterSeed:      masterSeed,
		WithdrawSigningKey:    signingKey,
		WithdrawAutoLimitUSD:  atof(getenv("WITHDRAW_AUTO_LIMIT_USD", "10000")),
		WithdrawAutoThreshold: atoi(getenv("WITHDRAW_AUTO_THRESHOLD", "100")),
	}
}

func atof(s string) float64 {
	var f float64
	_, _ = fmt.Sscanf(s, "%g", &f)
	return f
}

func atoi(s string) int {
	var n int
	_, _ = fmt.Sscanf(s, "%d", &n)
	return n
}
