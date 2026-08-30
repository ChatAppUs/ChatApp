package main

import (
	"context"
	"embed"
	"encoding/json"
	"log"
	"net/http"
	"strings"
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
	app.startScheduler()
	app.startExpirySweeper()
	app.startPushWorker()
	app.startBotWebhookWorker()
	app.startSubscriptionWorker()
	app.startRelayBridge()
	app.startSubscriptionWorker()
	app.smtp = &Mailer{host: cfg.SMTPHost, port: cfg.SMTPPort, user: cfg.SMTPUser, pass: cfg.SMTPPass}
	app.otp = NewOTPService(app, cfg.AppEnv == "development")

	origins := map[string]bool{}
	for _, o := range strings.Split(cfg.AllowedOrigins, ",") {
		if o = strings.TrimSpace(o); o != "" && o != "*" {
			origins[o] = true
		}
	}
	upgrader.CheckOrigin = func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		if origin == "" {
			return true // native/mobile clients send no Origin
		}
		return len(origins) == 0 || origins[origin]
	}

	mux := http.NewServeMux()

	// Rate limiters for abuse-sensitive public endpoints (per client IP).
	// Generous enough for legitimate bursts, tight enough to blunt
	// credential stuffing, SMS bombing and reset-token brute force.
	loginLimiter := newRateLimiter(15, 5)
	registerLimiter := newRateLimiter(10, 3)
	resetLimiter := newRateLimiter(5, 2)
	smsSendLimiter := newRateLimiter(5, 2)
	smsCheckLimiter := newRateLimiter(15, 5)
	oauthLimiter := newRateLimiter(60, 20)
	qrLimiter := newRateLimiter(20, 5)

	// public
	mux.HandleFunc("GET /health", app.handleHealth)
	mux.HandleFunc("GET /api/countries", app.handleCountries)
	mux.HandleFunc("POST /api/auth/register", registerLimiter.limit(app.handleRegister))
	mux.HandleFunc("POST /api/auth/login", loginLimiter.limit(app.handleLogin))
	mux.HandleFunc("POST /api/admin/login", loginLimiter.limit(app.handleAdminLogin))
	mux.HandleFunc("POST /api/auth/refresh", app.handleRefresh)
	mux.HandleFunc("POST /api/auth/logout", app.handleLogout)
	mux.HandleFunc("POST /api/auth/forgot-password", resetLimiter.limit(app.handleForgotPassword))
	mux.HandleFunc("POST /api/auth/reset-password", resetLimiter.limit(app.handleResetPassword))
	mux.HandleFunc("POST /api/auth/phone/send-code", smsSendLimiter.limit(app.handlePhoneSendCode))
	mux.HandleFunc("POST /api/auth/phone/check-code", smsCheckLimiter.limit(app.handlePhoneCheckCode))

	// federated identity, passkeys, QR login
	mux.HandleFunc("POST /api/auth/google", oauthLimiter.limit(app.handleGoogleAuth))
	mux.HandleFunc("POST /api/auth/passkey/login/begin", oauthLimiter.limit(app.handlePasskeyLoginBegin))
	mux.HandleFunc("POST /api/auth/passkey/login/finish", oauthLimiter.limit(app.handlePasskeyLoginFinish))
	mux.HandleFunc("POST /api/auth/qr/new", qrLimiter.limit(app.handleQRLoginNew))
	mux.HandleFunc("GET /api/auth/qr/{token}", qrLimiter.limit(app.handleQRLoginStatus))

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
	mux.HandleFunc("POST /api/comments/{id}/like", app.requireAuth(app.handleLikeComment))
	mux.HandleFunc("DELETE /api/comments/{id}/like", app.requireAuth(app.handleUnlikeComment))
	mux.HandleFunc("POST /api/notifications/read", app.requireAuth(app.handleMarkNotificationsRead))
	mux.HandleFunc("GET /api/conversations/{id}/search", app.requireAuth(app.handleSearchMessages))
	mux.HandleFunc("POST /api/posts/{id}/share", app.requireAuth(app.handleSharePostToChat))
	mux.HandleFunc("GET /api/posts/{id}/comments", app.requireAuth(app.handleListComments))

	// chat
	mux.HandleFunc("GET /ws", app.handleWS)
	mux.HandleFunc("POST /api/conversations", app.requireAuth(app.handleCreateConversation))
	mux.HandleFunc("GET /api/conversations", app.requireAuth(app.handleListConversations))
	mux.HandleFunc("GET /api/conversations/{id}/messages", app.requireAuth(app.handleListMessages))
	mux.HandleFunc("POST /api/conversations/{id}/read", app.requireAuth(app.handleMarkRead))
	mux.HandleFunc("POST /api/conversations/{id}/schedule", app.requireAuth(app.handleScheduleMessage))
	mux.HandleFunc("GET /api/conversations/{id}/scheduled", app.requireAuth(app.handleListScheduled))
	mux.HandleFunc("DELETE /api/scheduled/{id}", app.requireAuth(app.handleCancelScheduled))
	mux.HandleFunc("GET /api/conversations/{id}/reads", app.requireAuth(app.handleReadState))
	mux.HandleFunc("POST /api/conversations/{id}/members", app.requireAuth(app.handleAddMember))
	mux.HandleFunc("DELETE /api/conversations/{id}/members/{uid}", app.requireAuth(app.handleRemoveMember))
	mux.HandleFunc("POST /api/messages/{id}/edit", app.requireAuth(app.handleEditMessage))
	mux.HandleFunc("POST /api/messages/{id}/forward", app.requireAuth(app.handleForwardMessage))
	mux.HandleFunc("POST /api/conversations/saved", app.requireAuth(app.handleSavedMessages))
	mux.HandleFunc("POST /api/conversations/{id}/pins/{messageId}", app.requireAuth(app.handlePinMessage))
	mux.HandleFunc("DELETE /api/conversations/{id}/pins/{messageId}", app.requireAuth(app.handleUnpinMessage))
	mux.HandleFunc("PUT /api/conversations/{id}/ttl", app.requireAuth(app.handleSetTTL))
	mux.HandleFunc("GET /api/conversations/{id}/ttl", app.requireAuth(app.handleGetTTL))
	mux.HandleFunc("GET /api/conversations/{id}/members", app.requireAuth(app.handleListMembers))
	mux.HandleFunc("GET /api/conversations/{id}/pins", app.requireAuth(app.handleListPins))
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
	mux.HandleFunc("DELETE /api/posts/{id}/repost", app.requireAuth(app.handleUnrepost))
	mux.HandleFunc("PATCH /api/posts/{id}", app.requireAuth(app.handleEditPost))
	mux.HandleFunc("GET /api/posts/{id}/thread", app.requireAuth(app.handleThread))
	mux.HandleFunc("POST /api/stories/{id}/view", app.requireAuth(app.handleStoryView))
	mux.HandleFunc("GET /api/stories/{id}/viewers", app.requireAuth(app.handleStoryViewers))
	mux.HandleFunc("POST /api/stories/{id}/react", app.requireAuth(app.handleStoryReact))
	mux.HandleFunc("POST /api/stories/{id}/reply", app.requireAuth(app.handleStoryReply))
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
	mux.HandleFunc("GET /api/cluster/nodes", app.requireAdmin("superadmin")(app.handleClusterNodes))
	mux.HandleFunc("POST /api/cluster/nodes/{id}/drain", app.requireAdmin("superadmin")(app.handleClusterDrain))
	mux.HandleFunc("DELETE /api/cluster/nodes/{id}", app.requireAdmin("superadmin")(app.handleClusterRemove))
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
	mux.HandleFunc("POST /api/calls/rooms", app.requireAuth(app.handleCreateCallRoom))
	mux.HandleFunc("POST /api/calls/rooms/{roomId}/join", app.requireAuth(app.handleJoinCallRoom))
	mux.HandleFunc("GET /api/live", app.requireAuth(app.handleLiveNow))
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

	// media upload grant (signed by the Rust security service)
	mux.HandleFunc("POST /api/media/upload-token", app.requireAuth(app.handleMediaUploadToken))

	// push notifications
	mux.HandleFunc("GET /api/push/public-key", app.handlePushPublicKey)
	mux.HandleFunc("POST /api/push/subscribe", app.requireAuth(app.handlePushSubscribe))
	mux.HandleFunc("POST /api/push/unsubscribe", app.requireAuth(app.handlePushUnsubscribe))

	// watch-time signals + FYP ranking
	mux.HandleFunc("POST /api/reels/{id}/watch", app.requireAuth(app.handleReelWatch))
	mux.HandleFunc("GET /api/fyp", app.requireAuth(app.handleFYP))

	// groups, pages, events
	mux.HandleFunc("POST /api/groups", app.requireAuth(app.handleCreateGroup))
	mux.HandleFunc("GET /api/groups", app.requireAuth(app.handleListGroups))
	mux.HandleFunc("GET /api/groups/{id}", app.requireAuth(app.handleGetGroup))
	mux.HandleFunc("POST /api/groups/{id}/join", app.requireAuth(app.handleJoinGroup))
	mux.HandleFunc("DELETE /api/groups/{id}/join", app.requireAuth(app.handleLeaveGroup))
	mux.HandleFunc("POST /api/groups/{id}/members/{uid}/review", app.requireAuth(app.handleReviewGroupMember))
	mux.HandleFunc("POST /api/groups/{id}/members/{uid}/role", app.requireAuth(app.handleSetGroupRole))
	mux.HandleFunc("GET /api/groups/{id}/feed", app.requireAuth(app.handleGroupFeed))
	mux.HandleFunc("POST /api/groups/{id}/posts", app.requireAuth(app.handleGroupPost))
	mux.HandleFunc("POST /api/pages", app.requireAuth(app.handleCreatePage))
	mux.HandleFunc("GET /api/pages", app.requireAuth(app.handleListPages))
	mux.HandleFunc("GET /api/pages/{id}", app.requireAuth(app.handleGetPage))
	mux.HandleFunc("POST /api/pages/{id}/follow", app.requireAuth(app.handleFollowPage))
	mux.HandleFunc("DELETE /api/pages/{id}/follow", app.requireAuth(app.handleUnfollowPage))
	mux.HandleFunc("GET /api/pages/{id}/feed", app.requireAuth(app.handlePageFeed))
	mux.HandleFunc("POST /api/pages/{id}/posts", app.requireAuth(app.handlePagePost))
	mux.HandleFunc("POST /api/events", app.requireAuth(app.handleCreateEvent))
	mux.HandleFunc("GET /api/events", app.requireAuth(app.handleListEvents))
	mux.HandleFunc("GET /api/events/{id}", app.requireAuth(app.handleGetEvent))
	mux.HandleFunc("POST /api/events/{id}/rsvp", app.requireAuth(app.handleRSVP))

	// monetization: tiers, subscriptions, tips, gifts
	mux.HandleFunc("POST /api/creator/tiers", app.requireAuth(app.handleCreateTier))
	mux.HandleFunc("GET /api/creator/tiers", app.requireAuth(app.handleListMyTiers))
	mux.HandleFunc("DELETE /api/creator/tiers/{id}", app.requireAuth(app.handleDeleteTier))
	mux.HandleFunc("GET /api/users/{id}/tiers", app.requireAuth(app.handleListCreatorTiers))
	mux.HandleFunc("POST /api/tiers/{id}/subscribe", app.requireAuth(app.handleSubscribe))
	mux.HandleFunc("DELETE /api/subscriptions/{id}", app.requireAuth(app.handleCancelSubscription))
	mux.HandleFunc("GET /api/subscriptions", app.requireAuth(app.handleMySubscriptions))
	mux.HandleFunc("GET /api/creator/subscribers", app.requireAuth(app.handleCreatorSubscribers))
	mux.HandleFunc("POST /api/users/{id}/tip", app.requireAuth(app.handleSendTip))
	mux.HandleFunc("GET /api/gifts", app.requireAuth(app.handleGiftCatalog))
	mux.HandleFunc("POST /api/users/{id}/gift", app.requireAuth(app.handleSendGift))

	// bots + mini-apps
	mux.HandleFunc("POST /api/bots", app.requireAuth(app.handleCreateBot))
	mux.HandleFunc("GET /api/bots", app.requireAuth(app.handleMyBots))
	mux.HandleFunc("DELETE /api/bots/{id}", app.requireAuth(app.handleDeleteBot))
	mux.HandleFunc("POST /api/bots/{id}/webhook", app.requireAuth(app.handleSetWebhook))
	mux.HandleFunc("POST /api/bots/{id}/mini-app", app.requireAuth(app.handleSetMiniApp))
	mux.HandleFunc("GET /api/bot/{token}/getUpdates", app.handleBotGetUpdates)
	mux.HandleFunc("POST /api/bot/{token}/sendMessage", app.handleBotSendMessage)
	mux.HandleFunc("GET /api/bot/{token}/getMe", app.handleBotGetMe)

	// stories extras: highlights + close friends
	mux.HandleFunc("POST /api/highlights", app.requireAuth(app.handleCreateHighlight))
	mux.HandleFunc("GET /api/highlights", app.requireAuth(app.handleMyHighlights))
	mux.HandleFunc("DELETE /api/highlights/{id}", app.requireAuth(app.handleDeleteHighlight))
	mux.HandleFunc("POST /api/highlights/{id}/items/{storyId}", app.requireAuth(app.handleAddHighlightItem))
	mux.HandleFunc("DELETE /api/highlights/{id}/items/{storyId}", app.requireAuth(app.handleRemoveHighlightItem))
	mux.HandleFunc("GET /api/users/{id}/highlights", app.requireAuth(app.handleUserHighlights))
	mux.HandleFunc("POST /api/users/{id}/close-friend", app.requireAuth(app.handleAddCloseFriend))
	mux.HandleFunc("DELETE /api/users/{id}/close-friend", app.requireAuth(app.handleRemoveCloseFriend))
	mux.HandleFunc("GET /api/me/close-friends", app.requireAuth(app.handleListCloseFriends))

	// privacy suite
	mux.HandleFunc("POST /api/users/{id}/mute", app.requireAuth(app.handleMute))
	mux.HandleFunc("DELETE /api/users/{id}/mute", app.requireAuth(app.handleUnmute))
	mux.HandleFunc("GET /api/me/mutes", app.requireAuth(app.handleListMutes))
	mux.HandleFunc("POST /api/me/word-filters", app.requireAuth(app.handleAddWordFilter))
	mux.HandleFunc("DELETE /api/me/word-filters", app.requireAuth(app.handleRemoveWordFilter))
	mux.HandleFunc("GET /api/me/word-filters", app.requireAuth(app.handleListWordFilters))
	mux.HandleFunc("POST /api/users/{id}/restrict", app.requireAuth(app.handleRestrict))
	mux.HandleFunc("DELETE /api/users/{id}/restrict", app.requireAuth(app.handleUnrestrict))
	mux.HandleFunc("GET /api/me/restricted", app.requireAuth(app.handleListRestricted))
	mux.HandleFunc("PUT /api/me/profile-lock", app.requireAuth(app.handleSetProfileLock))
	mux.HandleFunc("PUT /api/me/active-status", app.requireAuth(app.handleSetActiveStatus))
	mux.HandleFunc("GET /api/me/follow-requests", app.requireAuth(app.handleFollowRequests))
	mux.HandleFunc("POST /api/me/follow-requests/{uid}/accept", app.requireAuth(app.handleAcceptFollowRequest))
	mux.HandleFunc("POST /api/me/follow-requests/{uid}/decline", app.requireAuth(app.handleDeclineFollowRequest))
	mux.HandleFunc("GET /api/me/message-requests", app.requireAuth(app.handleMessageRequests))
	mux.HandleFunc("POST /api/me/message-requests/{convId}/accept", app.requireAuth(app.handleAcceptMessageRequest))
	mux.HandleFunc("POST /api/me/message-requests/{convId}/decline", app.requireAuth(app.handleDeclineMessageRequest))

	// messaging polish
	mux.HandleFunc("POST /api/conversations/{id}/invites", app.requireAuth(app.handleCreateInvite))
	mux.HandleFunc("GET /api/conversations/{id}/invites", app.requireAuth(app.handleListInvites))
	mux.HandleFunc("DELETE /api/conversations/{id}/invites/{inviteId}", app.requireAuth(app.handleRevokeInvite))
	mux.HandleFunc("POST /api/invites/{code}/join", app.requireAuth(app.handleJoinByInvite))
	mux.HandleFunc("PUT /api/conversations/{id}/slow-mode", app.requireAuth(app.handleSetSlowMode))
	mux.HandleFunc("POST /api/conversations/{id}/topics", app.requireAuth(app.handleCreateTopic))
	mux.HandleFunc("GET /api/conversations/{id}/topics", app.requireAuth(app.handleListTopics))
	mux.HandleFunc("POST /api/messages/{id}/hide", app.requireAuth(app.handleHideMessageForMe))
	mux.HandleFunc("PUT /api/conversations/{id}/draft", app.requireAuth(app.handleSaveDraft))
	mux.HandleFunc("GET /api/conversations/{id}/draft", app.requireAuth(app.handleGetDraft))
	mux.HandleFunc("POST /api/link-preview", app.requireAuth(app.handleLinkPreview))
	mux.HandleFunc("POST /api/media/{id}/transcode", app.requireAuth(app.handleRequestTranscode))
	mux.HandleFunc("GET /api/media/{id}/transcode", app.requireAuth(app.handleTranscodeStatus))

	// internal control plane (transcode worker; shared-secret bearer)
	mux.HandleFunc("POST /internal/transcode/claim", app.requireInternal(app.handleTranscodeClaim))
	mux.HandleFunc("POST /internal/transcode/complete", app.requireInternal(app.handleTranscodeComplete))

	// admin (role-gated)
	mux.HandleFunc("GET /api/admin/stats", app.requireAdmin("superadmin", "moderator", "support", "finance", "ads_reviewer")(app.handleAdminStats))
	mux.HandleFunc("GET /api/admin/users", app.requireAdmin("superadmin", "support")(app.handleAdminListUsers))
	mux.HandleFunc("POST /api/admin/users/{id}/status", app.requireAdmin("superadmin", "moderator")(app.handleAdminSetUserStatus))
	mux.HandleFunc("GET /api/admin/reports", app.requireAdmin("superadmin", "moderator")(app.handleAdminListReports))
	mux.HandleFunc("POST /api/admin/reports/{id}/resolve", app.requireAdmin("superadmin", "moderator")(app.handleAdminResolveReport))
	mux.HandleFunc("GET /api/admin/kyc", app.requireAdmin("superadmin", "finance", "support")(app.handleAdminListKYC))
	mux.HandleFunc("POST /api/admin/kyc/{id}/review", app.requireAdmin("superadmin", "finance")(app.handleAdminReviewKYC))
	mux.HandleFunc("GET /api/admin/ads", app.requireAdmin("superadmin", "ads_reviewer")(app.handleAdminListAds))
	mux.HandleFunc("POST /api/admin/ads/{id}/review", app.requireAdmin("superadmin", "ads_reviewer")(app.handleAdminReviewAd))
	mux.HandleFunc("POST /api/admin/roles", app.requireAdmin("superadmin")(app.handleAdminGrantRole))
	mux.HandleFunc("GET /api/admin/wallet/tokens", app.requireAdmin("superadmin", "finance")(app.handleAdminListTokens))
	mux.HandleFunc("POST /api/admin/wallet/tokens", app.requireAdmin("superadmin", "finance")(app.handleAdminAddToken))
	mux.HandleFunc("POST /api/admin/wallet/tokens/{id}/status", app.requireAdmin("superadmin", "finance")(app.handleAdminSetTokenStatus))
	mux.HandleFunc("DELETE /api/admin/wallet/tokens/{id}", app.requireAdmin("superadmin")(app.handleAdminDeleteToken))
	mux.HandleFunc("GET /api/admin/payouts", app.requireAdmin("superadmin", "finance")(app.handleAdminListPayouts))
	mux.HandleFunc("POST /api/admin/payouts/{id}/review", app.requireAdmin("superadmin", "finance")(app.handleAdminReviewPayout))

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           withSecurityHeaders(withCORS(mux, cfg.AllowedOrigins)),
		ReadHeaderTimeout: 10 * time.Second,
	}
	log.Printf("ChatApp API listening on :%s (env=%s)", cfg.Port, cfg.AppEnv)
	log.Fatal(srv.ListenAndServe())
}
