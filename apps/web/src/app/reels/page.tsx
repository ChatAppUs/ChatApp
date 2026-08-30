"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { useRouter } from "next/navigation";
import { api, getAccessToken, getUserId } from "@/lib/api";
import { useI18n } from "@/lib/i18n";
import type { Post } from "@/lib/types";
import Composer from "@/components/Composer";
import { RemixModal, ReelAnalytics, RemixList } from "@/components/ReelExtras";
import CommunityNotes from "@/components/CommunityNotes";

function Reel({ post }: { post: Post }) {
  const { t } = useI18n();
  const videoRef = useRef<HTMLVideoElement>(null);
  const viewed = useRef(false);
  const [liked, setLiked] = useState(post.liked_by_me);
  const [likes, setLikes] = useState(post.like_count);
  const [remixOpen, setRemixOpen] = useState(false);
  const [showAnalytics, setShowAnalytics] = useState(false);
  const [showRemixes, setShowRemixes] = useState(false);

  useEffect(() => {
    const el = videoRef.current;
    if (!el) return;
    const observer = new IntersectionObserver(
      ([entry]) => {
        if (entry.isIntersecting) {
          el.play().catch(() => {});
          if (!viewed.current) {
            viewed.current = true;
            api(`/api/posts/${post.id}/view`, { method: "POST" }).catch(() => {});
          }
        } else {
          el.pause();
        }
      },
      { threshold: 0.6 }
    );
    observer.observe(el);
    return () => observer.disconnect();
  }, [post.id]);

  const toggleLike = async () => {
    try {
      if (liked) {
        await api(`/api/posts/${post.id}/like`, { method: "DELETE" });
        setLiked(false);
        setLikes((c) => Math.max(0, c - 1));
      } else {
        await api(`/api/posts/${post.id}/like`, { method: "POST" });
        setLiked(true);
        setLikes((c) => c + 1);
      }
    } catch { /* ignore */ }
  };

  const video = post.media.find((m) => m.kind === "video");
  return (
    <div className="reel">
      {video ? (
        <video ref={videoRef} src={video.url} loop playsInline controls={false} onClick={() => {
          const el = videoRef.current;
          if (el) el.paused ? el.play() : el.pause();
        }} />
      ) : post.media[0] ? (
        <img src={post.media[0].url} alt="" style={{ width: "100%" }} />
      ) : (
        <div style={{ padding: 40 }}>{post.body}</div>
      )}
      <div className="overlay">
        <div className="row">
          <div>
            <strong>{post.author_name}</strong>
            <div className="muted">@{post.author_username}</div>
            {post.body && video && <p style={{ margin: "6px 0 0" }}>{post.body}</p>}
          </div>
          <div className="spacer" />
          <button className={liked ? "small" : "secondary small"} onClick={toggleLike}>
            {t("like")} · {likes}
          </button>
          <button className="secondary small" title="Remix" onClick={() => setRemixOpen(true)}>🎬</button>
          <button className="secondary small" title="Remixes" onClick={() => setShowRemixes((v) => !v)}>⑂</button>
          {post.author_id === getUserId() && (
            <button className="secondary small" title="Analytics"
              onClick={() => setShowAnalytics((v) => !v)}>📊</button>
          )}
        </div>
        {showAnalytics && <ReelAnalytics reelId={post.id} />}
        {showRemixes && <RemixList reelId={post.id} />}
        <CommunityNotes postId={post.id} />
        {remixOpen && <RemixModal reelId={post.id} onClose={() => setRemixOpen(false)} />}
      </div>
    </div>
  );
}

export default function ReelsPage() {
  const { t } = useI18n();
  const router = useRouter();
  const [reels, setReels] = useState<Post[]>([]);
  const [loading, setLoading] = useState(true);

  const load = useCallback(() => {
    api<{ reels: Post[] }>("/api/reels")
      .then((d) => setReels(d.reels))
      .catch(() => {})
      .finally(() => setLoading(false));
  }, []);

  useEffect(() => {
    if (!getAccessToken()) {
      router.push("/login");
      return;
    }
    load();
  }, [load, router]);

  return (
    <>
      <Composer type="reel" onPosted={load} />
      {loading && <div className="card muted">{t("loading")}</div>}
      {!loading && reels.length === 0 && <div className="card muted">{t("noResults")}</div>}
      {reels.map((r) => (
        <Reel key={r.id} post={r} />
      ))}
    </>
  );
}
