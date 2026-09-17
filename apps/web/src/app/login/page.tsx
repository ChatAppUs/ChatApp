"use client";

import { useMemo, useState } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { api, saveTokens, Tokens } from "@/lib/api";
import { useI18n } from "@/lib/i18n";
import GoogleSignIn from "@/components/GoogleSignIn";
import AppleSignIn from "@/components/AppleSignIn";
import QRLogin from "@/components/QRLogin";
import CountryPicker from "@/components/CountryPicker";
import { loginWithPasskey, passkeySupported } from "@/lib/passkey";
import { startGuestSession } from "@/lib/api";

function detectMode(value: string): "email" | "phone" | "unknown" {
  const v = value.trim();
  if (!v) return "unknown";
  if (/^[+0-9][0-9()\-.\s]*$/.test(v) && /\d/.test(v)) return "phone";
  if (v.includes("@") || /[a-zA-Z]/.test(v)) return "email";
  return "phone";
}

function normalisePhone(value: string): string {
  const trimmed = value.trim();
  if (!trimmed) return "";
  const hasPlus = trimmed.startsWith("+");
  const digits = trimmed.replace(/\D/g, "");
  return hasPlus ? `+${digits}` : digits;
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
  const [deviceToken] = useState(() => typeof window === "undefined" ? "" : localStorage.getItem("chatapp.deviceToken") ?? "");
  const [dial, setDial] = useState("+1");
  const [countryISO, setCountryISO] = useState("US");
  const mode = useMemo(() => detectMode(identifier), [identifier]);
  const isPhone = mode === "phone";

  const handleIdentifier = (raw: string) => setIdentifier(raw);
  const handleCountryChange = (nextDial: string, iso: string) => {
    setDial(nextDial); setCountryISO(iso);
    if (!isPhone) return;
    const raw = normalisePhone(identifier);
    const local = raw.replace(/^\+\d+/, "");
    setIdentifier(`${nextDial}${local}`);
  };

  const passkeyLogin = async () => {
    setBusy(true); setError("");
    try { await loginWithPasskey(identifier, totpCode || undefined); router.push("/"); router.refresh(); }
    catch (err) { const msg = err instanceof Error ? err.message : t("error"); if (msg === "totp_required") { setNeeds2FA(true); setError("Enter the 6-digit code from your authenticator app, then try again"); } else setError(msg); }
    finally { setBusy(false); }
  };

  const continueAsGuest = async () => {
    setBusy(true); setError("");
    try { await startGuestSession(); router.push("/"); router.refresh(); }
    catch (err) { setError(err instanceof Error ? err.message : t("error")); }
    finally { setBusy(false); }
  };

  const trustedLogin = async () => {
    setBusy(true); setError("");
    try {
      const tokens = await api<Tokens>("/api/auth/trusted-device/login", { method: "POST", body: JSON.stringify({ device_token: deviceToken, totp_code: totpCode }) }, false);
      saveTokens(tokens, undefined, rememberMe); router.push("/"); router.refresh();
    } catch (err) {
      const msg = err instanceof Error ? err.message : t("error");
      if (msg === "totp_required") { setNeeds2FA(true); setError("Enter the 6-digit code from your authenticator app"); }
      else { localStorage.removeItem("chatapp.deviceToken"); setError(msg); }
    } finally { setBusy(false); }
  };

  const submit = async (e: React.FormEvent) => {
    e.preventDefault(); setBusy(true); setError("");
    try {
      const typedPhone = normalisePhone(identifier);
      const finalIdentifier = isPhone && typedPhone && !typedPhone.startsWith("+") ? `${dial}${typedPhone}` : (isPhone ? typedPhone : identifier.trim());
      const probe = await api<{ exists: boolean }>("/api/auth/identifier/check", { method: "POST", body: JSON.stringify({ identifier: finalIdentifier }) }, false);
      if (!probe.exists) { router.push(`/register?identifier=${encodeURIComponent(finalIdentifier)}&reason=not_found`); return; }
      const tokens = await api<Tokens>("/api/auth/login", {
        method: "POST",
        body: JSON.stringify({ identifier: finalIdentifier, type: isPhone ? "phone" : "email", country: isPhone ? countryISO : undefined, password, totp_code: totpCode }),
      }, false);
      const username = finalIdentifier.includes("@") ? undefined : finalIdentifier;
      saveTokens(tokens, username, rememberMe);
      if (rememberMe) {
        try {
          const enrolled = await api<{ device_token: string }>("/api/auth/trusted-device/enroll", { method: "POST", body: JSON.stringify({ device_name: navigator.userAgent.slice(0, 80) }) });
          localStorage.setItem("chatapp.deviceToken", enrolled.device_token);
        } catch { }
      }
      router.push("/"); router.refresh();
    } catch (err) {
      const msg = err instanceof Error ? err.message : t("error");
      if (msg === "totp_required") { setNeeds2FA(true); setError("Enter the 6-digit code from your authenticator app"); } else setError(msg);
    } finally { setBusy(false); }
  };

  return (
    <div className="card" style={{ maxWidth: 420, margin: "40px auto" }}>
      <h2>{t("login")}</h2>
      <form onSubmit={submit} className="col">
        <div>
          <label>{t("username")} / {t("email")} / {t("phone")}</label>
          <input value={identifier} onChange={(e) => handleIdentifier(e.target.value)} placeholder="you@example.com  +41 55 555 2671" inputMode={isPhone ? "tel" : "email"} autoComplete="username" required />
          <p className="muted" style={{ fontSize: 12 }}>{isPhone ? "Phone mode — choose a country code without changing this input" : "Email mode — RFC-style validation applies"}</p>
        </div>
        {isPhone && <CountryPicker value={dial} onChange={handleCountryChange} />}
        <div>
          <label>{t("password")}</label>
          <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
            <input type={showPassword ? "text" : "password"} value={password} onChange={(e) => setPassword(e.target.value)} required style={{ flex: 1 }} />
            <button type="button" className="secondary" onClick={() => setShowPassword((v) => !v)} aria-label={showPassword ? "Hide password" : "Show password"} title={showPassword ? "Hide password" : "Show password"}>{showPassword ? "🙈" : "👁️"}</button>
          </div>
        </div>
        <label style={{ display: "flex", alignItems: "center", gap: 8, fontSize: 14 }}><input type="checkbox" checked={rememberMe} onChange={(e) => setRememberMe(e.target.checked)} />{t("rememberMe")}</label>
        {needs2FA && <div><label>2FA code</label><input value={totpCode} onChange={(e) => setTotpCode(e.target.value)} maxLength={6} inputMode="numeric" required /></div>}
        {error && <div className="error-text">{error}</div>}
        <button type="submit" disabled={busy}>{busy ? t("loading") : t("login")}</button>
      </form>
      <div className="col" style={{ marginTop: 12 }}>
        <GoogleSignIn totpCode={totpCode} /><AppleSignIn totpCode={totpCode} />
        {deviceToken && <button className="secondary" onClick={trustedLogin} disabled={busy}>🔏 Sign in without password (trusted device)</button>}
        {passkeySupported() && <button className="secondary" onClick={passkeyLogin} disabled={busy || !identifier.trim()} title={!identifier.trim() ? "Enter your username first" : undefined}>🔑 Sign in with passkey (fingerprint / face / PIN)</button>}
        <button className="secondary" onClick={() => setShowQR((v) => !v)}>{showQR ? "Hide QR code" : "📱 Log in by QR code"}</button>
        {showQR && <QRLogin />}
      </div>
      <div className="col" style={{ marginTop: 16, borderTop: "1px solid var(--border)", paddingTop: 16 }}>
        <button className="secondary" onClick={continueAsGuest} disabled={busy}>{t("continueWithoutAccount")}</button>
        <p className="muted" style={{ fontSize: 12 }}>{t("guestHint")}</p>
      </div>
      <p className="muted"><Link href="/forgot-password">{t("forgotPassword")}</Link>{" · "}<Link href="/register">{t("register")}</Link>{" · "}<Link href="/">{t("backToHome")}</Link></p>
    </div>
  );
}
