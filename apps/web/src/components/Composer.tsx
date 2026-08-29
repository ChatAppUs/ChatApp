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
  const fileRef = useRef<HTMLInputElement>(null);

  const submit = async () => {
    if (!body.trim() && !fileRef.current?.files?.length) return;
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
        body: JSON.stringify({ type, body, media, visibility: "public" }),
      });
      setBody("");
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
      <div className="row" style={{ marginTop: 8 }}>
        <input
          ref={fileRef}
          type="file"
          accept="image/*,video/*,audio/*"
          multiple={type === "post"}
          style={{ fontSize: 12 }}
          aria-label={t("uploadMedia")}
        />
        <div className="spacer" />
        <button onClick={submit} disabled={busy}>
          {busy ? t("loading") : t("post")}
        </button>
      </div>
      {error && <div className="error-text">{error}</div>}
    </div>
  );
}
