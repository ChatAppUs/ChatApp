package main

import (
	"net/http"
	"time"
)

type AnonymousAddress struct {
	ID          string     `json:"id"`
	AddressType string     `json:"address_type"`
	Address     string     `json:"address"`
	PublicKey   string     `json:"public_key"`
	PairwiseID  string     `json:"pairwise_id"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	LastUsedAt  time.Time  `json:"last_used_at"`
}

func (a *App) handleCreateAnonymousAddress(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	var req struct {
		AddressType string `json:"address_type"`
		Address     string `json:"address"`
		PublicKey   string `json:"public_key"`
		PairwiseID  string `json:"pairwise_id"`
	}
	if !decodeJSON(w, r, &req) { return }
	if req.AddressType == "" { req.AddressType = "onion" }

	var addr AnonymousAddress
	err := a.db.QueryRow(r.Context(),
		`INSERT INTO anonymous_addresses (user_id, address_type, address, public_key, pairwise_id)
		 VALUES ($1,$2,$3,$4,$5)
		 RETURNING id, address_type, address, public_key, pairwise_id, expires_at, created_at, last_used_at`,
		uid, req.AddressType, req.Address, req.PublicKey, req.PairwiseID,
	).Scan(&addr.ID, &addr.AddressType, &addr.Address, &addr.PublicKey, &addr.PairwiseID, &addr.ExpiresAt, &addr.CreatedAt, &addr.LastUsedAt)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to create anonymous address")
		return
	}
	writeJSON(w, http.StatusCreated, addr)
}

func (a *App) handleListAnonymousAddresses(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	rows, err := a.db.Query(r.Context(),
		`SELECT id, address_type, address, public_key, pairwise_id, expires_at, created_at, last_used_at
		 FROM anonymous_addresses WHERE user_id=$1 ORDER BY created_at DESC`, uid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "query failed")
		return
	}
	defer rows.Close()
	var addrs []AnonymousAddress
	for rows.Next() {
		var a AnonymousAddress
		if rows.Scan(&a.ID, &a.AddressType, &a.Address, &a.PublicKey, &a.PairwiseID, &a.ExpiresAt, &a.CreatedAt, &a.LastUsedAt) == nil {
			addrs = append(addrs, a)
		}
	}
	if addrs == nil { addrs = []AnonymousAddress{} }
	writeJSON(w, http.StatusOK, addrs)
}

func (a *App) handleDeleteAnonymousAddress(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	addrID := r.PathValue("id")
	_, err := a.db.Exec(r.Context(),
		`DELETE FROM anonymous_addresses WHERE id=$1 AND user_id=$2`, addrID, uid)
	if err != nil {
		writeErr(w, http.StatusNotFound, "address not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}