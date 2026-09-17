"use client";

// Identity spec §3.2/§4.2: landing page for Apple's form_post callback.
// Mobile users bounce into the native app via the chatapp:// deep link;
// everyone else completes the federated sign-in right here by handing the
// id_token to POST /api/auth/apple.

import { useEffect, useState, Suspense } from "react";
import Link from "next/link";
import { useSearchParams } from "next/navigation";
import { api, saveTokens } from "@/lib/api";
import { useI18n } from "@/lib/i18n";

function AppleCallbackInner() {
  const { t } = useI18n();
  const params = useSearchParams();
  const idToken = params.get("id_token") ?? "";
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [done, setDone] = useState(false);

  // Try the native app first: mobile browsers honour the custom scheme.
  useEffect(() => {
    if (!idToken) return;
    const target = new URL("chatapp://auth/apple");
    target.searchParams.set("id_token", idToken);
    window.location.href = target.toString();
  }, [idToken]);

  const finishInBrowser = async () => {
    setBusy(true);
    setError("");
    try {
      const tokens = await api<import("@/lib/api").Tokens>("/api/auth/apple", {
        method: "POST",
        body: JSON.stringify({ id_token: idToken, totp_code: "" }),
      });
      saveTokens(tokens, undefined, true);
      setDone(true);
      window.location.href = "/";
    } catch (e) {
      const msg = e instanceof Error ? e.message : "sign-in failed";
      setError(msg.includes("totp_required") ? "Enter your authenticator code in the app, then retry." : msg);
    } finally {
      setBusy(false);
    }
  };

  if (!idToken) {
    return (
      <div className="card" style={{ maxWidth: 420, margin: "80px auto", textAlign: "center" }}>
        <h1>Apple sign-in</h1>
        <p className="error-text">Missing identity token — start the sign-in again.</p>
        <Link href="/login">{t("backToLogin")}</Link>
      </div>
    );
  }

  return (
    <div className="card" style={{ maxWidth: 420, margin: "80px auto", textAlign: "center" }}>
      <h1>Apple sign-in</h1>
      <p className="muted">If ChatApp didn&apos;t open automatically, finish signing in here.</p>
      <button type="button" onClick={finishInBrowser} disabled={busy}>
        {busy ? "…" : "Continue in browser"}
      </button>
      {done && <div className="success-text">Signed in — taking you home.</div>}
      {error && <div className="error-text">{error}</div>}
      <p>
        <Link href="/login">{t("backToLogin")}</Link>
      </p>
    </div>
  );
}

export default function AppleCallbackPage() {
  return (
    <Suspense fallback={null}>
      <AppleCallbackInner />
    </Suspense>
  );
}
