"use client";

import { useRef, useState } from "react";
import { api, uploadMedia } from "@/lib/api";
import { useI18n } from "@/lib/i18n";
import type { Media } from "@/lib/types";
import CameraRecorder from "@/components/CameraRecorder";

function mediaKind(file: File): Media["kind"] {
  if (file.type.startsWith("video/")) return "video";
  if (file.type.startsWith("audio/")) return "audio";
  return "image";
}

export default function Composer({
  type = "post",
  onPosted,
}: {
  type?: "post" | "reel" | "story";
  onPosted?: () => void;
}) {
  const { t } = useI18n();
  const [body, setBody] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [pollMode, setPollMode] = useState(false);
  const [pollOptions, setPollOptions] = useState<string[]>(["", ""]);
  const [visibility, setVisibility] = useState<"public" | "followers" | "private">("public");
  const [feeling, setFeeling] = useState("");
  const [location, setLocation] = useState("");
  const [tagged, setTagged] = useState("");
  const [scheduleAt, setScheduleAt] = useState("");
  const [extrasOpen, setExtrasOpen] = useState(false);
  const [captured, setCaptured] = useState<File[]>([]);
  const [storyBg, setStoryBg] = useState("");
  const [stickers, setStickers] = useState<string[]>([]);
  const [musicTrack, setMusicTrack] = useState("");
  const [musicOffset, setMusicOffset] = useState(0);
  const fileRef = useRef<HTMLInputElement>(null);

  const STORY_BGS: Record<string, string> = {
    sunset: "linear-gradient(135deg,#ff9966,#ff5e62)",
    ocean: "linear-gradient(135deg,#2193b0,#6dd5ed)",
    forest: "linear-gradient(135deg,#134e5e,#71b280)",
    candy: "linear-gradient(135deg,#fc5c7d,#6a82fb)",
    midnight: "linear-gradient(135deg,#0f2027,#2c5364)",
    mono: "linear-gradient(135deg,#232526,#414345)",
  };
  const STICKER_CHOICES = ["😂", "😍", "🔥", "🎉", "👍", "💯", "🎵", "❤️", "😮", "🙌"];

  const activePollOptions = pollOptions.map((o) => o.trim()).filter(Boolean);

  const submit = async () => {
    const hasPoll = pollMode && activePollOptions.length >= 2;
    if (!body.trim() && !fileRef.current?.files?.length && !captured.length && !hasPoll) return;
    setBusy(true);
    setError("");
    try {
      const media: Media[] = [];
      const files = [...Array.from(fileRef.current?.files ?? []), ...captured].slice(0, 10);
      for (const f of files) {
        const url = await uploadMedia(f);
        media.push({ kind: mediaKind(f), url });
      }
      await api("/api/posts", {
        method: "POST",
        body: JSON.stringify({
          type,
          body,
          media,
          visibility,
          ...(hasPoll ? { poll_options: activePollOptions.slice(0, 4) } : {}),
          ...(feeling.trim() ? { feeling: feeling.trim() } : {}),
          ...(location.trim() ? { location: location.trim() } : {}),
          ...(tagged.trim()
            ? { tagged_users: tagged.split(/[,\s]+/).map((u) => u.replace(/^@/, "").trim()).filter(Boolean) }
            : {}),
          ...(scheduleAt ? { schedule_at: new Date(scheduleAt).toISOString() } : {}),
          ...(type === "story" && storyBg ? { story_background: storyBg } : {}),
          ...(type === "story" && stickers.length
            ? { story_stickers: JSON.stringify(stickers.map((emoji, i) => ({ emoji, x: 0.2 + (i % 5) * 0.15, y: 0.2 + Math.floor(i / 5) * 0.3 }))) }
            : {}),
          ...(type === "story" && musicTrack.trim()
            ? { story_music: JSON.stringify({ track: musicTrack.trim(), offset_s: musicOffset }) }
            : {}),
        }),
      });
      setBody("");
      setFeeling("");
      setLocation("");
      setTagged("");
      setScheduleAt("");
      setStoryBg("");
      setStickers([]);
      setMusicTrack("");
      setMusicOffset(0);
      setPollMode(false);
      setPollOptions(["", ""]);
      setCaptured([]);
      if (fileRef.current) fileRef.current.value = "";
      onPosted?.();
    } catch (e) {
      setError(e instanceof Error ? e.message : t("error"));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="card">
      {type === "story" && storyBg ? (
        <div style={{
          background: STORY_BGS[storyBg], borderRadius: 10, minHeight: 120,
          display: "flex", alignItems: "center", justifyContent: "center", padding: 16,
        }}>
          <textarea
            value={body}
            onChange={(e) => setBody(e.target.value)}
            placeholder={t("story")}
            style={{ background: "transparent", border: "none", textAlign: "center", fontSize: 18, color: "#fff" }}
          />
          {stickers.map((s, i) => (
            <span key={i} style={{ position: "absolute", fontSize: 28 }}>{s}</span>
          ))}
        </div>
      ) : (
        <textarea
          value={body}
          onChange={(e) => setBody(e.target.value)}
          placeholder={type === "story" ? t("story") : type === "reel" ? t("reel") : t("whatsOnYourMind")}
        />
      )}
      {type === "story" && (
        <div className="col" style={{ marginTop: 8, gap: 6 }}>
          <div className="row" style={{ gap: 6, flexWrap: "wrap" }}>
            {(Object.keys(STORY_BGS) as (keyof typeof STORY_BGS)[]).map((k) => (
              <button key={k} aria-label={`background ${k}`}
                onClick={() => setStoryBg(storyBg === k ? "" : k)}
                style={{
                  width: 28, height: 28, borderRadius: 6, border: storyBg === k ? "2px solid var(--accent)" : "1px solid var(--border)",
                  background: STORY_BGS[k], cursor: "pointer", padding: 0,
                }} />
            ))}
            <span className="muted" style={{ fontSize: 12 }}>{t("textBackground")}</span>
          </div>
          <div className="row" style={{ gap: 4, flexWrap: "wrap" }}>
            {STICKER_CHOICES.map((s) => (
              <button key={s} className="secondary small" style={{ padding: "2px 6px" }}
                onClick={() => setStickers((prev) => prev.includes(s) ? prev.filter((x) => x !== s) : [...prev, s])}>
                {s}{stickers.includes(s) ? " ✓" : ""}
              </button>
            ))}
          </div>
          <div className="row" style={{ gap: 6 }}>
            <input value={musicTrack} maxLength={120} placeholder={t("musicTrack")}
              onChange={(e) => setMusicTrack(e.target.value)} />
            {musicTrack && (
              <input type="number" min={0} max={300} value={musicOffset} style={{ width: 80 }}
                title="Start offset (seconds)"
                onChange={(e) => setMusicOffset(Math.max(0, Number(e.target.value) || 0))} />
            )}
          </div>
        </div>
      )}
      {pollMode && (
        <div className="col" style={{ marginTop: 8 }}>
          {pollOptions.map((opt, i) => (
            <input
              key={i}
              value={opt}
              maxLength={80}
              placeholder={`Option ${i + 1}`}
              onChange={(e) =>
                setPollOptions((prev) => prev.map((p, j) => (j === i ? e.target.value : p)))
              }
            />
          ))}
          {pollOptions.length < 4 && (
            <button
              className="secondary small"
              onClick={() => setPollOptions((prev) => [...prev, ""])}
            >
              {t("addOption")}
            </button>
          )}
        </div>
      )}
      {extrasOpen && (
        <div className="col" style={{ marginTop: 8 }}>
          <div className="row">
            <input value={feeling} maxLength={60} placeholder={t("feelingPlaceholder")}
              onChange={(e) => setFeeling(e.target.value)} />
            <input value={location} maxLength={120} placeholder={t("locationPlaceholder")}
              onChange={(e) => setLocation(e.target.value)} />
          </div>
          <input value={tagged} placeholder={t("tagPeople")}
            onChange={(e) => setTagged(e.target.value)} />
          <label className="muted" style={{ fontSize: 12 }}>
            {t("scheduleForLater")}
            <input type="datetime-local" value={scheduleAt}
              min={new Date(Date.now() + 3600_000).toISOString().slice(0, 16)}
              onChange={(e) => setScheduleAt(e.target.value)} style={{ marginInlineStart: 8 }} />
          </label>
        </div>
      )}
      <div className="row" style={{ marginTop: 8 }}>
        <input
          ref={fileRef}
          type="file"
          accept="image/*,video/*,audio/*"
          multiple={type === "post"}
          style={{ fontSize: 12 }}
          aria-label={t("uploadMedia")}
        />
        {type === "post" && (
          <button
            className={pollMode ? "small" : "secondary small"}
            onClick={() => setPollMode((v) => !v)}
          >
            📊 Poll
          </button>
        )}
        {type === "post" && (
          <button className={extrasOpen ? "small" : "secondary small"}
            title="Feeling, location, tags, schedule"
            onClick={() => setExtrasOpen((v) => !v)}>
            😊 Extras
          </button>
        )}
        {type === "reel" && (
          <span className="col" style={{ gap: 4 }}>
            <CameraRecorder onCaptured={(f) => setCaptured((prev) => [...prev, f])} />
            {captured.length > 0 && (
              <div className="row" style={{ gap: 6 }}>
                <span className="muted small">📹 {captured.length}</span>
                <button className="secondary small" onClick={() => setCaptured([])}>{t("remove")}</button>
              </div>
            )}
          </span>
        )}
        {type === "post" && (
          <select
            value={visibility}
            onChange={(e) => setVisibility(e.target.value as typeof visibility)}
            style={{ fontSize: 12 }}
            aria-label="Audience"
          >
            <option value="public">🌍 Public</option>
            <option value="followers">👥 Followers</option>
            <option value="private">🔒 Only me</option>
          </select>
        )}
        <div className="spacer" />
        <button onClick={submit} disabled={busy}>
          {busy ? t("loading") : t("post")}
        </button>
      </div>
      {error && <div className="error-text">{error}</div>}
    </div>
  );
}
