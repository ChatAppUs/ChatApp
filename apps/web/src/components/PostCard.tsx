"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { api, getUserId } from "@/lib/api";
import { useI18n } from "@/lib/i18n";
import type { Post, Comment, PollOption, Conversation, Album } from "@/lib/types";
import { REACTIONS } from "@/lib/types";
import CommunityNotes from "@/components/CommunityNotes";
import EmojiText from "@/components/EmojiText";

export default function PostCard({ post, onChanged }: { post: Post; onChanged?: () => void }) {
  const { t } = useI18n();
  const [myReaction, setMyReaction] = useState(post.my_reaction || "");
  const [likeCount, setLikeCount] = useState(post.like_count);
  const [pickerOpen, setPickerOpen] = useState(false);
  const [showComments, setShowComments] = useState(false);
  const [comments, setComments] = useState<Comment[]>([]);
  const [commentSort, setCommentSort] = useState("old");
  const [commentCount, setCommentCount] = useState(post.comment_count);
  const [draft, setDraft] = useState("");
  const [bookmarked, setBookmarked] = useState(false);
  const [poll, setPoll] = useState<PollOption[]>([]);
  const [pollTotal, setPollTotal] = useState(0);
  const [replyTo, setReplyTo] = useState<string | null>(null);
  const [shareOpen, setShareOpen] = useState(false);
  const [shareConvs, setShareConvs] = useState<Conversation[]>([]);
  const [shareSent, setShareSent] = useState(false);
  const [edits, setEdits] = useState<{ old_body: string; edited_at: string }[] | null>(null);
  const [pinned, setPinned] = useState(false);
  const [albumOpen, setAlbumOpen] = useState(false);
  const [albums, setAlbums] = useState<Album[]>([]);

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

  const react = async (reaction: string) => {
    setPickerOpen(false);
    try {
      if (myReaction === reaction) {
        await api(`/api/posts/${post.id}/react`, { method: "DELETE" });
        setMyReaction("");
        setLikeCount((c) => Math.max(0, c - 1));
        return;
      }
      await api(`/api/posts/${post.id}/react`, {
        method: "PUT",
        body: JSON.stringify({ reaction }),
      });
      if (!myReaction) setLikeCount((c) => c + 1);
      setMyReaction(reaction);
    } catch {
      /* keep UI state on network error */
    }
  };

  const togglePin = async () => {
    const method = pinned ? "DELETE" : "PUT";
    const body = pinned ? undefined : JSON.stringify({ post_id: post.id });
    await api(`/api/me/pinned-post`, { method, body }).catch(() => {});
    setPinned(!pinned);
    onChanged?.();
  };

  const loadEditHistory = async () => {
    if (edits) {
      setEdits(null);
      return;
    }
    const d = await api<{ edits: { old_body: string; edited_at: string }[] }>(
      `/api/posts/${post.id}/edits`).catch(() => null);
    setEdits(d?.edits ?? []);
  };

  const openAlbumPicker = async () => {
    if (!albumOpen) {
      const d = await api<{ albums: Album[] }>(`/api/albums`).catch(() => null);
      if (d) setAlbums(d.albums);
    }
    setAlbumOpen(!albumOpen);
  };

  const addToAlbum = async (albumId: string) => {
    await api(`/api/albums/${albumId}/items`, {
      method: "POST",
      body: JSON.stringify({ post_id: post.id }),
    }).catch(() => {});
    setAlbumOpen(false);
  };

  const loadComments = async (sort?: string) => {
    const s = sort ?? commentSort;
    if (sort) setCommentSort(sort);
    const data = await api<{ comments: Comment[] }>(
      `/api/posts/${post.id}/comments?sort=${s}`).catch(() => null);
    if (data) setComments(data.comments);
    if (!showComments && !sort) setShowComments(true);
    else if (!sort) setShowComments(false);
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
            {post.feeling && <> · feeling {post.feeling}</>}
            {post.location && <> · 📍 {post.location}</>}
          </div>
          {post.tagged_usernames && post.tagged_usernames.length > 0 && (
            <div className="muted" style={{ fontSize: 12 }}>
              with {post.tagged_usernames.map((u) => `@${u}`).join(", ")}
            </div>
          )}
          {post.publish_at && new Date(post.publish_at) > new Date() && (
            <div className="badge" style={{ fontSize: 11 }}>
              🕒 scheduled {new Date(post.publish_at).toLocaleString()}
            </div>
          )}
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
        post.body && <p style={{ whiteSpace: "pre-wrap" }}><EmojiText text={post.body} /></p>
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
      {post.edited_at && (
        <button className="secondary small" style={{ padding: 0, fontSize: 11 }} onClick={loadEditHistory}>
          (edited)
        </button>
      )}
      {edits && (
        <div className="card" style={{ marginTop: 8, padding: 8 }}>
          <strong style={{ fontSize: 12 }}>Edit history</strong>
          {edits.length === 0 && <div className="muted" style={{ fontSize: 12 }}>No previous versions.</div>}
          {edits.map((e, i) => (
            <div key={i} style={{ fontSize: 12, marginTop: 4 }}>
              <span className="muted">{new Date(e.edited_at).toLocaleString()}</span>
              <div style={{ textDecoration: "line-through", opacity: 0.7 }}>{e.old_body}</div>
            </div>
          ))}
        </div>
      )}
      <div className="row" style={{ marginTop: 10, flexWrap: "wrap" }}>
        <span style={{ position: "relative" }}>
          <button className={myReaction ? "small" : "secondary small"}
            onClick={() => (myReaction ? react(myReaction) : setPickerOpen((v) => !v))}
            onContextMenu={(e) => { e.preventDefault(); setPickerOpen((v) => !v); }}>
            {myReaction ? REACTIONS[myReaction] : "👍"} {likeCount}
          </button>
          {pickerOpen && (
            <span className="card" style={{
              position: "absolute", bottom: "110%", insetInlineStart: 0, padding: 4,
              display: "flex", gap: 2, zIndex: 5, whiteSpace: "nowrap",
            }}>
              {Object.entries(REACTIONS).map(([name, emoji]) => (
                <button key={name} className={myReaction === name ? "small" : "secondary small"}
                  style={{ padding: "2px 6px", fontSize: 16 }} title={name}
                  onClick={() => react(name)}>
                  {emoji}
                </button>
              ))}
            </span>
          )}
        </span>
        <button className="secondary small" onClick={() => loadComments()}>
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
          <>
            <button className="secondary small" onClick={() => { setEditText(post.body); setEditing(true); }}>
              ✏️
            </button>
            <button className={pinned ? "small" : "secondary small"} title="Pin to profile" onClick={togglePin}>
              📌
            </button>
            {post.media.length > 0 && (
              <button className="secondary small" title="Add to album" onClick={openAlbumPicker}>
                🖼️
              </button>
            )}
          </>
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
      {albumOpen && (
        <div className="card" style={{ marginTop: 8, padding: 8 }}>
          <strong style={{ fontSize: 13 }}>Add to album…</strong>
          <div className="col" style={{ maxHeight: 140, overflowY: "auto" }}>
            {albums.length === 0 && <span className="muted" style={{ fontSize: 12 }}>No albums yet — create one on your profile.</span>}
            {albums.map((a) => (
              <button key={a.id} className="secondary small" style={{ textAlign: "start" }}
                onClick={() => addToAlbum(a.id)}>
                🖼️ {a.title} ({a.item_count})
              </button>
            ))}
          </div>
        </div>
      )}
      {showComments && (
        <div className="col" style={{ marginTop: 10 }}>
          <div className="row" style={{ gap: 6 }}>
            {(["top", "new", "old"] as const).map((s) => (
              <button key={s} className={commentSort === s ? "small" : "secondary small"}
                style={{ padding: "0 8px", fontSize: 11 }} onClick={() => loadComments(s)}>
                {s === "top" ? "Top" : s === "new" ? "Newest" : "Oldest"}
              </button>
            ))}
          </div>
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
      <CommunityNotes postId={post.id} />
    </div>
  );
}
