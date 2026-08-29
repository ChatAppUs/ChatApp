"use client";

import { useEffect, useState } from "react";
import { api } from "@/lib/api";
import type { Post } from "@/lib/types";

export default function StoryBar() {
  const [stories, setStories] = useState<Post[]>([]);
  const [active, setActive] = useState<Post | null>(null);

  useEffect(() => {
    api<{ stories: Post[] }>("/api/stories")
      .then((d) => setStories(d.stories))
      .catch(() => {});
  }, []);

  if (stories.length === 0) return null;

  return (
    <>
      <div className="story-bar">
        {stories.map((s) => (
          <div key={s.id} className="story" onClick={() => setActive(s)} role="button" tabIndex={0}>
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
        </div>
      )}
    </>
  );
}
