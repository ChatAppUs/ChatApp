"use client";

import { useEffect, useState } from "react";
import { api } from "@/lib/api";
import PostCard from "@/components/PostCard";
import type { Post, TrendingTag } from "@/lib/types";

export default function TrendingPage() {
  const [tags, setTags] = useState<TrendingTag[]>([]);
  const [activeTag, setActiveTag] = useState<string | null>(null);
  const [posts, setPosts] = useState<Post[]>([]);

  useEffect(() => {
    api<{ trending: TrendingTag[] }>("/api/hashtags/trending")
      .then((d) => setTags(d.trending))
      .catch(() => {});
  }, []);

  const loadTag = (tag: string) => {
    setActiveTag(tag);
    api<{ posts: Post[] }>(`/api/hashtags/${encodeURIComponent(tag)}/posts`)
      .then((d) => setPosts(d.posts))
      .catch(() => {});
  };

  return (
    <div className="col">
      <div className="card">
        <h2 style={{ marginTop: 0 }}>Trending</h2>
        <div className="row" style={{ flexWrap: "wrap" }}>
          {tags.map((t) => (
            <button
              key={t.tag}
              className={activeTag === t.tag ? "small" : "secondary small"}
              onClick={() => loadTag(t.tag)}
            >
              #{t.tag} · {t.count}
            </button>
          ))}
          {tags.length === 0 && <span className="muted">No trending hashtags yet — post with #tags!</span>}
        </div>
      </div>
      {activeTag && <h3>#{activeTag}</h3>}
      {posts.map((p) => (
        <PostCard key={p.id} post={p} onChanged={() => activeTag && loadTag(activeTag)} />
      ))}
    </div>
  );
}
