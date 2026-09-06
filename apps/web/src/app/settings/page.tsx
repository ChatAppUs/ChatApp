"use client";

import { useEffect, useState } from "react";
import { api } from "@/lib/api";
import { passkeySupported, registerPasskey } from "@/lib/passkey";
import AccountSafety from "@/components/AccountSafety";
import ScreenTimePanel from "@/components/ScreenTime";

type Passkey = {
  id: string;
  name: string;
  transports: string[];
  created_at: string;
  last_used_at: string | null;
};

type Session = {
  id: string;
  user_agent: string;
  ip: string;
  created_at: string;
  expires_at: string;
};

export default function SettingsPage() {
  const [secret, setSecret] = useState("");
  const [otpauth, setOtpauth] = useState("");
  const [code, setCode] = useState("");
  const [status, setStatus] = useState("");
  const [error, setError] = useState("");
  const [passkeys, setPasskeys] = useState<Passkey[]>([]);
  const [pkName, setPkName] = useState("");
  const [sessions, setSessions] = useState<Session[]>([]);
  const [recoveryCodes, setRecoveryCodes] = useState<string[]>([]);
  const [recoveryLeft, setRecoveryLeft] = useState<number | null>(null);
  const [verifyCode, setVerifyCode] = useState("");
  const [disableCode, setDisableCode] = useState("");
  const [notif, setNotif] = useState<Record<string, boolean>>({});
  const NOTIF_LABELS: Record<string, string> = {
    messages: "Direct messages", groups: "Group chats", calls: "Calls",
    live: "Live streams & rooms", gifts: "Gifts", reposts: "Reposts & quotes",
    replies: "Replies to your posts", mentions: "Mentions of you", sounds: "Sounds",
    stories: "Stories", marketplace: "Marketplace", withdrawals: "Withdrawals",
    deposits: "Deposits", kyc: "KYC & verification", system: "System",
  };

  const loadPasskeys = async () => {
    try {
      const d = await api<{ passkeys: Passkey[] }>("/api/auth/passkeys");
      setPasskeys(d.passkeys);
    } catch {
      /* not critical */
    }
  };

  useEffect(() => {
    loadPasskeys();
    api<{ sessions: Session[] }>("/api/me/sessions").then((d) => setSessions(d.sessions)).catch(() => {});
    api<{ settings: Record<string, boolean> }>("/api/me/notification-settings").then((d) => setNotif(d.settings ?? {})).catch(() => {});
    api<{ remaining: number }>("/api/auth/2fa/recovery-codes").then((d) => setRecoveryLeft(d.remaining)).catch(() => {});
  }, []);

  const toggleNotif = async (key: string, value: boolean) => {
    setNotif((prev) => ({ ...prev, [key]: value }));
    try {
      await api(`/api/me/notification-settings/${key}`, { method: "PUT", body: JSON.stringify({ enabled: value }) });
    } catch (e) {
      setError(e instanceof Error ? e.message : "failed to save notification settings");
    }
  };

  const revokeSession = async (id: string) => {
    try {
      await api(`/api/me/sessions/${id}`, { method: "DELETE" });
      setSessions((prev) => prev.filter((s) => s.id !== id));
      setStatus("Session revoked. The device will be signed out on its next request.");
    } catch (e) {
      setError(e instanceof Error ? e.message : "revoke failed");
    }
  };

  const reissueRecovery = async () => {
    setError("");
    try {
      const d = await api<{ codes: string[] }>("/api/auth/2fa/recovery-codes", {
        method: "POST",
        body: JSON.stringify({ code: verifyCode.trim() }),
      });
      setRecoveryCodes(d.codes);
      setRecoveryLeft(d.codes.length);
      setStatus("New one-time recovery codes generated. Each code can be used exactly once; store them safely.");
    } catch (e) {
      setError(e instanceof Error ? e.message : "current code required");
    }
  };

  const addPasskey = async () => {
    setError("");
    setStatus("");
    try {
      await registerPasskey(pkName.trim() || "My passkey");
      setPkName("");
      setStatus("Passkey added. You can now sign in with your fingerprint, face, or device PIN.");
      loadPasskeys();
    } catch (e) {
      setError(e instanceof Error ? e.message : "passkey registration failed");
    }
  };

  const removePasskey = async (id: string) => {
    try {
      await api(`/api/auth/passkeys/${id}`, { method: "DELETE" });
      loadPasskeys();
    } catch (e) {
      setError(e instanceof Error ? e.message : "delete failed");
    }
  };

  const setup = async () => {
    setError("");
    setStatus("");
    try {
      const d = await api<{ secret: string; otpauth_url: string }>("/api/auth/2fa/setup", {
        method: "POST",
        body: "{}",
      });
      setSecret(d.secret);
      setOtpauth(d.otpauth_url);
    } catch (e) {
      setError(e instanceof Error ? e.message : "setup failed");
    }
  };

  const enable = async () => {
    setError("");
    try {
      const d = await api<{ status: string; recovery_codes?: string[] }>("/api/auth/2fa/enable", {
        method: "POST",
        body: JSON.stringify({ code }),
      });
      if (d?.recovery_codes?.length) {
        setRecoveryCodes(d.recovery_codes);
        setStatus("Two-factor authentication is now ON. Save these one-time recovery codes — each can be used once if you lose your authenticator app.");
      } else {
        setStatus("Two-factor authentication is now ON. You will need your authenticator code at every login.");
      }
      setSecret("");
      setOtpauth("");
      setVerifyCode("");
    } catch (e) {
      setError(e instanceof Error ? e.message : "invalid code");
    }
  };

  const disable = async () => {
    setError("");
    try {
      await api("/api/auth/2fa/disable", {
        method: "POST",
        body: JSON.stringify({ code: disableCode }),
      });
      setStatus("Two-factor authentication disabled.");
      setDisableCode("");
      setRecoveryCodes([]);
    } catch (e) {
      setError(e instanceof Error ? e.message : "invalid code");
    }
  };

  return (
    <div className="col" style={{ maxWidth: 560, margin: "0 auto" }}>
      <div className="card col">
        <h2 style={{ marginTop: 0 }}>Security settings</h2>
        <h3>Two-factor authentication (TOTP)</h3>
        <p className="muted">
          Works with any RFC 6238 authenticator app (Google Authenticator, Authy, 1Password…).
        </p>
        {!secret && <button onClick={setup}>Start 2FA setup</button>}
        {secret && (
          <>
            <p>Add this secret to your authenticator app:</p>
            <code style={{ wordBreak: "break-all", background: "var(--surface2)", padding: 8, borderRadius: 8 }}>
              {secret}
            </code>
            <p className="muted" style={{ wordBreak: "break-all", fontSize: 12 }}>{otpauth}</p>
            <input
              placeholder="Enter the 6-digit code to confirm"
              value={code}
              onChange={(e) => setCode(e.target.value)}
              maxLength={6}
              inputMode="numeric"
            />
            <button onClick={enable}>Enable 2FA</button>
          </>
        )}
        <hr style={{ border: "none", borderTop: "1px solid var(--border)", width: "100%" }} />

        <h3>Recovery codes</h3>
        <p className="muted">
          One-time scratch codes unlock your account when the authenticator app is unavailable.
          Each code works exactly once. Codes are hashed at rest — not even the server can read them after issue.
          {recoveryLeft != null && recoveryLeft > 0 && (
            <span className="badge green"> {recoveryLeft} unused code{recoveryLeft === 1 ? "" : "s"} remaining</span>
          )}
        </p>
        {recoveryCodes.length > 0 && (
          <div className="badge green" style={{ whiteSpace: "pre-line" }}>
            {recoveryCodes.join("\n")}
          </div>
        )}
        <div className="row">
          <input
            placeholder="Current 6-digit code (required to re-issue)"
            value={verifyCode}
            onChange={(e) => setVerifyCode(e.target.value)}
            maxLength={6}
            inputMode="numeric"
            style={{ flex: 1 }}
          />
          <button className="secondary" onClick={reissueRecovery}>Generate / re-issue</button>
        </div>

        <hr style={{ border: "none", borderTop: "1px solid var(--border)", width: "100%" }} />
        <h3>Disable 2FA</h3>
        <input
          placeholder="Current 6-digit code"
          value={disableCode}
          onChange={(e) => setDisableCode(e.target.value)}
          maxLength={6}
          inputMode="numeric"
        />
        <button className="danger" onClick={disable}>Disable 2FA</button>
        {error && <div className="error">{error}</div>}
        {status && <div className="badge green">{status}</div>}
      </div>

      <div className="card col">
        <h3 style={{ marginTop: 0 }}>Active sessions ({sessions.length})</h3>
        <p className="muted">
          This device, plus any where you are currently signed in. End a session to force
          that device to sign in again next time.

        </p>
        {sessions.map((s) => (
          <div key={s.id} className="row" style={{ alignItems: "center" }}>
            <div style={{ flex: 1 }}>
              <div>{s.user_agent.slice(0, 70)}</div>
              <div className="muted" style={{ fontSize: 12 }}>
                {s.ip} · expires {new Date(s.expires_at).toLocaleString()}
              </div>
            </div>
            <button className="danger small" onClick={() => revokeSession(s.id)}>End session</button>
          </div>
        ))}
        {sessions.length === 0 && <p className="muted">No active sessions.</p>}
      </div>

      <div className="card col">
        <h3 style={{ marginTop: 0 }}>Notification settings</h3>
        <p className="muted">
          Control which notification families reach you. Chat-level mutes still
          override these at delivery time.
        </p>
        {Object.keys(NOTIF_LABELS).map((k) => {
          return (
            <label key={k} className="row" style={{ justifyContent: "space-between", alignItems: "center" }}>
              <span>{NOTIF_LABELS[k]}</span>
              <input
                type="checkbox"
                checked={notif[k]}
                onChange={(e) => toggleNotif(k, e.target.checked)}
              />
            </label>
          );
        })}
      </div>

      <div className="card col">
        <h3 style={{ marginTop: 0 }}>Passkeys — fingerprint / face / device passcode</h3>
        <p className="muted">
          Passkeys use your device unlock (Touch ID, Face ID, Windows Hello, Android
          biometrics, or screen-lock PIN) to sign in without a password. Biometric
          data never leaves your device.
        </p>
        {passkeySupported() ? (
          <>
            <div className="row">
              <input
                placeholder="Key name (e.g. MacBook Touch ID)"
                value={pkName}
                onChange={(e) => setPkName(e.target.value)}
                maxLength={60}
              />
              <button onClick={addPasskey}>Add passkey</button>
            </div>
            {passkeys.map((k) => (
              <div key={k.id} className="row" style={{ alignItems: "center" }}>
                <div>
                  <div>{k.name}</div>
                  <div className="muted" style={{ fontSize: 12 }}>
                    added {new Date(k.created_at).toLocaleDateString()}
                    {k.last_used_at && ` · last used ${new Date(k.last_used_at).toLocaleDateString()}`}
                    {k.transports.includes("internal") && " · this device"}
                  </div>
                </div>
                <div className="spacer" />
                <button className="danger small" onClick={() => removePasskey(k.id)}>
                  Remove
                </button>
              </div>
            ))}
            {passkeys.length === 0 && <p className="muted">No passkeys yet.</p>}
          </>
        ) : (
          <p className="muted">This browser does not support passkeys.</p>
        )}
      </div>

      <AccountSafety />
      <ScreenTimePanel />
    </div>
  );
}
