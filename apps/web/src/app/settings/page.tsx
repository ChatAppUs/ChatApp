"use client";

import { useEffect, useState } from "react";
import { api } from "@/lib/api";
import { passkeySupported, registerPasskey } from "@/lib/passkey";
import AccountSafety from "@/components/AccountSafety";

type Passkey = {
  id: string;
  name: string;
  transports: string[];
  created_at: string;
  last_used_at: string | null;
};

export default function SettingsPage() {
  const [secret, setSecret] = useState("");
  const [otpauth, setOtpauth] = useState("");
  const [code, setCode] = useState("");
  const [status, setStatus] = useState("");
  const [error, setError] = useState("");
  const [passkeys, setPasskeys] = useState<Passkey[]>([]);
  const [pkName, setPkName] = useState("");

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
  }, []);

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
      await api("/api/auth/2fa/enable", {
        method: "POST",
        body: JSON.stringify({ code }),
      });
      setStatus("Two-factor authentication is now ON. You will need your authenticator code at every login.");
      setSecret("");
      setOtpauth("");
      setCode("");
    } catch (e) {
      setError(e instanceof Error ? e.message : "invalid code");
    }
  };

  const disable = async () => {
    setError("");
    try {
      await api("/api/auth/2fa/disable", {
        method: "POST",
        body: JSON.stringify({ code }),
      });
      setStatus("Two-factor authentication disabled.");
      setCode("");
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
        <h3>Disable 2FA</h3>
        <input
          placeholder="Current 6-digit code"
          value={code}
          onChange={(e) => setCode(e.target.value)}
          maxLength={6}
          inputMode="numeric"
        />
        <button className="danger" onClick={disable}>Disable 2FA</button>
        {error && <div className="error">{error}</div>}
        {status && <div className="badge green">{status}</div>}
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
    </div>
  );
}
