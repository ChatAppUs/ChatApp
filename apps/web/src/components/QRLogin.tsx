"use client";

import { useEffect, useRef, useState } from "react";
import { useRouter } from "next/navigation";
import QRCode from "qrcode";
import { api, saveTokens, Tokens } from "@/lib/api";

// Telegram-style QR login: this (signed-out) device shows a QR; a signed-in
// device scans it at /scan and approves; we poll until approved.

export default function QRLogin() {
  const router = useRouter();
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const [status, setStatus] = useState("loading");
  const [token, setToken] = useState("");

  const refresh = async () => {
    setStatus("loading");
    try {
      const d = await api<{ token: string; url: string }>(
        "/api/auth/qr/new",
        { method: "POST", body: JSON.stringify({}) },
        false
      );
      setToken(d.token);
      setStatus("pending");
      if (canvasRef.current) {
        await QRCode.toCanvas(canvasRef.current, d.url, { width: 220, margin: 1 });
      }
    } catch {
      setStatus("error");
    }
  };

  useEffect(() => {
    refresh();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  useEffect(() => {
    if (!token || status !== "pending") return;
    const iv = setInterval(async () => {
      try {
        const d = await api<Tokens & { status: string }>(
          `/api/auth/qr/${token}`,
          {},
          false
        );
        if (d.status === "approved" && d.access_token) {
          clearInterval(iv);
          saveTokens(d);
          router.push("/");
          router.refresh();
        } else if (d.status === "expired" || d.status === "consumed") {
          clearInterval(iv);
          setStatus("expired");
        }
      } catch {
        /* transient network error — keep polling */
      }
    }, 2000);
    return () => clearInterval(iv);
  }, [token, status, router]);

  return (
    <div className="col" style={{ alignItems: "center" }}>
      {status === "expired" ? (
        <>
          <p className="muted">QR code expired</p>
          <button className="secondary" onClick={refresh}>
            Refresh QR code
          </button>
        </>
      ) : (
        <>
          <canvas ref={canvasRef} />
          <p className="muted" style={{ textAlign: "center", fontSize: 13 }}>
            Scan with the ChatApp mobile app
            <br />
            (logged-in device → Scan QR)
          </p>
        </>
      )}
    </div>
  );
}
