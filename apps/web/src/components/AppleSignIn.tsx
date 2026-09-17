"use client";

// §3.2 / §4.2: Apple OAuth. Uses Sign in with Apple JS to obtain an RS256
// id_token, which the backend verifies against Apple's JWKS (POST
// /api/auth/apple) on the same shared find-or-create path as Google.
import { useEffect, useRef, useState } from "react";
import { useRouter } from "next/navigation";
import { api, saveTokens, Tokens } from "@/lib/api";

declare global {
  interface Window {
    AppleID?: any;
  }
}

const APPLE_JS_SRC = "https://appleid.cdn-apple.com/appleauth/static/jsapi/appleid/1/en_US/appleid.auth.js";
const APPLE_BTN_SRC = "https://appleid.cdn-apple.com/appleauth/static/jsapi/appleid.signin/1/appleid.signin.js";

export default function AppleSignIn({ totpCode }: { totpCode?: string }) {
  const router = useRouter();
  const btnRef = useRef<HTMLDivElement>(null);
  const [error, setError] = useState("");
  const clientId = process.env.NEXT_PUBLIC_APPLE_CLIENT_ID ?? "";
  const redirectURI = typeof window !== "undefined" ? `${window.location.origin}/login` : "";

  useEffect(() => {
    if (!clientId || !btnRef.current) return;
    let cancelled = false;
    const init = () => {
      if (cancelled || !window.AppleID || !btnRef.current) return;
      try {
        window.AppleID.auth.init({
          clientId,
          scope: "name email",
          redirectURI,
          responseMode: "popup",
          usePopup: true,
        });
      } catch {
        // init is idempotent; ignore double-init errors in dev strict mode
      }
    };
    const onSignIn = async (event: any) => {
      if (cancelled) return;
      setError("");
      try {
        const idToken = event?.detail?.authorization?.id_token ?? "";
        const tokens = await api<Tokens>(
          "/api/auth/apple",
          { method: "POST", body: JSON.stringify({ id_token: idToken, totp_code: totpCode ?? "" }) },
          false
        );
        saveTokens(tokens);
        router.push("/");
        router.refresh();
      } catch (e) {
        const msg = e instanceof Error ? e.message : "Apple sign-in failed";
        setError(msg === "totp_required" ? "Enter your 2FA code, then try Apple sign-in again" : msg);
      }
    };
    const onFail = (event: any) => {
      if (!cancelled && event?.detail?.error !== "popup_closed_by_user") {
        setError("Apple sign-in failed");
      }
    };
    const load = (src: string, onload: () => void) => {
      const script = document.createElement("script");
      script.src = src;
      script.async = true;
      script.onload = onload;
      document.head.appendChild(script);
    };
    if (window.AppleID) {
      init();
    } else {
      load(APPLE_JS_SRC, init);
    }
    load(APPLE_BTN_SRC, () => {
      if (!cancelled && window.AppleID?.signin?.renderButton && btnRef.current) {
        try {
          window.AppleID.signin.renderButton(btnRef.current, {
            theme: "outline",
            size: "large",
            width: 320,
          });
        } catch {
          // rendered button already present
        }
      }
    });
    document.addEventListener("AppleIDSignInOnSuccess", onSignIn);
    document.addEventListener("AppleIDSignInOnFailure", onFail);
    return () => {
      cancelled = true;
      document.removeEventListener("AppleIDSignInOnSuccess", onSignIn);
      document.removeEventListener("AppleIDSignInOnFailure", onFail);
    };
  }, [clientId, redirectURI, totpCode, router]);

  if (!clientId) return null;
  return (
    <div className="col" style={{ alignItems: "center" }}>
      <div ref={btnRef} />
      {error && <div className="error-text">{error}</div>}
    </div>
  );
}
