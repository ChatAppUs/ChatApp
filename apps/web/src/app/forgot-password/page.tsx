"use client";

import { useMemo, useState } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { api } from "@/lib/api";
import { useI18n } from "@/lib/i18n";

const EMAIL_RE = /^[^\s@]+@[^\s@]+\.[^\s@]{2,}$/;

export default function ForgotPasswordPage() {
  const { t } = useI18n();
  const router = useRouter();
  const [identifier, setIdentifier] = useState("");
  const [sent, setSent] = useState(false);
  const [devToken, setDevToken] = useState("");
  const [error, setError] = useState("");

  const isPhone = useMemo(() => {
    const v = identifier.trim();
    return /^\+?[0-9][0-9()\-.\s]*$/.test(v) && /\d/.test(v);
  }, [identifier]);
  const identifierValid = isPhone ? identifier.replace(/\D/g, "").length >= 7 : EMAIL_RE.test(identifier.trim());

  // §5: real-time account-existence check; unknown identifiers are redirected
  // to Sign Up instead of a dead end.
  const checkAndContinue = async (e: React.FormEvent) => {
    e.preventDefault();
    setError("");
    const id = identifier.trim();
    if (!id) return;
    try {
      const res = await api<{ exists: boolean }>(
        "/api/auth/identifier/check",
        { method: "POST", body: JSON.stringify({ identifier: id }) },
        false
      );
      if (!res.exists) {
        router.push(`/register?identifier=${encodeURIComponent(id)}&reason=not_found`);
        return;
      }
    } catch {
      // Probe unavailable: fall through to the reset request itself, which
      // still keeps the response ambiguous.
    }
    await sendReset();
  };

  const sendReset = async () => {
    setError("");
    try {
      const res = await api<{ dev_reset_token?: string }>(
        "/api/auth/forgot-password",
        { method: "POST", body: JSON.stringify({ identifier: identifier.trim() }) },
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
        <form onSubmit={checkAndContinue} className="col">
          <div>
            <label>{t("email")} / {t("phone")}</label>
            <input
              value={identifier}
              onChange={(e) => setIdentifier(e.target.value)}
              placeholder="you@example.com  +41 55 555 2671"
              inputMode={isPhone ? "tel" : "email"}
              required
            />
            <p className="muted" style={{ fontSize: 12 }}>
              {isPhone ? "Phone mode — reset link goes to your account email" : "Email mode — RFC-style validation applies"}
            </p>
          </div>
          {error && <div className="error-text">{error}</div>}
          <button type="submit" disabled={!identifierValid}>{t("sendResetLink")}</button>
        </form>
      )}
    </div>
  );
}
