"use client";

import { useEffect, useState } from "react";
import { api } from "@/lib/api";

// Identity spec §9 — Account Deletion:
// Verification required: Email OTP, Phone OTP, Liveness verification, and the
// "I have withdrawn all assets" checkbox. 30-day pending deletion grace period.
// Login during the grace period cancels deletion. Permanent deletion after 30
// days. No recovery after final deletion. Auto logout after request.
export default function AccountDeletion() {
  const [status, setStatus] = useState("");
  const [pending, setPending] = useState(false);
  const [scheduledAt, setScheduledAt] = useState<string | null>(null);
  const [password, setPassword] = useState("");
  const [assetsWithdrawn, setAssetsWithdrawn] = useState(false);
  const [emailOtp, setEmailOtp] = useState("");
  const [phoneOtp, setPhoneOtp] = useState("");
  const [selfieUrl, setSelfieUrl] = useState("");
  const [emailVerified, setEmailVerified] = useState(false);
  const [phoneVerified, setPhoneVerified] = useState(false);
  const [livenessVerified, setLivenessVerified] = useState(false);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  const load = () => {
    api<{ status: string; pending: boolean; scheduled_at: string | null }>("/api/me/deletion")
      .then((d) => { setStatus(d.status); setPending(d.pending); setScheduledAt(d.scheduled_at); })
      .catch(() => {});
  };

  useEffect(() => { load(); }, []);

  const startChallenge = async (kind: "email" | "phone" | "liveness") => {
    setError("");
    setBusy(true);
    try {
      const body = kind === "liveness" ? { kind, selfie_url: selfieUrl } : { kind };
      const d = await api<{ dev_code?: string; instruction?: string }>("/api/me/deletion/challenges", {
        method: "POST", body: JSON.stringify(body),
      });
      if (kind === "email") setStatus(d.dev_code ? `Email OTP sent (dev: ${d.dev_code})` : "Email OTP sent. Check your inbox.");
      if (kind === "phone") setStatus(d.dev_code ? `Phone OTP sent (dev: ${d.dev_code})` : "Phone OTP sent.");
      if (kind === "liveness") setStatus(d.instruction ? `Liveness: ${d.instruction}` : "Liveness challenge created.");
    } catch (e) {
      setError(e instanceof Error ? e.message : "challenge failed");
    } finally { setBusy(false); }
  };

  const verifyChallenge = async (kind: "email" | "phone" | "liveness") => {
    setError("");
    setBusy(true);
    try {
      const code = kind === "email" ? emailOtp : phoneOtp;
      const body = kind === "liveness" ? { selfie_url: selfieUrl } : { code };
      await api(`/api/me/deletion/challenges/${kind}/verify`, { method: "POST", body: JSON.stringify(body) });
      if (kind === "email") setEmailVerified(true);
      if (kind === "phone") setPhoneVerified(true);
      if (kind === "liveness") setLivenessVerified(true);
      setStatus(`${kind} verified`);
    } catch (e) {
      setError(e instanceof Error ? e.message : "verification failed");
    } finally { setBusy(false); }
  };

  const requestDeletion = async () => {
    setError("");
    if (!password) { setError("Password required"); return; }
    if (!assetsWithdrawn) { setError("Confirm that all assets have been withdrawn"); return; }
    if (!emailVerified || !phoneVerified || !livenessVerified) { setError("Complete email, phone, and liveness verification first"); return; }
    setBusy(true);
    try {
      const d = await api<{ status: string; scheduled_at: string; grace_period_days: number }>("/api/me/deletion", {
        method: "POST", body: JSON.stringify({ password, assets_withdrawn: assetsWithdrawn }),
      });
      setStatus(`Deletion scheduled. Permanent deletion in ${d.grace_period_days} days. Logging out…`);
      setPending(true);
      setScheduledAt(d.scheduled_at);
      // Auto logout after request (Identity spec §9).
      setTimeout(() => { window.location.href = "/login"; }, 1500);
    } catch (e) {
      setError(e instanceof Error ? e.message : "deletion request failed");
    } finally { setBusy(false); }
  };

  const cancelDeletion = async () => {
    setError("");
    setBusy(true);
    try {
      await api("/api/me/deletion", { method: "DELETE" });
      setStatus("Deletion cancelled. Your account is active again.");
      setPending(false);
      setScheduledAt(null);
      load();
    } catch (e) {
      setError(e instanceof Error ? e.message : "cancel failed");
    } finally { setBusy(false); }
  };

  return (
    <div className="card col">
      <h3 style={{ marginTop: 0 }}>Delete account</h3>
      <p className="muted">
        Deleting your account requires email OTP, phone OTP, and a fresh KYC selfie for
        liveness verification, plus confirmation that all assets have been withdrawn.
        After a 30-day grace period the account is permanently deleted and cannot be recovered.
        Logging in during the grace period cancels the deletion.
      </p>

      {pending ? (
        <>
          <p className="badge" style={{ background: "#c62828", color: "#fff" }}>
            Deletion pending — scheduled for {scheduledAt ? new Date(scheduledAt).toLocaleString() : "…"}
          </p>
          <button className="secondary" onClick={cancelDeletion} disabled={busy}>Cancel deletion</button>
        </>
      ) : (
        <>
          <div className="col" style={{ gap: 8 }}>
            <div className="row" style={{ alignItems: "center", gap: 8 }}>
              <button className="secondary small" onClick={() => startChallenge("email")} disabled={busy || emailVerified}>Send email OTP</button>
              <input value={emailOtp} onChange={(e) => setEmailOtp(e.target.value)} placeholder="Email OTP" maxLength={6} inputMode="numeric" disabled={emailVerified} style={{ flex: 1 }} />
              <button className="secondary small" onClick={() => verifyChallenge("email")} disabled={busy || !emailOtp || emailVerified}>Verify</button>
              {emailVerified && <span className="badge green">✓</span>}
            </div>
            <div className="row" style={{ alignItems: "center", gap: 8 }}>
              <button className="secondary small" onClick={() => startChallenge("phone")} disabled={busy || phoneVerified}>Send phone OTP</button>
              <input value={phoneOtp} onChange={(e) => setPhoneOtp(e.target.value)} placeholder="Phone OTP" maxLength={6} inputMode="numeric" disabled={phoneVerified} style={{ flex: 1 }} />
              <button className="secondary small" onClick={() => verifyChallenge("phone")} disabled={busy || !phoneOtp || phoneVerified}>Verify</button>
              {phoneVerified && <span className="badge green">✓</span>}
            </div>
            <div className="row" style={{ alignItems: "center", gap: 8 }}>
              <input value={selfieUrl} onChange={(e) => setSelfieUrl(e.target.value)} placeholder="Fresh selfie URL (secure media upload)" disabled={livenessVerified} style={{ flex: 1 }} />
              <button className="secondary small" onClick={() => startChallenge("liveness")} disabled={busy || !selfieUrl || livenessVerified}>Start liveness</button>
              <button className="secondary small" onClick={() => verifyChallenge("liveness")} disabled={busy || !selfieUrl || livenessVerified}>Verify</button>
              {livenessVerified && <span className="badge green">✓</span>}
            </div>
          </div>
          <input type="password" value={password} onChange={(e) => setPassword(e.target.value)} placeholder="Current password" />
          <label style={{ display: "flex", alignItems: "center", gap: 8, fontSize: 14 }}>
            <input type="checkbox" checked={assetsWithdrawn} onChange={(e) => setAssetsWithdrawn(e.target.checked)} />
            I have withdrawn all assets
          </label>
          {error && <div className="error-text">{error}</div>}
          <button className="danger" onClick={requestDeletion} disabled={busy}>Request account deletion</button>
        </>
      )}
      {status && !error && <p className="badge green">{status}</p>}
    </div>
  );
}
