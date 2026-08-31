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

	app := &App{cfg: cfg, db: db, hub: newHub(), cache: newCache(cfg.RedisURL)}
	app.startCluster()
	app.startScheduler()
	app.startExpirySweeper()
	app.startPushWorker()
	app.startBotWebhookWorker()
	app.startSubscriptionWorker()
	app.startRelayBridge()
	app.smtp = &Mailer{host: cfg.SMTPHost, port: cfg.SMTPPort, user: cfg.SMTPUser, pass: cfg.SMTPPass}
	app.otp = NewOTPService(app, cfg.AppEnv == "development")
	app.startDigestWorker()
	app.startAccountTTLWorker()
	app.startPremiumWorker()
	app.startChainWatchers()
	app.startPriceWorker()

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
	recoveryLimiter := newRateLimiter(5, 2)
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
	// GET /api/u/{username} — public profile by @username (web /u/<name> page).
	mux.HandleFunc("GET /api/u/{username}", app.requireAuth(app.handleGetUserByUsername))
	mux.HandleFunc("GET /api/search/posts", app.requireAuth(app.handleSearchPosts))
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
	mux.HandleFunc("GET /api/moments", app.requireAuth(app.handleListMoments))
	mux.HandleFunc("GET /api/moments/{id}", app.requireAuth(app.handleGetMoment))
	mux.HandleFunc("GET /api/hashtags/trending", app.requireAuth(app.handleTrending))
	mux.HandleFunc("DELETE /api/posts/{id}/repost", app.requireAuth(app.handleUnrepost))
	mux.HandleFunc("PATCH /api/posts/{id}", app.requireAuth(app.handleEditPost))
	mux.HandleFunc("GET /api/posts/{id}", app.requireAuth(app.handleGetPost))
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
	mux.HandleFunc("GET /api/e2e/verify/{userId}", app.requireAuth(app.handleE2EVerify))

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
	mux.HandleFunc("GET /api/prices", app.requireAuth(app.handlePrices))
	mux.HandleFunc("GET /api/staking/assets", app.requireAuth(app.handleStakingAssets))
	mux.HandleFunc("POST /api/staking/stake", app.requireAuth(app.handleStake))
	mux.HandleFunc("GET /api/staking/positions", app.requireAuth(app.handleStakingPositions))
	mux.HandleFunc("POST /api/staking/positions/{id}/unlock", app.requireAuth(app.handleStakingUnlock))
	mux.HandleFunc("POST /api/calls/rooms", app.requireAuth(app.handleCreateCallRoom))
	// persistent drop-in rooms (Messenger Rooms parity)
	mux.HandleFunc("POST /api/rooms", app.requireAuth(app.handleCreateDropinRoom))
	mux.HandleFunc("GET /api/rooms/{slug}", app.requireAuth(app.handleGetDropinRoom))
	mux.HandleFunc("POST /api/rooms/{slug}/join", app.requireAuth(app.handleJoinDropinRoom))
	mux.HandleFunc("POST /api/rooms/{slug}/end", app.requireAuth(app.handleEndDropinRoom))
	mux.HandleFunc("GET /api/me/rooms", app.requireAuth(app.handleListMyDropinRooms))
	mux.HandleFunc("POST /api/calls/rooms/{roomId}/join", app.requireAuth(app.handleJoinCallRoom))
	mux.HandleFunc("GET /api/live", app.requireAuth(app.handleLiveNow))
	mux.HandleFunc("GET /api/wallet/history", app.requireAuth(app.handleWalletHistory))
	mux.HandleFunc("POST /api/wallet/withdraw", app.requireAuth(app.handleWithdraw))
	mux.HandleFunc("GET /api/wallet/withdrawals", app.requireAuth(app.handleListWithdrawals))
	mux.HandleFunc("POST /api/kyc/submit", app.requireAuth(app.handleKYCSubmit))
	mux.HandleFunc("GET /api/kyc/status", app.requireAuth(app.handleKYCStatus))

	// crypto convert
	mux.HandleFunc("GET /api/convert/rates", app.requireAuth(app.handleConvertRates))
	mux.HandleFunc("GET /api/convert/quote", app.requireAuth(app.handleConvertQuote))
	mux.HandleFunc("POST /api/convert", app.requireAuth(app.handleConvert))
	mux.HandleFunc("GET /api/convert/history", app.requireAuth(app.handleConvertHistory))

	// P2P marketplace
	mux.HandleFunc("GET /api/p2p/payment-methods", app.requireAuth(app.handleP2PPaymentMethods))
	mux.HandleFunc("GET /api/p2p/offers", app.requireAuth(app.handleP2PListOffers))
	mux.HandleFunc("POST /api/p2p/offers", app.requireAuth(app.handleP2PCreateOffer))
	mux.HandleFunc("GET /api/p2p/offers/mine", app.requireAuth(app.handleP2PMyOffers))
	mux.HandleFunc("POST /api/p2p/offers/{id}/status", app.requireAuth(app.handleP2PSetOfferActive))
	mux.HandleFunc("POST /api/p2p/trades", app.requireAuth(app.handleP2POpenTrade))
	mux.HandleFunc("GET /api/p2p/trades", app.requireAuth(app.handleP2PMyTrades))
	mux.HandleFunc("POST /api/p2p/trades/{id}/paid", app.requireAuth(app.handleP2PTradePaid))
	mux.HandleFunc("POST /api/p2p/trades/{id}/release", app.requireAuth(app.handleP2PTradeRelease))
	mux.HandleFunc("POST /api/p2p/trades/{id}/cancel", app.requireAuth(app.handleP2PTradeCancel))
	mux.HandleFunc("POST /api/p2p/trades/{id}/dispute", app.requireAuth(app.handleP2PTradeDispute))

	// P2P merchant program
	mux.HandleFunc("POST /api/p2p/merchant/apply", app.requireAuth(app.handleMerchantApply))
	mux.HandleFunc("GET /api/p2p/merchant/status", app.requireAuth(app.handleMerchantStatus))
	mux.HandleFunc("GET /api/p2p/merchant/tiers", app.requireAuth(app.handleMerchantTiers))

	// crypto cards
	mux.HandleFunc("POST /api/cards", app.requireAuth(app.handleCardIssue))
	mux.HandleFunc("GET /api/cards", app.requireAuth(app.handleCardList))
	mux.HandleFunc("POST /api/cards/charge", app.requireAuth(app.handleCardCharge))
	mux.HandleFunc("POST /api/cards/{id}/topup", app.requireAuth(app.handleCardTopUp))
	mux.HandleFunc("POST /api/cards/{id}/refund", app.requireAuth(app.handleCardRefund))
	mux.HandleFunc("POST /api/cards/{id}/status", app.requireAuth(app.handleCardSetStatus))
	mux.HandleFunc("PUT /api/cards/{id}/limits", app.requireAuth(app.handleCardSetLimits))
	mux.HandleFunc("GET /api/cards/{id}/transactions", app.requireAuth(app.handleCardTransactions))

	// social parity: reactions, pinning, edit history, scheduled posts, albums
	mux.HandleFunc("PUT /api/posts/{id}/react", app.requireAuth(app.handleReactPost))
	mux.HandleFunc("DELETE /api/posts/{id}/react", app.requireAuth(app.handleUnreactPost))
	mux.HandleFunc("GET /api/posts/{id}/reactions", app.requireAuth(app.handlePostReactions))
	mux.HandleFunc("PUT /api/me/pinned-post", app.requireAuth(app.handlePinPost))
	mux.HandleFunc("DELETE /api/me/pinned-post", app.requireAuth(app.handleUnpinPost))
	mux.HandleFunc("GET /api/posts/{id}/edits", app.requireAuth(app.handlePostEditHistory))
	mux.HandleFunc("GET /api/me/scheduled-posts", app.requireAuth(app.handleScheduledPosts))
	mux.HandleFunc("DELETE /api/scheduled-posts/{id}", app.requireAuth(app.handleCancelScheduledPost))
	mux.HandleFunc("POST /api/albums", app.requireAuth(app.handleCreateAlbum))
	mux.HandleFunc("GET /api/albums", app.requireAuth(app.handleMyAlbums))
	mux.HandleFunc("GET /api/albums/{id}", app.requireAuth(app.handleGetAlbum))
	mux.HandleFunc("DELETE /api/albums/{id}", app.requireAuth(app.handleDeleteAlbum))
	mux.HandleFunc("POST /api/albums/{id}/items", app.requireAuth(app.handleAlbumAddItem))
	mux.HandleFunc("DELETE /api/albums/{id}/items/{postId}", app.requireAuth(app.handleAlbumRemoveItem))
	mux.HandleFunc("GET /api/users/{id}/albums", app.requireAuth(app.handleUserAlbums))

	// account safety: trusted contacts, recovery, legacy contact, profiles
	mux.HandleFunc("GET /api/me/trusted-contacts", app.requireAuth(app.handleListTrustedContacts))
	mux.HandleFunc("POST /api/me/trusted-contacts", app.requireAuth(app.handleAddTrustedContact))
	mux.HandleFunc("DELETE /api/me/trusted-contacts/{contactId}", app.requireAuth(app.handleRemoveTrustedContact))
	mux.HandleFunc("POST /api/recovery/trusted/request", recoveryLimiter.limit(app.handleRecoveryRequest))
	mux.HandleFunc("GET /api/recovery/trusted/pending", app.requireAuth(app.handleRecoveryPending))
	mux.HandleFunc("POST /api/recovery/trusted/reveal", app.requireAuth(app.handleRecoveryReveal))
	mux.HandleFunc("POST /api/recovery/trusted/redeem", recoveryLimiter.limit(app.handleRecoveryRedeem))
	mux.HandleFunc("GET /api/me/legacy-contact", app.requireAuth(app.handleGetLegacyContact))
	mux.HandleFunc("PUT /api/me/legacy-contact", app.requireAuth(app.handleSetLegacyContact))
	mux.HandleFunc("DELETE /api/me/legacy-contact", app.requireAuth(app.handleRemoveLegacyContact))
	mux.HandleFunc("GET /api/legacy/{userId}/export", app.requireAuth(app.handleLegacyExport))
	mux.HandleFunc("GET /api/me/profiles", app.requireAuth(app.handleListMyProfiles))
	mux.HandleFunc("POST /api/me/profiles", app.requireAuth(app.handleCreateProfile))
	mux.HandleFunc("DELETE /api/me/profiles/{id}", app.requireAuth(app.handleDeleteProfile))
	mux.HandleFunc("PUT /api/me/active-profile", app.requireAuth(app.handleSwitchProfile))
	mux.HandleFunc("PUT /api/me/digest", app.requireAuth(app.handleSetDigest))

	// chat extras: polls, video notes, live location, pay-in-chat
	mux.HandleFunc("POST /api/conversations/{id}/polls", app.requireAuth(app.handleCreateChatPoll))
	mux.HandleFunc("GET /api/chat-polls/{id}", app.requireAuth(app.handleGetChatPoll))
	mux.HandleFunc("POST /api/chat-polls/{id}/vote", app.requireAuth(app.handleChatPollVote))
	mux.HandleFunc("POST /api/conversations/{id}/video-note", app.requireAuth(app.handleSendVideoNote))
	mux.HandleFunc("PUT /api/conversations/{id}/live-location", app.requireAuth(app.handleShareLiveLocation))
	mux.HandleFunc("DELETE /api/conversations/{id}/live-location", app.requireAuth(app.handleStopLiveLocation))
	mux.HandleFunc("GET /api/conversations/{id}/live-location", app.requireAuth(app.handleGetLiveLocations))
	mux.HandleFunc("POST /api/conversations/{id}/pay", app.requireAuth(app.handlePayInChat))

	// reels + notes
	mux.HandleFunc("GET /api/reels/{id}/related", app.requireAuth(app.handleRelatedReels))
	mux.HandleFunc("GET /api/reels/{id}/analytics", app.requireAuth(app.handleReelAnalytics))
	mux.HandleFunc("GET /api/reels/{id}/remixes", app.requireAuth(app.handleReelRemixes))
	mux.HandleFunc("POST /api/posts/{id}/notes", app.requireAuth(app.handleCreateNote))
	mux.HandleFunc("GET /api/posts/{id}/notes", app.requireAuth(app.handleListNotes))
	mux.HandleFunc("DELETE /api/notes/{id}", app.requireAuth(app.handleDeleteNote))
	mux.HandleFunc("POST /api/notes/{id}/vote", app.requireAuth(app.handleVoteNote))

	// calls: screen share + recordings
	mux.HandleFunc("POST /api/calls/rooms/{roomId}/screenshare", app.requireAuth(app.handleScreenShare))
	mux.HandleFunc("POST /api/calls/rooms/{roomId}/recordings", app.requireAuth(app.handleSaveRecording))
	mux.HandleFunc("GET /api/calls/rooms/{roomId}/recordings", app.requireAuth(app.handleListRecordings))
	mux.HandleFunc("DELETE /api/calls/recordings/{id}", app.requireAuth(app.handleDeleteRecording))

	// admin: memorialize, media moderation, sanctions, derived rates
	mux.HandleFunc("POST /api/admin/users/{userId}/memorialize", app.requireAdmin("superadmin", "admin")(app.handleAdminMemorialize))
	mux.HandleFunc("POST /api/admin/moderation/block-hash", app.requireAdmin("superadmin", "admin")(app.handleAdminBlockHash))
	mux.HandleFunc("GET /api/admin/moderation/blocked-hashes", app.requireAdmin("superadmin", "admin")(app.handleAdminListBlockedHashes))
	mux.HandleFunc("DELETE /api/admin/moderation/block-hash/{id}", app.requireAdmin("superadmin", "admin")(app.handleAdminUnblockHash))
	mux.HandleFunc("GET /api/admin/moderation/media", app.requireAdmin("superadmin", "admin")(app.handleAdminMediaModeration))
	mux.HandleFunc("POST /api/admin/sanctions/import", app.requireAdmin("superadmin", "admin")(app.handleAdminImportSanctions))
	mux.HandleFunc("GET /api/admin/sanctions/stats", app.requireAdmin("superadmin", "admin")(app.handleAdminSanctionsStats))
	mux.HandleFunc("GET /api/admin/convert/rates/derived", app.requireAdmin("superadmin", "admin")(app.handleDerivedRates))
	mux.HandleFunc("POST /api/admin/convert/rates/apply-derived", app.requireAdmin("superadmin", "admin")(app.handleApplyDerivedRates))

	// chat personalization
	mux.HandleFunc("PUT /api/conversations/{id}/theme", app.requireAuth(app.handleSetChatTheme))
	mux.HandleFunc("PUT /api/conversations/{id}/nicknames/{userId}", app.requireAuth(app.handleSetNickname))

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
	mux.HandleFunc("POST /api/uploads", app.requireAuth(app.handleCreateUploadSession))
	mux.HandleFunc("GET /api/uploads/{id}", app.requireAuth(app.handleGetUploadSession))
	mux.HandleFunc("POST /api/uploads/{id}/complete", app.requireAuth(app.handleCompleteUploadSession))
	mux.HandleFunc("POST /api/uploads/{id}/abort", app.requireAuth(app.handleAbortUploadSession))

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
	mux.HandleFunc("POST /api/bot/{token}/editMessageText", app.handleBotEditMessage)
	mux.HandleFunc("POST /api/bot/{token}/deleteMessage", app.handleBotDeleteMessage)
	mux.HandleFunc("POST /api/bot/{token}/sendPhoto", app.handleBotSendPhoto)
	mux.HandleFunc("GET /api/bot/{token}/getChat", app.handleBotGetChat)
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

	// gap pack 3: privacy depth, sessions, archive, stickers, folders, lists,
	// bookmark folders, profile visitors, playlists, verification, reply tools
	mux.HandleFunc("GET /api/me/privacy", app.requireAuth(app.handleGetPrivacy))
	mux.HandleFunc("PUT /api/me/privacy", app.requireAuth(app.handleSetPrivacy))
	mux.HandleFunc("GET /api/me/sessions", app.requireAuth(app.handleListSessions))
	mux.HandleFunc("DELETE /api/me/sessions/{id}", app.requireAuth(app.handleRevokeSession))
	mux.HandleFunc("PUT /api/conversations/{id}/archive", app.requireAuth(app.handleArchiveConversation))
	mux.HandleFunc("DELETE /api/conversations/{id}/archive", app.requireAuth(app.handleUnarchiveConversation))
	mux.HandleFunc("PUT /api/conversations/{id}/handle", app.requireAuth(app.handleSetGroupHandle))
	mux.HandleFunc("PUT /api/conversations/{id}/members/{uid}/role", app.requireAuth(app.handleSetMemberRole))

	// ---- Gap pack 4 (migration 019) ----
	// Drafts (X/TikTok)
	mux.HandleFunc("POST /api/me/drafts", app.requireAuth(app.handleCreateDraft))
	mux.HandleFunc("GET /api/me/drafts", app.requireAuth(app.handleListDrafts))
	mux.HandleFunc("DELETE /api/me/drafts/{id}", app.requireAuth(app.handleDeleteDraft))
	// Topics / interests (X)
	mux.HandleFunc("GET /api/topics", app.requireAuth(app.handleListInterestTopics))
	mux.HandleFunc("POST /api/topics", app.requireAuth(app.handleCreateTopic4))
	mux.HandleFunc("POST /api/topics/{id}/follow", app.requireAuth(app.handleFollowTopic))
	mux.HandleFunc("DELETE /api/topics/{id}/follow", app.requireAuth(app.handleFollowTopic))
	// Verified organizations (X)
	mux.HandleFunc("POST /api/organizations", app.requireAuth(app.handleCreateOrg))
	mux.HandleFunc("GET /api/organizations/{id}", app.requireAuth(app.handleGetOrg))
	mux.HandleFunc("POST /api/organizations/{id}/members", app.requireAuth(app.handleOrgAddMember))
	mux.HandleFunc("DELETE /api/organizations/{id}/members/{uid}", app.requireAuth(app.handleOrgRemoveMember))
	mux.HandleFunc("POST /api/admin/organizations/{id}/verify", app.requireAdmin("superadmin")(app.handleAdminVerifyOrg))
	// Who-to-follow suggestions (X)
	mux.HandleFunc("GET /api/me/suggestions", app.requireAuth(app.handleWhoToFollow))
	// Audio rooms (X Spaces / Telegram voice chats / imo voice clubs)
	mux.HandleFunc("POST /api/audio-rooms", app.requireAuth(app.handleCreateAudioRoom))
	mux.HandleFunc("GET /api/audio-rooms", app.requireAuth(app.handleDiscoverAudioRooms))
	mux.HandleFunc("GET /api/audio-rooms/{id}", app.requireAuth(app.handleGetAudioRoom))
	mux.HandleFunc("POST /api/audio-rooms/{id}/start", app.requireAuth(app.handleStartAudioRoom))
	mux.HandleFunc("POST /api/audio-rooms/{id}/end", app.requireAuth(app.handleEndAudioRoom))
	mux.HandleFunc("POST /api/audio-rooms/{id}/join", app.requireAuth(app.handleJoinAudioRoom))
	mux.HandleFunc("POST /api/audio-rooms/{id}/recordings", app.requireAuth(app.handleSaveAudioRoomRecording))
	mux.HandleFunc("GET /api/audio-rooms/{id}/recordings", app.requireAuth(app.handleListAudioRoomRecordings))
	mux.HandleFunc("DELETE /api/audio-room-recordings/{id}", app.requireAuth(app.handleDeleteAudioRoomRecording))
	mux.HandleFunc("POST /api/audio-rooms/{id}/hand", app.requireAuth(app.handleRoomHand))
	mux.HandleFunc("PUT /api/audio-rooms/{id}/speakers/{uid}", app.requireAuth(app.handleRoomSpeaker))
	mux.HandleFunc("DELETE /api/audio-rooms/{id}/speakers/{uid}", app.requireAuth(app.handleRoomSpeaker))
	// Premium plans (X Premium / Telegram Premium)
	mux.HandleFunc("GET /api/premium/plans", app.requireAuth(app.handlePremiumPlans))
	mux.HandleFunc("POST /api/premium/subscribe", app.requireAuth(app.handlePremiumSubscribe))
	// Self-hosted GIF catalog + GIF messages
	mux.HandleFunc("POST /api/gifs", app.requireAuth(app.handleUploadGIF))
	mux.HandleFunc("GET /api/gifs", app.requireAuth(app.handleSearchGIFs))
	mux.HandleFunc("POST /api/conversations/{id}/gif", app.requireAuth(app.handleSendGIFMessage))
	// Contact-card messages (Telegram)
	mux.HandleFunc("POST /api/conversations/{id}/contact", app.requireAuth(app.handleSendContactCard))
	// Channel discussion groups + stats, anonymous admins (Telegram)
	mux.HandleFunc("PUT /api/channels/{id}/discussion", app.requireAuth(app.handleLinkDiscussion))
	mux.HandleFunc("GET /api/channels/{id}/stats", app.requireAuth(app.handleChannelStats))
	mux.HandleFunc("PUT /api/conversations/{id}/anonymous-admin", app.requireAuth(app.handleAnonymousAdmin))
	// Sounds library (TikTok/FB)
	mux.HandleFunc("POST /api/sounds", app.requireAuth(app.handleCreateSound))
	mux.HandleFunc("GET /api/sounds", app.requireAuth(app.handleSearchSounds))
	// Shares with counter ship via POST /api/posts/{id}/share (handleSharePostToChat);
	// paywalled series, content ratings (TikTok)
	mux.HandleFunc("PUT /api/posts/{id}/price", app.requireAuth(app.handleSetPostPrice))
	mux.HandleFunc("POST /api/posts/{id}/purchase", app.requireAuth(app.handlePurchasePost))
	mux.HandleFunc("PUT /api/posts/{id}/rating", app.requireAuth(app.handleSetContentRating))
	// Marketplace + fundraisers (Facebook)
	mux.HandleFunc("POST /api/marketplace", app.requireAuth(app.handleCreateListing))
	mux.HandleFunc("GET /api/marketplace", app.requireAuth(app.handleListListings))
	mux.HandleFunc("PUT /api/marketplace/{id}/status", app.requireAuth(app.handleListingStatus))
	mux.HandleFunc("POST /api/fundraisers", app.requireAuth(app.handleCreateFundraiser))
	mux.HandleFunc("GET /api/fundraisers/{id}", app.requireAuth(app.handleGetFundraiser))
	mux.HandleFunc("POST /api/fundraisers/{id}/donate", app.requireAuth(app.handleDonate))
	// Restricted mode + family pairing (TikTok)
	mux.HandleFunc("PUT /api/me/restricted-mode", app.requireAuth(app.handleRestrictedMode))
	mux.HandleFunc("POST /api/family/link", app.requireAuth(app.handleFamilyLink))
	mux.HandleFunc("POST /api/family/accept", app.requireAuth(app.handleFamilyAccept))
	mux.HandleFunc("GET /api/family", app.requireAuth(app.handleFamilyList))
	// XP / levels (imo)
	mux.HandleFunc("GET /api/me/level", app.requireAuth(app.handleMyLevel))
	mux.HandleFunc("GET /api/users/{username}/level", app.requireAuth(app.handleUserLevel))
	// People nearby + group discovery (Telegram/imo)
	mux.HandleFunc("PUT /api/me/discoverable", app.requireAuth(app.handleDiscoverable))
	mux.HandleFunc("GET /api/nearby", app.requireAuth(app.handlePeopleNearby))
	mux.HandleFunc("GET /api/discover/groups", app.requireAuth(app.handleDiscoverGroups))
	mux.HandleFunc("PUT /api/conversations/{id}/category", app.requireAuth(app.handleSetGroupCategory))
	// Chat backup + screenshot alerts (imo)
	mux.HandleFunc("GET /api/me/export", app.requireAuth(app.handleExportData))
	mux.HandleFunc("POST /api/conversations/{id}/screenshot", app.requireAuth(app.handleScreenshotAlert))
	// Bot payments + inline bots (Telegram); mini apps via POST /api/bots/{id}/mini-app
	mux.HandleFunc("POST /api/bot/{token}/createInvoice", app.handleBotCreateInvoice)
	mux.HandleFunc("POST /api/bots/invoices/{id}/pay", app.requireAuth(app.handleBotPayInvoice))
	mux.HandleFunc("GET /api/bots/inline", app.requireAuth(app.handleInlineQuery))
	// Live gifts + leaderboard (TikTok/imo)
	mux.HandleFunc("POST /api/live/{roomId}/gifts", app.requireAuth(app.handleSendLiveGift))
	mux.HandleFunc("GET /api/live/{roomId}/leaderboard", app.requireAuth(app.handleLiveLeaderboard))
	// Live rooms: broadcaster lifecycle + viewer tracking (Facebook Live / TikTok LIVE)
	mux.HandleFunc("POST /api/live-rooms", app.requireAuth(app.handleCreateLiveRoom))
	mux.HandleFunc("GET /api/live-rooms", app.requireAuth(app.handleListLiveRooms))
	mux.HandleFunc("GET /api/live-rooms/{id}", app.requireAuth(app.handleGetLiveRoom))
	mux.HandleFunc("POST /api/live-rooms/{id}/end", app.requireAuth(app.handleEndLiveRoom))
	mux.HandleFunc("PUT /api/live-rooms/{id}/replay", app.requireAuth(app.handleSetLiveRoomReplay))
	mux.HandleFunc("POST /api/live-rooms/{id}/join", app.requireAuth(app.handleLiveRoomJoin))
	mux.HandleFunc("POST /api/live-rooms/{id}/leave", app.requireAuth(app.handleLiveRoomLeave))
	mux.HandleFunc("POST /api/live-rooms/{id}/like", app.requireAuth(app.handleLiveRoomLike))
	// Custom emoji (Telegram) + message translation (imo)
	mux.HandleFunc("GET /api/custom-emoji", app.requireAuth(app.handleListCustomEmoji))
	mux.HandleFunc("POST /api/admin/custom-emoji", app.requireAdmin("superadmin", "admin")(app.handleAdminAddCustomEmoji))
	mux.HandleFunc("DELETE /api/admin/custom-emoji/{code}", app.requireAdmin("superadmin", "admin")(app.handleAdminDeleteCustomEmoji))
	mux.HandleFunc("POST /api/messages/{id}/translate", app.requireAuth(app.handleTranslateMessage))
	mux.HandleFunc("GET /api/messages/{id}/translations", app.requireAuth(app.handleMessageTranslations))
	// Creator marketplace (TikTok)
	mux.HandleFunc("POST /api/brand-deals", app.requireAuth(app.handleCreateBrandDeal))
	mux.HandleFunc("GET /api/brand-deals", app.requireAuth(app.handleListBrandDeals))
	mux.HandleFunc("POST /api/brand-deals/{id}/accept", app.requireAuth(app.handleAcceptBrandDeal))

	// ---- Gap pack 8 (migration 024) ----
	// TikTok editor: duet/stitch compositor + trim + voiceover mix
	mux.HandleFunc("POST /api/reels/{id}/duet", app.requireAuth(app.handleDuet))
	mux.HandleFunc("POST /api/reels/{id}/stitch", app.requireAuth(app.handleStitch))
	mux.HandleFunc("POST /api/media/{id}/trim", app.requireAuth(app.handleTrimMedia))
	mux.HandleFunc("POST /api/media/{id}/mix", app.requireAuth(app.handleMixMedia))
	// HLS live ingest (unlimited viewers) + live co-hosting
	mux.HandleFunc("POST /api/live-rooms/{id}/ingest", app.requireAuth(app.handleLiveIngestStart))
	mux.HandleFunc("GET /api/live-rooms/{id}/stream", app.requireAuth(app.handleLiveIngestInfo))
	mux.HandleFunc("POST /api/live-rooms/{id}/ingest/end", app.requireAuth(app.handleLiveIngestEnd))
	mux.HandleFunc("POST /api/live-rooms/{id}/cohosts", app.requireAuth(app.handleLiveCohost))
	// Marketplace checkout + affiliate attribution (Shop)
	mux.HandleFunc("POST /api/marketplace/{id}/buy", app.requireAuth(app.handleMarketplaceBuy))
	mux.HandleFunc("GET /api/me/orders", app.requireAuth(app.handleListOrders))
	// Profile Q&A (TikTok)
	mux.HandleFunc("GET /api/users/{id}/questions", app.requireAuth(app.handleListQuestions))
	mux.HandleFunc("POST /api/users/{id}/questions", app.requireAuth(app.handleAskQuestion))
	mux.HandleFunc("POST /api/questions/{id}/answer", app.requireAuth(app.handleAnswerQuestion))
	mux.HandleFunc("DELETE /api/questions/{id}", app.requireAuth(app.handleDeleteQuestion))
	// Screen-time limits + app lock + password proof (Telegram)
	mux.HandleFunc("PUT /api/me/screen-time", app.requireAuth(app.handleSetScreenTime))
	mux.HandleFunc("GET /api/me/screen-time", app.requireAuth(app.handleGetScreenTime))
	mux.HandleFunc("POST /api/me/screen-time/ping", app.requireAuth(app.handlePingScreenTime))
	mux.HandleFunc("PUT /api/me/app-lock", app.requireAuth(app.handleSetAppLock))
	mux.HandleFunc("POST /api/auth/verify-password", app.requireAuth(app.handleVerifyPassword))
	// FYP feature-store rollup (X) + group scale probe (admin)
	mux.HandleFunc("GET /api/me/feature-vector", app.requireAuth(app.handleFeatureVector))
	mux.HandleFunc("GET /api/admin/groups/scale", app.requireAdmin("superadmin")(app.handleGroupScaleReport))
	// Professional dashboard (Facebook)
	mux.HandleFunc("GET /api/me/analytics", app.requireAuth(app.handleProDashboard))
	mux.HandleFunc("PUT /api/conversations/{id}/members/{uid}/permissions", app.requireAuth(app.handleSetMemberPermissions))
	mux.HandleFunc("GET /api/handles/{handle}", app.requireAuth(app.handleGetByHandle))
	mux.HandleFunc("POST /api/handles/{handle}/join", app.requireAuth(app.handleJoinByHandle))
	mux.HandleFunc("POST /api/sticker-packs", app.requireAuth(app.handleCreateStickerPack))
	mux.HandleFunc("GET /api/sticker-packs", app.requireAuth(app.handleListStickerPacks))
	mux.HandleFunc("POST /api/sticker-packs/{id}/stickers", app.requireAuth(app.handleAddSticker))
	mux.HandleFunc("GET /api/sticker-packs/{id}/stickers", app.requireAuth(app.handleListStickers))
	mux.HandleFunc("POST /api/conversations/{id}/sticker", app.requireAuth(app.handleSendSticker))
	mux.HandleFunc("POST /api/me/chat-folders", app.requireAuth(app.handleCreateChatFolder))
	mux.HandleFunc("GET /api/me/chat-folders", app.requireAuth(app.handleListChatFolders))
	mux.HandleFunc("DELETE /api/me/chat-folders/{id}", app.requireAuth(app.handleDeleteChatFolder))
	mux.HandleFunc("PUT /api/me/chat-folders/{id}/conversations", app.requireAuth(app.handleSetChatFolderConversations))
	mux.HandleFunc("POST /api/me/lists", app.requireAuth(app.handleCreateList))
	mux.HandleFunc("GET /api/me/lists", app.requireAuth(app.handleMyLists))
	mux.HandleFunc("DELETE /api/me/lists/{id}", app.requireAuth(app.handleDeleteList))
	mux.HandleFunc("PUT /api/lists/{id}/members/{uid}", app.requireAuth(app.handleListAddMember))
	mux.HandleFunc("DELETE /api/lists/{id}/members/{uid}", app.requireAuth(app.handleListRemoveMember))
	mux.HandleFunc("GET /api/lists/{id}/feed", app.requireAuth(app.handleListFeed))
	mux.HandleFunc("POST /api/me/bookmark-folders", app.requireAuth(app.handleCreateBookmarkFolder))
	mux.HandleFunc("GET /api/me/bookmark-folders", app.requireAuth(app.handleListBookmarkFolders))
	mux.HandleFunc("DELETE /api/me/bookmark-folders/{id}", app.requireAuth(app.handleDeleteBookmarkFolder))
	mux.HandleFunc("PUT /api/bookmarks/{postId}/folder", app.requireAuth(app.handleBookmarkSetFolder))
	mux.HandleFunc("GET /api/me/profile-visitors", app.requireAuth(app.handleProfileVisitors))
	mux.HandleFunc("POST /api/me/playlists", app.requireAuth(app.handleCreatePlaylist))
	mux.HandleFunc("GET /api/me/playlists", app.requireAuth(app.handleMyPlaylists))
	mux.HandleFunc("DELETE /api/me/playlists/{id}", app.requireAuth(app.handleDeletePlaylist))
	mux.HandleFunc("GET /api/users/{id}/playlists", app.requireAuth(app.handleUserPlaylists))
	mux.HandleFunc("GET /api/playlists/{id}", app.requireAuth(app.handleGetPlaylist))
	mux.HandleFunc("POST /api/playlists/{id}/items", app.requireAuth(app.handlePlaylistAddItem))
	mux.HandleFunc("DELETE /api/playlists/{id}/items/{postId}", app.requireAuth(app.handlePlaylistRemoveItem))
	mux.HandleFunc("POST /api/me/verification-requests", app.requireAuth(app.handleRequestVerification))
	mux.HandleFunc("GET /api/me/verification-requests", app.requireAuth(app.handleMyVerificationRequests))
	mux.HandleFunc("GET /api/admin/verification-requests", app.requireAdmin("superadmin", "moderator")(app.handleAdminListVerification))
	mux.HandleFunc("POST /api/admin/verification-requests/{id}/review", app.requireAdmin("superadmin", "moderator")(app.handleAdminReviewVerification))
	mux.HandleFunc("POST /api/comments/{id}/hide", app.requireAuth(app.handleHideComment))
	mux.HandleFunc("POST /api/comments/{id}/unhide", app.requireAuth(app.handleUnhideComment))
	mux.HandleFunc("PUT /api/posts/{id}/pinned-comment", app.requireAuth(app.handlePinComment))

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
	mux.HandleFunc("GET /api/admin/moments", app.requireAdmin("superadmin", "moderator")(app.handleAdminListMoments))
