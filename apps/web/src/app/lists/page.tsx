"use client";

import { FormEvent, useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { api, getAccessToken } from "@/lib/api";
import PostCard from "@/components/PostCard";
import type { Post } from "@/lib/types";

type List = {
  id: string;
  name: string;
  member_count: number;
};

export default function ListsPage() {
  const router = useRouter();
  const [lists, setLists] = useState<List[]>([]);
  const [selected, setSelected] = useState<List | null>(null);
  const [posts, setPosts] = useState<Post[]>([]);
  const [name, setName] = useState("");
  const [error, setError] = useState("");
  const [status, setStatus] = useState("");

  const loadLists = () =>
    api<{ lists: List[] }>("/api/me/lists")
      .then((d) => setLists(d.lists))
      .catch((e) => setError(e instanceof Error ? e.message : "failed to load lists"));

  useEffect(() => {
    if (!getAccessToken()) {
      router.push("/login");
      return;
    }
    loadLists();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const create = async (e: FormEvent) => {
    e.preventDefault();
    setError("");
    setStatus("");
    try {
      const d = await api<{ id: string }>("/api/me/lists", {
        method: "POST",
        body: JSON.stringify({ name }),
      });
      setStatus(`List "${name}" created.`);
      setName("");
      void d;
      loadLists();
    } catch (e) {
      setError(e instanceof Error ? e.message : "failed to create list");
    }
  };

  const remove = async (l: List) => {
    setError("");
    try {
      await api(`/api/me/lists/${l.id}`, { method: "DELETE" });
      if (selected?.id === l.id) setSelected(null);
      loadLists();
    } catch (e) {
      setError(e instanceof Error ? e.message : "delete failed");
    }
  };

  const open = async (l: List) => {
    setSelected(l);
    setError("");
    try {
      const d = await api<{ posts: Post[] }>(`/api/lists/${l.id}/feed`);
      setPosts(d.posts);
    } catch (e) {
      setError(e instanceof Error ? e.message : "failed to load list feed");
    }
  };

  return (
    <div className="col">
      <div className="card col" style={{ gap: 8 }}>
        <h2 style={{ margin: 0 }}>📚 Lists</h2>
        <div className="muted">Group people into private lists to filter their latest posts into one feed (e.g., “Close friends”, “News”).</div>
        {error && <div className="error-text">{error}</div>}
        {status && <div className="muted" style={{ color: "var(--accent-2)" }}>{status}</div>}
        <form onSubmit={create} className="row" style={{ gap: 6 }}>
          <input value={name} onChange={(e) => setName(e.target.value)} placeholder="New list name" maxLength={60} style={{ flex: 1 }} />
          <button type="submit" disabled={name.trim().length === 0}>Create</button>
        </form>
        {lists.length === 0 && !error && <div className="muted">No lists yet — create one above.</div>}
        {lists.map((l) => (
          <div key={l.id} className="row" style={{ gap: 6 }}>
            <button className={selected?.id === l.id ? "small" : "secondary small"} style={{ flex: 1, textAlign: "left" }} onClick={() => open(l)}>
              {l.name} <span className="muted">· {l.member_count}</span>
            </button>
            <button className="secondary small" onClick={() => remove(l)}>🗑</button>
          </div>
        ))}
      </div>
      {selected && (
        <>
          <h3 style={{ margin: "12px 0 0" }}>Feed — {selected.name}</h3>
          {posts.length === 0 && <div className="card muted">No posts from list members yet.</div>}
          {posts.map((p) => <PostCard key={p.id} post={p} onChanged={() => open(selected)} />)}
        </>
      )}
    </div>
  );
}