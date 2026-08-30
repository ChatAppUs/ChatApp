package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"math/big"
	"net/http"
	"strings"
	"time"
)

// Account safety pack: trusted-contacts recovery (2-of-N one-time shares),
// legacy contact for memorialized accounts, and multiple profiles per user.

// ---------- Trusted contacts ----------

func (a *App) handleListTrustedContacts(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Query(r.Context(),
		`SELECT t.contact_id, u.username, u.display_name, t.created_at
                 FROM trusted_contacts t JOIN users u ON u.id = t.contact_id
                 WHERE t.user_id = $1 ORDER BY t.created_at`, userIDFrom(r))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load trusted contacts")
		return
	}
	defer rows.Close()
	type tc struct {
		ID       string    `json:"id"`
		Username string    `json:"username"`
		Name     string    `json:"display_name"`
		Since    time.Time `json:"created_at"`
	}
	out := []tc{}
	for rows.Next() {
		var c tc
		if err := rows.Scan(&c.ID, &c.Username, &c.Name, &c.Since); err == nil {
			out = append(out, c)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"contacts": out})
}

func (a *App) handleAddTrustedContact(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	uid := userIDFrom(r)
	var count int
	_ = a.db.QueryRow(r.Context(),
		`SELECT count(*) FROM trusted_contacts WHERE user_id=$1`, uid).Scan(&count)
	if count >= 5 {
		writeErr(w, http.StatusBadRequest, "at most 5 trusted contacts")
		return
	}
	var contactID string
	err := a.db.QueryRow(r.Context(),
		`SELECT id FROM users WHERE lower(username)=lower($1) AND status='active'`,
		strings.TrimSpace(req.Username)).Scan(&contactID)
	if err != nil {
		writeErr(w, http.StatusNotFound, "user not found")
		return
	}
	if contactID == uid {
		writeErr(w, http.StatusBadRequest, "cannot add yourself")
		return
	}
	if _, err := a.db.Exec(r.Context(),
		`INSERT INTO trusted_contacts (user_id, contact_id) VALUES ($1,$2) ON CONFLICT DO NOTHING`,
		uid, contactID); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to add contact")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"status": "added"})
}

