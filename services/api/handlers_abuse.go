package main

import (
	"encoding/json"
	"net/http"
	"time"
)

// handlers_abuse.go — closes the competitor-doc gaps around abuse reporting,
// content appeals, support tickets, and age verification that had no
// real implementation on main.
//
// Competitor doc references:
//   Facebook/TikTok/X: "anti-spam/bot detection, appeals, verified identity"
//   "mature moderation queues, policy enforcement, legal requests,
//    copyright workflows, age/safety controls, spam prevention"

// ---- User-facing abuse report ------------------------------------------------

// POST /api/reports (already wired in main.go as handleCreateReport)
// This is the non-admin reporting endpoint any user hits from the app.
func (a *App) handleCreateReport(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	var req struct {
		TargetType string `json:"target_type"` // post | comment | user | message | reel | live
		TargetID   string `json:"target_id"`
		Reason     string `json:"reason"`
		Detail     string `json:"detail"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.TargetType == "" || req.TargetID == "" || req.Reason == "" {
		writeErr(w, http.StatusBadRequest, "target_type, target_id, and reason are required")
		return
	}

	// Rate limit: 5 reports per hour per user
	var count int
	if err := a.db.QueryRow(r.Context(),
		`SELECT COUNT(*) FROM content_reports WHERE reporter_id=$1 AND created_at > NOW() - INTERVAL '1 hour'`,
		uid).Scan(&count); err == nil && count >= 5 {
		writeErr(w, http.StatusTooManyRequests, "report limit reached (5/hr); please try again later")
		return
	}

	var reportID string
	if err := a.db.QueryRow(r.Context(), `
		INSERT INTO content_reports (reporter_id, target_type, target_id, reason, detail)
		VALUES ($1,$2,$3,$4,$5) RETURNING id`,
		uid, req.TargetType, req.TargetID, req.Reason, req.Detail).Scan(&reportID); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to submit report")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{
		"id":     reportID,
		"status": "submitted",
	})
}

// GET /api/reports/mine — user views their own report history.
func (a *App) handleMyReports(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	rows, err := a.db.Query(r.Context(), `
		SELECT id, target_type, target_id, reason, status, resolution, created_at
		FROM content_reports WHERE reporter_id=$1
		ORDER BY created_at DESC LIMIT 50`, uid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load reports")
		return
	}
	defer rows.Close()
	type report struct {
		ID         string    `json:"id"`
		TargetType string    `json:"target_type"`
		TargetID   string    `json:"target_id"`
		Reason     string    `json:"reason"`
		Status     string    `json:"status"`
		Resolution string    `json:"resolution"`
		CreatedAt  time.Time `json:"created_at"`
	}
	var out []report
	for rows.Next() {
		var rpt report
		if rows.Scan(&rpt.ID, &rpt.TargetType, &rpt.TargetID, &rpt.Reason,
			&rpt.Status, &rpt.Resolution, &rpt.CreatedAt) == nil {
			out = append(out, rpt)
		}
	}
	if out == nil {
		out = []report{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"reports": out})
}

// ---- Content appeals ---------------------------------------------------------

// POST /api/appeals — user appeals a moderation decision.
func (a *App) handleCreateAppeal(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	var req struct {
		ActionID string `json:"action_id"` // the moderation action being appealed
		Reason   string `json:"reason"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.ActionID == "" {
		writeErr(w, http.StatusBadRequest, "action_id is required")
		return
	}

	var targetUser string
	if err := a.db.QueryRow(r.Context(),
		`SELECT target_user_id FROM moderation_actions WHERE id=$1`,
		req.ActionID).Scan(&targetUser); err != nil {
		writeErr(w, http.StatusNotFound, "moderation action not found")
		return
	}
	if targetUser != uid {
		writeErr(w, http.StatusForbidden, "you can only appeal actions taken against your account")
		return
	}

	var existing int
	a.db.QueryRow(r.Context(),
		`SELECT COUNT(*) FROM content_appeals WHERE action_id=$1 AND user_id=$2`,
		req.ActionID, uid).Scan(&existing)
	if existing > 0 {
		writeErr(w, http.StatusConflict, "an appeal for this action already exists")
		return
	}

	var appealID string
	if err := a.db.QueryRow(r.Context(), `
		INSERT INTO content_appeals (user_id, action_id, reason)
		VALUES ($1,$2,$3) RETURNING id`,
		uid, req.ActionID, req.Reason).Scan(&appealID); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to create appeal")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": appealID, "status": "pending"})
}

