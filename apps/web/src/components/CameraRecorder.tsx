"use client";

import { useEffect, useRef, useState } from "react";
import { useI18n } from "@/lib/i18n";

/** TikTok-style in-app camera: getUserMedia live preview + MediaRecorder.
 *  Emits the recorded clip as a File once the user stops. */
export default function CameraRecorder({ onCaptured }: { onCaptured: (file: File) => void }) {
  const { t } = useI18n();
  const previewRef = useRef<HTMLVideoElement>(null);
  const recRef = useRef<MediaRecorder | null>(null);
  const chunksRef = useRef<Blob[]>([]);
  const [active, setActive] = useState(false);
  const [recording, setRecording] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => () => {
    recRef.current?.stop();
    const stream = previewRef.current?.srcObject as MediaStream | null;
    stream?.getTracks().forEach((tr) => tr.stop());
  }, []);

  const start = async () => {
    setError("");
    try {
      const stream = await navigator.mediaDevices.getUserMedia({ video: true, audio: { noiseSuppression: true, echoCancellation: true } });
      if (previewRef.current) previewRef.current.srcObject = stream;
      const mime = MediaRecorder.isTypeSupported("video/webm;codecs=vp9,opus")
        ? "video/webm;codecs=vp9,opus"
        : "video/webm";
      const rec = new MediaRecorder(stream, { mimeType: mime });
      recRef.current = rec;
      chunksRef.current = [];
      rec.ondataavailable = (e) => e.data.size && chunksRef.current.push(e.data);
      rec.onstop = () => {
        const blob = new Blob(chunksRef.current, { type: "video/webm" });
        onCaptured(new File([blob], "capture.webm", { type: "video/webm" }));
        stream.getTracks().forEach((tr) => tr.stop());
        setActive(false);
        setRecording(false);
      };
      setActive(true);
    } catch {
      setError(t("cameraUnavailable"));
    }
  };

  const toggle = () => {
    if (!recording) {
      setRecording(true);
      recRef.current?.start();
    } else {
      recRef.current?.stop();
    }
  };

  return (
    <div className="col" style={{ gap: 8 }}>
      {error && <div className="error">{error}</div>}
      {!active && (
        <button className="secondary small" onClick={start}>{t("recordClip")}</button>
      )}
      {active && (
        <>
          <video ref={previewRef} autoPlay playsInline muted style={{ width: "100%", maxHeight: 240, borderRadius: 8 }} />
          <div className="row" style={{ gap: 6 }}>
            <button className={recording ? "small" : "secondary small"} onClick={toggle}>
              {recording ? t("stopAndAttach") : t("rec")}
            </button>
          </div>
        </>
      )}
    </div>
  );
}