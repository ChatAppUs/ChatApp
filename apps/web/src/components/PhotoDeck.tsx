"use client";

import { useState } from "react";
import { useI18n } from "@/lib/i18n";
import type { Media } from "@/lib/types";

/** TikTok photo-mode (slideshow) carousel for image-only reels. */
export default function PhotoDeck({ media }: { media: Media[] }) {
  const { t } = useI18n();
  const [idx, setIdx] = useState(0);
  const items = media.filter((m) => m.kind === "image");
  if (items.length === 0) return null;
  const cur = items[Math.min(idx, items.length - 1)];
  return (
    <div className="card" style={{ padding: 0, position: "relative", overflow: "hidden" }}>
      {/* eslint-disable-next-line @next/next/no-img-element */}
      <img src={cur.url} alt={t("photoMode")} style={{ width: "100%", display: "block", objectFit: "contain" }} />
      {items.length > 1 && (
        <div className="row" style={{ position: "absolute", bottom: 8, left: 8, right: 8, gap: 4, justifyContent: "space-between" }}>
          <button className="secondary small" onClick={() => setIdx((i) => Math.max(0, i - 1))}>‹</button>
          <span className="muted small">{idx + 1}/{items.length}</span>
          <button className="secondary small" onClick={() => setIdx((i) => Math.min(items.length - 1, i + 1))}>›</button>
        </div>
      )}
    </div>
  );
}