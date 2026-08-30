"use client";

import { useEffect, useState } from "react";
import { api, uploadMedia } from "@/lib/api";
import { useI18n } from "@/lib/i18n";
import type { Media } from "@/lib/types";

type Analytics = {
  views: number; unique_viewers: number; likes: number;
  comments: number; shares: number; remixes: number; created_at: string;
};

type RemixRow = { id: string; author_username: string; body: string; created_at: string };

export function RemixModal({ reelId, onClose }: { reelId: string; onClose: () => void }) {
  const { t } = useI18n();
  const [body, setBody] = useState("");
  const [file, setFile] = useState<File | null>(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  const submit = async () => {
    setError("");
    setBusy(true);
    try {
      const media: Media[] = [];
      if (file) {
        const url = await uploadMedia(file);
        media.push({ kind: file.type.startsWith("video/") ? "video" : "image", url });
      }
      await api("/api/posts", {
        method: "POST",
        body: JSON.stringify({ type: "reel", body, remix_of: reelId, media }),
      });
      onClose();
    } catch (e) {
      setError(e instanceof Error ? e.message : "remix failed");
      setBusy(false);
    }
  };

  return (
    <div className="card col" style={{ gap: 8 }}>
      <h4 style={{ margin: 0 }}>{t("remixThisReel")}</h4>
      <textarea placeholder={t("addYourTake")} value={body} maxLength={2000}
        onChange={(e) => setBody(e.target.value)} rows={2} />
      <input type="file" accept="video/*,image/*" onChange={(e) => setFile(e.target.files?.[0] ?? null)} />
      {error && <div className="error">{error}</div>}
      <div className="row" style={{ gap: 6 }}>
        <button onClick={submit} disabled={busy || (!body.trim() && !file)}>{busy ? t("posting") : t("postRemix")}</button>
        <button className="secondary" onClick={onClose}>{t("cancel")}</button>
      </div>
    </div>
  );
}

export function ReelAnalytics({ reelId }: { reelId: string }) {
  const { t } = useI18n();
  const [stats, setStats] = useState<Analytics | null>(null);
  const [error, setError] = useState("");

  useEffect(() => {
    api<Analytics>(`/api/reels/${reelId}/analytics`)
      .then(setStats)
      .catch((e) => setError(e instanceof Error ? e.message : "unavailable"));
  }, [reelId]);

  if (error) return null;
  if (!stats) return <div className="muted" style={{ fontSize: 12 }}>{t("loadingAnalytics")}</div>;
  return (
    <div className="row" style={{ fontSize: 12, gap: 10, flexWrap: "wrap" }}>
      <span>▶ {stats.views} ({stats.unique_viewers} viewers)</span>
      <span>👍 {stats.likes}</span>
      <span>💬 {stats.comments}</span>
      <span>↪ {stats.shares}</span>
      <span>🎬 {stats.remixes}</span>
    </div>
  );
}

export function RemixList({ reelId }: { reelId: string }) {
  const { t } = useI18n();
  const [rows, setRows] = useState<RemixRow[]>([]);
  useEffect(() => {
    api<{ remixes: RemixRow[] }>(`/api/reels/${reelId}/remixes`)
      .then((d) => setRows(d.remixes)).catch(() => {});
  }, [reelId]);
  if (rows.length === 0) return null;
  return (
    <div className="col" style={{ gap: 4, fontSize: 13 }}>
      <strong>{t("remixes")}</strong>
      {rows.map((r) => (
        <div key={r.id} className="row" style={{ gap: 6 }}>
          <span className="muted">@{r.author_username}</span>
          <span>{r.body.slice(0, 60)}</span>
        </div>
      ))}
    </div>
  );
}
