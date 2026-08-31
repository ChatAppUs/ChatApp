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

  const myVote = poll?.options.find((o) => o.my_vote);
  // Quiz answers are final and reveal the correct option + explanation.
  const quizRevealed = !!poll?.is_quiz && (!!myVote || !!poll?.correct_option_id);

  if (!poll) return <div className="muted">{error || "…"}</div>;

  const closed = poll.closes_at ? new Date(poll.closes_at) < new Date() : false;
  return (
    <div className="col" style={{ gap: 6 }}>
      <strong>
        {poll.is_quiz && <span title="Quiz" style={{ marginRight: 4 }}>🎯</span>}
        {poll.question}
      </strong>
      {poll.options.map((o) => {
        const pct = poll.total_votes ? Math.round((o.votes / poll.total_votes) * 100) : 0;
        const isCorrect = quizRevealed && poll.correct_option_id === o.id;
        const isWrongPick = quizRevealed && o.my_vote && poll.correct_option_id !== o.id;
        return (
          <button
            key={o.id}
            className={o.my_vote ? "small" : "secondary small"}
            disabled={closed || (poll.is_quiz && !!myVote)}
            onClick={() => vote(o.id)}
            style={{ textAlign: "left", position: "relative", overflow: "hidden" }}
          >
            <span
              style={{
                position: "absolute", inset: 0, width: `${pct}%`,
                background: isCorrect ? "rgba(46,160,67,0.35)" : isWrongPick ? "rgba(218,54,51,0.3)" : "var(--surface2)",
                opacity: 0.7,
              }}
            />
            <span style={{ position: "relative" }}>
              {isCorrect ? "✅ " : isWrongPick ? "❌ " : ""}
              {o.label} — {o.votes} ({pct}%){o.my_vote ? " ✓" : ""}
            </span>
          </button>
        );
      })}
      {quizRevealed && poll.explanation && (
        <span className="muted" style={{ fontSize: 12 }}>💡 {poll.explanation}</span>
      )}
      <span className="muted" style={{ fontSize: 12 }}>
        {poll.total_votes} votes
        {poll.is_quiz ? " · quiz" : poll.multi ? " · multiple choice" : ""}
        {poll.anonymous ? " · anonymous" : ""}
        {poll.closes_at && ` · ${closed ? "closed" : `closes ${new Date(poll.closes_at).toLocaleString()}`}`}
      </span>
      {error && <span className="error">{error}</span>}
    </div>
  );
}