func (a *App) handleRemoveTrustedContact(w http.ResponseWriter, r *http.Request) {
	tag, err := a.db.Exec(r.Context(),
		`DELETE FROM trusted_contacts WHERE user_id=$1 AND contact_id=$2`,
		userIDFrom(r), r.PathValue("contactId"))
	if err != nil || tag.RowsAffected() == 0 {
		writeErr(w, http.StatusNotFound, "contact not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "removed"})
}

// ---------- Trusted-contacts recovery ----------

// POST /api/recovery/trusted/request — unauthenticated (account owner is locked
// out). Generates one fresh share per trusted contact; each share is only
// revealed to that contact after they authenticate.
func (a *App) handleRecoveryRequest(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	var uid string
	err := a.db.QueryRow(r.Context(),
		`SELECT id FROM users WHERE lower(username)=lower($1) AND status='active'`,
		strings.TrimSpace(req.Username)).Scan(&uid)
	if err != nil {
		// Do not reveal whether the account exists.
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}
	rows, err := a.db.Query(r.Context(),
		`SELECT contact_id FROM trusted_contacts WHERE user_id=$1`, uid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "recovery unavailable")
		return
	}
	defer rows.Close()
	var contacts []string
	for rows.Next() {
		var c string
		if rows.Scan(&c) == nil {
			contacts = append(contacts, c)
		}
	}
	if len(contacts) < 3 {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}
	for _, contactID := range contacts {
		code := recoveryCode()
		sum := sha256.Sum256([]byte(code))
		if _, err := a.db.Exec(r.Context(),
			`INSERT INTO recovery_shares (user_id, contact_id, code_hash, expires_at)
                         VALUES ($1,$2,$3, now() + interval '24 hours')`,
			uid, contactID, hex.EncodeToString(sum[:])); err != nil {
			continue
		}
		a.notifyUser(r.Context(), contactID, "recovery_share",
			map[string]string{"for": strings.TrimSpace(req.Username)})
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// GET /api/recovery/trusted/pending — a trusted contact sees who needs their
// code and the code itself (reveal-once, authenticated).
func (a *App) handleRecoveryPending(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	rows, err := a.db.Query(r.Context(),
		`SELECT s.id, u.username, s.expires_at, s.revealed_at IS NOT NULL
                 FROM recovery_shares s JOIN users u ON u.id = s.user_id
                 WHERE s.contact_id=$1 AND s.used_at IS NULL AND s.expires_at > now()
                 ORDER BY s.created_at DESC`, uid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load")
		return
	}
	defer rows.Close()
	type pending struct {
		ID        string    `json:"id"`
		Username  string    `json:"username"`
		ExpiresAt time.Time `json:"expires_at"`
		Revealed  bool      `json:"revealed"`
	}
	out := []pending{}
	for rows.Next() {
		var p pending
		if err := rows.Scan(&p.ID, &p.Username, &p.ExpiresAt, &p.Revealed); err == nil {
			out = append(out, p)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"pending": out})
}

// POST /api/recovery/trusted/reveal — the code value is stored hashed; the
// plaintext was embedded in the reveal token we generate here on demand. To
// keep shares retrievable we generate a fresh code at reveal time and rotate
// the stored hash atomically.
func (a *App) handleRecoveryReveal(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	var req struct {
		ShareID string `json:"share_id"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	code := recoveryCode()
	sum := sha256.Sum256([]byte(code))
	tag, err := a.db.Exec(r.Context(),
		`UPDATE recovery_shares SET code_hash=$2, revealed_at=now()
                 WHERE id=$1 AND contact_id=$3 AND used_at IS NULL AND expires_at > now()`,
		req.ShareID, hex.EncodeToString(sum[:]), uid)
	if err != nil || tag.RowsAffected() == 0 {
		writeErr(w, http.StatusNotFound, "share not found or expired")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"code": code})
}

// POST /api/recovery/trusted/redeem — unauthenticated. Two or more distinct
// valid shares issue a password-reset token for the account.
func (a *App) handleRecoveryRedeem(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string   `json:"username"`
		Codes    []string `json:"codes"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	username := strings.TrimSpace(req.Username)
	if username == "" || len(req.Codes) < 2 {
		writeErr(w, http.StatusBadRequest, "username and at least 2 recovery codes required")
		return
	}
	var uid string
	err := a.db.QueryRow(r.Context(),
		`SELECT id FROM users WHERE lower(username)=lower($1) AND status='active'`,
		username).Scan(&uid)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid codes")
		return
	}
	distinct := map[string]bool{}
	matched := 0
	for _, code := range req.Codes {
		code = strings.ToUpper(strings.TrimSpace(code))
		if code == "" || distinct[code] {
			continue
		}
		distinct[code] = true
		sum := sha256.Sum256([]byte(code))
		tag, err := a.db.Exec(r.Context(),
			`UPDATE recovery_shares SET used_at=now()
                         WHERE user_id=$1 AND code_hash=$2 AND used_at IS NULL AND expires_at > now()`,
			uid, hex.EncodeToString(sum[:]))
		if err == nil && tag.RowsAffected() > 0 {
			matched++
		}
	}
	if matched < 2 {
		writeErr(w, http.StatusBadRequest, "invalid codes")
		return
	}
	token, err := a.issuePasswordReset(r, uid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "recovery failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"reset_token": token})
}

func recoveryCode() string {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	var b strings.Builder
	for i := 0; i < 10; i++ {
		n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
		b.WriteByte(alphabet[n.Int64()])
		if i == 4 {
			b.WriteByte('-')
		}
	}
	return b.String()
}

// ---------- Legacy contact ----------

func (a *App) handleGetLegacyContact(w http.ResponseWriter, r *http.Request) {
	var contactID, username, name string
	err := a.db.QueryRow(r.Context(),
		`SELECT l.contact_id, u.username, u.display_name
                 FROM legacy_contacts l JOIN users u ON u.id = l.contact_id
                 WHERE l.user_id=$1`, userIDFrom(r)).Scan(&contactID, &username, &name)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"legacy_contact": nil})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"legacy_contact": map[string]string{
		"id": contactID, "username": username, "display_name": name,
	}})
}

