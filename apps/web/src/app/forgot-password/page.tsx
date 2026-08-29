"use client";

import { useState } from "react";
import Link from "next/link";
import { api } from "@/lib/api";
import { useI18n } from "@/lib/i18n";

export default function ForgotPasswordPage() {
  const { t } = useI18n();
  const [email, setEmail] = useState("");
  const [sent, setSent] = useState(false);
  const [devToken, setDevToken] = useState("");
  const [error, setError] = useState("");

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError("");
    try {
      const res = await api<{ dev_reset_token?: string }>(
        "/api/auth/forgot-password",
        { method: "POST", body: JSON.stringify({ email }) },
        false
      );
      setSent(true);
      if (res.dev_reset_token) setDevToken(res.dev_reset_token);
    } catch (err) {
      setError(err instanceof Error ? err.message : t("error"));
    }
  };

  return (
    <div className="card" style={{ maxWidth: 420, margin: "40px auto" }}>
      <h2>{t("forgotPassword")}</h2>
      {sent ? (
        <div className="col">
          <p className="success-text">{t("resetSent")}</p>
          {devToken && (
            <p className="muted">
              dev token: <code>{devToken}</code>
            </p>
          )}
          <Link href="/reset-password">{t("resetPassword")} →</Link>
        </div>
      ) : (
        <form onSubmit={submit} className="col">
          <div>
            <label>{t("email")}</label>
            <input type="email" value={email} onChange={(e) => setEmail(e.target.value)} required />
          </div>
          {error && <div className="error-text">{error}</div>}
          <button type="submit">{t("sendResetLink")}</button>
        </form>
      )}
    </div>
  );
}
