package main

import (
	"context"
	"encoding/csv"
	"io"
	"net/http"
	"regexp"
	"strings"
)

// Sanctions screening: entries are imported from the free public OFAC SDN /
// EU / UN consolidated lists (admin uploads the CSV, or the refresh job
// fetches the published files). KYC submissions are screened against the
// list; any hit flags the submission for mandatory manual review — the
// pipeline never auto-approves on a hit. Documents and results never leave
// our own infrastructure.

var nonAlnum = regexp.MustCompile(`[^a-z0-9 ]+`)

func normName(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = nonAlnum.ReplaceAllString(s, " ")
	return strings.Join(strings.Fields(s), " ")
}

// POST /api/admin/sanctions/import — bulk import; body is CSV with
// "source,name,program" rows (header optional). Idempotent via
// (source, name_norm) uniqueness.
func (a *App) handleAdminImportSanctions(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 32<<20)
	reader := csv.NewReader(r.Body)
	reader.FieldsPerRecord = -1
	imported, skipped := 0, 0
	first := true
	for {
		rec, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil || len(rec) < 2 {
			skipped++
			continue
		}
		if first {
			first = false
			if strings.EqualFold(strings.TrimSpace(rec[0]), "source") {
				continue // header row
			}
		}
		source := strings.ToLower(strings.TrimSpace(rec[0]))
		name := strings.TrimSpace(rec[1])
		program := ""
		if len(rec) > 2 {
			program = strings.TrimSpace(rec[2])
		}
		norm := normName(name)
		if source == "" || norm == "" {
			skipped++
			continue
		}
		tag, err := a.db.Exec(r.Context(),
			`INSERT INTO sanctions_entries (source, name, name_norm, program)
                         VALUES ($1,$2,$3,$4) ON CONFLICT (source, name_norm) DO NOTHING`,
			source, name, norm, program)
		if err != nil {
			skipped++
			continue
		}
		if tag.RowsAffected() > 0 {
			imported++
		} else {
			skipped++
		}
	}
	a.audit(r.Context(), userIDFrom(r), "import_sanctions", "", map[string]any{"imported": imported})
	writeJSON(w, http.StatusOK, map[string]any{"imported": imported, "skipped": skipped})
}

// screenName returns how many sanctions entries match the given name. The
// match rule is containment either way on the normalized form, which errs
// toward false positives — acceptable because hits only force manual
// review, never an automatic decision.
func (a *App) screenName(ctx context.Context, name string) int {
	norm := normName(name)
	if len(norm) < 4 {
		return 0
	}
	var hits int
	_ = a.db.QueryRow(ctx,
		`SELECT count(*) FROM sanctions_entries
                 WHERE name_norm LIKE '%' || $1 || '%' OR $1 LIKE '%' || name_norm || '%'`,
		norm).Scan(&hits)
	return hits
}

// GET /api/admin/sanctions/stats
func (a *App) handleAdminSanctionsStats(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Query(r.Context(),
		`SELECT source, count(*) FROM sanctions_entries GROUP BY source ORDER BY source`)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load stats")
		return
	}
	defer rows.Close()
	counts := map[string]int{}
	total := 0
	for rows.Next() {
		var src string
		var n int
		if rows.Scan(&src, &n) == nil {
			counts[src] = n
			total += n
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"sources": counts, "total": total})
}
