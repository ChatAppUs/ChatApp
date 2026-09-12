"use client";

import { useMemo, useState } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { api, saveTokens, Tokens } from "@/lib/api";
import { startGuestSession } from "@/lib/api";
import { useI18n } from "@/lib/i18n";
import CountryPicker from "@/components/CountryPicker";
import GoogleSignIn from "@/components/GoogleSignIn";

const EMAIL_RE = /^[^\s@]+@[^\s@]+\.[^\s@]{2,}$/;

function detectMode(value: string): "email" | "phone" | "unknown" {
  const v = value.trim();
  if (!v) return "unknown";
  if (/^[+0-9][0-9()\-.\s]*$/.test(v) && /\d/.test(v)) return "phone";
  if (v.includes("@") || /[a-zA-Z]/.test(v)) return "email";
  return "phone";
}

// Identity spec §4.1: password strength indicator (Red Weak / Yellow Medium / Green Strong).
function passwordStrength(pw: string): { score: number; label: "weak" | "medium" | "strong" } {
  if (!pw) return { score: 0, label: "weak" };
  let s = 0;
  if (pw.length >= 8) s++;
  if (pw.length >= 12) s++;
  if (/[A-Z]/.test(pw) && /[a-z]/.test(pw)) s++;
  if (/\d/.test(pw)) s++;
  if (/[^A-Za-z0-9]/.test(pw)) s++;
  if (s <= 2) return { score: s, label: "weak" };
  if (s <= 3) return { score: s, label: "medium" };
  return { score: s, label: "strong" };
}

