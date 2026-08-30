"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { useI18n } from "@/lib/i18n";
import { fypFeed, sendWatchSignal, type FypPost } from "@/lib/features";
import { api } from "@/lib/api";

// Tracks playback of one reel and reports real watch signals: watch_ms,
// completion, rewatch. Sent when the element leaves the viewport or unmounts
// so signals survive rapid scrolling.
function FypReel({ post }: { post: FypPost }) {
  const { t } = useI18n();
  const videoRef = useRef<HTMLVideoElement>(null);
  const [liked, setLiked] = useState(false);
  const [likes, setLikes] = useState(post.like_count);
  const state = useRef({ startedAt: 0, watchedMs: 0, plays: 0, durationMs: 0, reported: false });

  const report = useCallback(() => {
    const s = state.current;
    if (s.reported || s.watchedMs < 300) return; // ignore glances
    s.reported = true;
    const completed = s.durationMs > 0 && s.watchedMs >= s.durationMs * 0.9;
    sendWatchSignal(post.id, {
      watched_ms: Math.round(s.watchedMs),
      duration_ms: Math.round(s.durationMs),
      completed,
      rewatched: s.plays > 1,
    }).catch(() => {});
  }, [post.id]);

  useEffect(() => {
    const el = videoRef.current;
    if (!el) return;
    const tick = () => {
      const s = state.current;
      if (!el.paused) {
        s.durationMs = el.duration > 0 ? el.duration * 1000 : s.durationMs;
      }
    };
    const onEnded = () => {
      state.current.plays += 1;
      report();
    };
    const observer = new IntersectionObserver(
      ([entry]) => {
        const s = state.current;
        if (entry.isIntersecting) {
          el.play().catch(() => {});
          if (s.startedAt === 0) s.startedAt = performance.now();
        } else {
          el.pause();
          if (s.startedAt > 0) {
            s.watchedMs += performance.now() - s.startedAt;
            s.startedAt = 0;
          }
        }
      },
      { threshold: 0.6 }
    );
    observer.observe(el);
    const interval = window.setInterval(tick, 1000);
    el.addEventListener("ended", onEnded);
    return () => {
      observer.disconnect();
      window.clearInterval(interval);
      el.removeEventListener("ended", onEnded);
      const s = state.current;
      if (s.startedAt > 0) {
        s.watchedMs += performance.now() - s.startedAt;
        s.startedAt = 0;
      }
      report();
    };
  }, [report]);

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

  const notInterested = () => {
    state.current.reported = true;
    sendWatchSignal(post.id, {
      watched_ms: Math.round(state.current.watchedMs),
      duration_ms: Math.round(state.current.durationMs),
      completed: false,
      rewatched: false,
      not_interested: true,
    }).catch(() => {});
  };

  return (
    <div className="reel">
      {post.media_url ? (
        <video ref={videoRef} src={post.media_url} loop muted playsInline />
      ) : (
        <div className="card"><p>{post.body}</p></div>
      )}
      <div className="reel-overlay">
        <div className="col" style={{ gap: 4 }}>
          <div className="row">
            <strong>@{post.username}</strong>
            <span className="badge">{post.view_count} views</span>
          </div>
          {post.media_url && <p>{post.body}</p>}
          <div className="row">
            <button className="secondary" onClick={toggleLike}>
              {liked ? "❤" : "♡"} {likes}
            </button>
            <button className="secondary" onClick={notInterested}>
              {t("notInterested")}
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}

export default function FypPage() {
  const { t } = useI18n();
  const [posts, setPosts] = useState<FypPost[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  useEffect(() => {
    (async () => {
      try {
        const data = await fypFeed(20, 0);
        setPosts(data.posts);
      } catch (e) {
        setError(e instanceof Error ? e.message : "feed failed");
      } finally {
        setLoading(false);
      }
    })();
  }, []);

  if (loading) return <div className="muted">{t("loading")}</div>;
  if (error) return <div className="error">{error}</div>;

  return (
    <div className="col">
      <h1>{t("forYou")}</h1>
      {posts.map((p) => (
        <FypReel key={p.id} post={p} />
      ))}
    </div>
  );
}
