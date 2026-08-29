"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { useRouter } from "next/navigation";
import { api, getAccessToken, getUserId, uploadMedia, wsURL } from "@/lib/api";
import { useI18n } from "@/lib/i18n";
import {
  publishIdentityKey, hasIdentityKey, encryptFor, decryptFrom, looksEncrypted,
} from "@/lib/e2e";
import type { Conversation, Message, PublicUser } from "@/lib/types";

interface WSEvent {
  type: string;
  conversation_id?: string;
  id?: string;
  message_id?: string;
  sender_id?: string;
  user_id?: string;
  body?: string;
  media_url?: string;
  is_encrypted?: boolean;
  reply_to?: string;
  emoji?: string;
  action?: string;
  created_at?: string;
  at?: string;
}

export default function ChatPage() {
  const { t } = useI18n();
  const router = useRouter();
  const [conversations, setConversations] = useState<Conversation[]>([]);
  const [active, setActive] = useState<Conversation | null>(null);
  const [messages, setMessages] = useState<Message[]>([]);
  const [draft, setDraft] = useState("");
  const [search, setSearch] = useState("");
  const [hits, setHits] = useState<PublicUser[]>([]);
  const [typingUsers, setTypingUsers] = useState<Set<string>>(new Set());
  const [peerId, setPeerId] = useState<string | null>(null);
  const [peerOnline, setPeerOnline] = useState<boolean | null>(null);
  const [e2eReady, setE2eReady] = useState(false);
  const [e2eOn, setE2eOn] = useState(false);
  const [editingId, setEditingId] = useState<string | null>(null);
  const [reads, setReads] = useState<Record<string, string>>({});
  const [pins, setPins] = useState<Message[]>([]);
  const [forwardingId, setForwardingId] = useState<string | null>(null);
  const [recording, setRecording] = useState(false);
  const [scheduled, setScheduled] = useState<{ id: string; body: string; send_at: string }[]>([]);
  const [scheduleAt, setScheduleAt] = useState("");
  const [msgQuery, setMsgQuery] = useState("");
  const [msgHits, setMsgHits] = useState<Message[] | null>(null);
  const [groupOpen, setGroupOpen] = useState(false);
  const [groupTitle, setGroupTitle] = useState("");
  const [groupMembers, setGroupMembers] = useState<string[]>([]);
  const recorderRef = useRef<MediaRecorder | null>(null);
  const chunksRef = useRef<Blob[]>([]);
  const wsRef = useRef<WebSocket | null>(null);
  const activeRef = useRef<Conversation | null>(null);
  const peerRef = useRef<string | null>(null);
  const e2eRef = useRef(false);
  const bottomRef = useRef<HTMLDivElement>(null);
  const typingTimer = useRef<ReturnType<typeof setTimeout> | null>(null);

  const loadConversations = useCallback(() => {
    api<{ conversations: Conversation[] }>("/api/conversations")
      .then((d) => setConversations(d.conversations))
      .catch(() => {});
  }, []);

  const decryptMessage = useCallback(async (m: Message): Promise<Message> => {
    if (m.is_encrypted && looksEncrypted(m.body)) {
      const peer = m.sender_id === getUserId() ? peerRef.current : m.sender_id;
      if (peer) {
        const body = await decryptFrom(peer, m.body);
        return { ...m, body };
      }
    }
    return m;
  }, []);

  useEffect(() => {
    if (!getAccessToken()) {
      router.push("/login");
      return;
    }
    loadConversations();
    hasIdentityKey().then(async (has) => {
      if (!has) await publishIdentityKey().catch(() => {});
      setE2eReady(true);
    });
    const ws = new WebSocket(wsURL());
    wsRef.current = ws;
    ws.onmessage = (ev) => {
      let data: WSEvent;
      try {
        data = JSON.parse(ev.data as string);
      } catch {
        return;
      }
      const conv = activeRef.current;
      if (data.type === "message") {
        loadConversations();
        if (conv && data.conversation_id === conv.id) {
          const m: Message = {
            id: data.id!, sender_id: data.sender_id!, sender_name: "",
            body: data.body ?? "", media_url: data.media_url ?? "",
            is_encrypted: data.is_encrypted, reply_to: data.reply_to,
            created_at: data.created_at ?? new Date().toISOString(),
            reactions: {},
          };
          decryptMessage(m).then((dm) => setMessages((prev) => [...prev, dm]));
          if (data.sender_id !== getUserId()) markRead(conv.id);
        }
      } else if (data.type === "typing" && conv && data.conversation_id === conv.id) {
        const uid = data.user_id!;
        setTypingUsers((prev) => new Set(prev).add(uid));
        setTimeout(() => setTypingUsers((prev) => {
          const next = new Set(prev);
          next.delete(uid);
          return next;
        }), 3000);
      } else if (data.type === "message_edited" && conv && data.conversation_id === conv.id) {
        setMessages((prev) => prev.map((m) => (m.id === data.id ? { ...m, body: data.body ?? m.body } : m)));
      } else if (data.type === "message_deleted" && conv && data.conversation_id === conv.id) {
        setMessages((prev) => prev.filter((m) => m.id !== data.id));
      } else if (data.type === "reaction" && conv && data.conversation_id === conv.id) {
        setMessages((prev) => prev.map((m) => {
          if (m.id !== data.message_id) return m;
          const reactions = { ...(m.reactions ?? {}) };
          const emoji = data.emoji!;
          if (data.action === "add") reactions[emoji] = (reactions[emoji] ?? 0) + 1;
          else if (reactions[emoji]) {
            reactions[emoji] -= 1;
            if (reactions[emoji] <= 0) delete reactions[emoji];
          }
          return { ...m, reactions };
        }));
      } else if (data.type === "read" && conv && data.conversation_id === conv.id) {
        setReads((prev) => ({ ...prev, [data.user_id!]: data.at as string ?? new Date().toISOString() }));
      }
    };
    return () => ws.close();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [loadConversations, router]);

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: "smooth" });
  }, [messages]);

  const markRead = (convId: string) => {
    api(`/api/conversations/${convId}/read`, { method: "POST", body: "{}" }).catch(() => {});
  };

  const loadReads = (convId: string) => {
    api<{ reads: { user_id: string; at: string }[] }>(`/api/conversations/${convId}/reads`)
      .then((d) => {
        const map: Record<string, string> = {};
        d.reads.forEach((r) => (map[r.user_id] = r.at));
        setReads(map);
      })
      .catch(() => {});
  };

  const loadPins = (convId: string) => {
    api<{ pins: Message[] }>(`/api/conversations/${convId}/pins`)
      .then((d) => setPins(d.pins))
      .catch(() => setPins([]));
  };

  const togglePin = async (m: Message) => {
    if (!active) return;
    if (m.pinned) {
      await api(`/api/conversations/${active.id}/pins/${m.id}`, { method: "DELETE" }).catch(() => {});
    } else {
      await api(`/api/conversations/${active.id}/pins/${m.id}`, { method: "POST", body: "{}" }).catch(() => {});
    }
    setMessages((prev) => prev.map((x) => (x.id === m.id ? { ...x, pinned: !m.pinned } : x)));
    loadPins(active.id);
  };

  const forwardMessage = async (targetConvId: string) => {
    if (!forwardingId) return;
    await api(`/api/messages/${forwardingId}/forward`, {
      method: "POST",
      body: JSON.stringify({ conversation_id: targetConvId }),
    }).catch(() => {});
    setForwardingId(null);
  };

  const openSavedMessages = async () => {
    const d = await api<{ conversation_id: string }>("/api/conversations/saved", {
      method: "POST", body: "{}",
    }).catch(() => null);
    if (!d) return;
    loadConversations();
    openConversation({
      id: d.conversation_id, is_group: false, is_channel: false,
      title: "Saved Messages", created_at: "", last_message: null, unread: 0,
    });
  };

  const loadScheduled = (convId: string) => {
    api<{ scheduled: { id: string; body: string; send_at: string }[] }>(
      `/api/conversations/${convId}/scheduled`
    ).then((d) => setScheduled(d.scheduled)).catch(() => setScheduled([]));
  };

  const scheduleMessage = async () => {
    if (!active || !draft.trim() || !scheduleAt) return;
    await api(`/api/conversations/${active.id}/schedule`, {
      method: "POST",
      body: JSON.stringify({ body: draft, send_at: new Date(scheduleAt).toISOString() }),
    }).catch(() => {});
    setDraft("");
    setScheduleAt("");
    saveDraft(active.id, "");
    loadScheduled(active.id);
  };

  const cancelScheduled = async (id: string) => {
    await api(`/api/scheduled/${id}`, { method: "DELETE" }).catch(() => {});
    if (active) loadScheduled(active.id);
  };

  const draftKey = (convId: string) => `chatapp.draft.${convId}`;
  const saveDraft = (convId: string, text: string) => {
    try {
      if (text) localStorage.setItem(draftKey(convId), text);
      else localStorage.removeItem(draftKey(convId));
    } catch { /* storage unavailable */ }
  };

  const toggleRecording = async () => {
    if (recording) {
      recorderRef.current?.stop();
      return;
    }
    try {
      const stream = await navigator.mediaDevices.getUserMedia({ audio: true });
      const rec = new MediaRecorder(stream);
      chunksRef.current = [];
      rec.ondataavailable = (e) => e.data.size > 0 && chunksRef.current.push(e.data);
      rec.onstop = async () => {
        stream.getTracks().forEach((tr) => tr.stop());
        setRecording(false);
        const blob = new Blob(chunksRef.current, { type: rec.mimeType || "audio/webm" });
        if (blob.size === 0 || !activeRef.current) return;
        const ext = rec.mimeType.includes("ogg") ? "ogg" : "webm";
        const file = new File([blob], `voice-${Date.now()}.${ext}`, { type: blob.type });
        try {
          const url = await uploadMedia(file);
          wsRef.current?.send(JSON.stringify({
            type: "message", conversation_id: activeRef.current.id, body: "", media_url: url,
          }));
        } catch { /* upload failed */ }
      };
      recorderRef.current = rec;
      rec.start();
      setRecording(true);
    } catch { /* mic permission denied */ }
  };

  const searchMessages = async (q: string) => {
    setMsgQuery(q);
    if (!active || q.trim().length < 2) {
      setMsgHits(null);
      return;
    }
    const d = await api<{ messages: Message[] }>(
      `/api/conversations/${active.id}/search?q=${encodeURIComponent(q)}`
    ).catch(() => null);
    setMsgHits(d ? d.messages : []);
  };

  const createGroup = async () => {
    if (!groupTitle.trim() || groupMembers.length === 0) return;
    const d = await api<{ id: string }>("/api/conversations", {
      method: "POST",
      body: JSON.stringify({ is_group: true, title: groupTitle, member_ids: groupMembers }),
    }).catch(() => null);
    setGroupOpen(false);
    setGroupTitle("");
    setGroupMembers([]);
    if (d) {
      loadConversations();
      openConversation({
        id: d.id, is_group: true, is_channel: false,
        title: groupTitle, created_at: "", last_message: null, unread: 0,
      });
    }
  };

  const toggleGroupMember = (id: string) => {
    setGroupMembers((prev) =>
      prev.includes(id) ? prev.filter((x) => x !== id) : [...prev, id]);
  };

  const openConversation = async (c: Conversation) => {
    setActive(c);
    activeRef.current = c;
    setMsgQuery("");
    setMsgHits(null);
    setMessages([]);
    setTypingUsers(new Set());
    const data = await api<{ messages: Message[] }>(`/api/conversations/${c.id}/messages`);
    const ordered = data.messages.reverse();
    const decrypted = await Promise.all(ordered.map(decryptMessage));
    setMessages(decrypted);
    markRead(c.id);
    loadReads(c.id);
    loadPins(c.id);
    loadScheduled(c.id);
    try {
      setDraft(localStorage.getItem(draftKey(c.id)) ?? "");
    } catch {
      setDraft("");
    }
    // For DMs, find the peer for E2EE + presence.
    if (!c.is_group && !c.is_channel) {
      const other = ordered.find((m) => m.sender_id !== getUserId())?.sender_id ?? null;
      setPeerId(other);
      peerRef.current = other;
      if (other) {
        api<{ online: boolean }>(`/api/users/${other}/presence`)
          .then((p) => setPeerOnline(p.online))
          .catch(() => setPeerOnline(null));
      }
    } else {
      setPeerId(null);
      peerRef.current = null;
      setPeerOnline(null);
    }
  };

  const send = async () => {
    if (!draft.trim() || !active || wsRef.current?.readyState !== WebSocket.OPEN) return;
    let body = draft;
    let isEncrypted = false;
    if (e2eRef.current && peerRef.current) {
      try {
        body = await encryptFor(peerRef.current, draft);
        isEncrypted = true;
      } catch {
        /* peer has no key; send plaintext */
      }
    }
    wsRef.current.send(JSON.stringify({
      type: "message", conversation_id: active.id, body, is_encrypted: isEncrypted,
    }));
    setDraft("");
    saveDraft(active.id, "");
  };

  const onDraftChange = (v: string) => {
    setDraft(v);
    if (active) saveDraft(active.id, v);
    if (active && wsRef.current?.readyState === WebSocket.OPEN) {
      if (typingTimer.current) clearTimeout(typingTimer.current);
      typingTimer.current = setTimeout(() => {
        wsRef.current?.send(JSON.stringify({ type: "typing", conversation_id: active.id }));
      }, 400);
    }
  };

  const saveEdit = async () => {
    if (!editingId || !draft.trim()) return;
    await api(`/api/messages/${editingId}/edit`, {
      method: "POST",
      body: JSON.stringify({ body: draft }),
    }).catch(() => {});
    setMessages((prev) => prev.map((m) => (m.id === editingId ? { ...m, body: draft } : m)));
    setEditingId(null);
    setDraft("");
  };

  const deleteMessage = async (id: string) => {
    await api(`/api/messages/${id}`, { method: "DELETE" }).catch(() => {});
    setMessages((prev) => prev.filter((m) => m.id !== id));
  };

  const react = async (id: string, emoji: string) => {
    await api(`/api/messages/${id}/reactions`, {
      method: "POST",
      body: JSON.stringify({ emoji }),
    }).catch(() => {});
    setMessages((prev) => prev.map((m) => {
      if (m.id !== id) return m;
      const reactions = { ...(m.reactions ?? {}) };
      reactions[emoji] = (reactions[emoji] ?? 0) + 1;
      return { ...m, reactions };
    }));
  };

  const searchUsers = async (q: string) => {
    setSearch(q);
    if (q.trim().length < 2) {
      setHits([]);
      return;
    }
    const data = await api<{ users: PublicUser[] }>(`/api/users/search?q=${encodeURIComponent(q)}`);
    setHits(data.users.filter((u) => u.id !== getUserId()));
  };

  const startChat = async (userId: string) => {
    const data = await api<{ id: string }>("/api/conversations", {
      method: "POST",
      body: JSON.stringify({ is_group: false, member_ids: [userId] }),
    });
    setHits([]);
    setSearch("");
    loadConversations();
    openConversation({
      id: data.id, is_group: false, is_channel: false, title: "", created_at: "",
      last_message: null, unread: 0,
    });
    setPeerId(userId);
    peerRef.current = userId;
  };

  const myLastRead = (m: Message): boolean => {
    if (m.sender_id !== getUserId()) return false;
    return Object.entries(reads).some(([uid, at]) => uid !== getUserId() && at >= m.created_at);
  };

  return (
    <div className="chat-layout">
      <div className="card chat-list" style={{ marginBottom: 0 }}>
        <input placeholder={t("searchUsers")} value={search} onChange={(e) => searchUsers(e.target.value)} />
        <div className="chat-item row" onClick={openSavedMessages} role="button" tabIndex={0}>
          <div className="avatar sm" style={{ display: "grid", placeItems: "center" }}>🔖</div>
          <div>Saved Messages</div>
        </div>
        <button className="secondary small" onClick={() => setGroupOpen((v) => !v)}>
          👥 New group
        </button>
        {groupOpen && (
          <div className="card" style={{ padding: 8 }}>
            <input
              placeholder="Group name"
              value={groupTitle}
              onChange={(e) => setGroupTitle(e.target.value)}
            />
            <div className="col" style={{ maxHeight: 140, overflowY: "auto", marginTop: 4 }}>
              {hits.map((u) => (
                <label key={u.id} className="row" style={{ fontSize: 13, cursor: "pointer" }}>
                  <input
                    type="checkbox"
                    checked={groupMembers.includes(u.id)}
                    onChange={() => toggleGroupMember(u.id)}
                  />
                  {u.display_name} <span className="muted">@{u.username}</span>
                </label>
              ))}
              {hits.length === 0 && <div className="muted" style={{ fontSize: 12 }}>Search users above to add members</div>}
            </div>
            <button className="small" style={{ marginTop: 4 }}
              disabled={!groupTitle.trim() || groupMembers.length === 0}
              onClick={createGroup}>
              Create ({groupMembers.length})
            </button>
          </div>
        )}
        {hits.map((u) => (
          <div key={u.id} className="chat-item row" onClick={() => startChat(u.id)} role="button" tabIndex={0}>
            <div className="avatar sm" />
            <div>
              <div>{u.display_name}</div>
              <div className="muted">@{u.username}</div>
            </div>
          </div>
        ))}
        {hits.length === 0 && conversations.map((c) => (
          <div
            key={c.id}
            className={`chat-item ${active?.id === c.id ? "active" : ""}`}
            onClick={() => openConversation(c)}
            role="button"
            tabIndex={0}
          >
            <div className="row">
              <div>{c.title || (c.is_channel ? "📢 Channel" : c.is_group ? "👥 Group" : "DM")}</div>
              <div className="spacer" />
              {c.unread > 0 && <span className="badge green">{c.unread}</span>}
            </div>
            {c.last_message && <div className="muted">{c.last_message.slice(0, 40)}</div>}
          </div>
        ))}
      </div>
      <div className="card col" style={{ marginBottom: 0 }}>
        {active ? (
          <>
            <div className="row">
              <strong>{active.title || t("chat")}</strong>
              {peerOnline !== null && (
                <span className={`badge ${peerOnline ? "green" : ""}`}>
                  {peerOnline ? "online" : "offline"}
                </span>
              )}
              {typingUsers.size > 0 && <span className="muted">typing…</span>}
              <div className="spacer" />
              {peerId && e2eReady && (
                <button
                  className={e2eOn ? "success small" : "secondary small"}
                  title="End-to-end encryption"
                  onClick={() => { setE2eOn(!e2eOn); e2eRef.current = !e2eOn; }}
                >
                  🔒 E2EE {e2eOn ? "on" : "off"}
                </button>
              )}
              <button className="small" onClick={() => router.push(`/call/${active.id}?video=1`)}>
                {t("videoCall")}
              </button>
              <button className="secondary small" onClick={() => router.push(`/call/${active.id}?video=0`)}>
                {t("audioCall")}
              </button>
            </div>
            <input
              placeholder="Search in conversation…"
              value={msgQuery}
              onChange={(e) => searchMessages(e.target.value)}
              style={{ fontSize: 13 }}
            />
            {msgHits && (
              <div className="card" style={{ padding: 8, maxHeight: 180, overflowY: "auto" }}>
                {msgHits.length === 0 && <div className="muted" style={{ fontSize: 12 }}>No matches</div>}
                {msgHits.map((h) => (
                  <div key={h.id} style={{ fontSize: 13 }}>
                    <strong>{h.sender_name}:</strong> {h.body}
                    <span className="muted" style={{ fontSize: 11 }}> · {new Date(h.created_at).toLocaleString()}</span>
                  </div>
                ))}
              </div>
            )}
            {pins.length > 0 && (
              <div className="row" style={{ fontSize: 12, borderBottom: "1px solid var(--border, #333)", paddingBottom: 6 }}>
                📌 <span className="muted">{pins[0].body.slice(0, 80)}</span>
                {pins.length > 1 && <span className="badge">+{pins.length - 1}</span>}
              </div>
            )}
            <div className="chat-messages" style={{ flex: 1 }}>
              {messages.map((m) => (
                <div key={m.id} className={`bubble ${m.sender_id === getUserId() ? "mine" : ""}`}>
                  {active.is_group && m.sender_id !== getUserId() && (
                    <div style={{ fontSize: 11, opacity: 0.8 }}>{m.sender_name}</div>
                  )}
                  {m.forwarded_from && <div style={{ fontSize: 10, opacity: 0.7 }}>↪ forwarded</div>}
                  {m.story_id && <div style={{ fontSize: 10, opacity: 0.7 }}>📸 story reply</div>}
                  {m.body}
                  {m.media_url && (/\.(ogg|mp3|wav|m4a)(\?|$)/i.test(m.media_url) || /voice-[^/]*\.webm/i.test(m.media_url)) ? (
                    <audio src={m.media_url} controls style={{ maxWidth: "100%", marginTop: 4 }} />
                  ) : m.media_url && /\.(mp4|mov|webm)(\?|$)/i.test(m.media_url) ? (
                    <video src={m.media_url} controls style={{ maxWidth: "100%", borderRadius: 8, marginTop: 4 }} />
                  ) : m.media_url ? (
                    <img src={m.media_url} alt="" style={{ maxWidth: "100%", borderRadius: 8, marginTop: 4 }} />
                  ) : null}
                  <div className="row" style={{ marginTop: 4, gap: 6 }}>
                    {m.is_encrypted && <span title="end-to-end encrypted">🔒</span>}
                    {m.edited_at && <span style={{ fontSize: 10, opacity: 0.7 }}>(edited)</span>}
                    {m.sender_id === getUserId() && (
                      <>
                        <span style={{ fontSize: 10, opacity: 0.7 }}>{myLastRead(m) ? "✓✓" : "✓"}</span>
                        <button className="secondary small" style={{ padding: "1px 6px" }}
                          onClick={() => { setEditingId(m.id); setDraft(m.body); }}>✎</button>
                        <button className="secondary small" style={{ padding: "1px 6px" }}
                          onClick={() => deleteMessage(m.id)}>🗑</button>
                      </>
                    )}
                    <button className="secondary small" style={{ padding: "1px 6px" }}
                      onClick={() => react(m.id, "👍")}>👍</button>
                    <button className="secondary small" style={{ padding: "1px 6px" }}
                      onClick={() => react(m.id, "❤️")}>❤️</button>
                    <button className="secondary small" style={{ padding: "1px 6px" }}
                      title={m.pinned ? "Unpin" : "Pin"}
                      onClick={() => togglePin(m)}>{m.pinned ? "📌" : "📍"}</button>
                    {!m.is_encrypted && (
                      <button className="secondary small" style={{ padding: "1px 6px" }}
                        title="Forward"
                        onClick={() => setForwardingId(m.id)}>↪</button>
                    )}
                  </div>
                  {m.reactions && Object.keys(m.reactions).length > 0 && (
                    <div className="row" style={{ marginTop: 2, gap: 4 }}>
                      {Object.entries(m.reactions).map(([emoji, count]) => (
                        <span key={emoji} className="badge" style={{ fontSize: 11 }}>{emoji} {count}</span>
                      ))}
                    </div>
                  )}
                </div>
              ))}
              <div ref={bottomRef} />
            </div>
            {forwardingId && (
              <div className="card" style={{ padding: 8 }}>
                <div className="row">
                  <strong style={{ fontSize: 13 }}>Forward to…</strong>
                  <div className="spacer" />
                  <button className="secondary small" onClick={() => setForwardingId(null)}>✕</button>
                </div>
                <div className="col" style={{ maxHeight: 180, overflowY: "auto" }}>
                  {conversations.filter((c) => c.id !== active.id).map((c) => (
                    <button key={c.id} className="secondary small" style={{ textAlign: "start" }}
                      onClick={() => forwardMessage(c.id)}>
                      {c.title || (c.is_channel ? "📢 Channel" : c.is_group ? "👥 Group" : "DM")}
                    </button>
                  ))}
                </div>
              </div>
            )}
            {scheduled.length > 0 && (
              <div className="col" style={{ fontSize: 12, gap: 4 }}>
                {scheduled.map((s) => (
                  <div key={s.id} className="row">
                    <span className="muted">🕒 {new Date(s.send_at).toLocaleString()} — {s.body.slice(0, 40)}</span>
                    <div className="spacer" />
                    <button className="secondary small" onClick={() => cancelScheduled(s.id)}>✕</button>
                  </div>
                ))}
              </div>
            )}
            <div className="chat-input">
              {editingId && <span className="badge yellow">editing</span>}
              <input
                value={draft}
                onChange={(e) => onDraftChange(e.target.value)}
                placeholder={t("typeMessage")}
                onKeyDown={(e) => e.key === "Enter" && (editingId ? saveEdit() : send())}
              />
              {editingId ? (
                <>
                  <button onClick={saveEdit}>{t("send")}</button>
                  <button className="secondary" onClick={() => { setEditingId(null); setDraft(""); }}>✕</button>
                </>
              ) : (
                <>
                  <button
                    className={recording ? "small" : "secondary"}
                    title={recording ? "Stop & send voice message" : "Record voice message"}
                    onClick={toggleRecording}
                  >
                    {recording ? "⏹" : "🎤"}
                  </button>
                  <button onClick={send}>{t("send")}</button>
                </>
              )}
            </div>
            {!editingId && (
              <div className="row" style={{ marginTop: 4 }}>
                <input
                  type="datetime-local"
                  value={scheduleAt}
                  onChange={(e) => setScheduleAt(e.target.value)}
                  style={{ fontSize: 12 }}
                  aria-label="Schedule send time"
                />
                <button
                  className="secondary small"
                  disabled={!scheduleAt || !draft.trim()}
                  onClick={scheduleMessage}
                >
                  🕒 Schedule
                </button>
              </div>
            )}
          </>
        ) : (
          <div className="muted" style={{ textAlign: "center", marginTop: 40 }}>
            {t("newChat")}
          </div>
        )}
      </div>
    </div>
  );
}
