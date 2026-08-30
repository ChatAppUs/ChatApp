"use client";

import { useEffect, useRef, useState } from "react";
import { useI18n } from "@/lib/i18n";

declare class BarcodeDetector {
  constructor(options?: { formats?: string[] });
  detect(source: CanvasImageSource): Promise<{ rawValue: string }[]>;
}

// Camera QR scanner used for withdrawal addresses. Accepts raw addresses and
// payment URIs (bitcoin:, ethereum:, tron:, solana:) and returns the address.
export function parseAddressPayload(raw: string): string {
  const v = raw.trim();
  const m = v.match(/^(?:bitcoin|ethereum|tron|solana|litecoin|dogecoin):([a-zA-Z0-9]+)/);
  return m ? m[1] : v;
}

export default function QRScanModal({
  onResult,
  onClose,
}: {
  onResult: (address: string) => void;
  onClose: () => void;
}) {
  const { t } = useI18n();
  const videoRef = useRef<HTMLVideoElement>(null);
  const [error, setError] = useState("");

  useEffect(() => {
    let stream: MediaStream | null = null;
    let stopped = false;
    let timer: ReturnType<typeof setInterval>;

    const start = async () => {
      if (typeof BarcodeDetector === "undefined") {
        setError(t("cameraUnsupported"));
        return;
      }
      try {
        stream = await navigator.mediaDevices.getUserMedia({ video: { facingMode: "environment" } });
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
          if (codes.length > 0) {
            stopped = true;
            onResult(parseAddressPayload(codes[0].rawValue));
          }
        } catch {
          /* frame not ready */
        }
      }, 350);
    };
    start();
    return () => {
      stopped = true;
      if (timer) clearInterval(timer);
      stream?.getTracks().forEach((tr) => tr.stop());
    };
  }, [onResult, t]);

  return (
    <div className="card" style={{ border: "1px solid var(--accent)" }}>
      <h4>{t("scanWithdrawAddress")}</h4>
      {error ? (
        <p className="error-text">{error}</p>
      ) : (
        <video ref={videoRef} style={{ width: "100%", maxWidth: 360, borderRadius: 8 }} muted playsInline />
      )}
      <div className="row" style={{ marginTop: 8 }}>
        <button className="secondary small" onClick={onClose}>{t("cancel")}</button>
      </div>
    </div>
  );
}
