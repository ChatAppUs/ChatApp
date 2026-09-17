package main

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"time"
)

type VerifiedContact struct {
	UserID        string    `json:"user_id"`
	ContactUserID string    `json:"contact_user_id"`
	SafetyNumber  string    `json:"safety_number"`
	VerifiedAt    time.Time `json:"verified_at"`
}

func computeSafetyNumber(a, b string) string {
	if a < b {
		h := sha256.Sum256([]byte(a + ":" + b))
		return fmt.Sprintf("%x", h[:16])
	}
	h := sha256.Sum256([]byte(b + ":" + a))
	return fmt.Sprintf("%x", h[:16])
}

func (a *App) handleVerifyContact(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	var req struct {
		ContactUserID string `json:"contact_user_id"`
		TheirPubkey   string `json:"their_public_key"`
		MyPubkey      string `json:"my_public_key"`
	}
	if !decodeJSON(w, r, &req) { return }
	sn := computeSafetyNumber(req.TheirPubkey, req.MyPubkey)
	_, err := a.db.Exec(r.Context(),
		`INSERT INTO contact_verification_saf (user_id, contact_user_id, safety_number)
		 VALUES ($1,$2,$3) ON CONFLICT (user_id, contact_user_id) DO UPDATE SET safety_number=$3, verified_at=now()`,
		uid, req.ContactUserID, sn)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "verification failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"safety_number": sn, "status": "verified"})
}

func (a *App) handleListVerifiedContacts(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	rows, err := a.db.Query(r.Context(),
		`SELECT user_id, contact_user_id, safety_number, verified_at
		 FROM contact_verification_saf WHERE user_id=$1 ORDER BY verified_at DESC`, uid)
	if err != nil { writeErr(w, http.StatusInternalServerError, "query failed"); return }
	defer rows.Close()
	var contacts []VerifiedContact
	for rows.Next() {
		var c VerifiedContact
		rows.Scan(&c.UserID, &c.ContactUserID, &c.SafetyNumber, &c.VerifiedAt)
		contacts = append(contacts, c)
	}
	writeJSON(w, http.StatusOK, contacts)
}

func (a *App) handleUnverifyContact(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	cid := r.PathValue("contactId")
	_, err := a.db.Exec(r.Context(),
		`DELETE FROM contact_verification_saf WHERE user_id=$1 AND contact_user_id=$2`, uid, cid)
	if err != nil {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "unverified"})
}