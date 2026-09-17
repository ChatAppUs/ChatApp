"use client";

import { Suspense, useMemo, useState } from "react";
import Link from "next/link";
import { useSearchParams } from "next/navigation";
import { api } from "@/lib/api";
import { useI18n } from "@/lib/i18n";

const EMAIL_RE = /^[^\s@]+@[^\s@]+\.[^\s@]{2,}$/;

type VerifyMethod = "email_otp" | "phone_otp" | "totp";

function ResetPasswordForm() {
  const { t } = useI18n();
  const params = useSearchParams();

  // A token arriving via the emailed link skips verification; otherwise the
  // user proves control of the account through email/phone OTP or 2FA (§5).
  const [token, setToken] = useState(params.get("token") ?? "");
  const [identifier, setIdentifier] = useState("");
  const [method, setMethod] = useState<VerifyMethod>("email_otp");
  const [verifyCode, setVerifyCode] = useState("");
  const [password, setPassword] = useState("");
  const [confirmPassword, setConfirmPassword] = useState("");
  const [showPassword, setShowPassword] = useState(false);
  const [totpCode, setTotpCode] = useState("");
  const [needsTOTP, setNeedsTOTP] = useState(false);
  const [done, setDone] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const isPhone = useMemo(() => {
    const v = identifier.trim();
    return /^\+?[0-9][0-9()\-.\s]*$/.test(v) && /\d/.test(v);
  }, [identifier]);
  const identifierValid = isPhone
    ? identifier.replace(/\D/g, "").length >= 7
    : EMAIL_RE.test(identifier.trim());
  const hasToken = token.trim().length > 0;

  // §5: exchange a verification code (email OTP / phone OTP / 2FA code) for a
  // single-use reset token. Password reset then proceeds as with the link.
  const verifyIdentity = async () => {
    setBusy(true);
    setError("");
    try {
      const res = await api<{ reset_token: string }>(
        "/api/auth/reset/verify",
        {
          method: "POST",
          body: JSON.stringify({
            identifier: identifier.trim(),
            method,
            code: verifyCode.trim(),
          }),
        },
        false
      );
      setToken(res.reset_token);
    } catch (err) {
      setError(err instanceof Error ? err.message : t("error"));
    } finally {
      setBusy(false);
    }
  };

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError("");
    if (password !== confirmPassword) {
      setError("Passwords do not match");
      return;
    }
    setBusy(true);
    try {
      await api<{ status: string }>(
        "/api/auth/reset-password",
        {
          method: "POST",
          body: JSON.stringify({ token, new_password: password, totp_code: totpCode }),
        },
        false
      );
      setDone(true);
    } catch (err) {
      const message = err instanceof Error ? err.message : t("error");
      if (message === "totp_required") setNeedsTOTP(true);
      setError(message);
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="card" style={{ maxWidth: 420, margin: "40px auto" }}>
      <h2>{t("resetPassword")}</h2>
      {done ? (
        <div className="col">
          <p className="success-text">{t("passwordUpdated")}</p>
          <Link href="/login">{t("login")} →</Link>
        </div>
      ) : !hasToken ? (
        <div className="col">
          <p className="muted" style={{ fontSize: 13 }}>
            Prove it is your account with an email code, a phone code, or your
            authenticator — or open the link we emailed you.
          </p>
          <form
            onSubmit={(e) => {
              e.preventDefault();
              verifyIdentity();
            }}
            className="col"
          >
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
                {isPhone ? "Phone mode detected" : "Email mode detected"}
              </p>
            </div>
            <div>
              <label>Verification method</label>
              <select value={method} onChange={(e) => setMethod(e.target.value as VerifyMethod)}>
                <option value="email_otp">Email OTP (6-digit code)</option>
                <option value="phone_otp">Phone OTP (6-digit code)</option>
                <option value="totp">2FA authenticator / recovery code</option>
              </select>
            </div>
            {method !== "totp" && (
              <div>
                <label>6-digit code</label>
                <input
                  value={verifyCode}
                  onChange={(e) => setVerifyCode(e.target.value.replace(/\D/g, ""))}
                  maxLength={6}
                  inputMode="numeric"
                  autoComplete="one-time-code"
                  required
                />
              </div>
            )}
            {method === "totp" && (
              <div>
                <label>Authenticator or recovery code</label>
                <input
                  value={verifyCode}
                  onChange={(e) => setVerifyCode(e.target.value)}
                  autoComplete="one-time-code"
                  required
                />
              </div>
            )}
            {error && <div className="error-text">{error}</div>}
            <button type="submit" disabled={busy || !identifierValid || !verifyCode.trim()}>
              {busy ? t("loading") : "Verify & continue"}
            </button>
          </form>
          <p className="muted" style={{ fontSize: 13 }}>
            Have a reset link?{" "}
            <a
              href="#"
              onClick={(e) => {
                e.preventDefault();
                const tok = window.prompt("Paste the reset token from your email:");
                if (tok) setToken(tok.trim());
              }}
            >
              Enter it here
            </a>
          </p>
          <p className="muted" style={{ fontSize: 12 }}>
            Tip: codes arrive only if the contact matches your account. If nothing
            arrives, check the identifier you typed.
          </p>
        </div>
      ) : (
        <form onSubmit={submit} className="col">
          <div>
            <label>Reset token</label>
            <input value={token} onChange={(e) => setToken(e.target.value)} required />
          </div>
          <div>
            <label>{t("newPassword")}</label>
            <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
              <input
                type={showPassword ? "text" : "password"}
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                required
                minLength={8}
                style={{ flex: 1 }}
              />
              <button
                type="button"
                className="secondary"
                onClick={() => setShowPassword((v) => !v)}
                aria-label={showPassword ? "Hide password" : "Show password"}
              >
                {showPassword ? "🙈" : "👁️"}
              </button>
            </div>
          </div>
          <div>
            <label>Confirm new password</label>
            <input
              type="password"
              value={confirmPassword}
              onChange={(e) => setConfirmPassword(e.target.value)}
              required
              minLength={8}
            />
            {confirmPassword && confirmPassword !== password && (
              <p className="error-text">Passwords do not match</p>
            )}
          </div>
          {needsTOTP && (
            <div>
              <label>Authenticator or recovery code</label>
              <input
                value={totpCode}
                onChange={(e) => setTotpCode(e.target.value)}
                inputMode="text"
                autoComplete="one-time-code"
                placeholder="6-digit TOTP or one-time recovery code"
                required
              />
            </div>
          )}
          {error && <div className="error-text">{error}</div>}
          <button type="submit" disabled={busy}>{busy ? t("loading") : t("resetPassword")}</button>
          <button
            type="button"
            className="secondary"
            onClick={() => {
              setToken("");
              setVerifyCode("");
              setError("");
            }}
          >
            Use a different verification method
          </button>
        </form>
      )}
      <p className="muted" style={{ marginTop: 12 }}>
        <Link href="/login">{t("login")}</Link>
        {" · "}
        <Link href="/register">{t("register")}</Link>
        {" · "}
        <Link href="/2fa-reset">Reset 2FA</Link>
        {" · "}
        <Link href="/">{t("backToHome")}</Link>
      </p>
    </div>
  );
}

export default function ResetPasswordPage() {
  return (
    <Suspense fallback={null}>
      <ResetPasswordForm />
    </Suspense>
  );
}
