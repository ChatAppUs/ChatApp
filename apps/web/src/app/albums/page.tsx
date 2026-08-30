"use client";

import { useCallback, useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { api, getAccessToken } from "@/lib/api";
import PostCard from "@/components/PostCard";
import type { Album, Post } from "@/lib/types";

export default function AlbumsPage() {
  const router = useRouter();
  const [albums, setAlbums] = useState<Album[]>([]);
  const [open, setOpen] = useState<Album | null>(null);
  const [posts, setPosts] = useState<Post[]>([]);
  const [title, setTitle] = useState("");
  const [desc, setDesc] = useState("");
  const [err, setErr] = useState("");

  const load = useCallback(() => {
    api<{ albums: Album[] }>("/api/albums").then((d) => setAlbums(d.albums)).catch(() => {});
  }, []);

  useEffect(() => {
    if (!getAccessToken()) {
      router.push("/login");
      return;
    }
    load();
  }, [load, router]);

  const create = async () => {
    if (!title.trim()) return;
    setErr("");
    try {
      await api("/api/albums", {
        method: "POST",
        body: JSON.stringify({ title, description: desc }),
      });
      setTitle(""); setDesc("");
      load();
    } catch (e) {
      setErr(e instanceof Error ? e.message : "failed to create album");
    }
  };

  const openAlbum = async (a: Album) => {
    const d = await api<{ posts: Post[] }>(`/api/albums/${a.id}`).catch(() => null);
    if (d) {
      setPosts(d.posts);
      setOpen(a);
    }
  };

  const remove = async (a: Album) => {
    if (!confirm(`Delete album "${a.title}"?`)) return;
    await api(`/api/albums/${a.id}`, { method: "DELETE" }).catch(() => {});
    if (open?.id === a.id) setOpen(null);
    load();
  };

  const removeItem = async (postId: string) => {
    if (!open) return;
    await api(`/api/albums/${open.id}/items/${postId}`, { method: "DELETE" }).catch(() => {});
    setPosts((p) => p.filter((x) => x.id !== postId));
    load();
  };

  return (
    <main className="col">
      <h2>🖼️ Albums</h2>
      <div className="card">
        <div className="row">
          <input placeholder="Album title" value={title} maxLength={120} onChange={(e) => setTitle(e.target.value)} />
          <input placeholder="Description (optional)" value={desc} maxLength={500} onChange={(e) => setDesc(e.target.value)} />
          <button onClick={create} disabled={!title.trim()}>Create</button>
        </div>
      </div>
      {err && <div className="error-text">{err}</div>}

      <div className="row" style={{ flexWrap: "wrap", gap: 10 }}>
        {albums.map((a) => (
          <div className="card" key={a.id} style={{ width: 180, cursor: "pointer" }} onClick={() => openAlbum(a)}>
            {a.cover_url
              ? <img src={a.cover_url} alt="" style={{ width: "100%", borderRadius: 8, aspectRatio: "1", objectFit: "cover" }} />
              : <div style={{ width: "100%", aspectRatio: "1", borderRadius: 8, background: "var(--border)", display: "grid", placeItems: "center", fontSize: 32 }}>🖼️</div>}
            <strong style={{ fontSize: 13 }}>{a.title}</strong>
            <span className="muted" style={{ fontSize: 12 }}>{a.item_count} items</span>
            <button className="secondary small" onClick={(e) => { e.stopPropagation(); remove(a); }}>Delete</button>
          </div>
        ))}
        {albums.length === 0 && <span className="muted">No albums yet. Add media posts from the feed with the 🖼️ button.</span>}
      </div>

      {open && (
        <>
          <div className="row">
            <h3>{open.title}</h3>
            <div className="spacer" />
            <button className="secondary small" onClick={() => setOpen(null)}>✕ Close</button>
          </div>
          {open.description && <p className="muted">{open.description}</p>}
          {posts.map((p) => (
            <div key={p.id} style={{ position: "relative" }}>
              <PostCard post={p} />
              <button className="secondary small" style={{ position: "absolute", top: 8, insetInlineEnd: 8 }}
                onClick={() => removeItem(p.id)}>
                Remove
              </button>
            </div>
          ))}
          {posts.length === 0 && <span className="muted">Album is empty.</span>}
        </>
      )}
    </main>
  );
}
