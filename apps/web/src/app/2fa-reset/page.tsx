"use client";

import { useMemo, useState } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { api, getAccessToken } from "@/lib/api";
import { useI18n } from "@/lib/i18n";

const EMAIL_RE = /^[^\s@]+@[^\s@]+\.[^\s@]{2,}$/;

// Identity spec §6 — 2FA Reset System. One unified smart input; email AND
// phone OTP; KYC face-match with a 5-second live capture; the old 2FA is
// removed server-side and a fresh authenticator enrolment begins immediately.
export default function TwoFAResetPage() {
  const { t } = useI18n();
  const router = useRouter();
  const [identifier, setIdentifier] = useState("");
  const [step, setStep] = useState<"input" | "verify" | "selfie" | "re-enrol" | "done">("input");
  const [emailCode, setEmailCode] = useState("");
  const [phoneCode, setPhoneCode] = useState("");
  const [selfieURL, setSelfieURL] = useState("");
  const [uploading, setUploading] = useState(false);
  const [newSecret, setNewSecret] = useState("");
  const [newCode, setNewCode] = useState("");
  const [recoveryCodes, setRecoveryCodes] = useState<string[]>([]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const isPhone = useMemo(() => {
    const v = identifier.trim();
    return /^\+?[0-9][0-9()\-.\s]*$/.test(v) && /\d/.test(v);
  }, [identifier]);
  const identifierValid = isPhone
    ? identifier.replace(/\D/g, "").length >= 7
    : EMAIL_RE.test(identifier.trim());

  const begin = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      await api(
        "/api/auth/2fa-reset/begin",
        { method: "POST", body: JSON.stringify({ identifier: identifier.trim() }) },
        false
      );
      setStep("verify");
    } catch (err) {
      setError(err instanceof Error ? err.message : t("error"));
    } finally {
      setBusy(false);
    }
  };

  const verify = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      await api(
        "/api/auth/2fa-reset/verify",
        {
          method: "POST",
          body: JSON.stringify({
            identifier: identifier.trim(),
            email_code: emailCode.trim(),
            phone_code: phoneCode.trim(),
          }),
        },
        false
      );
      setStep("selfie");
    } catch (err) {
      setError(err instanceof Error ? err.message : t("error"));
    } finally {
      setBusy(false);
    }
  };

  const captureSelfie = async (file: File) => {
    setUploading(true);
    setError("");
    try {
      const url = await (await import("@/lib/api")).uploadMedia(file);
      setSelfieURL(url);
    } catch (err) {
      setError(err instanceof Error ? err.message : "selfie upload failed");
    } finally {
      setUploading(false);
    }
  };

  const attest = async () => {
    setBusy(true);
    setError("");
    try {
      await api(
        "/api/auth/2fa-reset/verify",
        {
          method: "POST",
          body: JSON.stringify({
            identifier: identifier.trim(),
            email_code: emailCode.trim(),
            phone_code: phoneCode.trim(),
            selfie_url: selfieURL,
          }),
        },
        false
      );
      setStep("re-enrol");
    } catch (err) {
      const msg = err instanceof Error ? err.message : t("error");
      setError(
        msg.includes("face") || msg.includes("liveness")
          ? "Face match failed — retake the live capture with good lighting and try again"
          : msg
      );
    } finally {
      setBusy(false);
    }
  };

  const reEnrol = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      const access = getAccessToken();
      if (!access) {
        router.push("/login");
        return;
      }
      // Old 2FA is already removed server-side by /complete; start a fresh
      // enrolment immediately.
      await api("/api/auth/2fa-reset/complete", { method: "POST", body: "{}" });
      const setup = await api<{ secret: string }>("/api/auth/2fa/setup", { method: "POST" });
      setNewSecret(setup.secret);
      setStep("done");
    } catch (err) {
      setError(err instanceof Error ? err.message : t("error"));
    } finally {
      setBusy(false);
    }
  };

  const enableNew = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      const res = await api<{ recovery_codes?: string[] }>("/api/auth/2fa/enable", {
        method: "POST",
        body: JSON.stringify({ code: newCode.trim() }),
      });
      setRecoveryCodes(res.recovery_codes ?? []);
      setError("");
      alert("2FA reset complete. Sign in again with your new authenticator.");
      router.push("/login");
    } catch (err) {
      setError(err instanceof Error ? err.message : t("error"));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="card" style={{ maxWidth: 460, margin: "40px auto" }}>
      <h2>Reset 2FA</h2>
      <p className="muted" style={{ fontSize: 13 }}>
        Lost your authenticator? Verify both contacts, pass a live face check, and
        enrol a new device. Takes about two minutes.
      </p>

      {step === "input" && (
        <form onSubmit={begin} className="col">
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
          {error && <div className="error-text">{error}</div>}
          <button type="submit" disabled={busy || !identifierValid}>
            {busy ? t("loading") : "Send verification codes"}
          </button>
        </form>
      )}

      {step === "verify" && (
        <form onSubmit={verify} className="col">
          <div>
            <label>Email code</label>
            <input
              value={emailCode}
              onChange={(e) => setEmailCode(e.target.value.replace(/\D/g, ""))}
              maxLength={6}
              inputMode="numeric"
              required
            />
          </div>
          <div>
            <label>Phone code</label>
            <input
              value={phoneCode}
              onChange={(e) => setPhoneCode(e.target.value.replace(/\D/g, ""))}
              maxLength={6}
              inputMode="numeric"
              required
            />
          </div>
          <p className="muted" style={{ fontSize: 12 }}>
            Codes went to the email and phone on your account. They expire in 10 minutes.
          </p>
          {error && <div className="error-text">{error}</div>}
          <button type="submit" disabled={busy || emailCode.length < 6 || phoneCode.length < 6}>
            {busy ? t("loading") : "Verify both codes"}
          </button>
        </form>
      )}

      {step === "selfie" && (
        <div className="col">
          <p>
            Live face check — look at the camera and hold still for 5 seconds.
            We compare the capture with your verified KYC document.
          </p>
          <label className="secondary" style={{ cursor: "pointer" }}>
            {selfieURL ? "Retake live capture" : uploading ? "Uploading…" : "Capture 5-second selfie"}
            <input
              type="file"
              accept="image/*"
              capture="user"
              style={{ display: "none" }}
              disabled={uploading}
              onChange={(e) => {
                const f = e.target.files?.[0];
                if (f) captureSelfie(f);
              }}
            />
          </label>
          {selfieURL && <p className="success-text">Live capture ready</p>}
          {error && <div className="error-text">{error}</div>}
          <button onClick={attest} disabled={busy || !selfieURL}>
            {busy ? t("loading") : "Verify my identity"}
          </button>
        </div>
      )}

      {step === "re-enrol" && (
        <form onSubmit={reEnrol} className="col">
          <p className="success-text">
            Identity verified. Your old 2FA has been removed — now enrol a new authenticator.
          </p>
          {error && <div className="error-text">{error}</div>}
          <button type="submit" disabled={busy}>
            {busy ? t("loading") : "Set up new authenticator"}
          </button>
        </form>
      )}

      {step === "done" && (
        <form onSubmit={enableNew} className="col">
          <p>Scan this secret into your authenticator app:</p>
          <code style={{ wordBreak: "break-all", userSelect: "all" }}>{newSecret}</code>
          <p className="muted" style={{ fontSize: 12 }}>
            Or enter it manually (base32). Then confirm the 6-digit code below.
          </p>
          <div>
            <label>6-digit code from the new device</label>
            <input
              value={newCode}
              onChange={(e) => setNewCode(e.target.value.replace(/\D/g, ""))}
              maxLength={6}
              inputMode="numeric"
              required
            />
          </div>
          {recoveryCodes.length > 0 && (
            <div>
              <p className="success-text">New recovery codes (shown once — store them safely):</p>
              <code style={{ wordBreak: "break-all" }}>{recoveryCodes.join(" · ")}</code>
            </div>
          )}
          {error && <div className="error-text">{error}</div>}
          <button type="submit" disabled={busy || newCode.length < 6}>
            {busy ? t("loading") : "Enable new 2FA"}
          </button>
        </form>
      )}

      <p className="muted" style={{ marginTop: 12 }}>
        <Link href="/login">{t("login")}</Link>
        {" · "}
        <Link href="/forgot-password">{t("forgotPassword")}</Link>
      </p>
    </div>
  );
}
