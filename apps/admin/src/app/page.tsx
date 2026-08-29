"use client";

import { FormEvent, useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { adminLogin, getAdminToken } from "@/lib/api";

export default function AdminLoginPage() {
  const router = useRouter();
  const [identifier, setIdentifier] = useState("");
  const [password, setPassword] = useState("");
  const [totp, setTotp] = useState("");
  const [needTotp, setNeedTotp] = useState(false);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    if (getAdminToken()) router.push("/dashboard");
  }, [router]);

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      await adminLogin(identifier, password, totp || undefined);
      router.push("/dashboard");
    } catch (err) {
      const msg = err instanceof Error ? err.message : "login failed";
      if (msg === "totp_required") {
        setNeedTotp(true);
        setError("Two-factor code required");
      } else {
        setError(msg);
      }
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="card" style={{ maxWidth: 400, margin: "48px auto" }}>
      <h3>Admin sign in</h3>
      <form onSubmit={submit}>
        <input
          placeholder="Username or email"
          value={identifier}
          onChange={(e) => setIdentifier(e.target.value)}
          autoComplete="username"
          required
        />
        <input
          type="password"
          placeholder="Password"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          autoComplete="current-password"
          required
        />
        {needTotp && (
          <input
            placeholder="2FA code"
            value={totp}
            onChange={(e) => setTotp(e.target.value)}
            inputMode="numeric"
            required
          />
        )}
        {error && <div className="error-text">{error}</div>}
        <button type="submit" disabled={busy} style={{ marginTop: 8 }}>
          {busy ? "…" : "Sign in"}
        </button>
      </form>
    </div>
  );
}
