"use client";

import { useState } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { api, saveTokens, Tokens } from "@/lib/api";
import { useI18n } from "@/lib/i18n";

export default function LoginPage() {
  const { t } = useI18n();
  const router = useRouter();
  const [identifier, setIdentifier] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      const tokens = await api<Tokens>(
        "/api/auth/login",
        { method: "POST", body: JSON.stringify({ identifier, password }) },
        false
      );
      saveTokens(tokens);
      router.push("/");
      router.refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : t("error"));
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
          <input value={identifier} onChange={(e) => setIdentifier(e.target.value)} required />
        </div>
        <div>
          <label>{t("password")}</label>
          <input type="password" value={password} onChange={(e) => setPassword(e.target.value)} required />
        </div>
        {error && <div className="error-text">{error}</div>}
        <button type="submit" disabled={busy}>{busy ? t("loading") : t("login")}</button>
      </form>
      <p className="muted">
        <Link href="/forgot-password">{t("forgotPassword")}</Link>
        {" · "}
        <Link href="/register">{t("register")}</Link>
      </p>
    </div>
  );
}
