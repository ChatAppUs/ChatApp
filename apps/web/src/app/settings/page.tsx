"use client";

import { useState } from "react";
import { api } from "@/lib/api";

export default function SettingsPage() {
  const [secret, setSecret] = useState("");
  const [otpauth, setOtpauth] = useState("");
  const [code, setCode] = useState("");
  const [status, setStatus] = useState("");
  const [error, setError] = useState("");

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
    </div>
  );
}
