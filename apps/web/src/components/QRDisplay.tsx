"use client";

import { useEffect, useState } from "react";
import QRCode from "qrcode";

// Renders a payload (address or payment URI) as a QR code image.
export default function QRDisplay({ value, size = 180 }: { value: string; size?: number }) {
  const [dataUrl, setDataUrl] = useState("");

  useEffect(() => {
    let cancelled = false;
    QRCode.toDataURL(value, { width: size, margin: 1 })
      .then((url) => {
        if (!cancelled) setDataUrl(url);
      })
      .catch(() => setDataUrl(""));
    return () => {
      cancelled = true;
    };
  }, [value, size]);

  if (!dataUrl) return null;
  // eslint-disable-next-line @next/next/no-img-element
  return <img src={dataUrl} alt="QR code" width={size} height={size} style={{ borderRadius: 8 }} />;
}