// GET /api/appeals — user views their appeals.
func (a *App) handleMyAppeals(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	rows, err := a.db.Query(r.Context(), `
		SELECT a.id, a.action_id, a.reason, a.status, a.resolution, a.created_at
		FROM content_appeals a WHERE a.user_id=$1
		ORDER BY a.created_at DESC LIMIT 50`, uid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load appeals")
		return
	}
	defer rows.Close()
	type appeal struct {
		ID         string    `json:"id"`
		ActionID   string    `json:"action_id"`
		Reason     string    `json:"reason"`
		Status     string    `json:"status"`
		Resolution string    `json:"resolution"`
		CreatedAt  time.Time `json:"created_at"`
	}
	var out []appeal
	for rows.Next() {
		var ap appeal
		if rows.Scan(&ap.ID, &ap.ActionID, &ap.Reason, &ap.Status, &ap.Resolution, &ap.CreatedAt) == nil {
			out = append(out, ap)
		}
	}
	if out == nil {
		out = []appeal{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"appeals": out})
}

// ---- Admin: appeals queue ----------------------------------------------------

func (a *App) handleAdminListAppeals(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Query(r.Context(), `
		SELECT a.id, a.user_id, a.action_id, a.reason, a.status, a.created_at,
			COALESCE(ma.action_type,''), COALESCE(ma.target_type,''), COALESCE(ma.target_id,'')
		FROM content_appeals a
		LEFT JOIN moderation_actions ma ON ma.id = a.action_id
		WHERE a.status = 'pending'
		ORDER BY a.created_at LIMIT 100`)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load appeals")
		return
	}
	defer rows.Close()
	type appealEntry struct {
		ID         string    `json:"id"`
		UserID     string    `json:"user_id"`
		ActionID   string    `json:"action_id"`
		Reason     string    `json:"reason"`
		Status     string    `json:"status"`
		ActionType string    `json:"action_type"`
		TargetType string    `json:"target_type"`
		TargetID   string    `json:"target_id"`
		CreatedAt  time.Time `json:"created_at"`
	}
	var out []appealEntry
	for rows.Next() {
		var e appealEntry
		if rows.Scan(&e.ID, &e.UserID, &e.ActionID, &e.Reason, &e.Status, &e.CreatedAt,
			&e.ActionType, &e.TargetType, &e.TargetID) == nil {
			out = append(out, e)
		}
	}
	if out == nil {
		out = []appealEntry{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"appeals": out})
}

