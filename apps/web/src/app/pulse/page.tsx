"use client";

import { useCallback, useEffect, useState } from "react";
import { api, getAccessToken } from "@/lib/api";

// ChatApp Pulse — master plan §32. Short posts, threads, quotes, reposts,
// topics, local feed and trends computed from real post volume.

type PulsePost = {
  id: string;
  author: string;
  author_id: string;
  body: string;
  topics: string[] | null;
  reply_count: number;
  repost_count: number;
  quote_count: number;
  created_at: string;
};
type Trend = { topic: string; score: number; scope: string };

export default function PulsePage() {
  const [authed, setAuthed] = useState(false);
  const [posts, setPosts] = useState<PulsePost[]>([]);
  const [trends, setTrends] = useState<Trend[]>([]);
  const [scope, setScope] = useState<"global" | "local">("global");
  const [region, setRegion] = useState("");
  const [topic, setTopic] = useState("");
  const [body, setBody] = useState("");
  const [replyTo, setReplyTo] = useState<PulsePost | null>(null);
  const [error, setError] = useState("");

  const load = useCallback(async () => {
    setError("");
    try {
      const qs = new URLSearchParams({ scope });
      if (scope === "local" && region) qs.set("region", region);
      if (topic) qs.set("topic", topic);
      const [feed, tr] = await Promise.all([
        api<{ posts: PulsePost[] }>(`/api/pulse/posts?${qs.toString()}`),
        api<{ trends: Trend[] }>(
          `/api/pulse/trends?scope=${scope}${region ? `&region=${encodeURIComponent(region)}` : ""}`
        ),
      ]);
      setPosts(feed.posts ?? []);
      setTrends(tr.trends ?? []);
    } catch (e) {
      setError(String(e));
    }
  }, [scope, region, topic]);

  useEffect(() => {
    setAuthed(!!getAccessToken());
    if (getAccessToken()) void load();
  }, [load]);

  const submit = async () => {
    if (!body.trim() && !replyTo) return;
    try {
      const payload: Record<string, unknown> = { body, local_tag: region };
      if (replyTo) payload.parent_id = replyTo.id;
      const tags = body.match(/#[\w]+/g)?.map((t) => t.slice(1)) ?? [];
      if (tags.length) payload.topics = tags;
      await api("/api/pulse/posts", { method: "POST", body: JSON.stringify(payload) });
      setBody("");
      setReplyTo(null);
      await load();
    } catch (e) {
      setError(String(e));
    }
  };

  const repost = async (p: PulsePost) => {
    try {
      await api("/api/pulse/posts", {
        method: "POST",
        body: JSON.stringify({ body: "", repost_of: p.id }),
      });
      await load();
    } catch (e) {
      setError(String(e));
    }
  };

  const remove = async (p: PulsePost) => {
    try {
      await api(`/api/pulse/posts/${p.id}`, { method: "DELETE" });
      await load();
    } catch (e) {
      setError(String(e));
    }
  };

  if (!authed) {
    return (
      <main className="container">
        <h1>Pulse</h1>
        <p>Sign in to see the conversation.</p>
        <a className="btn" href="/login">Sign in</a>
      </main>
    );
  }

  return (
    <main className="container">
      <h1>Pulse</h1>
      {error && <p className="error">{error}</p>}

      <section className="card">
        <h2>Trending now (24h)</h2>
        {trends.length === 0 ? (
          <p className="muted">No trends yet — post with a #topic to start one.</p>
        ) : (
          <p>
            {trends.map((t) => (
              <button key={t.topic} className="chip" onClick={() => setTopic(t.topic)}>
                #{t.topic} <span className="muted">{t.score}</span>
              </button>
            ))}
          </p>
        )}
      </section>

      <section className="card">
        {replyTo && (
          <p className="muted">
            Replying to <strong>{replyTo.author}</strong>{" "}
            <button className="link" onClick={() => setReplyTo(null)}>
              cancel
            </button>
          </p>
        )}
        <textarea
          placeholder="What's happening? Use #topics"
          value={body}
          onChange={(e) => setBody(e.target.value)}
        />
        <div className="row">
          <button className="btn" onClick={submit} disabled={!body.trim()}>
            {replyTo ? "Reply" : "Post"}
          </button>
          <select value={scope} onChange={(e) => setScope(e.target.value as "global" | "local")}>
            <option value="global">Global feed</option>
            <option value="local">Local feed</option>
          </select>
          {scope === "local" && (
            <input
              placeholder="region (e.g. dhaka)"
              value={region}
              onChange={(e) => setRegion(e.target.value)}
            />
          )}
          {topic && (
            <button className="chip" onClick={() => setTopic("")}>
              filtering #{topic} ✕
            </button>
          )}
        </div>
      </section>

      <ul className="list">
        {posts.map((p) => (
          <li key={p.id} className="card">
            <div>
              <strong>{p.author}</strong>{" "}
              <span className="muted">{new Date(p.created_at).toLocaleString()}</span>
            </div>
            <p>{p.body}</p>
            <div>
              {(p.topics ?? []).map((t) => (
                <button key={t} className="chip" onClick={() => setTopic(t)}>
                  #{t}
                </button>
              ))}
            </div>
            <div className="row">
              <span className="muted">
                {p.reply_count} replies · {p.repost_count} reposts · {p.quote_count} quotes
              </span>
              <button className="btn small" onClick={() => setReplyTo(p)}>
                Reply
              </button>
              <button className="btn small" onClick={() => repost(p)}>
                Repost
              </button>
              <a className="btn small" href={`/pulse/thread/${p.id}`}>
                Thread
              </a>
              <button className="btn small" onClick={() => remove(p)}>
                Delete
              </button>
            </div>
          </li>
        ))}
        {posts.length === 0 && <li className="muted">No posts yet.</li>}
      </ul>
    </main>
  );
}
