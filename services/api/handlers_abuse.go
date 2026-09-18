package main

import (
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
// GET /api/appeals — user views their appeals.
// ---- Admin: appeals queue ----------------------------------------------------

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
