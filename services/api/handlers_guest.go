package main

// Guest / anonymous session support (TorChat/SimpleX/Session/Briar-style).
//
// A guest session is a device-local ephemeral identity: no server-side account
// row is created, no username/password/email/phone is collected, and no
// IP-bound identity is stored. The client keeps a `chatapp.guest` marker in
// localStorage and calls POST /api/auth/guest to obtain a short-lived
// guest-scoped access token that lets it browse the public feature surface
// (feed, FYP, reels, stories, groups, pages, trending, search, public
// profiles, marketplace listings, price tickers) and use anonymous chat/calls
// where the backend permits. Logging out simply clears the device marker.

import (
	"context"
	"net/http"
	"strings"
	"time"
)

// handleGuestSession mints a guest-scoped access token. No account row is
// created. The token carries Scope "guest" so requireAuth accepts it on
// public/read routes while the rest of the platform still treats it as a
// non-member (no wallet, no posts, no admin).
func (a *App) handleGuestSession(w http.ResponseWriter, r *http.Request) {
	guestID, err := a.randomNum(16)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to create guest session")
		return
	}
	now := time.Now()
	access, err := a.mintClaims(Claims{
		Sub:   "guest_" + guestID,
		Type:  "access",
		Scope: "guest",
		Iat:   now.Unix(),
		Exp:   now.Add(a.cfg.AccessTokenTTL).Unix(),
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to create guest session")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"access_token": access,
		"token_type":   "Bearer",
		"expires_in":   int(a.cfg.AccessTokenTTL.Seconds()),
		"guest":        true,
		"guest_id":     guestID,
	})
}

// guestIDFrom returns the guest identity from a guest-scoped request, or "".
func guestIDFrom(r *http.Request) string {
	id := userIDFrom(r)
	if len(id) > 6 && id[:6] == "guest_" {
		return id[6:]
	}
	return ""
}

// isGuestRequest reports whether the current request carries a guest token.
func isGuestRequest(r *http.Request) bool {
	return guestIDFrom(r) != ""
}

// requireGuestOrAuth accepts either a guest token or a full member token. It
// is used on public/read surfaces that guests may browse but that also accept
// members. The handler can distinguish via isGuestRequest.
func (a *App) requireGuestOrAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		h := r.Header.Get("Authorization")
		if !strings.HasPrefix(h, "Bearer ") {
			writeErr(w, http.StatusUnauthorized, "missing bearer token")
			return
		}
		claims, err := a.parseClaims(strings.TrimPrefix(h, "Bearer "))
		if err != nil || claims.Type != "access" || claims.Scope == "admin" {
			writeErr(w, http.StatusUnauthorized, "invalid or expired token")
			return
		}
		ctx := context.WithValue(r.Context(), ctxUserID, claims.Sub)
		next(w, r.WithContext(ctx))
	}
}