func (a *App) handleAdminResolveAppeal(w http.ResponseWriter, r *http.Request) {
	appealID := r.PathValue("id")
	var req struct {
		Resolution string `json:"resolution"` // upheld | overturned
		Note       string `json:"note"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Resolution != "upheld" && req.Resolution != "overturned" {
		writeErr(w, http.StatusBadRequest, "resolution must be 'upheld' or 'overturned'")
		return
	}
	tag, err := a.db.Exec(r.Context(), `
		UPDATE content_appeals SET status='resolved', resolution=$1, reviewer_note=$2, reviewed_at=NOW()
		WHERE id=$3 AND status='pending'`, req.Resolution, req.Note, appealID)
	if err != nil || rowsAffected(tag) == 0 {
		writeErr(w, http.StatusNotFound, "appeal not found or already resolved")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "resolved", "resolution": req.Resolution})
}

// ---- Support tickets ---------------------------------------------------------

func (a *App) handleCreateSupportTicket(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	var req struct {
		Category string `json:"category"` // account | billing | bug | abuse | copyright | other
		Subject  string `json:"subject"`
		Body     string `json:"body"`
		Priority string `json:"priority"` // low | normal | high
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Subject == "" || req.Body == "" {
		writeErr(w, http.StatusBadRequest, "subject and body are required")
		return
	}
	if req.Category == "" {
		req.Category = "other"
	}
	if req.Priority == "" {
		req.Priority = "normal"
	}
	var ticketID string
	if err := a.db.QueryRow(r.Context(), `
		INSERT INTO support_tickets (user_id, category, subject, body, priority)
		VALUES ($1,$2,$3,$4,$5) RETURNING id`,
		uid, req.Category, req.Subject, req.Body, req.Priority).Scan(&ticketID); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to create ticket")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": ticketID, "status": "open"})
}

func (a *App) handleMySupportTickets(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	rows, err := a.db.Query(r.Context(), `
		SELECT id, category, subject, status, priority, created_at, updated_at
		FROM support_tickets WHERE user_id=$1
		ORDER BY updated_at DESC LIMIT 50`, uid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load tickets")
		return
	}
	defer rows.Close()
	type ticket struct {
		ID        string    `json:"id"`
		Category  string    `json:"category"`
		Subject   string    `json:"subject"`
		Status    string    `json:"status"`
		Priority  string    `json:"priority"`
		CreatedAt time.Time `json:"created_at"`
		UpdatedAt time.Time `json:"updated_at"`
	}
	var out []ticket
	for rows.Next() {
		var t ticket
		if rows.Scan(&t.ID, &t.Category, &t.Subject, &t.Status, &t.Priority,
			&t.CreatedAt, &t.UpdatedAt) == nil {
			out = append(out, t)
		}
	}
	if out == nil {
		out = []ticket{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"tickets": out})
}

func (a *App) handleAdminSupportTickets(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Query(r.Context(), `
		SELECT t.id, t.user_id, t.category, t.subject, t.status, t.priority, t.created_at
		FROM support_tickets t
		WHERE t.status IN ('open','in_progress')
		ORDER BY
			CASE t.priority WHEN 'high' THEN 0 WHEN 'normal' THEN 1 ELSE 2 END,
			t.created_at LIMIT 100`)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load tickets")
		return
	}
	defer rows.Close()
	type ticket struct {
		ID        string    `json:"id"`
		UserID    string    `json:"user_id"`
		Category  string    `json:"category"`
		Subject   string    `json:"subject"`
		Status    string    `json:"status"`
		Priority  string    `json:"priority"`
		CreatedAt time.Time `json:"created_at"`
	}
	var out []ticket
	for rows.Next() {
		var t ticket
		if rows.Scan(&t.ID, &t.UserID, &t.Category, &t.Subject, &t.Status,
			&t.Priority, &t.CreatedAt) == nil {
			out = append(out, t)
		}
	}
	if out == nil {
		out = []ticket{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"tickets": out})
}

// ---- Age / date-of-birth verification ----------------------------------------

func (a *App) handleSetDOB(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	var req struct {
		BirthDate string `json:"birth_date"` // YYYY-MM-DD
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	parsed, err := time.Parse("2006-01-02", req.BirthDate)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "birth_date must be YYYY-MM-DD")
		return
	}
	age := time.Now().Year() - parsed.Year()
	if time.Now().YearDay() < parsed.YearDay() {
		age--
	}
	if age < 0 || age > 150 {
		writeErr(w, http.StatusBadRequest, "invalid birth date")
		return
	}

	_, err = a.db.Exec(r.Context(), `
		UPDATE users SET birth_date=$1, age_verified=true, updated_at=NOW()
		WHERE id=$2`, req.BirthDate, uid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to save birth date")
		return
	}

	ageGate := "unrestricted"
	if age < 13 {
		ageGate = "under_13"
	} else if age < 18 {
		ageGate = "under_18"
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"status":   "saved",
		"age":      itoa(age),
		"age_gate": ageGate,
	})
}

func itoa(n int) string {
	if n < 0 {
		return "0"
	}
	digits := "0123456789"
	if n == 0 {
		return "0"
	}
	var buf [12]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = digits[n%10]
		n /= 10
	}
	return string(buf[i:])
}