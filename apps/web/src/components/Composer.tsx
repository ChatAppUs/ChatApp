"use client";

import { useRef, useState } from "react";
import { api, uploadMedia } from "@/lib/api";
import { useI18n } from "@/lib/i18n";
import type { Media } from "@/lib/types";

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
  const fileRef = useRef<HTMLInputElement>(null);

  const activePollOptions = pollOptions.map((o) => o.trim()).filter(Boolean);

  const submit = async () => {
    const hasPoll = pollMode && activePollOptions.length >= 2;
    if (!body.trim() && !fileRef.current?.files?.length && !hasPoll) return;
    setBusy(true);
    setError("");
    try {
      const media: Media[] = [];
      const files = Array.from(fileRef.current?.files ?? []).slice(0, 10);
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
        }),
      });
      setBody("");
      setFeeling("");
      setLocation("");
      setTagged("");
      setScheduleAt("");
      setPollMode(false);
      setPollOptions(["", ""]);
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
      <textarea
        value={body}
        onChange={(e) => setBody(e.target.value)}
        placeholder={type === "story" ? t("story") : type === "reel" ? t("reel") : t("whatsOnYourMind")}
      />
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
              + Add option
            </button>
          )}
        </div>
      )}
      {extrasOpen && (
        <div className="col" style={{ marginTop: 8 }}>
          <div className="row">
            <input value={feeling} maxLength={60} placeholder="Feeling / activity (e.g. 😊 happy)"
              onChange={(e) => setFeeling(e.target.value)} />
            <input value={location} maxLength={120} placeholder="📍 Location"
              onChange={(e) => setLocation(e.target.value)} />
          </div>
          <input value={tagged} placeholder="Tag people — @user1 @user2"
            onChange={(e) => setTagged(e.target.value)} />
          <label className="muted" style={{ fontSize: 12 }}>
            Schedule for later:
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
