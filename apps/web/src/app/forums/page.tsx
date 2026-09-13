"use client";

import { useCallback, useEffect, useState } from "react";
import { api, getAccessToken } from "@/lib/api";

// Forums — master plan §30 / master documentation §75 item 24.
// Communities of topics with threaded posts and moderator pin/lock.

type Forum = {
  id: string;
  title: string;
  slug: string;
  description: string;
  visibility: string;
  topic_count: number;
  post_count: number;
};

type Topic = {
  id: string;
  title: string;
  body: string;
  pinned: boolean;
  locked: boolean;
  post_count: number;
  author: string;
};

type Post = { id: string; body: string; author: string; created_at: string };

export default function ForumsPage() {
  const [authed, setAuthed] = useState(false);
  const [forums, setForums] = useState<Forum[]>([]);
  const [active, setActive] = useState<Forum | null>(null);
  const [topics, setTopics] = useState<Topic[]>([]);
  const [posts, setPosts] = useState<Post[]>([]);
  const [openTopic, setOpenTopic] = useState<Topic | null>(null);
  const [error, setError] = useState("");
  const [form, setForm] = useState({ title: "", slug: "", description: "" });
  const [topicForm, setTopicForm] = useState({ title: "", body: "" });
  const [reply, setReply] = useState("");

  const loadForums = useCallback(async () => {
    try {
      const r = await api<{ forums: Forum[] }>("/api/forums");
      setForums(r.forums ?? []);
    } catch (e) {
      setError(String(e));
    }
  }, []);

  useEffect(() => {
    setAuthed(!!getAccessToken());
    if (getAccessToken()) void loadForums();
  }, [loadForums]);

  const createForum = async () => {
    setError("");
    try {
      await api("/api/forums", { method: "POST", body: JSON.stringify(form) });
      setForm({ title: "", slug: "", description: "" });
      await loadForums();
    } catch (e) {
      setError(String(e));
    }
  };

  const openForum = async (f: Forum) => {
    setActive(f);
    setOpenTopic(null);
    try {
      const r = await api<{ topics: Topic[] }>(`/api/forums/${f.id}/topics`);
      setTopics(r.topics ?? []);
    } catch (e) {
      setError(String(e));
    }
  };

  const createTopic = async () => {
    if (!active) return;
    setError("");
    try {
      await api(`/api/forums/${active.id}/topics`, {
        method: "POST",
        body: JSON.stringify(topicForm),
      });
      setTopicForm({ title: "", body: "" });
      await openForum(active);
    } catch (e) {
      setError(String(e));
    }
  };

  const openThread = async (t: Topic) => {
    setOpenTopic(t);
    try {
      const r = await api<{ posts: Post[] }>(`/api/forums/topics/${t.id}/posts`);
      setPosts(r.posts ?? []);
    } catch (e) {
      setError(String(e));
    }
  };

  const sendReply = async () => {
    if (!openTopic || !reply.trim()) return;
    try {
      await api(`/api/forums/topics/${openTopic.id}/posts`, {
        method: "POST",
        body: JSON.stringify({ body: reply }),
      });
      setReply("");
      await openThread(openTopic);
    } catch (e) {
      setError(String(e));
    }
  };

  const toggleFlag = async (t: Topic, kind: "pin" | "lock") => {
    const next = kind === "pin" ? !t.pinned : !t.locked;
    try {
      await api(`/api/forums/topics/${t.id}/${kind}`, {
        method: "PUT",
        body: JSON.stringify({ value: next }),
      });
      if (active) await openForum(active);
    } catch (e) {
      setError(String(e));
    }
  };

  if (!authed) {
    return (
      <main className="container">
        <h1>Forums</h1>
        <p>Sign in to browse and join forums.</p>
        <a className="btn" href="/login">Sign in</a>
      </main>
    );
  }

  return (
    <main className="container">
      <h1>Forums</h1>
      {error && <p className="error">{error}</p>}

      <section className="card">
        <h2>Create a forum</h2>
        <input
          placeholder="Title"
          value={form.title}
          onChange={(e) => setForm({ ...form, title: e.target.value })}
        />
        <input
          placeholder="slug (lowercase-with-hyphens)"
          value={form.slug}
          onChange={(e) => setForm({ ...form, slug: e.target.value })}
        />
        <textarea
          placeholder="Description"
          value={form.description}
          onChange={(e) => setForm({ ...form, description: e.target.value })}
        />
        <button className="btn" onClick={createForum} disabled={!form.title || !form.slug}>
          Create
        </button>
      </section>

      <section className="card">
        <h2>All forums ({forums.length})</h2>
        <ul className="list">
          {forums.map((f) => (
            <li key={f.id}>
              <button className="link" onClick={() => openForum(f)}>
                <strong>{f.title}</strong>
              </button>{" "}
              <span className="muted">
                {f.visibility} · {f.topic_count} topics · {f.post_count} posts
              </span>
              <div className="muted">{f.description}</div>
            </li>
          ))}
          {forums.length === 0 && <li className="muted">No forums yet.</li>}
        </ul>
      </section>

      {active && (
        <section className="card">
          <h2>{active.title} — topics</h2>
          <input
            placeholder="New topic title"
            value={topicForm.title}
            onChange={(e) => setTopicForm({ ...topicForm, title: e.target.value })}
          />
          <textarea
            placeholder="Opening post"
            value={topicForm.body}
            onChange={(e) => setTopicForm({ ...topicForm, body: e.target.value })}
          />
          <button className="btn" onClick={createTopic} disabled={!topicForm.title}>
            Open topic
          </button>
          <ul className="list">
            {topics.map((t) => (
              <li key={t.id}>
                {t.pinned && <span title="pinned">📌 </span>}
                {t.locked && <span title="locked">🔒 </span>}
                <button className="link" onClick={() => openThread(t)}>
                  {t.title}
                </button>{" "}
                <span className="muted">
                  {t.post_count} replies · by {t.author}
                </span>
                <button className="btn small" onClick={() => toggleFlag(t, "pin")}>
                  {t.pinned ? "Unpin" : "Pin"}
                </button>
                <button className="btn small" onClick={() => toggleFlag(t, "lock")}>
                  {t.locked ? "Unlock" : "Lock"}
                </button>
              </li>
            ))}
            {topics.length === 0 && <li className="muted">No topics yet.</li>}
          </ul>
        </section>
      )}

      {openTopic && (
        <section className="card">
          <h2>{openTopic.title}</h2>
          <p>{openTopic.body}</p>
          <ul className="list">
            {posts.map((p) => (
              <li key={p.id}>
                <strong>{p.author}</strong>: {p.body}
              </li>
            ))}
            {posts.length === 0 && <li className="muted">No replies yet.</li>}
          </ul>
          <textarea
            placeholder="Write a reply"
            value={reply}
            onChange={(e) => setReply(e.target.value)}
            disabled={openTopic.locked}
          />
          <button className="btn" onClick={sendReply} disabled={openTopic.locked || !reply.trim()}>
            Reply
          </button>
        </section>
      )}
    </main>
  );
}
