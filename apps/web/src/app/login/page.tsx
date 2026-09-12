"use client";

import { useMemo, useState } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { api, saveTokens, Tokens } from "@/lib/api";
import { useI18n } from "@/lib/i18n";
import GoogleSignIn from "@/components/GoogleSignIn";
import QRLogin from "@/components/QRLogin";
import CountryPicker from "@/components/CountryPicker";
import { loginWithPasskey, passkeySupported } from "@/lib/passkey";
import { startGuestSession } from "@/lib/api";

const EMAIL_RE = /^[^\s@]+@[^\s@]+\.[^\s@]{2,}$/;

// Identity spec §2: unified smart input — one field auto-detects email vs phone
// in real time (no toggle/switch/dropdown mode selector).
function detectMode(value: string): "email" | "phone" | "unknown" {
  const v = value.trim();
  if (!v) return "unknown";
  if (/^[+0-9][0-9()\-.\s]*$/.test(v) && /\d/.test(v)) return "phone";
  if (v.includes("@") || /[a-zA-Z]/.test(v)) return "email";
  return "phone";
}

export default function LoginPage() {
  const { t } = useI18n();
  const router = useRouter();
  const [identifier, setIdentifier] = useState("");
  const [password, setPassword] = useState("");
  const [totpCode, setTotpCode] = useState("");
  const [needs2FA, setNeeds2FA] = useState(false);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [showQR, setShowQR] = useState(false);
  const [rememberMe, setRememberMe] = useState(true);
  const [showPassword, setShowPassword] = useState(false);
  const [dial, setDial] = useState("+1");
  const [countryISO, setCountryISO] = useState("US");
  const [phoneLocal, setPhoneLocal] = useState("");

  const mode = useMemo(() => detectMode(identifier), [identifier]);
  const isPhone = mode === "phone";

  // Auto-sync a typed international phone (with +…) into the dial + local fields.
  const handleIdentifier = (raw: string) => {
    if (/^\+[0-9]{1,15}$/.test(raw.replace(/[\s().\-]/g, ""))) {
      const digits = raw.replace(/[\s().\-]/g, "");
      for (const d of ["+1", "+44", "+91", "+86", "+49", "+33", "+81", "+7"]) {
        if (digits.startsWith(d)) {
          setDial(d);
          setPhoneLocal(digits.slice(d.length));
          break;
        }
      }
    }
    setIdentifier(raw);
  };

  const passkeyLogin = async () => {
    setBusy(true);
    setError("");
    try {
      await loginWithPasskey(identifier, totpCode || undefined);
      router.push("/");
      router.refresh();
    } catch (err) {
      const msg = err instanceof Error ? err.message : t("error");
      if (msg === "totp_required") {
        setNeeds2FA(true);
        setError("Enter the 6-digit code from your authenticator app, then try again");
      } else {
        setError(msg);
      }
    } finally {
      setBusy(false);
    }
  };

  const continueAsGuest = async () => {
    setBusy(true);
    setError("");
    try {
      await startGuestSession();
      router.push("/");
      router.refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : t("error"));
    } finally {
      setBusy(false);
    }
  };

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      // Identity spec §2: backend receives an explicit type flag (email/phone).
      const finalIdentifier = isPhone
        ? (phoneLocal ? `${dial}${phoneLocal.replace(/\D/g, "")}` : identifier.replace(/[\s().\-]/g, ""))
        : identifier.trim();
      const tokens = await api<Tokens>(
        "/api/auth/login",
        { method: "POST", body: JSON.stringify({ identifier: finalIdentifier, type: isPhone ? "phone" : "email", password, totp_code: totpCode }) },
        false
      );
      const username = finalIdentifier.includes("@") ? undefined : finalIdentifier;
      saveTokens(tokens, username, rememberMe);
      router.push("/");
      router.refresh();
    } catch (err) {
      const msg = err instanceof Error ? err.message : t("error");
      if (msg === "totp_required") {
        setNeeds2FA(true);
        setError("Enter the 6-digit code from your authenticator app");
      } else {
        setError(msg);
      }
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="card" style={{ maxWidth: 420, margin: "40px auto" }}>
      <h2>{t("login")}</h2>
      <form onSubmit={submit} className="col">
        <div>
          <label>{t("username")} / {t("email")} / {t("phone")}</label>
          <input
            value={identifier}
            onChange={(e) => handleIdentifier(e.target.value)}
            placeholder="you@example.com  +41 55 555 2671"
            inputMode={isPhone ? "tel" : "email"}
            autoComplete="email"
            required
          />
          <p className="muted" style={{ fontSize: 12 }}>
            {isPhone ? "Phone mode — country flag applies automatically" : "Email mode — RFC-style validation applies"}
          </p>
        </div>
        {isPhone && (
          <>
            <CountryPicker value={dial} onChange={(d, iso) => { setDial(d); setCountryISO(iso); }} />
            <div>
              <label>{t("phone")} (local)</label>
              <div className="row">
                <span className="badge">{dial}</span>
                <input
                  value={phoneLocal}
                  onChange={(e) => setPhoneLocal(e.target.value)}
                  placeholder="4155552671"
                  inputMode="tel"
                />
              </div>
            </div>
          </>
        )}
        <div>
          <label>{t("password")}</label>
          <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
            <input
              type={showPassword ? "text" : "password"}
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              required
              style={{ flex: 1 }}
            />
            <button
              type="button"
              className="secondary"
              onClick={() => setShowPassword((v) => !v)}
              aria-label={showPassword ? "Hide password" : "Show password"}
              title={showPassword ? "Hide password" : "Show password"}
            >
              {showPassword ? "🙈" : "👁️"}
            </button>
          </div>
        </div>
        <label style={{ display: "flex", alignItems: "center", gap: 8, fontSize: 14 }}>
          <input
            type="checkbox"
            checked={rememberMe}
            onChange={(e) => setRememberMe(e.target.checked)}
          />
          {t("rememberMe")}
        </label>
        {needs2FA && (
          <div>
            <label>2FA code</label>
            <input
              value={totpCode}
              onChange={(e) => setTotpCode(e.target.value)}
              maxLength={6}
              inputMode="numeric"
              required
            />
          </div>
        )}
        {error && <div className="error-text">{error}</div>}
        <button type="submit" disabled={busy}>{busy ? t("loading") : t("login")}</button>
      </form>
      <div className="col" style={{ marginTop: 12 }}>
        <GoogleSignIn totpCode={totpCode} />
        {passkeySupported() && (
          <button
            className="secondary"
            onClick={passkeyLogin}
            disabled={busy || !identifier.trim()}
            title={!identifier.trim() ? "Enter your username first" : undefined}
          >
            🔑 Sign in with passkey (fingerprint / face / PIN)
          </button>
        )}
        <button className="secondary" onClick={() => setShowQR((v) => !v)}>
          {showQR ? "Hide QR code" : "📱 Log in by QR code"}
        </button>
        {showQR && <QRLogin />}
      </div>
      <div className="col" style={{ marginTop: 16, borderTop: "1px solid var(--border)", paddingTop: 16 }}>
        <button className="secondary" onClick={continueAsGuest} disabled={busy}>
          {t("continueWithoutAccount")}
        </button>
        <p className="muted" style={{ fontSize: 12 }}>{t("guestHint")}</p>
      </div>
      <p className="muted">
        <Link href="/forgot-password">{t("forgotPassword")}</Link>
        {" · "}
        <Link href="/register">{t("register")}</Link>
        {" · "}
        <Link href="/">{t("backToHome")}</Link>
      </p>
    </div>
  );
}
