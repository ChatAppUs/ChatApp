"use client";

import { useCallback, useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { api, getAccessToken } from "@/lib/api";

interface Notification {
  id: number;
  kind: string;
  payload: Record<string, unknown>;
  read_at: string | null;
  created_at: string;
}

const KIND_LABELS: Record<string, string> = {
  like: "liked your post",
  comment: "commented on your post",
  comment_like: "liked your comment",
  mention: "mentioned you",
  follow: "started following you",
  repost: "reposted your post",
  story_reaction: "reacted to your story",
  story_reply: "replied to your story",
  kyc_decision: "KYC review update",
  ad_decision: "ad campaign review update",
  payout_decision: "payout review update",
};

export default function NotificationsPage() {
  const router = useRouter();
  const [items, setItems] = useState<Notification[]>([]);
  const [loading, setLoading] = useState(true);

  const load = useCallback(async () => {
    const d = await api<{ notifications: Notification[] }>("/api/notifications").catch(() => null);
    if (d) setItems(d.notifications);
    setLoading(false);
  }, []);

  useEffect(() => {
    if (!getAccessToken()) {
      router.push("/login");
      return;
    }
    load();
  }, [router, load]);

  const markAllRead = async () => {
    await api("/api/notifications/read", { method: "POST", body: "{}" }).catch(() => {});
    setItems((prev) => prev.map((n) => ({ ...n, read_at: n.read_at ?? new Date().toISOString() })));
  };

  const unread = items.filter((n) => !n.read_at).length;

  return (
    <div className="col" style={{ maxWidth: 640, margin: "0 auto" }}>
      <div className="row">
        <h2>Notifications {unread > 0 && <span className="badge yellow">{unread}</span>}</h2>
        <div className="spacer" />
        {unread > 0 && (
          <button className="secondary small" onClick={markAllRead}>Mark all read</button>
        )}
      </div>
      {loading && <div className="muted">Loading…</div>}
      {!loading && items.length === 0 && <div className="muted">No notifications yet.</div>}
      {items.map((n) => (
        <div key={n.id} className="card row" style={{ opacity: n.read_at ? 0.7 : 1 }}>
          <div>
            <strong>{KIND_LABELS[n.kind] ?? n.kind}</strong>
            <div className="muted" style={{ fontSize: 12 }}>
              {new Date(n.created_at).toLocaleString()}
            </div>
          </div>
          <div className="spacer" />
          {!n.read_at && <span className="badge">new</span>}
        </div>
      ))}
    </div>
  );
}
