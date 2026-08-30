"use client";

import { useState } from "react";
import { api } from "@/lib/api";

export default function RecoverPage() {
  const [username, setUsername] = useState("");
  const [codes, setCodes] = useState(["", ""]);
  const [resetToken, setResetToken] = useState("");
  const [requested, setRequested] = useState(false);
  const [error, setError] = useState("");

  const request = async () => {
    setError("");
    try {
      await api("/api/recovery/trusted/request", {
        method: "POST", body: JSON.stringify({ username: username.trim() }),
      }, false);
      setRequested(true);
    } catch (e) {
      setError(e instanceof Error ? e.message : "request failed");
    }
  };

  const redeem = async () => {
    setError("");
    try {
      const d = await api<{ reset_token: string }>("/api/recovery/trusted/redeem", {
        method: "POST",
        body: JSON.stringify({ username: username.trim(), codes: codes.map((c) => c.trim()).filter(Boolean) }),
      }, false);
      setResetToken(d.reset_token);
    } catch (e) {
      setError(e instanceof Error ? e.message : "invalid codes");
    }
  };

  return (
    <div className="col" style={{ maxWidth: 480, margin: "40px auto" }}>
      <div className="card col">
        <h2 style={{ marginTop: 0 }}>Recover your account</h2>
        <p className="muted">
          If you set up trusted contacts, ask at least two of them for their recovery codes
          (they&apos;ll find them in Settings → Trusted contacts).
        </p>
        <input placeholder="Your username" value={username} onChange={(e) => setUsername(e.target.value)} />
        {!requested ? (
          <button onClick={request} disabled={!username.trim()}>Request recovery codes</button>
        ) : (
          <>
            <p>Codes were sent to your trusted contacts. Enter at least two:</p>
            {codes.map((c, i) => (
              <div key={i} className="row" style={{ gap: 6 }}>
                <input placeholder={`Code ${i + 1} (e.g. XXXXX-XXXXX)`} value={c}
                  onChange={(e) => setCodes(codes.map((x, j) => (j === i ? e.target.value : x)))} />
                {codes.length > 2 && (
                  <button className="danger small" onClick={() => setCodes(codes.filter((_, j) => j !== i))}>×</button>
                )}
              </div>
            ))}
            {codes.length < 5 && (
              <button className="secondary small" onClick={() => setCodes([...codes, ""])}>Add another code</button>
            )}
            <button onClick={redeem}>Unlock account</button>
          </>
        )}
        {resetToken && (
          <div className="badge green">
            Unlocked! <a href={`/reset-password?token=${resetToken}`}>Set a new password</a>
          </div>
        )}
        {error && <div className="error">{error}</div>}
      </div>
    </div>
  );
}
