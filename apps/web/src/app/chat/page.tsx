"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { useRouter } from "next/navigation";
import { api, getAccessToken, getUserId, wsURL } from "@/lib/api";
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

  const openConversation = async (c: Conversation) => {
    setActive(c);
    activeRef.current = c;
    setMessages([]);
    setTypingUsers(new Set());
    const data = await api<{ messages: Message[] }>(`/api/conversations/${c.id}/messages`);
    const ordered = data.messages.reverse();
    const decrypted = await Promise.all(ordered.map(decryptMessage));
    setMessages(decrypted);
    markRead(c.id);
    loadReads(c.id);
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
  };

  const onDraftChange = (v: string) => {
    setDraft(v);
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
            <div className="chat-messages" style={{ flex: 1 }}>
              {messages.map((m) => (
                <div key={m.id} className={`bubble ${m.sender_id === getUserId() ? "mine" : ""}`}>
                  {active.is_group && m.sender_id !== getUserId() && (
                    <div style={{ fontSize: 11, opacity: 0.8 }}>{m.sender_name}</div>
                  )}
                  {m.body}
                  {m.media_url && (
                    <img src={m.media_url} alt="" style={{ maxWidth: "100%", borderRadius: 8, marginTop: 4 }} />
                  )}
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
                <button onClick={send}>{t("send")}</button>
              )}
            </div>
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
