"use client";

import { Suspense, useState } from "react";
import Link from "next/link";
import { useSearchParams } from "next/navigation";
import { api } from "@/lib/api";
import { useI18n } from "@/lib/i18n";

function ResetPasswordForm() {
  const { t } = useI18n();
  const params = useSearchParams();
  const [token, setToken] = useState(params.get("token") ?? "");
  const [password, setPassword] = useState("");
  const [done, setDone] = useState(false);
  const [error, setError] = useState("");

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError("");
    try {
      await api(
        "/api/auth/reset-password",
        { method: "POST", body: JSON.stringify({ token, new_password: password }) },
        false
      );
      setDone(true);
    } catch (err) {
      setError(err instanceof Error ? err.message : t("error"));
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
      ) : (
        <form onSubmit={submit} className="col">
          <div>
            <label>Token</label>
            <input value={token} onChange={(e) => setToken(e.target.value)} required />
          </div>
          <div>
            <label>{t("newPassword")}</label>
            <input type="password" value={password} onChange={(e) => setPassword(e.target.value)} required minLength={8} />
          </div>
          {error && <div className="error-text">{error}</div>}
          <button type="submit">{t("resetPassword")}</button>
        </form>
      )}
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
