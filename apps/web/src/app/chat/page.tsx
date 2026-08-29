"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { useRouter } from "next/navigation";
import { api, getAccessToken, getUserId, wsURL } from "@/lib/api";
import { useI18n } from "@/lib/i18n";
import type { Conversation, Message, PublicUser } from "@/lib/types";

export default function ChatPage() {
  const { t } = useI18n();
  const router = useRouter();
  const [conversations, setConversations] = useState<Conversation[]>([]);
  const [active, setActive] = useState<Conversation | null>(null);
  const [messages, setMessages] = useState<Message[]>([]);
  const [draft, setDraft] = useState("");
  const [search, setSearch] = useState("");
  const [hits, setHits] = useState<PublicUser[]>([]);
  const wsRef = useRef<WebSocket | null>(null);
  const activeRef = useRef<string | null>(null);
  const bottomRef = useRef<HTMLDivElement>(null);

  const loadConversations = useCallback(() => {
    api<{ conversations: Conversation[] }>("/api/conversations")
      .then((d) => setConversations(d.conversations))
      .catch(() => {});
  }, []);

  useEffect(() => {
    if (!getAccessToken()) {
      router.push("/login");
      return;
    }
    loadConversations();
    const ws = new WebSocket(wsURL());
    wsRef.current = ws;
    ws.onmessage = (ev) => {
      try {
        const data = JSON.parse(ev.data as string) as Message & { type: string; conversation_id: string };
        if (data.type === "message" && data.conversation_id === activeRef.current) {
          setMessages((prev) => [...prev, data]);
        }
        if (data.type === "message") loadConversations();
      } catch { /* ignore malformed frames */ }
    };
    return () => ws.close();
  }, [loadConversations, router]);

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: "smooth" });
  }, [messages]);

  const openConversation = async (c: Conversation) => {
    setActive(c);
    activeRef.current = c.id;
    const data = await api<{ messages: Message[] }>(`/api/conversations/${c.id}/messages`);
    setMessages(data.messages.reverse());
  };

  const send = () => {
    if (!draft.trim() || !active || wsRef.current?.readyState !== WebSocket.OPEN) return;
    wsRef.current.send(
      JSON.stringify({ type: "message", conversation_id: active.id, body: draft })
    );
    setDraft("");
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
    const conv: Conversation = {
      id: data.id, is_group: false, title: "", created_at: "", last_message: null,
    };
    openConversation(conv);
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
            <div>{c.title || (c.is_group ? t("chat") : "DM")}</div>
            {c.last_message && <div className="muted">{c.last_message.slice(0, 40)}</div>}
          </div>
        ))}
      </div>
      <div className="card col" style={{ marginBottom: 0 }}>
        {active ? (
          <>
            <div className="row">
              <strong>{active.title || t("chat")}</strong>
              <div className="spacer" />
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
                </div>
              ))}
              <div ref={bottomRef} />
            </div>
            <div className="chat-input">
              <input
                value={draft}
                onChange={(e) => setDraft(e.target.value)}
                placeholder={t("typeMessage")}
                onKeyDown={(e) => e.key === "Enter" && send()}
              />
              <button onClick={send}>{t("send")}</button>
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
