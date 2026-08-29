"use client";

import { useCallback, useEffect, useState } from "react";
import { api } from "@/lib/api";
import PostCard from "@/components/PostCard";
import type { Post } from "@/lib/types";

export default function BookmarksPage() {
  const [posts, setPosts] = useState<Post[]>([]);

  const load = useCallback(() => {
    api<{ posts: Post[] }>("/api/bookmarks")
      .then((d) => setPosts(d.posts))
      .catch(() => {});
  }, []);

  useEffect(load, [load]);

  return (
    <div className="col">
      <h2>Saved posts</h2>
      {posts.map((p) => (
        <PostCard key={p.id} post={p} onChanged={load} />
      ))}
      {posts.length === 0 && <div className="card muted">Nothing saved yet. Tap ☆ on any post.</div>}
    </div>
  );
}
