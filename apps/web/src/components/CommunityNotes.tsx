"use client";

import { useEffect, useState } from "react";
import { api, getUserId } from "@/lib/api";
import { useI18n } from "@/lib/i18n";

export type CommunityNote = {
  id: string;
  author_id: string;
  username: string;
  body: string;
  helpful: number;
  not_helpful: number;
  my_vote: string;
  shown: boolean;
  created_at: string;
};

export default function CommunityNotes({ postId }: { postId: string }) {
  const { t } = useI18n();
  const [open, setOpen] = useState(false);
  const [notes, setNotes] = useState<CommunityNote[]>([]);
  const [draft, setDraft] = useState("");
  const [error, setError] = useState("");

  const load = () => {
    api<{ notes: CommunityNote[] }>(`/api/posts/${postId}/notes`)
      .then((d) => setNotes(d.notes)).catch(() => {});
  };

  useEffect(() => {
    if (open) load();
  }, [open, postId]);

  const add = async () => {
    setError("");
    try {
      await api(`/api/posts/${postId}/notes`, { method: "POST", body: JSON.stringify({ body: draft }) });
      setDraft("");
      load();
    } catch (e) {
      setError(e instanceof Error ? e.message : "failed");
    }
  };

  const vote = async (id: string, helpful: boolean) => {
    try {
      await api(`/api/notes/${id}/vote`, { method: "POST", body: JSON.stringify({ helpful }) });
      load();
    } catch (e) {
      setError(e instanceof Error ? e.message : "vote failed");
    }
  };

  const remove = async (id: string) => {
    await api(`/api/notes/${id}`, { method: "DELETE" }).catch(() => {});
    load();
  };

  const shown = notes.filter((n) => n.shown);
  return (
    <div className="col" style={{ gap: 6, marginTop: 6 }}>
      {shown.map((n) => (
        <div key={n.id} className="badge" style={{ fontSize: 12, display: "block" }}>
          📝 {n.body} <span className="muted">— @{n.username}</span>
        </div>
      ))}
      <button className="secondary small" style={{ alignSelf: "flex-start" }}
        onClick={() => setOpen((v) => !v)}>
        {open ? t("hideNotes") : `${t("communityNotes")}${notes.length ? ` (${notes.length})` : ""}`}
      </button>
      {open && (
        <div className="col" style={{ gap: 6 }}>
          {notes.map((n) => (
            <div key={n.id} className="card" style={{ padding: 8, fontSize: 13 }}>
              <div>{n.body}</div>
              <div className="row" style={{ gap: 6, marginTop: 4, alignItems: "center" }}>
                <span className="muted" style={{ fontSize: 11 }}>@{n.username}</span>
                <div className="spacer" />
                <button className={n.my_vote === "true" ? "small" : "secondary small"}
                  onClick={() => vote(n.id, true)}>{t("helpful")} {n.helpful}</button>
                <button className={n.my_vote === "false" ? "small" : "secondary small"}
                  onClick={() => vote(n.id, false)}>{t("notHelpful")} {n.not_helpful}</button>
                {n.author_id === getUserId() && (
                  <button className="danger small" onClick={() => remove(n.id)}>{t("delete")}</button>
                )}
              </div>
            </div>
          ))}
          <div className="row" style={{ gap: 6 }}>
            <input placeholder={t("notePlaceholder")} value={draft} maxLength={500}
              onChange={(e) => setDraft(e.target.value)} />
            <button onClick={add} disabled={!draft.trim()}>{t("addNote")}</button>
          </div>
          {error && <div className="error">{error}</div>}
        </div>
      )}
    </div>
  );
}
