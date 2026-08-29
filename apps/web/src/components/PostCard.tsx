"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { api, getUserId } from "@/lib/api";
import { useI18n } from "@/lib/i18n";
import type { Post, Comment, PollOption, Conversation } from "@/lib/types";

export default function PostCard({ post, onChanged }: { post: Post; onChanged?: () => void }) {
  const { t } = useI18n();
  const [liked, setLiked] = useState(post.liked_by_me);
  const [likeCount, setLikeCount] = useState(post.like_count);
  const [showComments, setShowComments] = useState(false);
  const [comments, setComments] = useState<Comment[]>([]);
  const [commentCount, setCommentCount] = useState(post.comment_count);
  const [draft, setDraft] = useState("");
  const [bookmarked, setBookmarked] = useState(false);
  const [poll, setPoll] = useState<PollOption[]>([]);
  const [pollTotal, setPollTotal] = useState(0);
  const [replyTo, setReplyTo] = useState<string | null>(null);
  const [shareOpen, setShareOpen] = useState(false);
  const [shareConvs, setShareConvs] = useState<Conversation[]>([]);
  const [shareSent, setShareSent] = useState(false);

  useEffect(() => {
    api<{ options: PollOption[]; total_votes: number }>(`/api/posts/${post.id}/poll`)
      .then((d) => {
        if (d.options.length > 0) {
          setPoll(d.options);
          setPollTotal(d.total_votes);
        }
      })
      .catch(() => {});
  }, [post.id]);

  const vote = async (optionId: string) => {
    await api(`/api/posts/${post.id}/vote`, {
      method: "POST",
      body: JSON.stringify({ option_id: optionId }),
    }).catch(() => {});
    const d = await api<{ options: PollOption[]; total_votes: number }>(`/api/posts/${post.id}/poll`);
    setPoll(d.options);
    setPollTotal(d.total_votes);
  };

  const [reposted, setReposted] = useState(false);
  const [shareCount, setShareCount] = useState(post.share_count);
  const [quoting, setQuoting] = useState(false);
  const [quoteText, setQuoteText] = useState("");
  const [editing, setEditing] = useState(false);
  const [editText, setEditText] = useState(post.body);
  const isMine = getUserId() === post.author_id;

  const repost = async () => {
    if (reposted) {
      await api(`/api/posts/${post.id}/repost`, { method: "DELETE" }).catch(() => {});
      setReposted(false);
      setShareCount((c) => Math.max(0, c - 1));
      return;
    }
    await api(`/api/posts/${post.id}/repost`, { method: "POST", body: "{}" }).catch(() => {});
    setReposted(true);
    setShareCount((c) => c + 1);
    onChanged?.();
  };

  const quoteRepost = async () => {
    if (!quoteText.trim()) return;
    await api(`/api/posts/${post.id}/repost`, {
      method: "POST",
      body: JSON.stringify({ quote: quoteText }),
    }).catch(() => {});
    setQuoting(false);
    setQuoteText("");
    setReposted(true);
    setShareCount((c) => c + 1);
    onChanged?.();
  };

  const saveEdit = async () => {
    if (!editText.trim()) return;
    await api(`/api/posts/${post.id}`, {
      method: "PATCH",
      body: JSON.stringify({ body: editText }),
    }).catch(() => {});
    setEditing(false);
    onChanged?.();
  };

  const toggleBookmark = async () => {
    if (bookmarked) {
      await api(`/api/posts/${post.id}/bookmark`, { method: "DELETE" }).catch(() => {});
      setBookmarked(false);
    } else {
      await api(`/api/posts/${post.id}/bookmark`, { method: "POST", body: "{}" }).catch(() => {});
      setBookmarked(true);
    }
  };

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
      body: JSON.stringify({ body: draft, parent_id: replyTo ?? "" }),
    });
    setDraft("");
    setReplyTo(null);
    setCommentCount((c) => c + 1);
    const data = await api<{ comments: Comment[] }>(`/api/posts/${post.id}/comments`);
    setComments(data.comments);
    onChanged?.();
  };

  const toggleCommentLike = async (c: Comment) => {
    const method = c.liked_by_me ? "DELETE" : "POST";
    await api(`/api/comments/${c.id}/like`, { method, body: "{}" }).catch(() => {});
    setComments((prev) => prev.map((x) => x.id === c.id
      ? { ...x, liked_by_me: !x.liked_by_me, like_count: x.like_count + (x.liked_by_me ? -1 : 1) }
      : x));
  };

  const shareToChat = async (convId: string) => {
    await api(`/api/posts/${post.id}/share`, {
      method: "POST",
      body: JSON.stringify({ conversation_id: convId }),
    }).catch(() => {});
    setShareOpen(false);
    setShareSent(true);
    setTimeout(() => setShareSent(false), 2000);
  };

  const openShare = async () => {
    if (!shareOpen && shareConvs.length === 0) {
      const d = await api<{ conversations: Conversation[] }>("/api/conversations").catch(() => null);
      if (d) setShareConvs(d.conversations);
    }
    setShareOpen(!shareOpen);
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
      {editing ? (
        <div className="col" style={{ marginTop: 8 }}>
          <textarea value={editText} onChange={(e) => setEditText(e.target.value)} />
          <div className="row">
            <button className="small" onClick={saveEdit}>{t("save")}</button>
            <button className="secondary small" onClick={() => setEditing(false)}>{t("cancel")}</button>
          </div>
        </div>
      ) : (
        post.body && <p style={{ whiteSpace: "pre-wrap" }}>{post.body}</p>
      )}
      {poll.length > 0 && (
        <div className="col" style={{ marginTop: 8 }}>
          {poll.map((o) => {
            const pct = pollTotal > 0 ? Math.round((o.votes / pollTotal) * 100) : 0;
            return (
              <button
                key={o.id}
                className={o.voted_by_me ? "small" : "secondary small"}
                style={{ textAlign: "start", position: "relative", overflow: "hidden" }}
                onClick={() => vote(o.id)}
              >
                <span style={{
                  position: "absolute", inset: 0, width: `${pct}%`,
                  background: "rgba(79,124,255,0.25)",
                }} />
                <span style={{ position: "relative" }}>{o.label} — {pct}% ({o.votes})</span>
              </button>
            );
          })}
          <span className="muted">{pollTotal} votes</span>
        </div>
      )}
      {post.media.map((m, i) =>
        m.kind === "video" ? (
          <video key={i} className="post-media" src={m.url} controls playsInline />
        ) : m.kind === "audio" ? (
          <audio key={i} src={m.url} controls style={{ width: "100%", marginTop: 8 }} />
        ) : (
          <img key={i} className="post-media" src={m.url} alt="" />
        )
      )}
      {post.quoted && (
        <div className="card" style={{ marginTop: 8, padding: 10, borderStyle: "dashed" }}>
          <div className="muted" style={{ fontSize: 12 }}>
            {post.quoted.author_name} · @{post.quoted.author_username}
          </div>
          <p style={{ margin: "4px 0 0", whiteSpace: "pre-wrap" }}>{post.quoted.body}</p>
        </div>
      )}
      {post.edited_at && <div className="muted" style={{ fontSize: 11 }}>(edited)</div>}
      <div className="row" style={{ marginTop: 10 }}>
        <button className={liked ? "small" : "secondary small"} onClick={toggleLike}>
          {t("like")} · {likeCount}
        </button>
        <button className="secondary small" onClick={loadComments}>
          {t("comments")} · {commentCount}
        </button>
        <button className={reposted ? "small" : "secondary small"} onClick={repost}>
          🔁 {t("share")} · {shareCount}
        </button>
        <button className="secondary small" onClick={() => setQuoting((v) => !v)}>
          💬 Quote
        </button>
        <button className="secondary small" title="Send to chat" onClick={openShare}>
          📤
        </button>
        {isMine && !editing && (
          <button className="secondary small" onClick={() => { setEditText(post.body); setEditing(true); }}>
            ✏️
          </button>
        )}
        <button className={bookmarked ? "small" : "secondary small"} onClick={toggleBookmark}>
          {bookmarked ? "★" : "☆"}
        </button>
      </div>
      {quoting && (
        <div className="col" style={{ marginTop: 8 }}>
          <textarea
            value={quoteText}
            onChange={(e) => setQuoteText(e.target.value)}
            placeholder="Add a comment…"
            rows={2}
          />
          <div className="row">
            <button className="small" onClick={quoteRepost}>Quote post</button>
            <button className="secondary small" onClick={() => setQuoting(false)}>{t("cancel")}</button>
          </div>
        </div>
      )}
      {shareOpen && (
        <div className="card" style={{ marginTop: 8, padding: 8 }}>
          <div className="row">
            <strong style={{ fontSize: 13 }}>Send to chat…</strong>
            <div className="spacer" />
            <button className="secondary small" onClick={() => setShareOpen(false)}>✕</button>
          </div>
          <div className="col" style={{ maxHeight: 160, overflowY: "auto" }}>
            {shareConvs.map((c) => (
              <button key={c.id} className="secondary small" style={{ textAlign: "start" }}
                onClick={() => shareToChat(c.id)}>
                {c.title || (c.is_channel ? "📢 Channel" : c.is_group ? "👥 Group" : "DM")}
              </button>
            ))}
          </div>
        </div>
      )}
      {shareSent && <div className="muted" style={{ fontSize: 12, marginTop: 4 }}>Sent to chat ✓</div>}
      {showComments && (
        <div className="col" style={{ marginTop: 10 }}>
          {comments.map((c) => (
            <div key={c.id} className="row" style={c.parent_id ? { marginInlineStart: 28 } : undefined}>
              <div className="avatar sm" />
              <div>
                <strong style={{ fontSize: 13 }}>{c.author_name}</strong>{" "}
                <span style={{ fontSize: 14 }}>{c.body}</span>
                <div className="row" style={{ gap: 8 }}>
                  <span className="muted">{new Date(c.created_at).toLocaleString()}</span>
                  <button className="secondary small" style={{ padding: "0 6px", fontSize: 11 }}
                    onClick={() => toggleCommentLike(c)}>
                    {c.liked_by_me ? "❤️" : "🤍"} {c.like_count > 0 ? c.like_count : ""}
                  </button>
                  <button className="secondary small" style={{ padding: "0 6px", fontSize: 11 }}
                    onClick={() => { setReplyTo(c.id); setDraft(`@${c.author_username} `); }}>
                    Reply
                  </button>
                </div>
              </div>
            </div>
          ))}
          {replyTo && (
            <div className="row" style={{ fontSize: 12 }}>
              <span className="badge">replying</span>
              <button className="secondary small" onClick={() => setReplyTo(null)}>✕</button>
            </div>
          )}
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
