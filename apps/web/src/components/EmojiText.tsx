"use client";

import { useEffect, useState } from "react";
import { api } from "@/lib/api";

/** Renders plain text with inline animated custom-emoji (Telegram-style).
 *  `:code:` segments resolve to the /api/custom-emoji media URL — cached
 *  module-wide to avoid a fetch per node. */

let MAP: Record<string, { url: string; animated: boolean }> | undefined;
let loading: Promise<Record<string, { url: string; animated: boolean }>> | undefined;

function getMap(): Promise<Record<string, { url: string; animated: boolean }>> {
  if (MAP) return Promise.resolve(MAP);
  if (!loading) {
    loading = api("/api/custom-emoji", {}, true)
      .then((j) => {
        const list = ((j as { emojis?: { name: string; media_url: string; animated: boolean }[] }).emojis ?? []) as {
          name: string; media_url: string; animated: boolean;
        }[];
        const map: Record<string, { url: string; animated: boolean }> = {};
        for (const e of list) map[":" + e.name + ":"] = { url: e.media_url, animated: e.animated };
        MAP = map;
        return map;
      })
      .catch(() => MAP ?? {});
  }
  return loading;
}

export function useEmojiMap() {
  const [m, setM] = useState<Record<string, { url: string; animated: boolean }>>(MAP ?? {});
  useEffect(() => {
    getMap().then(setM);
  }, []);
  return m;
}

/** Replaces :shortcode: tokens with inline images that can be animated
 *  (webp/gif) — used from PostCard, chat bubbles, comment text, etc. */
export function renderEmojiText(text: string, m: Record<string, { url: string; animated: boolean }>) {
  const parts: (string | { url: string; animated: boolean })[] = [];
  let buf = "";
  for (const token of text.split(/(:[a-z0-9_+-]+:)/gi)) {
    const hit = m[token];
    if (hit) {
      buf && parts.push(buf);
      parts.push(hit);
      buf = "";
    } else {
      buf += token;
    }
  }
  buf && parts.push(buf);
  return parts;
}

export default function EmojiText({ text, className }: { text: string; className?: string }) {
  const m = useEmojiMap();
  const parts = renderEmojiText(text, m);
  return (
    <span className={className}>
      {parts.map((p, i) =>
        typeof p === "string" ? (
          <span key={i}>{p}</span>
        ) : (
          <img key={i} alt="emoji" src={p.url} className="inline-emoji" width={p.animated ? 32 : 24} height={p.animated ? 32 : 24} />
        )
      )}
    </span>
  );
}