func (a *App) handleSetLegacyContact(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	uid := userIDFrom(r)
	var contactID string
	err := a.db.QueryRow(r.Context(),
		`SELECT id FROM users WHERE lower(username)=lower($1) AND status='active'`,
		strings.TrimSpace(req.Username)).Scan(&contactID)
	if err != nil {
		writeErr(w, http.StatusNotFound, "user not found")
		return
	}
	if contactID == uid {
		writeErr(w, http.StatusBadRequest, "cannot designate yourself")
		return
	}
	if _, err := a.db.Exec(r.Context(),
		`INSERT INTO legacy_contacts (user_id, contact_id) VALUES ($1,$2)
                 ON CONFLICT (user_id) DO UPDATE SET contact_id=$2, created_at=now()`,
		uid, contactID); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to set legacy contact")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "set"})
}

func (a *App) handleRemoveLegacyContact(w http.ResponseWriter, r *http.Request) {
	_, _ = a.db.Exec(r.Context(), `DELETE FROM legacy_contacts WHERE user_id=$1`, userIDFrom(r))
	writeJSON(w, http.StatusOK, map[string]string{"status": "removed"})
}

// GET /api/legacy/export — the legacy contact of a memorialized account
// downloads the deceased user's public archive (profile, posts).
func (a *App) handleLegacyExport(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	owner := r.PathValue("userId")
	var memorialized bool
	err := a.db.QueryRow(r.Context(),
		`SELECT u.memorialized_at IS NOT NULL
                 FROM legacy_contacts l JOIN users u ON u.id = l.user_id
                 WHERE l.user_id=$1 AND l.contact_id=$2`, owner, uid).Scan(&memorialized)
	if err != nil || !memorialized {
		writeErr(w, http.StatusForbidden, "not the legacy contact of a memorialized account")
		return
	}
	var username, name, bio string
	_ = a.db.QueryRow(r.Context(),
		`SELECT username, display_name, bio FROM users WHERE id=$1`, owner).
		Scan(&username, &name, &bio)
	rows, _ := a.db.Query(r.Context(),
		`SELECT id, type, body, created_at FROM posts
                 WHERE author_id=$1 AND deleted_at IS NULL AND visibility='public'
                 ORDER BY created_at DESC LIMIT 500`, owner)
	defer rows.Close()
	posts := []map[string]any{}
	for rows.Next() {
		var id, typ, body string
		var created time.Time
		if rows.Scan(&id, &typ, &body, &created) == nil {
			posts = append(posts, map[string]any{
				"id": id, "type": typ, "body": body, "created_at": created,
			})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"account": map[string]string{"username": username, "display_name": name, "bio": bio},
		"posts":   posts,
	})
}

