"use client";

import { useCallback, useEffect, useState } from "react";
import { useParams, useRouter } from "next/navigation";
import { api, getAccessToken } from "@/lib/api";
import { useI18n } from "@/lib/i18n";
import PostCard from "@/components/PostCard";
import type { Post } from "@/lib/types";

interface MomentDetail {
  id: string;
  title: string;
  summary: string;
  cover_url: string;
  published_at: string;
}

export default function MomentDetailPage() {
  const { t } = useI18n();
  const router = useRouter();
  const params = useParams<{ id: string }>();
  const [moment, setMoment] = useState<MomentDetail | null>(null);
  const [posts, setPosts] = useState<Post[]>([]);
  const [error, setError] = useState("");

  const load = useCallback(async () => {
    try {
      const d = await api<{ moment: MomentDetail; posts: Post[] }>(
        `/api/moments/${params.id}`
      );
      setMoment(d.moment);
      setPosts(d.posts);
    } catch (e) {
      setError(e instanceof Error ? e.message : t("error"));
    }
  }, [params.id, t]);

  useEffect(() => {
    if (!getAccessToken()) {
      router.push("/login");
      return;
    }
    load();
  }, [load, router]);

  if (error) return <div className="card error-text">{error}</div>;
  if (!moment) return <div className="card muted">{t("loading")}</div>;

  return (
    <div className="col">
      <div className="card">
        {moment.cover_url && (
          // eslint-disable-next-line @next/next/no-img-element
          <img src={moment.cover_url} alt="" style={{ width: "100%", borderRadius: 8, maxHeight: 260, objectFit: "cover" }} />
        )}
        <h2 style={{ marginBottom: 4 }}>{moment.title}</h2>
        {moment.summary && <p className="muted" style={{ marginTop: 0 }}>{moment.summary}</p>}
      </div>
      {posts.map((p) => (
        <PostCard key={p.id} post={p} onChanged={load} />
      ))}
    </div>
  );
}
