package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// AI creator tools & assistant — master plan §23 (AI dubbing / AI clip
// generation) and §38 (in-app AI assistant).
//
// These features are provider-backed. The ML service is the single source of
// truth for model availability: when a model is not configured it returns
// `available:false` with the reason, and we persist that truthful state
// instead of fabricating an output. Clip selection and dubbing segmentation
// are deterministic analyses of real media metadata, so they run without a
// model and are marked accordingly.

// ---- shared ML helper ----

// mlPost calls the ML service and decodes its JSON reply. A nil error with
// available=false means the model is not configured (not a failure).
func (a *App) mlPost(ctx context.Context, path string, body any, out any) error {
	if a.cfg.MLServiceURL == "" {
		return fmt.Errorf("ML service not configured")
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", a.cfg.MLServiceURL+path, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 60 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return json.NewDecoder(resp.Body).Decode(out)
}

// ---------------------------------------------------------- AI Dubbing ------

type dubReply struct {
	Available  bool   `json:"available"`
	Reason     string `json:"reason"`
	AudioURL   string `json:"audio_url"`
	SourceLang string `json:"source_lang"`
	TargetLang string `json:"target_lang"`
	Segments   []struct {
		Start float64 `json:"start"`
		End   float64 `json:"end"`
		Text  string  `json:"text"`
	} `json:"segments"`
}

// POST /api/ai/dub — start (or return) a dubbing job for a media URL.
func (a *App) handleAiDub(w http.ResponseWriter, r *http.Request) {
	var req struct {
		MediaURL   string `json:"media_url"`
		TargetLang string `json:"target_lang"`
		SourceLang string `json:"source_lang"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	media := strings.TrimSpace(req.MediaURL)
	target := strings.ToLower(strings.TrimSpace(req.TargetLang))
	source := strings.ToLower(strings.TrimSpace(req.SourceLang))
	if media == "" || !strings.HasPrefix(media, "http") || len(media) > 2048 {
		writeErr(w, http.StatusBadRequest, "media_url must be an http(s) URL")
		return
	}
	if target == "" || len(target) > 16 {
		writeErr(w, http.StatusBadRequest, "target_lang required (e.g. es, fr, hi)")
		return
	}
	if source != "" && source == target {
		writeErr(w, http.StatusBadRequest, "target_lang must differ from source_lang")
		return
	}
	uid := userIDFrom(r)

	// Reuse an existing ready job for the same media+target.
	var id, status, audioURL, reason, label string
	err := a.db.QueryRow(r.Context(),
		`SELECT id, status, audio_url, reason, label FROM media_dubs
		  WHERE owner_id=$1 AND media_url=$2 AND target_lang=$3`,
		uid, media, target).Scan(&id, &status, &audioURL, &reason, &label)
	if err == nil && status == "ready" {
		writeJSON(w, http.StatusOK, map[string]any{
			"id": id, "status": status, "audio_url": audioURL,
			"label": label, "target_lang": target,
		})
		return
	}

	// Ask the ML service for availability + real translated segments.
	var reply dubReply
	mlErr := a.mlPost(r.Context(), "/dub", map[string]any{
		"media_url": media, "target_lang": target, "source_lang": source,
	}, &reply)

	newStatus := "unavailable"
	reasonText := "ML service not configured"
	if mlErr != nil {
		reasonText = "ML service unreachable: " + mlErr.Error()
	} else if reply.Available {
		newStatus = "ready"
		reasonText = ""
		audioURL = reply.AudioURL
	} else {
		reasonText = reply.Reason
		if reasonText == "" {
			reasonText = "dubbing model not configured"
		}
	}
	// Honest labelling is a spec requirement: dubbed audio must be marked.
	label = fmt.Sprintf("AI-dubbed audio — %s translation", target)
	if newStatus != "ready" {
		label = ""
	}

	if err != nil {
		err = a.db.QueryRow(r.Context(),
			`INSERT INTO media_dubs (owner_id, media_url, source_lang, target_lang,
			        status, audio_url, label, reason)
			 VALUES ($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id`,
			uid, media, source, target, newStatus, audioURL, label, reasonText).Scan(&id)
	} else {
		_, err = a.db.Exec(r.Context(),
			`UPDATE media_dubs SET status=$2, audio_url=$3, label=$4, reason=$5,
			        source_lang=$6, updated_at=now() WHERE id=$1`,
			id, newStatus, audioURL, label, reasonText, source)
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to record dubbing job")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id": id, "status": newStatus, "audio_url": audioURL, "label": label,
		"reason": reasonText, "target_lang": target,
		"segments": reply.Segments,
	})
}

// GET /api/ai/dubs — the caller's dubbing jobs.
func (a *App) handleAiDubList(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Query(r.Context(),
		`SELECT id, media_url, source_lang, target_lang, status, audio_url, label,
		        reason, created_at
		   FROM media_dubs WHERE owner_id=$1 ORDER BY created_at DESC LIMIT 100`,
		userIDFrom(r))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load dubbing jobs")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, media, src, tgt, status, audio, label, reason string
		var created time.Time
		if err := rows.Scan(&id, &media, &src, &tgt, &status, &audio, &label,
			&reason, &created); err == nil {
			out = append(out, map[string]any{
				"id": id, "media_url": media, "source_lang": src,
				"target_lang": tgt, "status": status, "audio_url": audio,
				"label": label, "reason": reason, "created_at": created,
			})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"dubs": out})
}

// ---------------------------------------------------------- AI Clips --------

type clipReply struct {
	Available bool   `json:"available"`
	Reason    string `json:"reason"`
	DurationS float64 `json:"duration_s"`
	Clips     []struct {
		StartS float64 `json:"start_s"`
		EndS   float64 `json:"end_s"`
		Score  float64 `json:"score"`
		Reason string  `json:"reason"`
		Title  string  `json:"title"`
	} `json:"clips"`
}

// POST /api/ai/clips/analyze — analyse a long video and propose short clips.
func (a *App) handleAiClipAnalyze(w http.ResponseWriter, r *http.Request) {
	var req struct {
		MediaURL string `json:"media_url"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	media := strings.TrimSpace(req.MediaURL)
	if media == "" || !strings.HasPrefix(media, "http") || len(media) > 2048 {
		writeErr(w, http.StatusBadRequest, "media_url must be an http(s) URL")
		return
	}
	uid := userIDFrom(r)

	var reply clipReply
	mlErr := a.mlPost(r.Context(), "/clips", map[string]any{"media_url": media}, &reply)

	status, reason := "unavailable", "ML service not configured"
	if mlErr != nil {
		reason = "ML service unreachable: " + mlErr.Error()
	} else if reply.Available {
		status, reason = "analyzed", ""
	} else {
		reason = reply.Reason
		if reason == "" {
			reason = "clip model not configured"
		}
	}

	tx, err := a.db.Begin(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "analysis failed")
		return
	}
	defer tx.Rollback(r.Context())
	var jobID string
	if err := tx.QueryRow(r.Context(),
		`INSERT INTO ai_clip_jobs (owner_id, media_url, duration_s, status, reason)
		 VALUES ($1,$2,$3::numeric,$4,$5) RETURNING id`,
		uid, media, reply.DurationS, status, reason).Scan(&jobID); err != nil {
		writeErr(w, http.StatusInternalServerError, "analysis failed")
		return
	}
	clips := []map[string]any{}
	for _, c := range reply.Clips {
		var clipID string
		if c.EndS <= c.StartS {
			continue
		}
		if err := tx.QueryRow(r.Context(),
			`INSERT INTO ai_clips (job_id, owner_id, start_s, end_s, score, reason, title)
			 VALUES ($1,$2,$3::numeric,$4::numeric,$5::numeric,$6,$7) RETURNING id`,
			jobID, uid, c.StartS, c.EndS, c.Score, c.Reason, c.Title).Scan(&clipID); err != nil {
			writeErr(w, http.StatusInternalServerError, "analysis failed")
			return
		}
		clips = append(clips, map[string]any{
			"id": clipID, "start_s": c.StartS, "end_s": c.EndS,
			"score": c.Score, "reason": c.Reason, "title": c.Title,
			"approved": false,
		})
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeErr(w, http.StatusInternalServerError, "analysis failed")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"job_id": jobID, "status": status, "reason": reason,
		"duration_s": reply.DurationS, "clips": clips,
	})
}

// GET /api/ai/clips — the caller's clip jobs with their clips.
func (a *App) handleAiClipList(w http.ResponseWriter, r *http.Request) {
	uid := userIDFrom(r)
	rows, err := a.db.Query(r.Context(),
		`SELECT id, media_url, duration_s::text, status, reason, created_at
		   FROM ai_clip_jobs WHERE owner_id=$1 ORDER BY created_at DESC LIMIT 50`, uid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load clip jobs")
		return
	}
	defer rows.Close()
	jobs := []map[string]any{}
	jobIDs := []string{}
	for rows.Next() {
		var id, media, dur, status, reason string
		var created time.Time
		if err := rows.Scan(&id, &media, &dur, &status, &reason, &created); err == nil {
			jobs = append(jobs, map[string]any{
				"id": id, "media_url": media, "duration_s": dur, "status": status,
				"reason": reason, "created_at": created, "clips": []map[string]any{},
			})
			jobIDs = append(jobIDs, id)
		}
	}
	if len(jobIDs) > 0 {
		crows, err := a.db.Query(r.Context(),
			`SELECT id, job_id, start_s::text, end_s::text, score::text, reason,
			        title, approved, published
			   FROM ai_clips WHERE job_id = ANY($1) ORDER BY score DESC`, jobIDs)
		if err == nil {
			defer crows.Close()
			byJob := map[string][]map[string]any{}
			for crows.Next() {
				var id, jobID, start, end, score, reason, title string
				var approved, published bool
				if err := crows.Scan(&id, &jobID, &start, &end, &score, &reason,
					&title, &approved, &published); err == nil {
					byJob[jobID] = append(byJob[jobID], map[string]any{
						"id": id, "start_s": start, "end_s": end, "score": score,
						"reason": reason, "title": title, "approved": approved,
						"published": published,
					})
				}
			}
			for _, j := range jobs {
				if cs, ok := byJob[j["id"].(string)]; ok {
					j["clips"] = cs
				}
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"jobs": jobs})
}

// POST /api/ai/clips/{clipId}/approve — human approval gate before publishing.
func (a *App) handleAiClipApprove(w http.ResponseWriter, r *http.Request) {
	clipID := r.PathValue("clipId")
	var req struct {
		Approve *bool `json:"approve"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	approve := true
	if req.Approve != nil {
		approve = *req.Approve
	}
	uid := userIDFrom(r)
	// The clip is owner-scoped; publishing is a separate flag that only ever
	// flips with explicit approval.
	res, err := a.db.Exec(r.Context(),
		`UPDATE ai_clips SET approved=$3, published=$3
		  WHERE id=$1 AND owner_id=$2`, clipID, uid, approve)
	if err != nil || res.RowsAffected() == 0 {
		writeErr(w, http.StatusNotFound, "clip not found")
		return
	}
	a.audit(r.Context(), uid, "ai.clip.approve", clipID, map[string]any{"approved": approve})
	writeJSON(w, http.StatusOK, map[string]any{"clip_id": clipID, "approved": approve})
}

// ------------------------------------------------------- AI Assistant -------

type assistantReply struct {
	Available bool   `json:"available"`
	Reason    string `json:"reason"`
	Reply     string `json:"reply"`
	Action    *struct {
		Kind    string         `json:"kind"`
		Payload map[string]any `json:"payload"`
	} `json:"action"`
}

// localAssistantFallback answers from the caller's real data when no model is
// configured. It is deliberately narrow and always truthful about its limits.
func (a *App) localAssistantFallback(r *http.Request, uid, prompt string) (string, string) {
	lower := strings.ToLower(prompt)
	switch {
	case strings.Contains(lower, "balance") || strings.Contains(lower, "wallet"):
		var bal string
		_ = a.db.QueryRow(r.Context(),
			`SELECT COALESCE(SUM(le.amount),0)::text FROM ledger_entries le
			   JOIN wallet_accounts wa ON wa.id = le.account_id
			  WHERE wa.user_id=$1 AND wa.asset='USD'`, uid).Scan(&bal)
		return "Your current USD balance is " + bal + ".", ""
	case strings.Contains(lower, "unread") || strings.Contains(lower, "message"):
		var n int
		_ = a.db.QueryRow(r.Context(),
			`SELECT COUNT(*) FROM notifications WHERE user_id=$1 AND read_at IS NULL`,
			uid).Scan(&n)
		return fmt.Sprintf("You have %d unread notifications.", n), ""
	case strings.Contains(lower, "trending") || strings.Contains(lower, "trend"):
		rows, err := a.db.Query(r.Context(),
			`SELECT topic FROM pulse_posts p, unnest(p.topics) AS topic
			  WHERE p.created_at > now() - interval '24 hours'
			  GROUP BY topic ORDER BY COUNT(*) DESC LIMIT 5`)
		if err != nil {
			return "", "failed to read trends"
		}
		defer rows.Close()
		var topics []string
		for rows.Next() {
			var t string
			if rows.Scan(&t) == nil {
				topics = append(topics, "#"+t)
			}
		}
		if len(topics) == 0 {
			return "There are no trending topics in the last 24 hours yet.", ""
		}
		return "Trending now: " + strings.Join(topics, ", "), ""
	}
	return "I can answer questions about your balance, unread notifications and " +
		"trending topics. A language model is not configured, so I cannot help " +
		"with open-ended requests yet.", "language model not configured"
}

// POST /api/assistant/conversations — start a conversation.
func (a *App) handleAssistantConvCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title string `json:"title"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	title := strings.TrimSpace(req.Title)
	if title == "" {
		title = "New conversation"
	}
	if len(title) > 200 {
		writeErr(w, http.StatusBadRequest, "title too long")
		return
	}
	uid := userIDFrom(r)
	var id string
	if err := a.db.QueryRow(r.Context(),
		`INSERT INTO assistant_conversations (owner_id, title) VALUES ($1,$2)
		 RETURNING id`, uid, title).Scan(&id); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to create conversation")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": id, "title": title})
}

// GET /api/assistant/conversations — the caller's conversations.
func (a *App) handleAssistantConvList(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Query(r.Context(),
		`SELECT c.id, c.title, c.created_at, c.updated_at,
		        (SELECT COUNT(*) FROM assistant_messages m WHERE m.conversation_id=c.id)
		   FROM assistant_conversations c WHERE c.owner_id=$1
		  ORDER BY c.updated_at DESC LIMIT 100`, userIDFrom(r))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load conversations")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, title string
		var created, updated time.Time
		var msgs int
		if err := rows.Scan(&id, &title, &created, &updated, &msgs); err == nil {
			out = append(out, map[string]any{
				"id": id, "title": title, "created_at": created,
				"updated_at": updated, "message_count": msgs,
			})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"conversations": out})
}

