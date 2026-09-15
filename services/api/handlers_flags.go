package main

// Feature flags (§74) and controlled experiments (§75). Flags gate rollouts
// per user with a stable FNV-1a bucket: the same user always sees the same
// variant for a given flag, so experiments are deterministic across sessions.
// Rollouts combine percentage, region and platform gates.

import (
	"context"
	"hash/fnv"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type featureFlag struct {
	Key         string   `json:"key"`
	Description string   `json:"description"`
	Enabled     bool     `json:"enabled"`
	RolloutPct  int      `json:"rollout_pct"`
	Regions     []string `json:"regions"`
	Platforms   []string `json:"platforms"`
	On          bool     `json:"on"`
}

// flagBucket returns a stable 0..99 bucket for (user, flag).
func flagBucket(userID, key string) int {
	h := fnv.New32a()
	h.Write([]byte(userID + "\x00" + key))
	return int(h.Sum32() % 100)
}

// evalFlags loads every flag and evaluates it for the caller.
func (a *App) evalFlags(ctx context.Context, userID, region, platform string) ([]featureFlag, error) {
	rows, err := a.db.Query(ctx,
		`SELECT key, description, enabled, rollout_pct, regions, platforms
		   FROM feature_flags ORDER BY key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []featureFlag{}
	for rows.Next() {
		var f featureFlag
		if err := rows.Scan(&f.Key, &f.Description, &f.Enabled, &f.RolloutPct, &f.Regions, &f.Platforms); err != nil {
			return nil, err
		}
		f.On = a.flagOn(f, userID, region, platform)
		out = append(out, f)
	}
	return out, rows.Err()
}

// flagOn applies the enabled/percentage/region/platform gates.
func (a *App) flagOn(f featureFlag, userID, region, platform string) bool {
	if !f.Enabled {
		return false
	}
	if len(f.Regions) > 0 && !containsFold(f.Regions, region) {
		return false
	}
	if len(f.Platforms) > 0 && !containsFold(f.Platforms, platform) {
		return false
	}
	return flagBucket(userID, f.Key) < f.RolloutPct
}

// nilArrayCoalesce keeps NULLs out of NOT NULL text[] columns: an omitted
// array in a partial update must leave the stored value untouched, and an
// omitted array on create must write an empty set, never NULL.
func strSliceOrEmpty(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func containsFold(list []string, v string) bool {
	for _, s := range list {
		if strings.EqualFold(s, v) {
			return true
		}
	}
	return false
}

// GET /api/me/flags — every flag evaluated for the caller. Region/platform
// come from query params (?region=us&platform=web) so clients can declare
// their context; unknown context falls back to no-region gating.
func (a *App) handleMyFlags(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	flags, err := a.evalFlags(r.Context(), uid, r.URL.Query().Get("region"), r.URL.Query().Get("platform"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "flags unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"flags": flags})
}

// GET /api/me/experiments — experiment keys resolved to the caller's variant
// ("on" when the backing flag evaluates on, "off" otherwise).
func (a *App) handleMyExperiments(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	region, platform := r.URL.Query().Get("region"), r.URL.Query().Get("platform")
	rows, err := a.db.Query(r.Context(),
		`SELECT e.key, e.flag_key, f.description, f.enabled, f.rollout_pct, f.regions, f.platforms
		   FROM experiments e JOIN feature_flags f ON f.key = e.flag_key
		  ORDER BY e.key`)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "experiments unavailable")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var key, flagKey, desc string
		var f featureFlag
		if err := rows.Scan(&key, &flagKey, &desc, &f.Enabled, &f.RolloutPct, &f.Regions, &f.Platforms); err != nil {
			writeErr(w, http.StatusInternalServerError, "experiments unavailable")
			return
		}
		variant := "off"
		if a.flagOn(f, uid, region, platform) {
			variant = "on"
		}
		out = append(out, map[string]any{"key": key, "flag": flagKey, "variant": variant})
	}
	writeJSON(w, http.StatusOK, map[string]any{"experiments": out})
}

// ---- Admin plane ----

type flagInput struct {
	Key         string   `json:"key"`
	Description string   `json:"description"`
	Enabled     *bool    `json:"enabled"`
	RolloutPct  *int     `json:"rollout_pct"`
	Regions     []string `json:"regions"`
	Platforms   []string `json:"platforms"`
}

func (in *flagInput) validate() string {
	if in.Key == "" {
		return "key required"
	}
	if len(in.Key) > 120 {
		return "key too long"
	}
	if in.RolloutPct != nil && (*in.RolloutPct < 0 || *in.RolloutPct > 100) {
		return "rollout_pct must be 0..100"
	}
	return ""
}

// GET /api/admin/flags
func (a *App) handleAdminListFlags(w http.ResponseWriter, r *http.Request) {
	flags, err := a.evalFlags(r.Context(), "admin-plane", "", "")
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "flags unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"flags": flags})
} // GET /api/admin/experiments — every experiment with its flag and variant
// assignment rule.
func (a *App) handleAdminListExperiments(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Query(r.Context(),
		`SELECT e.key, e.flag_key, e.description, e.created_at,
		        f.enabled, f.rollout_pct, f.regions, f.platforms
		 FROM experiments e JOIN feature_flags f ON f.key = e.flag_key
		 ORDER BY e.created_at DESC`)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "experiments unavailable")
		return
	}
	defer rows.Close()
	type expRow struct {
		Key         string    `json:"key"`
		FlagKey     string    `json:"flag_key"`
		Description string    `json:"description"`
		CreatedAt   time.Time `json:"created_at"`
		Enabled     bool      `json:"enabled"`
		RolloutPct  int       `json:"rollout_pct"`
		Regions     []string  `json:"regions"`
		Platforms   []string  `json:"platforms"`
	}
	out := []expRow{}
	for rows.Next() {
		var e expRow
		if err := rows.Scan(&e.Key, &e.FlagKey, &e.Description, &e.CreatedAt,
			&e.Enabled, &e.RolloutPct, &e.Regions, &e.Platforms); err != nil {
			writeErr(w, http.StatusInternalServerError, "experiments scan failed")
			return
		}
		out = append(out, e)
	}
	writeJSON(w, http.StatusOK, map[string]any{"experiments": out})
}

// POST /api/admin/flags — create or upsert.
func (a *App) handleAdminUpsertFlag(w http.ResponseWriter, r *http.Request) {
	var in flagInput
	if !decodeJSON(w, r, &in) {
		return
	}
	if msg := in.validate(); msg != "" {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}
	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	pct := 0
	if in.RolloutPct != nil {
		pct = *in.RolloutPct
	}
	regions := in.Regions
	if regions == nil {
		regions = []string{}
	}
	platforms := in.Platforms
	if platforms == nil {
		platforms = []string{}
	}
	_, err := a.db.Exec(r.Context(),
		`INSERT INTO feature_flags (key, description, enabled, rollout_pct, regions, platforms)
		 VALUES ($1,$2,$3,$4,$5,$6)
		 ON CONFLICT (key) DO UPDATE SET description=$2, enabled=$3,
		   rollout_pct=$4, regions=$5, platforms=$6, updated_at=now()`,
		in.Key, in.Description, enabled, pct, regions, platforms)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "flag write failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// PUT /api/admin/flags/{key} — partial update.
func (a *App) handleAdminUpdateFlag(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	var in flagInput
	if !decodeJSON(w, r, &in) {
		return
	}
	if in.RolloutPct != nil && (*in.RolloutPct < 0 || *in.RolloutPct > 100) {
		writeErr(w, http.StatusBadRequest, "rollout_pct must be 0..100")
		return
	}
	tag, err := a.db.Exec(r.Context(),
		`UPDATE feature_flags SET
		   enabled     = COALESCE($2, enabled),
		   rollout_pct = COALESCE($3, rollout_pct),
		   description = COALESCE($4, description),
		   regions     = COALESCE($5, regions),
		   platforms   = COALESCE($6, platforms),
		   updated_at  = now()
		 WHERE key=$1`,
		key, in.Enabled, in.RolloutPct, nilIfEmpty(in.Description), strSliceOrEmpty(in.Regions), strSliceOrEmpty(in.Platforms))
	if err != nil || tag.RowsAffected() == 0 {
		writeErr(w, http.StatusNotFound, "flag not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// DELETE /api/admin/flags/{key}
func (a *App) handleAdminDeleteFlag(w http.ResponseWriter, r *http.Request) {
	tag, err := a.db.Exec(r.Context(), `DELETE FROM feature_flags WHERE key=$1`, r.PathValue("key"))
	if err != nil || tag.RowsAffected() == 0 {
		writeErr(w, http.StatusNotFound, "flag not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// POST /api/admin/experiments — attach an experiment to a flag.
func (a *App) handleAdminCreateExperiment(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Key         string `json:"key"`
		FlagKey     string `json:"flag_key"`
		Description string `json:"description"`
	}
	if !decodeJSON(w, r, &in) || in.Key == "" || in.FlagKey == "" {
		writeErr(w, http.StatusBadRequest, "key and flag_key required")
		return
	}
	var exists bool
	if err := a.db.QueryRow(r.Context(),
		`SELECT EXISTS(SELECT 1 FROM feature_flags WHERE key=$1)`, in.FlagKey).Scan(&exists); err != nil || !exists {
		writeErr(w, http.StatusNotFound, "flag not found")
		return
	}
	_, err := a.db.Exec(r.Context(),
		`INSERT INTO experiments (key, flag_key, description) VALUES ($1,$2,$3)
		 ON CONFLICT (key) DO UPDATE SET flag_key=$2, description=$3`,
		in.Key, in.FlagKey, in.Description)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "experiment write failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// GET /api/admin/experiments/{key}/results — per-variant outcome metrics
// (§75: measure satisfaction/retention/watch time/reports, not only
// engagement). Variants come from bucketing every active user.
func (a *App) handleAdminExperimentResults(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	var flagKey string
	if err := a.db.QueryRow(r.Context(),
		`SELECT flag_key FROM experiments WHERE key=$1`, key).Scan(&flagKey); err != nil {
		if err == pgx.ErrNoRows {
			writeErr(w, http.StatusNotFound, "experiment not found")
			return
		}
		writeErr(w, http.StatusInternalServerError, "experiment lookup failed")
		return
	}
	var f featureFlag
	if err := a.db.QueryRow(r.Context(),
		`SELECT description, enabled, rollout_pct, regions, platforms FROM feature_flags WHERE key=$1`,
		flagKey).Scan(&f.Description, &f.Enabled, &f.RolloutPct, &f.Regions, &f.Platforms); err != nil {
		writeErr(w, http.StatusNotFound, "flag not found")
		return
	}

	// Variant assignment mirrors flagOn: region/platform gates resolve against
	// each user's phone country and locale, so experiment membership matches
	// what the client would evaluate.
	rows, err := a.db.Query(r.Context(), `
WITH members AS (
  SELECT id,
         CASE WHEN $2::bool
               AND ($3::text[] = '{}' OR COALESCE(phone_country,'') = ANY($3::text[]))
               AND ($4::text[] = '{}' OR COALESCE(locale,'') = ANY($4::text[]))
               AND (hashtext(id::text || ':' || $1) & 2147483647) % 100 < $5::int
              THEN 'on' ELSE 'off' END AS variant
  FROM users
)
SELECT m.variant,
       count(DISTINCT m.id)                                  AS users,
       count(DISTINCT r.id)                                  AS reports,
       COALESCE(round(avg(w.completed::int)::numeric, 4), 0) AS completion_rate,
       COALESCE(round(avg(w.watched_ms)), 0)                 AS avg_watch_ms,
       COALESCE(round(avg(GREATEST(w.duration_ms - w.watched_ms, 0))), 0) AS avg_unwatched_ms
FROM members m
LEFT JOIN reel_watch_events w ON w.user_id = m.id
LEFT JOIN reports r ON r.reporter_id = m.id
GROUP BY m.variant`, flagKey, f.Enabled, f.Regions, f.Platforms, f.RolloutPct)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "results unavailable")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var variant string
		var users, reports int
		var completion float64
		var watch, unwatched int
		if err := rows.Scan(&variant, &users, &reports, &completion, &watch, &unwatched); err != nil {
			writeErr(w, http.StatusInternalServerError, "results unavailable")
			return
		}
		out = append(out, map[string]any{
			"variant": variant, "users": users, "reports": reports,
			"completion_rate": completion, "avg_watch_ms": watch,
			"avg_abandon_ms": unwatched,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"experiment": key, "flag": flagKey, "variants": out})
}
