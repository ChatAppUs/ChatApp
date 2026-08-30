package main

import (
	"context"
)

// Event reminders: an RSVP of "going" or "interested" registers a reminder
// one hour before the event starts; the scheduler claims due rows and turns
// them into notifications (which the push worker then fans out).

func (a *App) deliverEventReminders(ctx context.Context) {
	rows, err := a.db.Query(ctx,
		`UPDATE event_reminders SET sent_at = now()
		 WHERE (event_id, user_id) IN (
		   SELECT event_id, user_id FROM event_reminders
		   WHERE sent_at IS NULL AND remind_at <= now()
		   LIMIT 100
		 )
		 RETURNING event_id, user_id`)
	if err != nil {
		return
	}
	type due struct{ eventID, userID string }
	var dues []due
	for rows.Next() {
		var d due
		if err := rows.Scan(&d.eventID, &d.userID); err == nil {
			dues = append(dues, d)
		}
	}
	rows.Close()
	for _, d := range dues {
		var title string
		var startsAt string
		if err := a.db.QueryRow(ctx,
			`SELECT title, starts_at::text FROM events WHERE id=$1`,
			d.eventID).Scan(&title, &startsAt); err != nil {
			continue
		}
		a.notifyUser(ctx, d.userID, "event_reminder", map[string]string{
			"event_id": d.eventID, "title": title, "starts_at": startsAt,
		})
	}
}