export default function RegisterPage() {
  const { t } = useI18n();
  const router = useRouter();
  const [form, setForm] = useState({
    username: "",
    display_name: "",
    identifier: "",
    password: "",
    confirmPassword: "",
    referral: "",
  });
  const [dial, setDial] = useState("+1");
  const [countryISO, setCountryISO] = useState("US");
  const [phoneLocal, setPhoneLocal] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [showPassword, setShowPassword] = useState(false);
  const [showConfirm, setShowConfirm] = useState(false);
  const [agreeTerms, setAgreeTerms] = useState(false);
  // OTP verification step (Identity spec §4: "Email/phone OTP verification is required").
  const [step, setStep] = useState<"details" | "otp">("details");
  const [otpCode, setOtpCode] = useState("");
  const [otpSent, setOtpSent] = useState(false);
  const [otpVerified, setOtpVerified] = useState(false);

  const mode = useMemo(() => detectMode(form.identifier), [form.identifier]);
  const isPhone = mode === "phone";
  const identifierValid = mode === "unknown" ? false : isPhone ? phoneLocal.length >= 7 : EMAIL_RE.test(form.identifier);
  const strength = useMemo(() => passwordStrength(form.password), [form.password]);
  const passwordsMatch = form.password === form.confirmPassword;
  const canSendOtp = identifierValid && !otpSent;
  const canVerifyOtp = otpCode.length === 6 && !otpVerified;

  const set = (k: keyof typeof form) => (e: React.ChangeEvent<HTMLInputElement>) =>
    setForm({ ...form, [k]: e.target.value });

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
    setForm({ ...form, identifier: raw });
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

  // Identity spec §4: send the 6-digit OTP to the email or phone before account creation.
  const sendOtp = async () => {
    setBusy(true);
    setError("");
    try {
      const phone = isPhone ? (phoneLocal ? `${dial}${phoneLocal.replace(/\D/g, "")}` : form.identifier.replace(/[\s().\-]/g, "")) : "";
      const email = isPhone ? "" : form.identifier.toLowerCase();
      const path = isPhone ? "/api/auth/phone/send-code" : "/api/auth/email/send-code";
      const body = isPhone ? { phone } : { email };
      await api(path, { method: "POST", body: JSON.stringify(body) }, false);
      setOtpSent(true);
      setStep("otp");
    } catch (err) {
      setError(err instanceof Error ? err.message : t("error"));
    } finally {
      setBusy(false);
    }
  };

  // Identity spec §4: verify the OTP; the backend marks the contact verified.
  const verifyOtp = async () => {
    setBusy(true);
    setError("");
    try {
      const phone = isPhone ? (phoneLocal ? `${dial}${phoneLocal.replace(/\D/g, "")}` : form.identifier.replace(/[\s().\-]/g, "")) : "";
      const email = isPhone ? "" : form.identifier.toLowerCase();
      const path = isPhone ? "/api/auth/phone/check-code" : "/api/auth/email/check-code";
      const body = isPhone ? { phone, code: otpCode } : { email, code: otpCode };
      await api(path, { method: "POST", body: JSON.stringify(body) }, false);
      setOtpVerified(true);
      setError("");
    } catch (err) {
      setError(err instanceof Error ? err.message : t("otpInvalid"));
    } finally {
      setBusy(false);
    }
  };

  const submit = async (ev: React.FormEvent) => {
    ev.preventDefault();
    setBusy(true);
    setError("");
    if (!passwordsMatch) {
      setError(t("passwordMismatch"));
      setBusy(false);
      return;
    }
    if (!agreeTerms) {
      setError(t("termsRequired"));
      setBusy(false);
      return;
    }
    if (!otpVerified) {
      setError(t("otpRequired"));
      setBusy(false);
      return;
    }
    try {
      const phone = isPhone ? (phoneLocal ? `${dial}${phoneLocal.replace(/\D/g, "")}` : form.identifier.replace(/[\s().\-]/g, "")) : "";
      const tokens = await api<Tokens>(
        "/api/auth/register",
        {
          method: "POST",
          body: JSON.stringify({
            username: form.username,
            display_name: form.display_name,
            email: isPhone ? "" : form.identifier.toLowerCase(),
            phone,
            phone_country: countryISO,
            password: form.password,
            referral_code: form.referral.trim() || undefined,
          }),
        },
        false
      );
      saveTokens(tokens, form.username);
      router.push("/");
      router.refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : t("error"));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="card" style={{ maxWidth: 460, margin: "40px auto" }}>
      <h2>{t("register")}</h2>
      <form onSubmit={submit} className="col">
        <div>
          <label>{t("username")}</label>
          <input value={form.username} onChange={set("username")} required pattern="[a-zA-Z0-9_]{3,30}" />
        </div>
        <div>
          <label>{t("displayName")}</label>
          <input value={form.display_name} onChange={set("display_name")} />
        </div>
        <div>
          <label>{t("email")} / {t("phone")}</label>
          <input
            value={form.identifier}
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

        {/* Identity spec §4: OTP verification step */}
        <div className="card" style={{ border: "1px solid var(--border)", padding: 12 }}>
          <h4 style={{ margin: 0 }}>{t("otpVerification")}</h4>
          {!otpSent ? (
            <button type="button" className="secondary" onClick={sendOtp} disabled={busy || !canSendOtp}>
              {t("sendCode")}
            </button>
          ) : (
            <>
              <p className="muted" style={{ fontSize: 12 }}>{t("otpSent")}</p>
              <div className="row">
                <input
                  value={otpCode}
                  onChange={(e) => setOtpCode(e.target.value)}
                  placeholder={t("code")}
                  maxLength={6}
                  inputMode="numeric"
                  disabled={otpVerified}
                  style={{ flex: 1 }}
                />
                <button type="button" className="secondary" onClick={verifyOtp} disabled={busy || !canVerifyOtp}>
                  {t("verifyCode")}
                </button>
              </div>
              {otpVerified && <p className="success-text" style={{ fontSize: 12 }}>✓ Verified</p>}
              <button type="button" className="secondary small" onClick={sendOtp} disabled={busy}>
                {t("otpResend")}
              </button>
            </>
          )}
        </div>

        <div>
          <label>{t("password")}</label>
          <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
            <input
              type={showPassword ? "text" : "password"}
              value={form.password}
              onChange={set("password")}
              required
              minLength={8}
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
          {form.password && (
            <div style={{ marginTop: 6 }}>
              <span className="muted" style={{ fontSize: 12 }}>{t("passwordStrength")}: </span>
              <span
                className="badge"
                style={{
                  background:
                    strength.label === "strong" ? "var(--success, #2e7d32)" :
                    strength.label === "medium" ? "#f9a825" : "#c62828",
                  color: "#fff",
                }}
              >
                {t(strength.label)}
              </span>
            </div>
          )}
        </div>
        <div>
          <label>{t("confirmPassword")}</label>
          <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
            <input
              type={showConfirm ? "text" : "password"}
              value={form.confirmPassword}
              onChange={set("confirmPassword")}
              required
              minLength={8}
              style={{ flex: 1 }}
            />
            <button
              type="button"
              className="secondary"
              onClick={() => setShowConfirm((v) => !v)}
              aria-label={showConfirm ? "Hide password" : "Show password"}
              title={showConfirm ? "Hide password" : "Show password"}
            >
              {showConfirm ? "🙈" : "👁️"}
            </button>
          </div>
          {form.confirmPassword && !passwordsMatch && (
            <p className="error-text" style={{ fontSize: 12 }}>{t("passwordMismatch")}</p>
          )}
        </div>
        <div>
          <label>{t("referralCode")}</label>
          <input value={form.referral} onChange={set("referral")} placeholder="CHATAPP-XXXX" />
        </div>
        <label style={{ display: "flex", alignItems: "center", gap: 8, fontSize: 14 }}>
          <input type="checkbox" checked={agreeTerms} onChange={(e) => setAgreeTerms(e.target.checked)} />
          {t("agreeToTerms")}
        </label>
        {error && <div className="error-text">{error}</div>}
        <button type="submit" disabled={busy || !identifierValid || !otpVerified || !passwordsMatch || !agreeTerms}>
          {busy ? t("loading") : t("register")}
        </button>
      </form>
      <div style={{ marginTop: 12 }}>
        <GoogleSignIn />
      </div>
      <div className="col" style={{ marginTop: 16, borderTop: "1px solid var(--border)", paddingTop: 16 }}>
        <button className="secondary" onClick={continueAsGuest} disabled={busy}>
          {t("continueWithoutAccount")}
        </button>
        <p className="muted" style={{ fontSize: 12 }}>{t("guestHint")}</p>
      </div>
      <p className="muted">
        <Link href="/login">{t("login")}</Link>
        {" · "}
        <Link href="/">{t("backToHome")}</Link>
      </p>
    </div>
  );
}