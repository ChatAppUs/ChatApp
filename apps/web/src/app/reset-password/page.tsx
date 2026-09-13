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
	const [totpCode, setTotpCode] = useState("");
	const [needsTOTP, setNeedsTOTP] = useState(false);
	const [done, setDone] = useState(false);
  const [error, setError] = useState("");

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError("");
    try {
		await api<{ status: string }>(
			"/api/auth/reset-password",
			{ method: "POST", body: JSON.stringify({ token, new_password: password, totp_code: totpCode }) },
			false
		);
		setDone(true);
	} catch (err) {
		const message = err instanceof Error ? err.message : t("error");
		if (message === "totp_required") setNeedsTOTP(true);
		setError(message);
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
			{needsTOTP && (
				<div>
					<label>Authenticator code</label>
					<input value={totpCode} onChange={(e) => setTotpCode(e.target.value)} inputMode="numeric" maxLength={6} autoComplete="one-time-code" required />
				</div>
			)}
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