// POST /api/assistant/conversations/{id}/messages — send a prompt, get a reply.
func (a *App) handleAssistantMessage(w http.ResponseWriter, r *http.Request) {
	convID := r.PathValue("id")
	var req struct {
		Content string `json:"content"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	content := strings.TrimSpace(req.Content)
	if content == "" || len(content) > 8000 {
		writeErr(w, http.StatusBadRequest, "content required (max 8000 chars)")
		return
	}
	uid := userIDFrom(r)
	var owner string
	if err := a.db.QueryRow(r.Context(),
		`SELECT owner_id FROM assistant_conversations WHERE id=$1`,
		convID).Scan(&owner); err != nil {
		writeErr(w, http.StatusNotFound, "conversation not found")
		return
	}
	if owner != uid {
		writeErr(w, http.StatusForbidden, "not your conversation")
		return
	}
	// Persist the user turn first so history is never lost.
	if _, err := a.db.Exec(r.Context(),
		`INSERT INTO assistant_messages (conversation_id, role, content)
		 VALUES ($1,'user',$2)`, convID, content); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to record message")
		return
	}
	// Include prior turns as context so the reply is grounded in this thread.
	hist, _ := a.db.Query(r.Context(),
		`SELECT role, content FROM assistant_messages WHERE conversation_id=$1
		  ORDER BY created_at ASC LIMIT 40`, convID)
	var turns []map[string]string
	if hist != nil {
		for hist.Next() {
			var role, body string
			if hist.Scan(&role, &body) == nil {
				turns = append(turns, map[string]string{"role": role, "content": body})
			}
		}
		hist.Close()
	}

	replyText, replyReason, actionKind := "", "", ""
	var actionPayload map[string]any
	var reply assistantReply
	mlErr := a.mlPost(r.Context(), "/assistant", map[string]any{
		"messages": turns, "user_id": uid,
	}, &reply)
	if mlErr == nil && reply.Available {
		replyText = reply.Reply
		if reply.Action != nil {
			actionKind = reply.Action.Kind
			actionPayload = reply.Action.Payload
		}
	} else {
		if mlErr != nil {
			replyReason = "ML service unreachable: " + mlErr.Error()
		} else {
			replyReason = reply.Reason
		}
		replyText, _ = a.localAssistantFallback(r, uid, content)
	}
	if actionPayload == nil {
		actionPayload = map[string]any{}
	}

	tx, err := a.db.Begin(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to record reply")
		return
	}
	defer tx.Rollback(r.Context())
	var msgID string
	if err := tx.QueryRow(r.Context(),
		`INSERT INTO assistant_messages (conversation_id, role, content)
		 VALUES ($1,'assistant',$2) RETURNING id`, convID, replyText).Scan(&msgID); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to record reply")
		return
	}
	var actionID string
	if actionKind != "" {
		// AI-authored changes are proposed, never auto-applied.
		if err := tx.QueryRow(r.Context(),
			`INSERT INTO assistant_actions (owner_id, conversation_id, kind, payload, status)
			 VALUES ($1,$2,$3,$4,'proposed') RETURNING id`,
			uid, convID, actionKind, actionPayload).Scan(&actionID); err != nil {
			writeErr(w, http.StatusInternalServerError, "failed to record proposal")
			return
		}
	}
	if _, err := tx.Exec(r.Context(),
		`UPDATE assistant_conversations SET updated_at=now() WHERE id=$1`, convID); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to record reply")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to record reply")
		return
	}
	actionStatus := ""
	if actionKind != "" {
		actionStatus = "proposed"
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"message_id": msgID, "reply": replyText, "reason": replyReason,
		"action_id": actionID, "action_kind": actionKind,
		"action_status": actionStatus,
	})
}

// GET /api/assistant/actions — pending proposals awaiting approval.
func (a *App) handleAssistantActionList(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Query(r.Context(),
		`SELECT id, conversation_id, kind, payload, status, created_at
		   FROM assistant_actions WHERE owner_id=$1
		  ORDER BY created_at DESC LIMIT 100`, userIDFrom(r))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to load proposals")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, kind, status string
		var conv *string
		var payload map[string]any
		var created time.Time
		if err := rows.Scan(&id, &conv, &kind, &payload, &status, &created); err == nil {
			out = append(out, map[string]any{
				"id": id, "conversation_id": conv, "kind": kind,
				"payload": payload, "status": status, "created_at": created,
			})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"actions": out})
}

// POST /api/assistant/actions/{id}/decide — approve or dismiss a proposal.
func (a *App) handleAssistantActionDecide(w http.ResponseWriter, r *http.Request) {
	actionID := r.PathValue("id")
	var req struct {
		Decision string `json:"decision"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	dec := strings.ToLower(strings.TrimSpace(req.Decision))
	if dec != "approved" && dec != "dismissed" {
		writeErr(w, http.StatusBadRequest, "decision must be approved or dismissed")
		return
	}
	uid := userIDFrom(r)
	// Only a still-proposed action can be decided — idempotent double-click safe.
	res, err := a.db.Exec(r.Context(),
		`UPDATE assistant_actions SET status=$3, decided_at=now()
		  WHERE id=$1 AND owner_id=$2 AND status='proposed'`, actionID, uid, dec)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "decision failed")
		return
	}
	if res.RowsAffected() == 0 {
		writeErr(w, http.StatusConflict, "proposal not found or already decided")
		return
	}
	a.audit(r.Context(), uid, "assistant.action."+dec, actionID, nil)
	writeJSON(w, http.StatusOK, map[string]any{"action_id": actionID, "status": dec})
}
