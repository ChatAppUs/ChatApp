package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

func (a *App) hasRole(ctx context.Context, userID string, roles ...string) bool {
	var ok bool
	_ = a.db.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM admin_roles WHERE user_id=$1 AND role = ANY($2))`,
		userID, roles).Scan(&ok)
	return ok
}

// hasPerm resolves dynamic role definitions: superadmin (or any role holding
// the '*' wildcard) passes everything; otherwise the permission must appear
// in at least one of the caller's role defs.
func (a *App) hasPerm(ctx context.Context, userID, perm string) bool {
	var ok bool
	_ = a.db.QueryRow(ctx,
		`SELECT EXISTS(
		   SELECT 1 FROM admin_roles ar JOIN admin_role_defs rd ON rd.name = ar.role
		   WHERE ar.user_id=$1 AND ('*' = ANY(rd.permissions) OR $2 = ANY(rd.permissions)))`,
		userID, perm).Scan(&ok)
	return ok
}

func (a *App) requireAdminPerm(perm string) func(http.HandlerFunc) http.HandlerFunc {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return a.requireAdminAuth(func(w http.ResponseWriter, r *http.Request) {
			if !a.hasPerm(r.Context(), userIDFrom(r), perm) {
				writeErr(w, http.StatusForbidden, "missing admin permission: "+perm)
				return
			}
			next(w, r)
		})
	}
}

// ---- Admin plane ----
//
// The admin system is fully separated from the user system:
//   - Admins authenticate at POST /api/admin/login and receive tokens with
//     scope="admin" and a short TTL (no refresh — re-authentication is
//     required when the token expires).
//   - requireAdmin only accepts admin-scoped tokens; requireAuth (user plane)
//     rejects them, and vice versa. A user token can never reach an admin
//     handler, even if the account happens to hold an admin role.

func (a *App) requireAdminAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		h := r.Header.Get("Authorization")
		if !strings.HasPrefix(h, "Bearer ") {
			writeErr(w, http.StatusUnauthorized, "missing bearer token")
			return
		}
		claims, err := a.parseClaims(strings.TrimPrefix(h, "Bearer "))
		if err != nil || claims.Type != "access" || claims.Scope != "admin" {
			writeErr(w, http.StatusUnauthorized, "admin session required")
			return
		}
		ctx := context.WithValue(r.Context(), ctxUserID, claims.Sub)
		next(w, r.WithContext(ctx))
	}
}

func (a *App) requireAdmin(roles ...string) func(http.HandlerFunc) http.HandlerFunc {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return a.requireAdminAuth(func(w http.ResponseWriter, r *http.Request) {
			if !a.hasRole(r.Context(), userIDFrom(r), roles...) {
				writeErr(w, http.StatusForbidden, "insufficient admin role")
				return
			}
			next(w, r)
		})
	}
}

const adminTokenTTL = 30 * time.Minute

// handleAdminLogin authenticates an account that holds an admin role and
// issues a short-lived admin-scoped token. Regular users receive 403.
func (a *App) handleAdminLogin(w http.ResponseWriter, r *http.Request) {
	var req loginReq
	if !decodeJSON(w, r, &req) {
		return
	}
	id := strings.TrimSpace(req.Identifier)
	var userID, hash, status string
	var totpSecret *string
	var totpEnabled bool
	err := a.db.QueryRow(r.Context(),
		`SELECT id, password_hash, status, totp_secret, totp_enabled FROM users
		 WHERE username = $1 OR email = lower($1)`, id).
		Scan(&userID, &hash, &status, &totpSecret, &totpEnabled)
	if err != nil || !a.passwordVerify(req.Password, hash) {
		writeErr(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	if status != "active" {
		writeErr(w, http.StatusForbidden, "account is "+status)
		return
	}
	roles := a.adminRoles(r.Context(), userID)
	if len(roles) == 0 {
		// Same error as bad credentials: do not reveal which accounts are admins.
		writeErr(w, http.StatusForbidden, "not an admin account")
		return
	}
	if totpEnabled && totpSecret != nil {
		if req.TOTPCode == "" {
			writeErr(w, http.StatusUnauthorized, "totp_required")
			return
		}
		if !a.checkTOTP(*totpSecret, req.TOTPCode) {
			writeErr(w, http.StatusUnauthorized, "invalid 2FA code")
			return
		}
	}
	now := time.Now()
	access, err := a.mintClaims(Claims{
		Sub: userID, Type: "access", Scope: "admin",
		Iat: now.Unix(), Exp: now.Add(adminTokenTTL).Unix(),
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "session creation failed")
		return
	}
	a.audit(r.Context(), userID, "admin.login", userID, map[string]any{"ip": clientIP(r)})
	writeJSON(w, http.StatusOK, map[string]any{
		"access_token": access,
		"token_type":   "Bearer",
		"expires_in":   int(adminTokenTTL.Seconds()),
		"roles":        roles,
		"user_id":      userID,
	})
}

func (a *App) adminRoles(ctx context.Context, userID string) []string {
	rows, err := a.db.Query(ctx,
		`SELECT role FROM admin_roles WHERE user_id=$1`, userID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var roles []string
	for rows.Next() {
		var role string
		if err := rows.Scan(&role); err == nil {
			roles = append(roles, role)
		}
	}
	return roles
}

func (a *App) audit(ctx context.Context, actorID, action, target string, meta map[string]any) {
	_, _ = a.db.Exec(ctx,
		`INSERT INTO audit_log (actor_id, action, target, meta) VALUES ($1,$2,$3,$4)`,
		actorID, action, target, meta)
}

func (a *App) handleAdminStats(w http.ResponseWriter, r *http.Request) {
	var users, posts, openReports, pendingKYC, activeAds int
	_ = a.db.QueryRow(r.Context(), `SELECT COUNT(*) FROM users WHERE status='active'`).Scan(&users)
	_ = a.db.QueryRow(r.Context(), `SELECT COUNT(*) FROM posts WHERE deleted_at IS NULL`).Scan(&posts)
	_ = a.db.QueryRow(r.Context(), `SELECT COUNT(*) FROM reports WHERE status='open'`).Scan(&openReports)
	_ = a.db.QueryRow(r.Context(), `SELECT COUNT(*) FROM users WHERE kyc_status='pending'`).Scan(&pendingKYC)
	_ = a.db.QueryRow(r.Context(), `SELECT COUNT(*) FROM ad_campaigns WHERE status='active'`).Scan(&activeAds)
	writeJSON(w, http.StatusOK, map[string]int{
		"users": users, "posts": posts, "open_reports": openReports,
		"pending_kyc": pendingKYC, "active_ads": activeAds,
	})
}

func (a *App) handleAdminListUsers(w http.ResponseWriter, r *http.Request) {
	limit, offset := pageParams(r)
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	rows, err := a.db.Query(r.Context(),
		`SELECT id, username, display_name, email, phone_e164, status, kyc_status, created_at
		 FROM users
		 WHERE ($1 = '' OR username ILIKE '%'||$1||'%' OR email ILIKE '%'||$1||'%')
		 ORDER BY created_at DESC LIMIT $2 OFFSET $3`, q, limit, offset)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load users")
		return
	}
	defer rows.Close()
	type u struct {
		ID       string    `json:"id"`
		Username string    `json:"username"`
		Name     string    `json:"display_name"`
		Email    *string   `json:"email"`
		Phone    *string   `json:"phone"`
		Status   string    `json:"status"`
		KYC      string    `json:"kyc_status"`
		Created  time.Time `json:"created_at"`
	}
	out := []u{}
	for rows.Next() {
		var x u
		if err := rows.Scan(&x.ID, &x.Username, &x.Name, &x.Email, &x.Phone, &x.Status, &x.KYC, &x.Created); err == nil {
			out = append(out, x)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": out})
}

func (a *App) handleAdminSetUserStatus(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Status string `json:"status"` // active | suspended
	}
	if !decodeJSON(w, r, &req) || (req.Status != "active" && req.Status != "suspended") {
		writeErr(w, http.StatusBadRequest, "status must be active or suspended")
		return
	}
	target := r.PathValue("id")
	res, err := a.db.Exec(r.Context(), `UPDATE users SET status=$1, updated_at=now() WHERE id=$2`, req.Status, target)
	if err != nil || res.RowsAffected() == 0 {
		writeErr(w, http.StatusNotFound, "user not found")
		return
	}
	if req.Status == "suspended" {
		_, _ = a.db.Exec(r.Context(), `UPDATE sessions SET revoked_at=now() WHERE user_id=$1 AND revoked_at IS NULL`, target)
	}
	a.audit(r.Context(), userIDFrom(r), "user_"+req.Status, target, nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": req.Status})
}

func (a *App) handleAdminListReports(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Query(r.Context(),
		`SELECT r.id, r.reporter_id, u.username, r.target_type, r.target_id, r.reason, r.status, r.created_at
		 FROM reports r JOIN users u ON u.id = r.reporter_id
		 WHERE r.status = 'open' ORDER BY r.created_at ASC LIMIT 100`)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load reports")
		return
	}
	defer rows.Close()
	type rep struct {
		ID         string    `json:"id"`
		ReporterID string    `json:"reporter_id"`
		Reporter   string    `json:"reporter"`
		TargetType string    `json:"target_type"`
		TargetID   string    `json:"target_id"`
		Reason     string    `json:"reason"`
		Status     string    `json:"status"`
		CreatedAt  time.Time `json:"created_at"`
	}
	out := []rep{}
	for rows.Next() {
		var x rep
		if err := rows.Scan(&x.ID, &x.ReporterID, &x.Reporter, &x.TargetType, &x.TargetID, &x.Reason, &x.Status, &x.CreatedAt); err == nil {
			out = append(out, x)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"reports": out})
}

func (a *App) handleAdminResolveReport(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Resolution string `json:"resolution"` // resolved | dismissed
	}
	if !decodeJSON(w, r, &req) || (req.Resolution != "resolved" && req.Resolution != "dismissed") {
		writeErr(w, http.StatusBadRequest, "resolution must be resolved or dismissed")
		return
	}
	res, err := a.db.Exec(r.Context(),
		`UPDATE reports SET status=$1 WHERE id=$2 AND status='open'`, req.Resolution, r.PathValue("id"))
	if err != nil || res.RowsAffected() == 0 {
		writeErr(w, http.StatusNotFound, "report not found")
		return
	}
	a.audit(r.Context(), userIDFrom(r), "report_"+req.Resolution, r.PathValue("id"), nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": req.Resolution})
}

func (a *App) handleAdminListKYC(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Query(r.Context(),
		`SELECT k.id, k.user_id, u.username, u.display_name, k.status, k.created_at,
		        k.auto_score, k.auto_checks
		 FROM kyc_submissions k JOIN users u ON u.id = k.user_id
		 WHERE k.status = 'pending' ORDER BY k.created_at ASC LIMIT 100`)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load KYC queue")
		return
	}
	defer rows.Close()
	type k struct {
		ID         string          `json:"id"`
		UserID     string          `json:"user_id"`
		Username   string          `json:"username"`
		Name       string          `json:"display_name"`
		Status     string          `json:"status"`
		CreatedAt  time.Time       `json:"created_at"`
		AutoScore  *float64        `json:"auto_score"`
		AutoChecks json.RawMessage `json:"auto_checks"`
	}
	out := []k{}
	for rows.Next() {
		var x k
		if err := rows.Scan(&x.ID, &x.UserID, &x.Username, &x.Name, &x.Status, &x.CreatedAt,
			&x.AutoScore, &x.AutoChecks); err == nil {
			out = append(out, x)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"submissions": out})
}

func (a *App) handleAdminReviewKYC(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Decision string `json:"decision"` // verified | rejected
		Note     string `json:"note"`
	}
	if !decodeJSON(w, r, &req) || (req.Decision != "verified" && req.Decision != "rejected") {
		writeErr(w, http.StatusBadRequest, "decision must be verified or rejected")
		return
	}
	subID := r.PathValue("id")
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "review failed")
		return
	}
	defer tx.Rollback(r.Context())
	var targetUser string
	err = tx.QueryRow(r.Context(),
		`UPDATE kyc_submissions SET status=$1, review_note=$2, reviewed_at=now()
		 WHERE id=$3 AND status='pending' RETURNING user_id`,
		req.Decision, req.Note, subID).Scan(&targetUser)
	if err != nil {
		writeErr(w, http.StatusNotFound, "submission not found or already reviewed")
		return
	}
	if _, err := tx.Exec(r.Context(), `UPDATE users SET kyc_status=$1 WHERE id=$2`, req.Decision, targetUser); err != nil {
		writeErr(w, http.StatusInternalServerError, "review failed")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeErr(w, http.StatusInternalServerError, "review failed")
		return
	}
	_, _ = a.db.Exec(r.Context(),
		`INSERT INTO notifications (user_id, kind, payload) VALUES ($1,'kyc_decision',$2)`,
		targetUser, map[string]string{"decision": req.Decision})
	a.audit(r.Context(), userIDFrom(r), "kyc_"+req.Decision, targetUser, map[string]any{"note": req.Note})
	writeJSON(w, http.StatusOK, map[string]string{"status": req.Decision})
}

func (a *App) handleAdminListAds(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Query(r.Context(),
		`SELECT c.id, c.name, u.username, c.objective, c.total_budget, c.currency, c.created_at
		 FROM ad_campaigns c JOIN users u ON u.id = c.advertiser_id
		 WHERE c.status = 'pending_review' ORDER BY c.created_at ASC LIMIT 100`)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load ad queue")
		return
	}
	defer rows.Close()
	type ad struct {
		ID         string    `json:"id"`
		Name       string    `json:"name"`
		Advertiser string    `json:"advertiser"`
		Objective  string    `json:"objective"`
		Budget     string    `json:"total_budget"`
		Currency   string    `json:"currency"`
		CreatedAt  time.Time `json:"created_at"`
	}
	out := []ad{}
	for rows.Next() {
		var x ad
		if err := rows.Scan(&x.ID, &x.Name, &x.Advertiser, &x.Objective, &x.Budget, &x.Currency, &x.CreatedAt); err == nil {
			out = append(out, x)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"campaigns": out})
}

func (a *App) handleAdminReviewAd(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Decision string `json:"decision"` // active | rejected
	}
	if !decodeJSON(w, r, &req) || (req.Decision != "active" && req.Decision != "rejected") {
		writeErr(w, http.StatusBadRequest, "decision must be active or rejected")
		return
	}
	res, err := a.db.Exec(r.Context(),
		`UPDATE ad_campaigns SET status=$1 WHERE id=$2 AND status='pending_review'`,
		req.Decision, r.PathValue("id"))
	if err != nil || res.RowsAffected() == 0 {
		writeErr(w, http.StatusNotFound, "campaign not found or already reviewed")
		return
	}
	a.audit(r.Context(), userIDFrom(r), "ad_"+req.Decision, r.PathValue("id"), nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": req.Decision})
}

func (a *App) handleAdminGrantRole(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UserID string `json:"user_id"`
		Role   string `json:"role"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	// Any role defined in admin_role_defs (built-in or superadmin-created)
	// can be granted.
	var known bool
	_ = a.db.QueryRow(r.Context(),
		`SELECT EXISTS(SELECT 1 FROM admin_role_defs WHERE name=$1)`, req.Role).Scan(&known)
	if !known {
		writeErr(w, http.StatusBadRequest, "unknown role")
		return
	}
	_, err := a.db.Exec(r.Context(),
		`INSERT INTO admin_roles (user_id, role, granted_by) VALUES ($1,$2,$3) ON CONFLICT DO NOTHING`,
		req.UserID, req.Role, userIDFrom(r))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to grant role")
		return
	}
	a.audit(r.Context(), userIDFrom(r), "grant_role_"+req.Role, req.UserID, nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": "granted"})
}

