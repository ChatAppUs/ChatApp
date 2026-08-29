"use client";

import { useCallback, useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { api, getAccessToken } from "@/lib/api";
import { useI18n } from "@/lib/i18n";
import type { Post } from "@/lib/types";
import Composer from "@/components/Composer";
import PostCard from "@/components/PostCard";
import StoryBar from "@/components/StoryBar";

export default function FeedPage() {
  const { t } = useI18n();
  const router = useRouter();
  const [posts, setPosts] = useState<Post[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  const load = useCallback(async () => {
    try {
      const data = await api<{ posts: Post[] }>("/api/feed");
      setPosts(data.posts);
      setError("");
    } catch (e) {
      setError(e instanceof Error ? e.message : t("error"));
    } finally {
      setLoading(false);
    }
  }, [t]);

  useEffect(() => {
    if (!getAccessToken()) {
      router.push("/login");
      return;
    }
    load();
  }, [load, router]);

  return (
    <>
      <StoryBar />
      <Composer onPosted={load} />
      {loading && <div className="card muted">{t("loading")}</div>}
      {error && <div className="card error-text">{error}</div>}
      {!loading && posts.length === 0 && !error && (
        <div className="card muted">{t("noResults")}</div>
      )}
      {posts.map((p) => (
        <PostCard key={p.id} post={p} onChanged={load} />
      ))}
    </>
  );
}
