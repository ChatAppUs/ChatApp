"use client";

import { useCallback, useEffect, useState } from "react";
import { useI18n } from "@/lib/i18n";
import {
  acceptFollowRequest, acceptMessageRequest, addWordFilter, declineFollowRequest,
  declineMessageRequest, followRequests, listMutes, listRestricted, listWordFilters,
  messageRequests, removeWordFilter, setActiveStatus, setProfileLock, unmute, unrestrict,
  type FollowRequest, type MessageRequest, type MutedUser,
} from "@/lib/features";
import PushSetup from "@/components/PushSetup";

export default function PrivacyPage() {
  const { t } = useI18n();
  const [mutes, setMutes] = useState<MutedUser[]>([]);
  const [restricted, setRestricted] = useState<MutedUser[]>([]);
  const [filters, setFilters] = useState<{ phrase: string; created_at: string }[]>([]);
  const [freqs, setFreqs] = useState<FollowRequest[]>([]);
  const [mreqs, setMreqs] = useState<MessageRequest[]>([]);
  const [phrase, setPhrase] = useState("");
  const [locked, setLocked] = useState(false);
  const [showActive, setShowActive] = useState(true);
  const [error, setError] = useState("");

  const load = useCallback(async () => {
    try {
      const [m, r, f, fr, mr] = await Promise.all([
        listMutes(), listRestricted(), listWordFilters(), followRequests(), messageRequests(),
      ]);
      setMutes(m.mutes);
      setRestricted(r.restricted);
      setFilters(f.filters);
      setFreqs(fr.requests);
      setMreqs(mr.requests);
    } catch (e) {
      setError(e instanceof Error ? e.message : "load failed");
    }
  }, []);

  useEffect(() => { load(); }, [load]);

  const addFilter = async (e: React.FormEvent) => {
    e.preventDefault();
    try {
      await addWordFilter(phrase);
      setPhrase("");
      load();
    } catch (err) {
      setError(err instanceof Error ? err.message : "filter failed");
    }
  };

  const PersonRow = ({ u, action, label }: { u: MutedUser; action: () => void; label: string }) => (
    <div className="card row" key={u.id}>
      <strong>@{u.username}</strong>
      <span className="muted">{u.display_name}</span>
      <button className="secondary" onClick={action}>{label}</button>
    </div>
  );

  return (
    <div className="col">
      <h1>{t("privacy")}</h1>
      {error && <div className="error">{error}</div>}

      <div className="card col">
        <h2>{t("profileLock")}</h2>
        <label className="row">
          <input
            type="checkbox"
            checked={locked}
            onChange={async (e) => {
              setLocked(e.target.checked);
              await setProfileLock(e.target.checked).catch((err) => setError(err.message));
            }}
          />
          {t("profileLock")}
        </label>
        <label className="row">
          <input
            type="checkbox"
            checked={showActive}
            onChange={async (e) => {
              setShowActive(e.target.checked);
              await setActiveStatus(e.target.checked).catch((err) => setError(err.message));
            }}
          />
          {t("activeStatus")}
        </label>
      </div>

      <PushSetup />

      <h2>{t("followRequests")}</h2>
      {freqs.map((r) => (
        <div className="card row" key={r.id}>
          <strong>@{r.username}</strong>
          <button onClick={async () => { await acceptFollowRequest(r.id); load(); }}>{t("accept")}</button>
          <button className="secondary" onClick={async () => { await declineFollowRequest(r.id); load(); }}>{t("decline")}</button>
        </div>
      ))}
      {freqs.length === 0 && <p className="muted">—</p>}

      <h2>{t("messageRequests")}</h2>
      {mreqs.map((r) => (
        <div className="card col" key={r.conversation_id}>
          <div className="row">
            <strong>@{r.username}</strong>
            <button onClick={async () => { await acceptMessageRequest(r.conversation_id); load(); }}>{t("accept")}</button>
            <button className="secondary" onClick={async () => { await declineMessageRequest(r.conversation_id); load(); }}>{t("decline")}</button>
          </div>
          {r.preview && <p className="muted">{r.preview}</p>}
        </div>
      ))}
      {mreqs.length === 0 && <p className="muted">—</p>}

      <h2>{t("mutedUsers")}</h2>
      {mutes.map((u) => (
        <PersonRow key={u.id} u={u} label={t("unmute") ?? t("unfollow")}
          action={async () => { await unmute(u.id); load(); }} />
      ))}
      {mutes.length === 0 && <p className="muted">—</p>}

      <h2>{t("restrictedList")}</h2>
      {restricted.map((u) => (
        <PersonRow key={u.id} u={u} label={t("decline")}
          action={async () => { await unrestrict(u.id); load(); }} />
      ))}
      {restricted.length === 0 && <p className="muted">—</p>}

      <h2>{t("wordFilters")}</h2>
      <form className="card col" onSubmit={addFilter}>
        <input value={phrase} onChange={(e) => setPhrase(e.target.value)} required maxLength={100} />
        <button type="submit">{t("wordFilters")}</button>
      </form>
      {filters.map((f) => (
        <div className="card row" key={f.phrase}>
          <strong>{f.phrase}</strong>
          <button className="secondary" onClick={async () => { await removeWordFilter(f.phrase); load(); }}>
            {t("delete")}
          </button>
        </div>
      ))}
    </div>
  );
}
