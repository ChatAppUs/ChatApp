"use client";

import { useCallback, useEffect, useState } from "react";
import { useParams } from "next/navigation";
import { useI18n } from "@/lib/i18n";
import { followPage, pageFeed, unfollowPage } from "@/lib/features";
import PostCard from "@/components/PostCard";
import type { Post } from "@/lib/types";

export default function PageDetailPage() {
  const { t } = useI18n();
  const params = useParams<{ id: string }>();
  const pageId = params.id;
  const [posts, setPosts] = useState<Post[]>([]);
  const [following, setFollowing] = useState(false);
  const [error, setError] = useState("");

  const load = useCallback(async () => {
    try {
      const data = await pageFeed(pageId, 20, 0);
      setPosts(data.posts as Post[]);
    } catch (e) {
      setError(e instanceof Error ? e.message : "load failed");
    }
  }, [pageId]);

  useEffect(() => { load(); }, [load]);

  const toggle = async () => {
    try {
      if (following) {
        await unfollowPage(pageId);
      } else {
        await followPage(pageId);
      }
      setFollowing(!following);
    } catch (e) {
      setError(e instanceof Error ? e.message : "follow failed");
    }
  };

  return (
    <div className="col">
      <h1>{t("pages")}</h1>
      {error && <div className="error">{error}</div>}
      <button onClick={toggle}>{following ? t("unfollow") : t("follow")}</button>
      {posts.map((p) => <PostCard key={p.id} post={p} />)}
      {posts.length === 0 && <p className="muted">{t("loading")}</p>}
    </div>
  );
}
