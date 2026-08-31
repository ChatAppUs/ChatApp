package main

import (
	"fmt"
	"log"
	"os"
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
	RedisURL              string
	SecuritySvcURL        string
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
	SFUInternalURL        string
	SFUPublicURL          string
	SFUHost               string
	SFUSecret             string
	TURNSecret            string
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

func loadConfig() Config {
	appEnv := getenv("APP_ENV", "development")
	jwtSecret := os.Getenv("JWT_SECRET")
	if appEnv == "production" {
		if len(jwtSecret) < 32 {
			log.Fatal("FATAL: JWT_SECRET must be at least 32 random bytes in production")
		}
	} else if jwtSecret == "" {
		jwtSecret = "dev-only-insecure-secret"
		log.Println("WARNING: JWT_SECRET not set; using development default")
	}
	masterSeed := os.Getenv("WALLET_MASTER_SEED")
	signingKey := os.Getenv("WITHDRAW_SIGNING_KEY")
	if appEnv == "production" {
		if len(masterSeed) < 32 {
			log.Fatal("FATAL: WALLET_MASTER_SEED must be at least 32 random bytes in production")
		}
		if len(signingKey) < 32 {
			log.Fatal("FATAL: WITHDRAW_SIGNING_KEY must be at least 32 random bytes in production")
		}
	} else {
		if masterSeed == "" {
			masterSeed = "dev-only-insecure-wallet-seed"
		}
		if signingKey == "" {
			signingKey = "dev-only-insecure-signing-key"
		}
	}
	return Config{
		Port:            getenv("API_PORT", "8080"),
		DatabaseURL:     getenv("DATABASE_URL", "postgres://chatapp:chatapp@localhost:5432/chatapp?sslmode=disable"),
		JWTSecret:       []byte(jwtSecret),
		AccessTokenTTL:  15 * time.Minute,
		RefreshTokenTTL: 30 * 24 * time.Hour,
		AppEnv:          appEnv,
		AllowedOrigins:  os.Getenv("ALLOWED_ORIGINS"),
		MediaServiceURL: getenv("MEDIA_SERVICE_URL", "http://localhost:8100"),
		MLServiceURL:    getenv("ML_SERVICE_URL", "http://localhost:8200"),
		RedisURL:        os.Getenv("REDIS_URL"),
		SecuritySvcURL:  getenv("SECURITY_SERVICE_URL", "http://localhost:8090"),
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
		SFUInternalURL:  getenv("SFU_INTERNAL_URL", "http://localhost:8095"),
		SFUPublicURL:    getenv("SFU_PUBLIC_URL", "ws://localhost:8095/ws"),
		SFUHost:         getenv("SFU_HOST", "localhost"),
		SFUSecret:       getenv("SFU_SECRET", "dev-sfu-secret"),
		TURNSecret:      getenv("TURN_SECRET", "dev-turn-secret"),
		VAPIDSubject:    getenv("VAPID_SUBJECT", "mailto:ops@chatapp.local"),
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
