"use client";

import { useEffect, useState } from "react";
import { api, getUserId } from "@/lib/api";
import type { Post } from "@/lib/types";

const STORY_EMOJIS = ["❤️", "😂", "😮", "😢", "👏", "🔥"];

export default function StoryBar() {
  const [stories, setStories] = useState<Post[]>([]);
  const [active, setActive] = useState<Post | null>(null);
  const [reply, setReply] = useState("");
  const [sent, setSent] = useState(false);
  const [viewerCount, setViewerCount] = useState<number | null>(null);

  useEffect(() => {
    api<{ stories: Post[] }>("/api/stories")
      .then((d) => setStories(d.stories))
      .catch(() => {});
  }, []);

  const open = (s: Post) => {
    setActive(s);
    setReply("");
    setSent(false);
    setViewerCount(null);
    api(`/api/stories/${s.id}/view`, { method: "POST", body: "{}" }).catch(() => {});
    if (s.author_id === getUserId()) {
      api<{ viewers: unknown[] }>(`/api/stories/${s.id}/viewers`)
        .then((d) => setViewerCount(d.viewers.length))
        .catch(() => {});
    }
  };

  const react = async (emoji: string) => {
    if (!active) return;
    await api(`/api/stories/${active.id}/react`, {
      method: "POST",
      body: JSON.stringify({ emoji }),
    }).catch(() => {});
    setSent(true);
  };

  const sendReply = async () => {
    if (!active || !reply.trim()) return;
    await api(`/api/stories/${active.id}/reply`, {
      method: "POST",
      body: JSON.stringify({ body: reply }),
    }).catch(() => {});
    setReply("");
    setSent(true);
  };

  if (stories.length === 0) return null;

  return (
    <>
      <div className="story-bar">
        {stories.map((s) => (
          <div key={s.id} className="story" onClick={() => open(s)} role="button" tabIndex={0}>
            {s.author_avatar ? (
              <img className="avatar" src={s.author_avatar} alt="" />
            ) : (
              <div className="avatar" />
            )}
            <span>{s.author_name}</span>
          </div>
        ))}
      </div>
      {active && (
        <div
          onClick={() => setActive(null)}
          style={{
            position: "fixed", inset: 0, background: "rgba(0,0,0,0.9)", zIndex: 100,
            display: "flex", alignItems: "center", justifyContent: "center", flexDirection: "column",
          }}
        >
          <div style={{ color: "#fff", marginBottom: 10 }}>
            <strong>{active.author_name}</strong> · {new Date(active.created_at).toLocaleString()}
          </div>
          {active.media[0]?.kind === "video" ? (
            <video src={active.media[0].url} controls autoPlay style={{ maxHeight: "80vh", maxWidth: "90vw" }} />
          ) : active.media[0] ? (
            <img src={active.media[0].url} alt="" style={{ maxHeight: "80vh", maxWidth: "90vw" }} />
          ) : (
            <p style={{ color: "#fff", maxWidth: 480, fontSize: 20 }}>{active.body}</p>
          )}
          {active.body && active.media[0] && <p style={{ color: "#fff" }}>{active.body}</p>}
          <div onClick={(e) => e.stopPropagation()} style={{ marginTop: 12, textAlign: "center" }}>
            <div className="row" style={{ justifyContent: "center", gap: 8 }}>
              {STORY_EMOJIS.map((e) => (
                <button key={e} className="secondary small" style={{ fontSize: 18 }}
                  onClick={() => react(e)}>{e}</button>
              ))}
            </div>
            {active.author_id !== getUserId() && (
              <div className="row" style={{ marginTop: 8, maxWidth: 420 }}>
                <input
                  value={reply}
                  onChange={(e) => setReply(e.target.value)}
                  placeholder="Reply to story…"
                  onKeyDown={(e) => e.key === "Enter" && sendReply()}
                />
                <button className="small" onClick={sendReply}>Send</button>
              </div>
            )}
            {viewerCount !== null && (
              <div style={{ color: "#aaa", fontSize: 12, marginTop: 6 }}>👁 {viewerCount} views</div>
            )}
            {sent && <div style={{ color: "#7c7", fontSize: 12, marginTop: 6 }}>Sent ✓</div>}
          </div>
        </div>
      )}
    </>
  );
}