// Admin: memorialize an account (after verified report of death, handled via
// the existing admin report workflow).
func (a *App) handleAdminMemorialize(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Memorialize bool `json:"memorialize"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	target := r.PathValue("userId")
	var err error
	if req.Memorialize {
		_, err = a.db.Exec(r.Context(),
			`UPDATE users SET memorialized_at=now() WHERE id=$1 AND memorialized_at IS NULL`, target)
	} else {
		_, err = a.db.Exec(r.Context(),
			`UPDATE users SET memorialized_at=NULL WHERE id=$1`, target)
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "update failed")
		return
	}
	a.audit(r.Context(), userIDFrom(r), "memorialize", target,
		map[string]any{"memorialized": req.Memorialize})
	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

// ---------- Multiple profiles ----------

func (a *App) handleListMyProfiles(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	var active *string
	_ = a.db.QueryRow(r.Context(),
		`SELECT active_profile_id::text FROM users WHERE id=$1`, uid).Scan(&active)
	rows, err := a.db.Query(r.Context(),
		`SELECT id, name, bio, avatar_url, created_at FROM user_profiles
                 WHERE user_id=$1 ORDER BY created_at`, uid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load profiles")
		return
	}
	defer rows.Close()
	type profile struct {
		ID        string    `json:"id"`
		Name      string    `json:"name"`
		Bio       string    `json:"bio"`
		AvatarURL string    `json:"avatar_url"`
		Active    bool      `json:"active"`
		CreatedAt time.Time `json:"created_at"`
	}
	out := []profile{}
	for rows.Next() {
		var p profile
		if err := rows.Scan(&p.ID, &p.Name, &p.Bio, &p.AvatarURL, &p.CreatedAt); err == nil {
			p.Active = active != nil && *active == p.ID
			out = append(out, p)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"profiles": out})
}

func (a *App) handleCreateProfile(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name      string `json:"name"`
		Bio       string `json:"bio"`
		AvatarURL string `json:"avatar_url"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" || len(req.Name) > 60 || len(req.Bio) > 500 {
		writeErr(w, http.StatusBadRequest, "name required (60 chars), bio up to 500")
		return
	}
	uid := userIDFrom(r)
	var count int
	_ = a.db.QueryRow(r.Context(),
		`SELECT count(*) FROM user_profiles WHERE user_id=$1`, uid).Scan(&count)
	if count >= 4 {
		writeErr(w, http.StatusBadRequest, "at most 4 additional profiles")
		return
	}
	var id string
	err := a.db.QueryRow(r.Context(),
		`INSERT INTO user_profiles (user_id, name, bio, avatar_url) VALUES ($1,$2,$3,$4) RETURNING id`,
		uid, req.Name, req.Bio, req.AvatarURL).Scan(&id)
	if err != nil {
		writeErr(w, http.StatusConflict, "a profile with that name already exists")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": id})
}

func (a *App) handleDeleteProfile(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	id := r.PathValue("id")
	tag, err := a.db.Exec(r.Context(),
		`DELETE FROM user_profiles WHERE id=$1 AND user_id=$2`, id, uid)
	if err != nil || tag.RowsAffected() == 0 {
		writeErr(w, http.StatusNotFound, "profile not found")
		return
	}
	_, _ = a.db.Exec(r.Context(),
		`UPDATE users SET active_profile_id=NULL WHERE id=$1 AND active_profile_id=$2`, uid, id)
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// PUT /api/me/active-profile — switch persona; empty profile_id returns to the
// main profile. Post rendering prefers the active profile's name/avatar.
func (a *App) handleSwitchProfile(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ProfileID string `json:"profile_id"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	uid := userIDFrom(r)
	if strings.TrimSpace(req.ProfileID) == "" {
		_, _ = a.db.Exec(r.Context(),
			`UPDATE users SET active_profile_id=NULL WHERE id=$1`, uid)
		writeJSON(w, http.StatusOK, map[string]string{"status": "main profile active"})
		return
	}
	var owns bool
	_ = a.db.QueryRow(r.Context(),
		`SELECT EXISTS(SELECT 1 FROM user_profiles WHERE id=$1 AND user_id=$2)`,
		req.ProfileID, uid).Scan(&owns)
	if !owns {
		writeErr(w, http.StatusNotFound, "profile not found")
		return
	}
	if _, err := a.db.Exec(r.Context(),
		`UPDATE users SET active_profile_id=$2 WHERE id=$1`, uid, req.ProfileID); err != nil {
		writeErr(w, http.StatusInternalServerError, "switch failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "switched"})
}

// issuePasswordReset creates a password_resets row and returns the raw token.
func (a *App) issuePasswordReset(r *http.Request, uid string) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := hex.EncodeToString(raw)
	sum := sha256.Sum256([]byte(token))
	_, err := a.db.Exec(r.Context(),
		`INSERT INTO password_resets (user_id, token_hash, expires_at)
                 VALUES ($1,$2, now() + interval '1 hour')`,
		uid, hex.EncodeToString(sum[:]))
	if err != nil {
		return "", err
	}
	return token, nil
}
