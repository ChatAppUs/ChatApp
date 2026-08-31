"use client";

import { useEffect, useRef, useState } from "react";
import { api, uploadMedia } from "@/lib/api";
import { useI18n } from "@/lib/i18n";
import type { Media, Post } from "@/lib/types";

type Analytics = {
  views: number; unique_viewers: number; likes: number;
  comments: number; shares: number; remixes: number; created_at: string;
};

type RemixRow = { id: string; username: string; author: string; created_at: string; remix_mode?: string };

export function RemixModal({ reelId, onClose }: { reelId: string; onClose: () => void }) {
  const { t } = useI18n();
  const [body, setBody] = useState("");
  const [file, setFile] = useState<File | null>(null);
  const [mode, setMode] = useState<"" | "duet" | "stitch">("");
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
        body: JSON.stringify({
          type: "reel", body, remix_of: reelId, media,
          ...(mode ? { remix_mode: mode } : {}),
        }),
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
      <div className="row" style={{ gap: 6, alignItems: "center" }}>
        <span className="muted" style={{ fontSize: 12 }}>{t("remixLayout")}:</span>
        {([["", t("remix")], ["duet", t("duet")], ["stitch", t("stitch")]] as const).map(([v, label]) => (
          <button key={v} className={mode === v ? "small" : "secondary small"} onClick={() => setMode(v)}>
            {label}
          </button>
        ))}
      </div>
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

// RemixPlayer renders a duet/stitch reel: duet plays the source and the
// response side-by-side; stitch plays the source clip once, then loops the
// response. The source reel is fetched through the permalink endpoint.
export function RemixPlayer({ post, videoRef, onTap }: {
  post: Post;
  videoRef: React.RefObject<HTMLVideoElement>;
  onTap: () => void;
}) {
  const [source, setSource] = useState<Post | null>(null);
  const [phase, setPhase] = useState<"source" | "response">(post.remix_mode === "stitch" ? "source" : "response");
  const srcRef = useRef<HTMLVideoElement>(null);

  useEffect(() => {
    if (!post.remix_of) return;
    api<{ post: Post }>(`/api/posts/${post.remix_of}`)
      .then((d) => setSource(d.post))
      .catch(() => {});
  }, [post.remix_of]);

  const ownVideo = post.media.find((m) => m.kind === "video");
  const srcVideo = source?.media.find((m) => m.kind === "video");

  if (post.remix_mode === "duet") {
    return (
      <div className="row" style={{ gap: 2, width: "100%", height: "100%" }}>
        {srcVideo && (
          <video ref={srcRef} src={srcVideo.url} loop muted playsInline autoPlay
            style={{ width: "50%", objectFit: "cover" }} preload="auto" />
        )}
        {ownVideo && (
          <video ref={videoRef} src={ownVideo.url} loop playsInline onClick={onTap}
            style={{ width: srcVideo ? "50%" : "100%", objectFit: "cover" }} preload="auto" />
        )}
      </div>
    );
  }
  // stitch: source clip first, then the response loops
  if (phase === "source" && srcVideo) {
    return (
      <video ref={srcRef} src={srcVideo.url} playsInline autoPlay muted={false}
        onEnded={() => setPhase("response")} style={{ width: "100%", objectFit: "cover" }} preload="auto" />
    );
  }
  return ownVideo ? (
    <video ref={videoRef} src={ownVideo.url} loop playsInline onClick={onTap}
      style={{ width: "100%", objectFit: "cover" }} preload="auto" autoPlay={post.remix_mode === "stitch"} />
  ) : null;
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
          <span className="muted">@{r.username}</span>
          {r.remix_mode && <span className="muted" style={{ fontSize: 11 }}>{t(r.remix_mode as "duet" | "stitch")}</span>}
        </div>
      ))}
    </div>
  );
}
