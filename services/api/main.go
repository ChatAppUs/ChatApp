package main

import (
	"context"
	"embed"
	"encoding/json"
	"log"
	"net/http"
	"time"
)

//go:embed data/countries.json
var countriesFS embed.FS

type Country struct {
	Name string `json:"name"`
	ISO  string `json:"iso"`
	Dial string `json:"dial"`
	Flag string `json:"flag"`
}

var countries []Country

func (a *App) handleCountries(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"countries": countries})
}

func (a *App) handleHealth(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := a.db.Ping(ctx); err != nil {
		writeErr(w, http.StatusServiceUnavailable, "database unreachable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func main() {
	cfg := loadConfig()

	raw, err := countriesFS.ReadFile("data/countries.json")
	if err != nil || json.Unmarshal(raw, &countries) != nil || len(countries) == 0 {
		log.Fatal("failed to load embedded countries data")
	}

	ctx := context.Background()
	db, err := connectDB(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("database connection failed: %v", err)
	}
	defer db.Close()

	app := &App{cfg: cfg, db: db, hub: newHub()}
	app.startCluster()
	app.smtp = &Mailer{host: cfg.SMTPHost, port: cfg.SMTPPort, user: cfg.SMTPUser, pass: cfg.SMTPPass}

	if cfg.TwilioSID != "" && cfg.TwilioToken != "" && cfg.TwilioVerifySID != "" {
		app.sms = &TwilioVerify{sid: cfg.TwilioSID, token: cfg.TwilioToken, serviceSID: cfg.TwilioVerifySID}
	} else {
		app.sms = &DevSMS{
			store: func(phone, codeHash string) error {
				_, err := db.Exec(ctx,
					`INSERT INTO phone_verifications (phone_e164, code_hash, expires_at)
					 VALUES ($1,$2, now() + interval '10 minutes')`, phone, codeHash)
				return err
			},
			check: func(phone, code string) (bool, error) {
				var id string
				err := db.QueryRow(ctx,
					`SELECT id FROM phone_verifications
					 WHERE phone_e164=$1 AND code_hash=$2 AND verified_at IS NULL AND expires_at > now() AND attempts < 5
					 ORDER BY created_at DESC LIMIT 1`, phone, sha256hex(code)).Scan(&id)
				if err != nil {
					_, _ = db.Exec(ctx,
						`UPDATE phone_verifications SET attempts = attempts + 1
						 WHERE phone_e164=$1 AND verified_at IS NULL`, phone)
					return false, nil
				}
				_, err = db.Exec(ctx, `UPDATE phone_verifications SET verified_at=now() WHERE id=$1`, id)
				return err == nil, err
			},
		}
		if cfg.AppEnv != "development" {
			log.Println("WARNING: SMS provider not configured; set TWILIO_* for production")
		}
	}

	mux := http.NewServeMux()

	// public
	mux.HandleFunc("GET /health", app.handleHealth)
	mux.HandleFunc("GET /api/countries", app.handleCountries)
	mux.HandleFunc("POST /api/auth/register", app.handleRegister)
	mux.HandleFunc("POST /api/auth/login", app.handleLogin)
	mux.HandleFunc("POST /api/auth/refresh", app.handleRefresh)
	mux.HandleFunc("POST /api/auth/logout", app.handleLogout)
	mux.HandleFunc("POST /api/auth/forgot-password", app.handleForgotPassword)
	mux.HandleFunc("POST /api/auth/reset-password", app.handleResetPassword)
	mux.HandleFunc("POST /api/auth/phone/send-code", app.handlePhoneSendCode)
	mux.HandleFunc("POST /api/auth/phone/check-code", app.handlePhoneCheckCode)

	// federated identity, passkeys, QR login
	mux.HandleFunc("POST /api/auth/google", app.handleGoogleAuth)
	mux.HandleFunc("POST /api/auth/passkey/login/begin", app.handlePasskeyLoginBegin)
	mux.HandleFunc("POST /api/auth/passkey/login/finish", app.handlePasskeyLoginFinish)
	mux.HandleFunc("POST /api/auth/qr/new", app.handleQRLoginNew)
	mux.HandleFunc("GET /api/auth/qr/{token}", app.handleQRLoginStatus)

	// cluster engine
	mux.HandleFunc("POST /api/cluster/heartbeat", app.handleClusterHeartbeat)
	mux.HandleFunc("GET /api/cluster/route", app.handleClusterRoute)

	// authenticated: profile & social
	mux.HandleFunc("GET /api/me", app.requireAuth(app.handleMe))
	mux.HandleFunc("PATCH /api/me", app.requireAuth(app.handleUpdateProfile))
	mux.HandleFunc("GET /api/users/search", app.requireAuth(app.handleSearchUsers))
	mux.HandleFunc("GET /api/users/{id}", app.requireAuth(app.handleGetUser))
	mux.HandleFunc("GET /api/users/{id}/posts", app.requireAuth(app.handleUserPosts))
	mux.HandleFunc("POST /api/users/{id}/follow", app.requireAuth(app.handleFollow))
	mux.HandleFunc("DELETE /api/users/{id}/follow", app.requireAuth(app.handleUnfollow))
	mux.HandleFunc("GET /api/notifications", app.requireAuth(app.handleNotifications))

	// posts, stories, reels
	mux.HandleFunc("POST /api/posts", app.requireAuth(app.handleCreatePost))
	mux.HandleFunc("GET /api/feed", app.requireAuth(app.handleFeed))
	mux.HandleFunc("GET /api/reels", app.requireAuth(app.handleReels))
	mux.HandleFunc("GET /api/stories", app.requireAuth(app.handleStories))
	mux.HandleFunc("DELETE /api/posts/{id}", app.requireAuth(app.handleDeletePost))
	mux.HandleFunc("POST /api/posts/{id}/view", app.requireAuth(app.handlePostView))
	mux.HandleFunc("POST /api/posts/{id}/like", app.requireAuth(app.handleLike))
	mux.HandleFunc("DELETE /api/posts/{id}/like", app.requireAuth(app.handleUnlike))
	mux.HandleFunc("POST /api/posts/{id}/comments", app.requireAuth(app.handleAddComment))
	mux.HandleFunc("GET /api/posts/{id}/comments", app.requireAuth(app.handleListComments))

	// chat
	mux.HandleFunc("GET /ws", app.handleWS)
	mux.HandleFunc("POST /api/conversations", app.requireAuth(app.handleCreateConversation))
	mux.HandleFunc("GET /api/conversations", app.requireAuth(app.handleListConversations))
	mux.HandleFunc("GET /api/conversations/{id}/messages", app.requireAuth(app.handleListMessages))
	mux.HandleFunc("POST /api/conversations/{id}/read", app.requireAuth(app.handleMarkRead))
	mux.HandleFunc("GET /api/conversations/{id}/reads", app.requireAuth(app.handleReadState))
	mux.HandleFunc("POST /api/conversations/{id}/members", app.requireAuth(app.handleAddMember))
	mux.HandleFunc("DELETE /api/conversations/{id}/members/{uid}", app.requireAuth(app.handleRemoveMember))
	mux.HandleFunc("POST /api/messages/{id}/edit", app.requireAuth(app.handleEditMessage))
	mux.HandleFunc("DELETE /api/messages/{id}", app.requireAuth(app.handleDeleteMessage))
	mux.HandleFunc("POST /api/messages/{id}/reactions", app.requireAuth(app.handleReactMessage))
	mux.HandleFunc("DELETE /api/messages/{id}/reactions", app.requireAuth(app.handleUnreactMessage))

	// channels (Telegram-style broadcast)
	mux.HandleFunc("GET /api/channels", app.requireAuth(app.handleListChannels))
	mux.HandleFunc("POST /api/channels/{id}/subscribe", app.requireAuth(app.handleChannelSubscribe))
	mux.HandleFunc("DELETE /api/channels/{id}/subscribe", app.requireAuth(app.handleChannelUnsubscribe))

	// presence
	mux.HandleFunc("GET /api/users/{id}/presence", app.requireAuth(app.handlePresence))

	// polls, hashtags, bookmarks, reposts
	mux.HandleFunc("POST /api/posts/{id}/vote", app.requireAuth(app.handleVotePoll))
	mux.HandleFunc("GET /api/posts/{id}/poll", app.requireAuth(app.handleGetPoll))
	mux.HandleFunc("GET /api/hashtags/trending", app.requireAuth(app.handleTrending))
	mux.HandleFunc("GET /api/hashtags/{tag}/posts", app.requireAuth(app.handleHashtagPosts))
	mux.HandleFunc("POST /api/posts/{id}/bookmark", app.requireAuth(app.handleBookmark))
	mux.HandleFunc("DELETE /api/posts/{id}/bookmark", app.requireAuth(app.handleUnbookmark))
	mux.HandleFunc("GET /api/bookmarks", app.requireAuth(app.handleListBookmarks))
	mux.HandleFunc("POST /api/posts/{id}/repost", app.requireAuth(app.handleRepost))

	// privacy blocks
	mux.HandleFunc("POST /api/users/{id}/block", app.requireAuth(app.handleBlock))
	mux.HandleFunc("DELETE /api/users/{id}/block", app.requireAuth(app.handleUnblock))

	// 2FA + E2EE keys
	mux.HandleFunc("POST /api/auth/2fa/setup", app.requireAuth(app.handle2FASetup))
	mux.HandleFunc("POST /api/auth/2fa/enable", app.requireAuth(app.handle2FAEnable))
	mux.HandleFunc("POST /api/auth/2fa/disable", app.requireAuth(app.handle2FADisable))
	mux.HandleFunc("POST /api/auth/passkey/register/begin", app.requireAuth(app.handlePasskeyRegisterBegin))
	mux.HandleFunc("POST /api/auth/passkey/register/finish", app.requireAuth(app.handlePasskeyRegisterFinish))
	mux.HandleFunc("GET /api/auth/passkeys", app.requireAuth(app.handlePasskeyList))
	mux.HandleFunc("DELETE /api/auth/passkeys/{id}", app.requireAuth(app.handlePasskeyDelete))
	mux.HandleFunc("POST /api/auth/qr/{token}/approve", app.requireAuth(app.handleQRLoginApprove))
	mux.HandleFunc("GET /api/cluster/nodes", app.requireRole("superadmin")(app.handleClusterNodes))
	mux.HandleFunc("POST /api/cluster/nodes/{id}/drain", app.requireRole("superadmin")(app.handleClusterDrain))
	mux.HandleFunc("DELETE /api/cluster/nodes/{id}", app.requireRole("superadmin")(app.handleClusterRemove))
	mux.HandleFunc("POST /api/auth/qr/{token}/reject", app.requireAuth(app.handleQRLoginReject))
	mux.HandleFunc("PUT /api/e2e/key", app.requireAuth(app.handleE2EPublishKey))
	mux.HandleFunc("GET /api/e2e/keys", app.requireAuth(app.handleE2EGetKeys))

	// creator monetization
	mux.HandleFunc("GET /api/creator/earnings", app.requireAuth(app.handleCreatorEarnings))
	mux.HandleFunc("POST /api/creator/payouts", app.requireAuth(app.handleCreatorPayout))
	mux.HandleFunc("GET /api/creator/payouts", app.requireAuth(app.handleCreatorPayouts))

	// wallet & KYC
	mux.HandleFunc("GET /api/wallet/assets", app.requireAuth(app.handleWalletAssets))
	mux.HandleFunc("GET /api/wallet/accounts", app.requireAuth(app.handleWalletAccounts))
	mux.HandleFunc("POST /api/wallet/accounts", app.requireAuth(app.handleWalletCreateAccount))
	mux.HandleFunc("POST /api/wallet/deposit-address", app.requireAuth(app.handleDepositAddress))
	mux.HandleFunc("POST /api/wallet/transfer", app.requireAuth(app.handleP2PTransfer))
	mux.HandleFunc("GET /api/wallet/history", app.requireAuth(app.handleWalletHistory))
	mux.HandleFunc("POST /api/kyc/submit", app.requireAuth(app.handleKYCSubmit))
	mux.HandleFunc("GET /api/kyc/status", app.requireAuth(app.handleKYCStatus))

	// ads
	mux.HandleFunc("POST /api/ads/campaigns", app.requireAuth(app.handleCreateCampaign))
	mux.HandleFunc("GET /api/ads/campaigns", app.requireAuth(app.handleListCampaigns))
	mux.HandleFunc("POST /api/ads/campaigns/{id}/creatives", app.requireAuth(app.handleAddCreative))
	mux.HandleFunc("POST /api/ads/campaigns/{id}/submit", app.requireAuth(app.handleSubmitCampaign))
	mux.HandleFunc("POST /api/ads/campaigns/{id}/fund", app.requireAuth(app.handleFundCampaign))
	mux.HandleFunc("GET /api/ads/serve", app.requireAuth(app.handleServeAd))
	mux.HandleFunc("POST /api/ads/creatives/{id}/click", app.requireAuth(app.handleAdClick))

	// reports
	mux.HandleFunc("POST /api/reports", app.requireAuth(app.handleCreateReport))

	// admin (role-gated)
	mux.HandleFunc("GET /api/admin/stats", app.requireRole("superadmin", "moderator", "support", "finance", "ads_reviewer")(app.handleAdminStats))
	mux.HandleFunc("GET /api/admin/users", app.requireRole("superadmin", "support")(app.handleAdminListUsers))
	mux.HandleFunc("POST /api/admin/users/{id}/status", app.requireRole("superadmin", "moderator")(app.handleAdminSetUserStatus))
	mux.HandleFunc("GET /api/admin/reports", app.requireRole("superadmin", "moderator")(app.handleAdminListReports))
	mux.HandleFunc("POST /api/admin/reports/{id}/resolve", app.requireRole("superadmin", "moderator")(app.handleAdminResolveReport))
	mux.HandleFunc("GET /api/admin/kyc", app.requireRole("superadmin", "finance", "support")(app.handleAdminListKYC))
	mux.HandleFunc("POST /api/admin/kyc/{id}/review", app.requireRole("superadmin", "finance")(app.handleAdminReviewKYC))
	mux.HandleFunc("GET /api/admin/ads", app.requireRole("superadmin", "ads_reviewer")(app.handleAdminListAds))
	mux.HandleFunc("POST /api/admin/ads/{id}/review", app.requireRole("superadmin", "ads_reviewer")(app.handleAdminReviewAd))
	mux.HandleFunc("POST /api/admin/roles", app.requireRole("superadmin")(app.handleAdminGrantRole))
	mux.HandleFunc("GET /api/admin/payouts", app.requireRole("superadmin", "finance")(app.handleAdminListPayouts))
	mux.HandleFunc("POST /api/admin/payouts/{id}/review", app.requireRole("superadmin", "finance")(app.handleAdminReviewPayout))

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           withCORS(mux),
		ReadHeaderTimeout: 10 * time.Second,
	}
	log.Printf("ChatApp API listening on :%s (env=%s)", cfg.Port, cfg.AppEnv)
	log.Fatal(srv.ListenAndServe())
}
