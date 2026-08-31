"use client";

import { useCallback, useEffect, useState } from "react";
import { api } from "@/lib/api";
import { useI18n } from "@/lib/i18n";

type Question = {
  id: string;
  question: string;
  answer: string | null;
  asker_id: string;
  asker: string;
  created_at: string;
  answered_at: string | null;
};

// TikTok-style profile Q&A: anyone can ask; the profile owner answers
// publicly, and answered pairs are visible on the profile.
export default function ProfileQA({ userId, isMe }: { userId: string; isMe: boolean }) {
  const { t } = useI18n();
  const [questions, setQuestions] = useState<Question[]>([]);
  const [draft, setDraft] = useState("");
  const [answers, setAnswers] = useState<Record<string, string>>({});
  const [error, setError] = useState("");

  const load = useCallback(() => {
    api<{ questions: Question[] }>(`/api/users/${userId}/questions`)
      .then((d) => setQuestions(d.questions))
      .catch(() => {});
  }, [userId]);

  useEffect(load, [load]);

  const ask = async () => {
    setError("");
    try {
      await api(`/api/users/${userId}/questions`, {
        method: "POST",
        body: JSON.stringify({ question: draft }),
      });
      setDraft("");
    } catch (e) {
      setError(e instanceof Error ? e.message : t("error"));
    }
  };

  const answer = async (id: string) => {
    try {
      await api(`/api/questions/${id}/answer`, {
        method: "POST",
        body: JSON.stringify({ answer: answers[id] ?? "" }),
      });
      setAnswers((a) => ({ ...a, [id]: "" }));
      load();
    } catch (e) {
      setError(e instanceof Error ? e.message : t("error"));
    }
  };

  const remove = async (id: string) => {
    try {
      await api(`/api/questions/${id}`, { method: "DELETE" });
      load();
    } catch { /* ignore */ }
  };

  const pending = isMe ? questions.filter((q) => !q.answer) : [];
  const answered = questions.filter((q) => q.answer);

  return (
    <div className="card col" style={{ gap: 8 }}>
      <strong>❓ {t("qa")}</strong>
      {!isMe && (
        <div className="row" style={{ gap: 6 }}>
          <input value={draft} maxLength={500} placeholder={t("askQuestion")}
            onChange={(e) => setDraft(e.target.value)} style={{ flex: 1 }} />
          <button onClick={ask} disabled={!draft.trim()}>{t("ask")}</button>
        </div>
      )}
      {error && <div className="error">{error}</div>}
      {isMe && pending.map((q) => (
        <div key={q.id} className="row" style={{ gap: 6, alignItems: "flex-start" }}>
          <div style={{ flex: 1 }}>
            <div style={{ fontSize: 14 }}>{q.question}</div>
            <div className="muted" style={{ fontSize: 12 }}>
              @{q.asker} · {new Date(q.created_at).toLocaleDateString()}
            </div>
            <div className="row" style={{ gap: 6, marginTop: 4 }}>
              <input value={answers[q.id] ?? ""} maxLength={1000}
                placeholder={t("answerPlaceholder")}
                onChange={(e) => setAnswers((a) => ({ ...a, [q.id]: e.target.value }))}
                style={{ flex: 1 }} />
              <button className="small" onClick={() => answer(q.id)}
                disabled={!(answers[q.id] ?? "").trim()}>{t("answer")}</button>
            </div>
          </div>
          <button className="secondary small" onClick={() => remove(q.id)}>{t("delete")}</button>
        </div>
      ))}
      {answered.map((q) => (
        <div key={q.id} style={{ borderTop: "1px solid var(--border)", paddingTop: 6 }}>
          <div style={{ fontSize: 14 }}>Q: {q.question}</div>
          <div style={{ fontSize: 14, marginTop: 2 }}>A: {q.answer}</div>
          {isMe && (
            <button className="secondary small" onClick={() => remove(q.id)}>{t("delete")}</button>
          )}
        </div>
      ))}
      {pending.length === 0 && answered.length === 0 && (
        <div className="muted">{t("noQuestions")}</div>
      )}
    </div>
  );
}
