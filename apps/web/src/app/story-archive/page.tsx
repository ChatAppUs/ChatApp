"use client";

import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { api, getAccessToken } from "@/lib/api";

type Story = {
  id: string;
  body: string;
  media_url: string;
  created_at: string;
};

export default function StoryArchivePage() {
  const router = useRouter();
  const [items, setItems] = useState<Story[]>([]);
  const [error, setError] = useState("");

  useEffect(() => {
    if (!getAccessToken()) {
      router.push("/login");
      return;
    }
    api<{ archive: Story[] }>("/api/me/story-archive")
      .then((d) => setItems(d.archive ?? []))
      .catch((e) => setError(e instanceof Error ? e.message : "failed to load archive"));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  return (
    <div className="col">
      <div className="card col" style={{ gap: 8 }}>
        <h2 style={{ margin: 0 }}>🗄️ Story archive</h2>
        <div className="muted">Every story you archived, newest first. Perfect for re-sharing later — “On this day” memories draw from here.</div>
        {error && <div className="error-text">{error}</div>}
      </div>
      {items.length === 0 && !error && <div className="card muted">No archived stories yet. Archive a story from the viewerto keep it forever一个个permanent.</div>}
      <div className="row" style={{ flexWrap: "wrap", gap: 10 }}>
        {items.map((s) => (
          <div key={s.id} className="card col" style={{ width: 180, gap: 6 }}>
            {s.media_url && (
              // eslint-disable-next-line @next/next/no-img-element
              <img src={s.media_url} alt="" style={{ width: "100%", aspectRatio: "9/16", objectFit: "cover", borderRadius: 10 }} />
            )}
            {s.body && <div className="muted" style={{ fontSize: 13 }}>{s.body}</div>}
            <div className="muted" style={{ fontSize: 11 }}>{new Date(s.created_at).toLocaleString()}</div>
          </div>
        ))}
      </div>
    </div>
  );
}