"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { useRouter } from "next/navigation";
import { api, getAccessToken, getUserId } from "@/lib/api";
import { useI18n } from "@/lib/i18n";
import type { Post } from "@/lib/types";
import Composer from "@/components/Composer";
import { RemixModal, ReelAnalytics, RemixList, RemixPlayer } from "@/components/ReelExtras";
import CommunityNotes from "@/components/CommunityNotes";

function Reel({ post }: { post: Post }) {
  const { t } = useI18n();
  const videoRef = useRef<HTMLVideoElement>(null);
  const viewed = useRef(false);
  const SPEEDS = [0.5, 1, 1.5, 2];
  const [liked, setLiked] = useState(post.liked_by_me);
  const [likes, setLikes] = useState(post.like_count);
  const [remixOpen, setRemixOpen] = useState(false);
  const [showAnalytics, setShowAnalytics] = useState(false);
  const [showRemixes, setShowRemixes] = useState(false);
  const [speedIdx, setSpeedIdx] = useState(1);

  const cycleSpeed = () => {
    const next = (speedIdx + 1) % SPEEDS.length;
    setSpeedIdx(next);
    const el = videoRef.current;
    if (el) el.playbackRate = SPEEDS[next];
  };

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
  const isLayoutRemix = !!post.remix_mode && !!post.remix_of && !!video;
  const tapToggle = () => {
    const el = videoRef.current;
    if (el) el.paused ? el.play() : el.pause();
  };
  return (
    <div className="reel">
      {isLayoutRemix ? (
        <RemixPlayer post={post} videoRef={videoRef} onTap={tapToggle} />
      ) : video ? (
        <video ref={videoRef} src={video.url} loop playsInline controls={false} onClick={() => {
          const el = videoRef.current;
          if (el) el.paused ? el.play() : el.pause();
        }} preload="auto" />
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
            {post.remix_mode && (
              <div className="muted" style={{ fontSize: 11 }}>
                🎬 {t(post.remix_mode)}
              </div>
            )}
            {post.body && video && <p style={{ margin: "6px 0 0" }}>{post.body}</p>}
          </div>
          <div className="spacer" />
          <button className={liked ? "small" : "secondary small"} onClick={toggleLike}>
            {t("like")} · {likes}
          </button>
          <button className="secondary small" title="Remix" onClick={() => setRemixOpen(true)}>🎬</button>
          <button className="secondary small" title="Remixes" onClick={() => setShowRemixes((v) => !v)}>⑂</button>
          {video && (
            <button className="secondary small" title="Playback speed"
              onClick={cycleSpeed}>{SPEEDS[speedIdx]}x</button>
          )}
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

  // Prefetch the next few video payloads so swiping feels instant.
  useEffect(() => {
    const links: HTMLLinkElement[] = [];
    reels.slice(0, 3).forEach((r) => {
      const video = r.media.find((m) => m.kind === "video");
      if (!video) return;
      const link = document.createElement("link");
      link.rel = "preload";
      link.as = "video";
      link.href = video.url;
      document.head.appendChild(link);
      links.push(link);
    });
    return () => links.forEach((l) => document.head.removeChild(l));
  }, [reels]);

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
