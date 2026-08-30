"use client";

import { useEffect, useState } from "react";
import { api } from "@/lib/api";
import type { ChatPollState } from "@/lib/types";

export default function ChatPoll({ pollId, onChanged }: { pollId: string; onChanged?: () => void }) {
  const [poll, setPoll] = useState<ChatPollState | null>(null);
  const [error, setError] = useState("");

  const load = () => {
    api<ChatPollState>(`/api/chat-polls/${pollId}`)
      .then(setPoll)
      .catch(() => setError("poll unavailable"));
  };

  useEffect(load, [pollId]);

  const vote = async (optionId: string) => {
    try {
      await api(`/api/chat-polls/${pollId}/vote`, {
        method: "POST",
        body: JSON.stringify({ option_id: optionId }),
      });
      load();
      onChanged?.();
    } catch (e) {
      setError(e instanceof Error ? e.message : "vote failed");
    }
  };

  if (!poll) return <div className="muted">{error || "…"}</div>;

  const closed = poll.closes_at ? new Date(poll.closes_at) < new Date() : false;
  return (
    <div className="col" style={{ gap: 6 }}>
      <strong>{poll.question}</strong>
      {poll.options.map((o) => {
        const pct = poll.total_votes ? Math.round((o.votes / poll.total_votes) * 100) : 0;
        return (
          <button
            key={o.id}
            className={o.my_vote ? "small" : "secondary small"}
            disabled={closed}
            onClick={() => vote(o.id)}
            style={{ textAlign: "left", position: "relative", overflow: "hidden" }}
          >
            <span
              style={{
                position: "absolute", inset: 0, width: `${pct}%`,
                background: "var(--surface2)", opacity: 0.6,
              }}
            />
            <span style={{ position: "relative" }}>
              {o.label} — {o.votes} ({pct}%){o.my_vote ? " ✓" : ""}
            </span>
          </button>
        );
      })}
      <span className="muted" style={{ fontSize: 12 }}>
        {poll.total_votes} votes{poll.multi ? " · multiple choice" : ""}
        {poll.closes_at && ` · ${closed ? "closed" : `closes ${new Date(poll.closes_at).toLocaleString()}`}`}
      </span>
      {error && <span className="error">{error}</span>}
    </div>
  );
}
