"use client";

import { useCallback, useEffect, useState } from "react";
import { useParams } from "next/navigation";
import { api, getAccessToken } from "@/lib/api";

// Pulse thread view — master plan §32. Shows the root post and every nested
// reply with X-style ordering modes (chronological / relevant).

type PulsePost = {
  id: string;
  author: string;
  body: string;
  topics: string[] | null;
  reply_count: number;
  repost_count: number;
  quote_count: number;
  created_at: string;
};

type ThreadResponse = {
  root: PulsePost;
  posts: PulsePost[];
  chronological: PulsePost[];
  relevant: PulsePost[];
};

export default function PulseThreadPage() {
  const params = useParams<{ id: string }>();
  const id = params?.id as string;
  const [authed, setAuthed] = useState(false);
  const [thread, setThread] = useState<ThreadResponse | null>(null);
  const [mode, setMode] = useState<"chronological" | "relevant">("chronological");
  const [reply, setReply] = useState("");
  const [error, setError] = useState("");

  const load = useCallback(async () => {
    if (!id) return;
    try {
      const r = await api<ThreadResponse>(`/api/pulse/posts/${id}/thread`);
      setThread(r);
    } catch (e) {
      setError(String(e));
    }
  }, [id]);

  useEffect(() => {
    setAuthed(!!getAccessToken());
    if (getAccessToken()) void load();
  }, [load]);

  const send = async () => {
    if (!reply.trim()) return;
    try {
      await api("/api/pulse/posts", {
        method: "POST",
        body: JSON.stringify({ body: reply, parent_id: id }),
      });
      setReply("");
      await load();
    } catch (e) {
      setError(String(e));
    }
  };

  if (!authed) {
    return (
      <main className="container">
        <h1>Thread</h1>
        <p>Sign in to read this thread.</p>
        <a className="btn" href="/login">
          Sign in
        </a>
      </main>
    );
  }

  const shown =
    mode === "relevant" ? thread?.relevant ?? [] : thread?.chronological ?? [];

  return (
    <main className="container">
      <h1>Thread</h1>
      <a className="link" href="/pulse">
        ← back to Pulse
      </a>
      {error && <p className="error">{error}</p>}

      {thread?.root && (
        <section className="card">
          <strong>{thread.root.author}</strong>{" "}
          <span className="muted">{new Date(thread.root.created_at).toLocaleString()}</span>
          <p>{thread.root.body}</p>
          <div className="muted">
            {thread.root.reply_count} replies · {thread.root.repost_count} reposts ·{" "}
            {thread.root.quote_count} quotes
          </div>
        </section>
      )}

      <div className="row">
        <button
          className={mode === "chronological" ? "btn" : "btn ghost"}
          onClick={() => setMode("chronological")}
        >
          Chronological
        </button>
        <button
          className={mode === "relevant" ? "btn" : "btn ghost"}
          onClick={() => setMode("relevant")}
        >
          Most relevant
        </button>
      </div>

      <ul className="list">
        {shown
          .filter((p) => p.id !== thread?.root?.id)
          .map((p) => (
            <li key={p.id} className="card">
              <strong>{p.author}</strong>{" "}
              <span className="muted">{new Date(p.created_at).toLocaleString()}</span>
              <p>{p.body}</p>
            </li>
          ))}
        {shown.length <= 1 && <li className="muted">No replies yet.</li>}
      </ul>

      <section className="card">
        <textarea
          placeholder="Reply to this thread"
          value={reply}
          onChange={(e) => setReply(e.target.value)}
        />
        <button className="btn" onClick={send} disabled={!reply.trim()}>
          Reply
        </button>
      </section>
    </main>
  );
}
