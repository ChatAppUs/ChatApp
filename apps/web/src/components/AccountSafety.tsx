"use client";

import { useEffect, useState } from "react";
import { api } from "@/lib/api";

type TrustedContact = { id: string; username: string; display_name: string; created_at: string };
type PendingShare = { id: string; username: string; expires_at: string; revealed: boolean };
type Profile = { id: string; name: string; bio: string; avatar_url: string; active: boolean; created_at: string };
type LegacyContact = { id: string; username: string; display_name: string } | null;

export default function AccountSafety() {
  const [contacts, setContacts] = useState<TrustedContact[]>([]);
  const [pending, setPending] = useState<PendingShare[]>([]);
  const [revealed, setRevealed] = useState<Record<string, string>>({});
  const [newContact, setNewContact] = useState("");
  const [legacy, setLegacy] = useState<LegacyContact>(null);
  const [legacyName, setLegacyName] = useState("");
  const [profiles, setProfiles] = useState<Profile[]>([]);
  const [profName, setProfName] = useState("");
  const [profBio, setProfBio] = useState("");
  const [digest, setDigest] = useState<boolean | null>(null);
  const [status, setStatus] = useState("");
  const [error, setError] = useState("");

  const ok = (msg: string) => { setStatus(msg); setError(""); };
  const fail = (e: unknown) => { setError(e instanceof Error ? e.message : "failed"); setStatus(""); };

  const loadContacts = () => api<{ contacts: TrustedContact[] }>("/api/me/trusted-contacts")
    .then((d) => setContacts(d.contacts)).catch(() => {});
  const loadPending = () => api<{ pending: PendingShare[] }>("/api/recovery/trusted/pending")
    .then((d) => setPending(d.pending)).catch(() => {});
  const loadLegacy = () => api<{ legacy_contact: LegacyContact }>("/api/me/legacy-contact")
    .then((d) => setLegacy(d.legacy_contact)).catch(() => {});
  const loadProfiles = () => api<{ profiles: Profile[] }>("/api/me/profiles")
    .then((d) => setProfiles(d.profiles)).catch(() => {});
  const loadDigest = () => api<{ digest_enabled: boolean }>("/api/me")
    .then((d) => setDigest(d.digest_enabled !== false)).catch(() => {});

  useEffect(() => {
    loadContacts(); loadPending(); loadLegacy(); loadProfiles(); loadDigest();
  }, []);

  const addContact = async () => {
    try {
      await api("/api/me/trusted-contacts", { method: "POST", body: JSON.stringify({ username: newContact.trim() }) });
      setNewContact(""); ok("Trusted contact added."); loadContacts();
    } catch (e) { fail(e); }
  };

  const removeContact = async (id: string) => {
    try {
      await api(`/api/me/trusted-contacts/${id}`, { method: "DELETE" });
      loadContacts();
    } catch (e) { fail(e); }
  };

  const reveal = async (shareId: string) => {
    try {
      const d = await api<{ code: string }>("/api/recovery/trusted/reveal", {
        method: "POST", body: JSON.stringify({ share_id: shareId }),
      });
      setRevealed((prev) => ({ ...prev, [shareId]: d.code }));
    } catch (e) { fail(e); }
  };

  const setLegacyContact = async () => {
    try {
      await api("/api/me/legacy-contact", { method: "PUT", body: JSON.stringify({ username: legacyName.trim() }) });
      setLegacyName(""); ok("Legacy contact set."); loadLegacy();
    } catch (e) { fail(e); }
  };

  const removeLegacy = async () => {
    await api("/api/me/legacy-contact", { method: "DELETE" }).catch(() => {});
    loadLegacy();
  };

  const createProfile = async () => {
    try {
      await api("/api/me/profiles", { method: "POST", body: JSON.stringify({ name: profName.trim(), bio: profBio }) });
      setProfName(""); setProfBio(""); ok("Profile created."); loadProfiles();
    } catch (e) { fail(e); }
  };

  const switchProfile = async (id: string) => {
    try {
      await api("/api/me/active-profile", { method: "PUT", body: JSON.stringify({ profile_id: id }) });
      loadProfiles();
    } catch (e) { fail(e); }
  };

  const deleteProfile = async (id: string) => {
    try {
      await api(`/api/me/profiles/${id}`, { method: "DELETE" });
      loadProfiles();
    } catch (e) { fail(e); }
  };

  const toggleDigest = async (enabled: boolean) => {
    try {
      await api("/api/me/digest", { method: "PUT", body: JSON.stringify({ enabled }) });
      setDigest(enabled);
    } catch (e) { fail(e); }
  };

  return (
    <>
      <div className="card col">
        <h3 style={{ marginTop: 0 }}>Trusted contacts (account recovery)</h3>
        <p className="muted">
          Pick 3–5 friends. If you get locked out, any two of them can give you recovery codes
          to get back in. <a href="/recover">Locked out? Recover your account</a>
        </p>
        <div className="row">
          <input placeholder="username" value={newContact} onChange={(e) => setNewContact(e.target.value)} />
          <button onClick={addContact} disabled={!newContact.trim() || contacts.length >= 5}>Add</button>
        </div>
        {contacts.map((c) => (
          <div key={c.id} className="row" style={{ alignItems: "center" }}>
            <span>@{c.username}</span>
            <div className="spacer" />
            <button className="danger small" onClick={() => removeContact(c.id)}>Remove</button>
          </div>
        ))}
        {contacts.length === 0 && <p className="muted">No trusted contacts yet.</p>}

        {pending.length > 0 && (
          <>
            <h4>Recovery requests waiting for you</h4>
            {pending.map((p) => (
              <div key={p.id} className="row" style={{ alignItems: "center" }}>
                <span>@{p.username} needs a recovery code</span>
                <div className="spacer" />
                {revealed[p.id] ? (
                  <code className="badge green">{revealed[p.id]}</code>
                ) : (
                  <button className="small" onClick={() => reveal(p.id)}>Reveal my code</button>
                )}
              </div>
            ))}
            <p className="muted" style={{ fontSize: 12 }}>
              Only give a code to the person directly (call them to confirm). Never send it to anyone claiming to be support.
            </p>
          </>
        )}
      </div>

      <div className="card col">
        <h3 style={{ marginTop: 0 }}>Legacy contact</h3>
        <p className="muted">
          A person who can download your public archive if your account is memorialized.
        </p>
        {legacy ? (
          <div className="row" style={{ alignItems: "center" }}>
            <span>@{legacy.username}</span>
            <div className="spacer" />
            <button className="danger small" onClick={removeLegacy}>Remove</button>
          </div>
        ) : (
          <div className="row">
            <input placeholder="username" value={legacyName} onChange={(e) => setLegacyName(e.target.value)} />
            <button onClick={setLegacyContact} disabled={!legacyName.trim()}>Set</button>
          </div>
        )}
      </div>

      <div className="card col">
        <h3 style={{ marginTop: 0 }}>Additional profiles</h3>
        <p className="muted">Up to 4 extra personas; switch which one appears on your new posts.</p>
        {profiles.map((p) => (
          <div key={p.id} className="row" style={{ alignItems: "center" }}>
            <span>{p.name}{p.active && <span className="badge green" style={{ marginLeft: 6 }}>active</span>}</span>
            <div className="spacer" />
            {!p.active && <button className="secondary small" onClick={() => switchProfile(p.id)}>Use</button>}
            {p.active && <button className="secondary small" onClick={() => switchProfile("")}>Main</button>}
            <button className="danger small" onClick={() => deleteProfile(p.id)}>Delete</button>
          </div>
        ))}
        <div className="row">
          <input placeholder="Profile name" value={profName} maxLength={60}
            onChange={(e) => setProfName(e.target.value)} />
          <input placeholder="Bio (optional)" value={profBio} maxLength={500}
            onChange={(e) => setProfBio(e.target.value)} />
          <button onClick={createProfile} disabled={!profName.trim() || profiles.length >= 4}>Create</button>
        </div>
      </div>

      <div className="card col">
        <h3 style={{ marginTop: 0 }}>Email digest</h3>
        <p className="muted">A daily summary of what you missed (unread notifications, requests, new followers).</p>
        {digest !== null && (
          <label className="row" style={{ gap: 6 }}>
            <input type="checkbox" style={{ width: "auto" }} checked={digest}
              onChange={(e) => toggleDigest(e.target.checked)} />
            <span>Send me the daily digest</span>
          </label>
        )}
      </div>

      {error && <div className="error">{error}</div>}
      {status && <div className="badge green">{status}</div>}
    </>
  );
}
