"use client";

import { useState } from "react";
import Link from "next/link";
import { api } from "@/lib/api";
import { useI18n } from "@/lib/i18n";
import type { Post, Comment } from "@/lib/types";

export default function PostCard({ post, onChanged }: { post: Post; onChanged?: () => void }) {
  const { t } = useI18n();
  const [liked, setLiked] = useState(post.liked_by_me);
  const [likeCount, setLikeCount] = useState(post.like_count);
  const [showComments, setShowComments] = useState(false);
  const [comments, setComments] = useState<Comment[]>([]);
  const [commentCount, setCommentCount] = useState(post.comment_count);
  const [draft, setDraft] = useState("");

  const toggleLike = async () => {
    try {
      if (liked) {
        await api(`/api/posts/${post.id}/like`, { method: "DELETE" });
        setLiked(false);
        setLikeCount((c) => Math.max(0, c - 1));
      } else {
        await api(`/api/posts/${post.id}/like`, { method: "POST" });
        setLiked(true);
        setLikeCount((c) => c + 1);
      }
    } catch {
      /* keep UI state on network error */
    }
  };

  const loadComments = async () => {
    if (!showComments) {
      const data = await api<{ comments: Comment[] }>(`/api/posts/${post.id}/comments`);
      setComments(data.comments);
    }
    setShowComments(!showComments);
  };

  const addComment = async () => {
    if (!draft.trim()) return;
    await api(`/api/posts/${post.id}/comments`, {
      method: "POST",
      body: JSON.stringify({ body: draft }),
    });
    setDraft("");
    setCommentCount((c) => c + 1);
    const data = await api<{ comments: Comment[] }>(`/api/posts/${post.id}/comments`);
    setComments(data.comments);
    onChanged?.();
  };

  return (
    <div className="card">
      <div className="row">
        {post.author_avatar ? (
          <img className="avatar" src={post.author_avatar} alt="" />
        ) : (
          <div className="avatar" />
        )}
        <div>
          <Link href={`/profile/${post.author_id}`}>
            <strong>{post.author_name}</strong>
          </Link>
          <div className="muted">
            @{post.author_username} · {new Date(post.created_at).toLocaleString()}
            {post.type !== "post" && <> · <span className="badge">{post.type}</span></>}
          </div>
        </div>
      </div>
      {post.body && <p style={{ whiteSpace: "pre-wrap" }}>{post.body}</p>}
      {post.media.map((m, i) =>
        m.kind === "video" ? (
          <video key={i} className="post-media" src={m.url} controls playsInline />
        ) : m.kind === "audio" ? (
          <audio key={i} src={m.url} controls style={{ width: "100%", marginTop: 8 }} />
        ) : (
          <img key={i} className="post-media" src={m.url} alt="" />
        )
      )}
      <div className="row" style={{ marginTop: 10 }}>
        <button className={liked ? "small" : "secondary small"} onClick={toggleLike}>
          {t("like")} · {likeCount}
        </button>
        <button className="secondary small" onClick={loadComments}>
          {t("comments")} · {commentCount}
        </button>
      </div>
      {showComments && (
        <div className="col" style={{ marginTop: 10 }}>
          {comments.map((c) => (
            <div key={c.id} className="row">
              <div className="avatar sm" />
              <div>
                <strong style={{ fontSize: 13 }}>{c.author_name}</strong>{" "}
                <span style={{ fontSize: 14 }}>{c.body}</span>
                <div className="muted">{new Date(c.created_at).toLocaleString()}</div>
              </div>
            </div>
          ))}
          <div className="row">
            <input
              value={draft}
              onChange={(e) => setDraft(e.target.value)}
              placeholder={t("writeComment")}
              onKeyDown={(e) => e.key === "Enter" && addComment()}
            />
            <button className="small" onClick={addComment}>{t("send")}</button>
          </div>
        </div>
      )}
    </div>
  );
}
