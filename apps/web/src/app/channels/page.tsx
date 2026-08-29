"use client";

import { useCallback, useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { api } from "@/lib/api";
import type { Channel } from "@/lib/types";

export default function ChannelsPage() {
  const router = useRouter();
  const [channels, setChannels] = useState<Channel[]>([]);
  const [q, setQ] = useState("");
  const [title, setTitle] = useState("");
  const [description, setDescription] = useState("");
  const [error, setError] = useState("");

  const load = useCallback((query = "") => {
    api<{ channels: Channel[] }>(`/api/channels?q=${encodeURIComponent(query)}`)
      .then((d) => setChannels(d.channels))
      .catch(() => {});
  }, []);

  useEffect(() => load(), [load]);

  const create = async () => {
    setError("");
    if (!title.trim()) return;
    try {
      await api("/api/conversations", {
        method: "POST",
        body: JSON.stringify({ is_channel: true, title, description, member_ids: [] }),
      });
      setTitle("");
      setDescription("");
      load(q);
    } catch (e) {
      setError(e instanceof Error ? e.message : "failed to create channel");
    }
  };

  const toggle = async (c: Channel) => {
    await api(`/api/channels/${c.id}/subscribe`, {
      method: c.joined ? "DELETE" : "POST",
      body: "{}",
    }).catch(() => {});
    load(q);
  };

  return (
    <div className="col">
      <div className="card col">
        <h2 style={{ marginTop: 0 }}>Create a channel</h2>
        <input placeholder="Channel title" value={title} onChange={(e) => setTitle(e.target.value)} />
        <input placeholder="Description" value={description} onChange={(e) => setDescription(e.target.value)} />
        {error && <div className="error">{error}</div>}
        <button onClick={create}>Create channel</button>
      </div>
      <div className="card col">
        <h2 style={{ marginTop: 0 }}>Discover channels</h2>
        <input
          placeholder="Search channels"
          value={q}
          onChange={(e) => { setQ(e.target.value); load(e.target.value); }}
        />
        {channels.map((c) => (
          <div key={c.id} className="row">
            <div>
              <div><strong>📢 {c.title}</strong></div>
              <div className="muted">{c.description} · {c.members} subscribers</div>
            </div>
            <div className="spacer" />
            <button className={c.joined ? "secondary small" : "small"} onClick={() => toggle(c)}>
              {c.joined ? "Leave" : "Join"}
            </button>
            {c.joined && (
              <button className="secondary small" onClick={() => router.push("/chat")}>Open</button>
            )}
          </div>
        ))}
        {channels.length === 0 && <span className="muted">No channels found.</span>}
      </div>
    </div>
  );
}
