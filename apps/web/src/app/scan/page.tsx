"use client";

import { useEffect, useRef, useState } from "react";
import { useRouter } from "next/navigation";
import { api } from "@/lib/api";

declare class BarcodeDetector {
  constructor(options?: { formats?: string[] });
  detect(source: CanvasImageSource): Promise<{ rawValue: string }[]>;
  static getSupportedFormats(): Promise<string[]>;
}

// QR scanner: approve a web/desktop login (chatapp://login?token=...) or
// open a profile (chatapp://user/{id}). Uses the built-in BarcodeDetector.

export default function ScanPage() {
  const router = useRouter();
  const videoRef = useRef<HTMLVideoElement>(null);
  const [error, setError] = useState("");
  const [loginToken, setLoginToken] = useState("");
  const [done, setDone] = useState("");

  useEffect(() => {
    if (loginToken || done) return;
    let stream: MediaStream | null = null;
    let stopped = false;
    let timer: ReturnType<typeof setInterval>;

    const start = async () => {
      if (typeof BarcodeDetector === "undefined") {
        setError("QR scanning is not supported by this browser. Use the mobile app.");
        return;
      }
      try {
        stream = await navigator.mediaDevices.getUserMedia({
          video: { facingMode: "environment" },
        });
      } catch {
        setError("Camera access denied");
        return;
      }
      if (!videoRef.current || stopped) return;
      videoRef.current.srcObject = stream;
      await videoRef.current.play();
      const detector = new BarcodeDetector({ formats: ["qr_code"] });
      timer = setInterval(async () => {
        if (!videoRef.current || stopped) return;
        try {
          const codes = await detector.detect(videoRef.current);
          for (const c of codes) {
            handleCode(c.rawValue);
          }
        } catch {
          /* frame not ready */
        }
      }, 400);
    };

    const handleCode = (raw: string) => {
      try {
        const url = new URL(raw);
        if (url.protocol === "chatapp:" && url.host === "login") {
          const tok = url.searchParams.get("token");
          if (tok) {
            stopped = true;
            setLoginToken(tok);
          }
        } else if (url.protocol === "chatapp:" && url.host === "user") {
          const id = url.pathname.replace(/^\//, "");
          if (id) {
            stopped = true;
            router.push(`/user/${id}`);
          }
        }
      } catch {
        /* not a URL we recognize */
      }
    };

    start();
    return () => {
      stopped = true;
      clearInterval(timer);
      stream?.getTracks().forEach((t) => t.stop());
    };
  }, [loginToken, done, router]);

  const approve = async () => {
    try {
      await api(`/api/auth/qr/${loginToken}/approve`, {
        method: "POST",
        body: JSON.stringify({}),
      });
      setDone("Login approved — the other device is now signed in.");
    } catch (e) {
      setError(e instanceof Error ? e.message : "Approval failed");
      setLoginToken("");
    }
  };

  const reject = async () => {
    await api(`/api/auth/qr/${loginToken}/reject`, {
      method: "POST",
      body: JSON.stringify({}),
    }).catch(() => {});
    setLoginToken("");
    setDone("Login request rejected.");
  };

  return (
    <div className="card" style={{ maxWidth: 480, margin: "40px auto" }}>
      <h2>Scan QR code</h2>
      {done ? (
        <p>{done}</p>
      ) : loginToken ? (
        <div className="col">
          <p>A device is asking to sign in to your account. Approve?</p>
          <div className="row">
            <button onClick={approve}>Approve login</button>
            <button className="secondary" onClick={reject}>
              Reject
            </button>
          </div>
        </div>
      ) : (
        <video ref={videoRef} style={{ width: "100%", borderRadius: 8 }} muted playsInline />
      )}
      {error && <div className="error-text">{error}</div>}
    </div>
  );
}
