"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { api, getAccessToken } from "@/lib/api";

// In-app AI assistant — master plan §38. AI-authored changes are proposed and
// require explicit human approval before they apply.

type Conversation = { id: string; title: string; message_count: number; updated_at: string };
type Action = { id: string; kind: string; status: string; payload: Record<string, unknown> };
type Msg = { role: "user" | "assistant"; content: string };

export default function AssistantPage() {
  const [authed, setAuthed] = useState(false);
  const [conversations, setConversations] = useState<Conversation[]>([]);
  const [active, setActive] = useState<string | null>(null);
  const [messages, setMessages] = useState<Msg[]>([]);
  const [actions, setActions] = useState<Action[]>([]);
  const [input, setInput] = useState("");
  const [note, setNote] = useState("");
  const [error, setError] = useState("");
  const endRef = useRef<HTMLDivElement>(null);

  const loadConvs = useCallback(async () => {
    try {
      const [c, a] = await Promise.all([
        api<{ conversations: Conversation[] }>("/api/assistant/conversations"),
        api<{ actions: Action[] }>("/api/assistant/actions"),
      ]);
      setConversations(c.conversations ?? []);
      setActions(a.actions ?? []);
    } catch (e) {
      setError(String(e));
    }
  }, []);

  useEffect(() => {
    setAuthed(!!getAccessToken());
    if (getAccessToken()) void loadConvs();
  }, [loadConvs]);

  useEffect(() => {
    endRef.current?.scrollIntoView({ behavior: "smooth" });
  }, [messages]);

  const newConversation = async () => {
    try {
      const r = await api<{ id: string }>("/api/assistant/conversations", {
        method: "POST",
        body: JSON.stringify({ title: "New conversation" }),
      });
      setActive(r.id);
      setMessages([]);
      await loadConvs();
    } catch (e) {
      setError(String(e));
    }
  };

  const send = async () => {
    if (!input.trim()) return;
    let conv = active;
    setNote("");
    setError("");
    try {
      if (!conv) {
        const r = await api<{ id: string }>("/api/assistant/conversations", {
          method: "POST",
          body: JSON.stringify({ title: input.slice(0, 60) }),
        });
        conv = r.id;
        setActive(conv);
      }
      const text = input;
      setInput("");
      setMessages((m) => [...m, { role: "user", content: text }]);
      const r = await api<{ reply: string; reason: string; action_kind: string }>(
        `/api/assistant/conversations/${conv}/messages`,
        { method: "POST", body: JSON.stringify({ content: text }) }
      );
      setMessages((m) => [...m, { role: "assistant", content: r.reply }]);
      if (r.reason) setNote(`Note: ${r.reason}`);
      if (r.action_kind) setNote(`The assistant proposed an action (${r.action_kind}) awaiting your approval.`);
      await loadConvs();
    } catch (e) {
      setError(String(e));
    }
  };

  const decide = async (a: Action, decision: "approved" | "dismissed") => {
    try {
      await api(`/api/assistant/actions/${a.id}/decide`, {
        method: "POST",
        body: JSON.stringify({ decision }),
      });
      await loadConvs();
    } catch (e) {
      setError(String(e));
    }
  };

  if (!authed) {
    return (
      <main className="container">
        <h1>Assistant</h1>
        <p>Sign in to use the in-app assistant.</p>
        <a className="btn" href="/login">Sign in</a>
      </main>
    );
  }

  const pending = actions.filter((a) => a.status === "proposed");

  return (
    <main className="container">
      <h1>Assistant</h1>
      {error && <p className="error">{error}</p>}
      {note && <p className="note">{note}</p>}

      <div className="row">
        <button className="btn" onClick={newConversation}>
          New conversation
        </button>
        <select
          value={active ?? ""}
          onChange={(e) => {
            setActive(e.target.value || null);
            setMessages([]);
          }}
        >
          <option value="">— pick a conversation —</option>
          {conversations.map((c) => (
            <option key={c.id} value={c.id}>
              {c.title} ({c.message_count})
            </option>
          ))}
        </select>
      </div>

      <section className="card">
        {messages.length === 0 && (
          <p className="muted">
            Ask about your balance, unread notifications or trending topics.
          </p>
        )}
        {messages.map((m, i) => (
          <div key={i} className={m.role === "user" ? "msg user" : "msg assistant"}>
            <strong>{m.role === "user" ? "You" : "Assistant"}</strong>
            <p>{m.content}</p>
          </div>
        ))}
        <div ref={endRef} />
        <textarea
          placeholder="Ask the assistant…"
          value={input}
          onChange={(e) => setInput(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter" && !e.shiftKey) {
              e.preventDefault();
              void send();
            }
          }}
        />
        <button className="btn" onClick={send} disabled={!input.trim()}>
          Send
        </button>
      </section>

      {pending.length > 0 && (
        <section className="card">
          <h2>Proposed actions — awaiting your approval ({pending.length})</h2>
          <ul className="list">
            {pending.map((a) => (
              <li key={a.id}>
                <strong>{a.kind}</strong>{" "}
                <span className="muted">{JSON.stringify(a.payload)}</span>
                <div className="row">
                  <button className="btn small" onClick={() => decide(a, "approved")}>
                    Approve
                  </button>
                  <button className="btn small" onClick={() => decide(a, "dismissed")}>
                    Dismiss
                  </button>
                </div>
              </li>
            ))}
          </ul>
        </section>
      )}
    </main>
  );
}