mux.HandleFunc("POST /api/admin/moments", app.requireAdmin("superadmin", "moderator")(app.handleAdminCreateMoment))
	mux.HandleFunc("POST /api/admin/moments/{id}/items", app.requireAdmin("superadmin", "moderator")(app.handleAdminMomentAddItem))
	mux.HandleFunc("DELETE /api/admin/moments/{id}/items/{postId}", app.requireAdmin("superadmin", "moderator")(app.handleAdminMomentRemoveItem))
	mux.HandleFunc("POST /api/admin/moments/{id}/publish", app.requireAdmin("superadmin", "moderator")(app.handleAdminMomentPublish))
	mux.HandleFunc("DELETE /api/admin/moments/{id}", app.requireAdmin("superadmin")(app.handleAdminDeleteMoment))
	mux.HandleFunc("POST /api/admin/ads/{id}/review", app.requireAdmin("superadmin", "ads_reviewer")(app.handleAdminReviewAd))
	mux.HandleFunc("POST /api/admin/roles", app.requireAdmin("superadmin")(app.handleAdminGrantRole))
	mux.HandleFunc("DELETE /api/admin/roles", app.requireAdmin("superadmin")(app.handleAdminRevokeRole))
	mux.HandleFunc("GET /api/admin/role-defs", app.requireAdmin("superadmin")(app.handleAdminListRoleDefs))
	mux.HandleFunc("POST /api/admin/role-defs", app.requireAdmin("superadmin")(app.handleAdminCreateRoleDef))
	mux.HandleFunc("DELETE /api/admin/role-defs/{name}", app.requireAdmin("superadmin")(app.handleAdminDeleteRoleDef))
	mux.HandleFunc("GET /api/admin/withdrawals", app.requireAdminPerm("withdrawals.review")(app.handleAdminListWithdrawals))
	mux.HandleFunc("POST /api/admin/withdrawals/{id}/review", app.requireAdminPerm("withdrawals.review")(app.handleAdminReviewWithdrawal))
	mux.HandleFunc("POST /api/admin/convert/rates", app.requireAdminPerm("convert.manage")(app.handleAdminSetConvertRate))
	mux.HandleFunc("GET /api/admin/p2p/disputes", app.requireAdminPerm("p2p.resolve")(app.handleAdminP2PDisputes))
	mux.HandleFunc("GET /api/admin/staking/assets", app.requireAdminPerm("staking.manage")(app.handleAdminStakingAssets))
	mux.HandleFunc("POST /api/admin/staking/assets", app.requireAdminPerm("staking.manage")(app.handleAdminStakingAssetCreate))
	mux.HandleFunc("PUT /api/admin/staking/assets/{id}", app.requireAdminPerm("staking.manage")(app.handleAdminStakingAssetUpdate))
	mux.HandleFunc("DELETE /api/admin/staking/assets/{id}", app.requireAdminPerm("staking.manage")(app.handleAdminStakingAssetDelete))
	mux.HandleFunc("GET /api/admin/staking/positions", app.requireAdminPerm("staking.manage")(app.handleAdminStakingPositions))
	mux.HandleFunc("GET /api/admin/staking/queue", app.requireAdminPerm("staking.manage")(app.handleAdminStakingQueue))
	mux.HandleFunc("POST /api/admin/staking/settle/{id}", app.requireAdminPerm("staking.manage")(app.handleAdminStakingSettle))
	mux.HandleFunc("GET /api/admin/staking/treasury", app.requireAdminPerm("staking.manage")(app.handleAdminStakingMovesList))
	mux.HandleFunc("POST /api/admin/staking/treasury/move", app.requireAdminPerm("staking.manage")(app.handleAdminStakingMove))
	mux.HandleFunc("PUT /api/admin/prices/{asset}/{chain}", app.requireAdminPerm("tokens.manage")(app.handleAdminPriceOverride))
	mux.HandleFunc("POST /api/admin/p2p/trades/{id}/resolve", app.requireAdminPerm("p2p.resolve")(app.handleAdminP2PResolve))
	mux.HandleFunc("GET /api/admin/p2p/merchants", app.requireAdminPerm("merchants.review")(app.handleAdminListMerchants))
	mux.HandleFunc("POST /api/admin/p2p/merchants/{userId}/review", app.requireAdminPerm("merchants.review")(app.handleAdminReviewMerchant))
	mux.HandleFunc("POST /api/admin/p2p/merchants/{userId}/revoke", app.requireAdminPerm("merchants.review")(app.handleAdminRevokeMerchant))
	mux.HandleFunc("POST /api/admin/p2p/merchants/{userId}/tier", app.requireAdminPerm("merchants.review")(app.handleAdminSetMerchantTier))
	mux.HandleFunc("POST /api/admin/p2p/merchant-tiers", app.requireAdminPerm("merchants.review")(app.handleAdminUpsertMerchantTier))
	mux.HandleFunc("GET /api/admin/cards", app.requireAdminPerm("cards.manage")(app.handleAdminListCards))
	mux.HandleFunc("POST /api/admin/cards/{id}/status", app.requireAdminPerm("cards.manage")(app.handleAdminSetCardStatus))
	mux.HandleFunc("GET /api/admin/transfers", app.requireAdminPerm("transfers.review")(app.handleAdminListTransfers))
	mux.HandleFunc("POST /api/admin/transfers/{txId}/reverse", app.requireAdminPerm("transfers.review")(app.handleAdminReverseTransfer))
	mux.HandleFunc("GET /api/admin/wallet/tokens", app.requireAdmin("superadmin", "finance")(app.handleAdminListTokens))
	mux.HandleFunc("POST /api/admin/wallet/tokens", app.requireAdmin("superadmin", "finance")(app.handleAdminAddToken))
	mux.HandleFunc("POST /api/admin/wallet/tokens/{id}/status", app.requireAdmin("superadmin", "finance")(app.handleAdminSetTokenStatus))
	mux.HandleFunc("POST /api/admin/wallet/tokens/{id}/features", app.requireAdminPerm("tokens.manage")(app.handleAdminSetTokenFeatures))
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
