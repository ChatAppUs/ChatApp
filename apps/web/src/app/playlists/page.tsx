"use client";

import { FormEvent, useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { api, getAccessToken } from "@/lib/api";
import PostCard from "@/components/PostCard";
import type { Post } from "@/lib/types";

type Playlist = {
  id: string;
  title: string;
  item_count: number;
};

export default function PlaylistsPage() {
  const router = useRouter();
  const [playlists, setPlaylists] = useState<Playlist[]>([]);
  const [selected, setSelected] = useState<Playlist | null>(null);
  const [posts, setPosts] = useState<Post[]>([]);
  const [title, setTitle] = useState("");
  const [postId, setPostId] = useState("");
  const [error, setError] = useState("");
  const [status, setStatus] = useState("");

  const loadList = () =>
    api<{ playlists: Playlist[] }>("/api/me/playlists")
      .then((d) => setPlaylists(d.playlists))
      .catch((e) => setError(e instanceof Error ? e.message : "failed to load playlists"));

  useEffect(() => {
    if (!getAccessToken()) {
      router.push("/login");
      return;
    }
    loadList();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const create = async (e: FormEvent) => {
    e.preventDefault();
    setError("");
    setStatus("");
    try {
      const d = await api<{ id: string }>("/api/me/playlists", {
        method: "POST",
        body: JSON.stringify({ title }),
      });
      setStatus(`Playlist "${title}" created.`);
      setTitle("");
      void d;
      loadList();
    } catch (e) {
      setError(e instanceof Error ? e.message : "failed to create playlist");
    }
  };

  const remove = async (p: Playlist) => {
    setError("");
    try {
      await api(`/api/me/playlists/${p.id}`, { method: "DELETE" });
      if (selected?.id === p.id) setSelected(null);
      loadList();
    } catch (e) {
      setError(e instanceof Error ? e.message : "delete failed");
    }
  };

  const open = async (p: Playlist) => {
    setSelected(p);
    setPostId("");
    setError("");
    try {
      const d = await api<{ id: string; title: string; posts: Post[] }>(`/api/playlists/${p.id}`);
      setPosts(d.posts ?? []);
    } catch (e) {
      setError(e instanceof Error ? e.message : "failed to load playlist");
    }
  };

  const addItem = async (e: FormEvent) => {
    e.preventDefault();
    if (!selected) return;
    setError("");
    setStatus("");
    try {
      await api(`/api/playlists/${selected.id}/items`, {
        method: "POST",
        body: JSON.stringify({ post_id: postId.trim() }),
      });
      setStatus("Post added to the playlist.");
      setPostId("");
      open(selected);
    } catch (e) {
      setError(e instanceof Error ? e.message : "only posts you authored can be added");
    }
  };

  const removeItem = async (id: string) => {
    if (!selected) return;
    setError("");
    try {
      await api(`/api/playlists/${selected.id}/items/${id}`, { method: "DELETE" });
      open(selected);
    } catch (e) {
      setError(e instanceof Error ? e.message : "remove failed");
    }
  };

  return (
    <div className="col">
      <div className="card col" style={{ gap: 8 }}>
        <h2 style={{ margin: 0 }}>🎞️ Playlists</h2>
        <div className="muted">Curate collections of your own posts — reels, photos and text — to feature on your profile.</div>
        {error && <div className="error-text">{error}</div>}
        {status && <div className="muted" style={{ color: "var(--accent-2)" }}>{status}</div>}
        <form onSubmit={create} className="row" style={{ gap: 6 }}>
          <input value={title} onChange={(e) => setTitle(e.target.value)} placeholder="New playlist title" maxLength={80} style={{ flex: 1 }} />
          <button type="submit" disabled={title.trim().length === 0}>Create</button>
        </form>
        {playlists.length === 0 && !error && <div className="muted">No playlists yet — create one above.</div>}
        {playlists.map((p) => (
          <div key={p.id} className="row" style={{ gap: 6 }}>
            <button className={selected?.id === p.id ? "small" : "secondary small"} style={{ flex: 1, textAlign: "left" }} onClick={() => open(p)}>
              {p.title} <span className="muted">· {p.item_count}</span>
            </button>
            <button className="secondary small" onClick={() => remove(p)}>🗑</button>
          </div>
        ))}
      </div>
      {selected && (
        <>
          <div className="card col" style={{ gap: 6 }}>
            <div className="row">
              <h3 style={{ margin: 0 }}>{selected.title}</h3>
            </div>
            <form onSubmit={addItem} className="row" style={{ gap: 6 }}>
              <input value={postId} onChange={(e) => setPostId(e.target.value)} placeholder="Add your own post by ID (UUID)" style={{ flex: 1 }} />
              <button type="submit" className="secondary small" disabled={postId.trim().length === 0}>Add</button>
            </form>
          </div>
          {posts.length === 0 && <div className="card muted">No posts in this playlist yet.</div>}
          {posts.map((p) => (
            <div key={p.id} className="col" style={{ gap: 4 }}>
              <PostCard post={p} onChanged={() => open(selected)} />
              <div className="row" style={{ justifyContent: "flex-end" }}>
                <button className="secondary small" onClick={() => removeItem(p.id)}>Remove from playlist</button>
              </div>
            </div>
          ))}
        </>
      )}
    </div>
  );
}