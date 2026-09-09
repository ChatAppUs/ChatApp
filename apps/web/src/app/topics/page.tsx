"use client";

import { FormEvent, useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { api, getAccessToken } from "@/lib/api";

type Topic = {
  id: string;
  name: string;
  followers: number;
  following: boolean;
};

export default function TopicsPage() {
  const router = useRouter();
  const [topics, setTopics] = useState<Topic[]>([]);
  const [name, setName] = useState("");
  const [error, setError] = useState("");
  const [status, setStatus] = useState("");

  const load = () =>
    api<{ topics: Topic[] }>("/api/topics")
      .then((d) => setTopics(d.topics))
      .catch((e) => setError(e instanceof Error ? e.message : "failed to load topics"));

  useEffect(() => {
    if (!getAccessToken()) {
      router.push("/login");
      return;
    }
    load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const create = async (e: FormEvent) => {
    e.preventDefault();
    setError("");
    setStatus("");
    try {
      const d = await api<{ id: string }>("/api/topics", {
        method: "POST",
        body: JSON.stringify({ name }),
      });
      setStatus(`Topic created — ${name}).`);
      setName("");
      load();
      void d;
    } catch (e) {
      setError(e instanceof Error ? e.message : "failed to create topic");
    }
  };

  const toggle = async (t: Topic) => {
    const method = t.following ? "DELETE" : "POST";
    setError("");
    try {
      await api(method === "POST" ? `/api/topics/${t.id}/follow` : `/api/topics/${t.id}/follow`, { method });
      setTopics((prev) => prev.map((x) => (x.id === t.id ? { ...x, following: !x.following } : x)));
    } catch (e) {
      setError(e instanceof Error ? e.message : "update failed");
    }
  };

  return (
    <div className="col">
      <div className="card col" style={{ gap: 8 }}>
        <h2 style={{ margin: 0 }}>🏷️ Topics</h2>
        <div className="muted">Follow the interests you care about to shape your personalized feed.</div>
        {error && <div className="error-text">{error}</div>}
        {status && <div className="muted" style={{ color: "var(--accent-2)" }}>{status}</div>}
        <form onSubmit={create} className="row" style={{ gap: 6 }}>
          <input value={name} onChange={(e) => setName(e.target.value)} placeholder="New topic name" maxLength={60} style={{ flex: 1 }} />
          <button type="submit" disabled={name.trim().length < 2}>Create</button>
        </form>
      </div>
      {topics.length === 0 && !error && <div className="card muted">No topics yet — create the first one.</div>}
      {topics.map((t) => (
        <div key={t.id} className="card row">
          <div style={{ flex: 1 }}>
            <strong>{t.name}</strong>
            <div className="muted" style={{ fontSize: 12 }}>
              {t.followers} follower{t.followers === 1 ? "" : "s"}
            </div>
          </div>
          <button className={t.following ? "secondary small" : "small"} onClick={() => toggle(t)}>
            {t.following ? "Following ✓" : "Follow"}
          </button>
        </div>
      ))}
    </div>
  );
}