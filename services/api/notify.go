package main

import "context"

// notify.go — the single choke point for in-app notification writes.
//
// Master Documentation §33 requires the pipeline
// event → eligibility → preference check → delivery. The per-kind preference
// matrix (notification_settings, §gap9) was stored and served by
// /api/me/notification-settings but no write path consulted it: muting a kind
// silenced nothing. This wrapper gates every write on the user's per-kind
// preference (default: enabled) so the toggle actually works, and it also
// feeds the fan-out of push deliveries through the same gate.

// notifyKind inserts an in-app notification of the given kind unless the
// recipient has muted that kind. Best-effort: a failed preference lookup or
// insert never fails the originating request.
func (a *App) notifyKind(userID, kind string, payload any) {
	if !a.notificationEnabled(context.Background(), userID, kind) {
		return
	}
	_, _ = a.db.Exec(context.Background(),
		`INSERT INTO notifications (user_id, kind, payload) VALUES ($1,$2,$3)`,
		userID, kind, payload)
}

// notify implements the existing map[string]string helper used across handlers,
// now routed through the same preference gate.
func (a *App) notifyUser(ctx context.Context, userID, kind string, payload map[string]string) {
	if !a.notificationEnabled(ctx, userID, kind) {
		return
	}
	_, _ = a.db.Exec(ctx,
		`INSERT INTO notifications (user_id, kind, payload) VALUES ($1,$2,$3)`, userID, kind, payload)
}