// handleAdminRevokeRole removes a role from a user. The last superadmin can
// never be revoked, which keeps the signing authority reachable at all times.
func (a *App) handleAdminRevokeRole(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UserID string `json:"user_id"`
		Role   string `json:"role"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Role == "superadmin" {
		var count int
		_ = a.db.QueryRow(r.Context(),
			`SELECT COUNT(*) FROM admin_roles WHERE role='superadmin'`).Scan(&count)
		if count <= 1 {
			writeErr(w, http.StatusConflict, "cannot revoke the last superadmin")
			return
		}
	}
	tag, err := a.db.Exec(r.Context(),
		`DELETE FROM admin_roles WHERE user_id=$1 AND role=$2`, req.UserID, req.Role)
	if err != nil || tag.RowsAffected() == 0 {
		writeErr(w, http.StatusNotFound, "role assignment not found")
		return
	}
	a.audit(r.Context(), userIDFrom(r), "revoke_role_"+req.Role, req.UserID, nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": "revoked"})
}

// User-facing report endpoint
func (a *App) handleCreateReport(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TargetType string `json:"target_type"`
		TargetID   string `json:"target_id"`
		Reason     string `json:"reason"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	switch req.TargetType {
	case "user", "post", "comment", "message", "ad":
	default:
		writeErr(w, http.StatusBadRequest, "invalid target type")
		return
	}
	if strings.TrimSpace(req.Reason) == "" || req.TargetID == "" {
		writeErr(w, http.StatusBadRequest, "target_id and reason required")
		return
	}
	var id string
	err := a.db.QueryRow(r.Context(),
		`INSERT INTO reports (reporter_id, target_type, target_id, reason) VALUES ($1,$2,$3,$4) RETURNING id`,
		userIDFrom(r), req.TargetType, req.TargetID, req.Reason).Scan(&id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to file report")
		return
	}
	// Negative feedback must leave the reporter's FYP immediately, not after
	// the 15s cache TTL.
	if req.TargetType == "post" || req.TargetType == "user" {
		a.invalidateFYP(r.Context(), userIDFrom(r))
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": id})
}
