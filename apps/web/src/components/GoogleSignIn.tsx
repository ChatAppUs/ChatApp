"use client";

import { useEffect, useRef, useState } from "react";
import { useRouter } from "next/navigation";
import { api, saveTokens, Tokens } from "@/lib/api";

declare global {
  interface Window {
    google?: any;
  }
}

const GSI_SRC = "https://accounts.google.com/gsi/client";

export default function GoogleSignIn({ totpCode }: { totpCode?: string }) {
  const router = useRouter();
  const btnRef = useRef<HTMLDivElement>(null);
  const [error, setError] = useState("");
  const clientId = process.env.NEXT_PUBLIC_GOOGLE_CLIENT_ID ?? "";

  useEffect(() => {
    if (!clientId) return;
    const init = () => {
      if (!window.google || !btnRef.current) return;
      window.google.accounts.id.initialize({
        client_id: clientId,
        callback: async ({ credential }: { credential: string }) => {
          setError("");
          try {
            const tokens = await api<Tokens>(
              "/api/auth/google",
              {
                method: "POST",
                body: JSON.stringify({ id_token: credential, totp_code: totpCode ?? "" }),
              },
              false
            );
            saveTokens(tokens);
            router.push("/");
            router.refresh();
          } catch (e) {
            setError(e instanceof Error ? e.message : "Google sign-in failed");
          }
        },
      });
      window.google.accounts.id.renderButton(btnRef.current, {
        theme: "outline",
        size: "large",
        width: 320,
        text: "continue_with",
      });
    };
    if (window.google) {
      init();
      return;
    }
    const script = document.createElement("script");
    script.src = GSI_SRC;
    script.async = true;
    script.onload = init;
    document.head.appendChild(script);
  }, [clientId, totpCode, router]);

  if (!clientId) return null;
  return (
    <div className="col" style={{ alignItems: "center" }}>
      <div ref={btnRef} />
      {error && <div className="error-text">{error}</div>}
    </div>
  );
}
