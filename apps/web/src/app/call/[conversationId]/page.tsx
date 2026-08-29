"use client";

import { useEffect, useRef, useState } from "react";
import { useParams, useRouter, useSearchParams } from "next/navigation";
import { getAccessToken, getUserId, wsURL } from "@/lib/api";
import { useI18n } from "@/lib/i18n";
import { MeshCall, SignalPayload } from "@/lib/webrtc";

export default function CallPage() {
  const { t } = useI18n();
  const router = useRouter();
  const params = useParams<{ conversationId: string }>();
  const searchParams = useSearchParams();
  const wantVideo = searchParams.get("video") !== "0";

  const [status, setStatus] = useState("connecting");
  const [error, setError] = useState("");
  const localRef = useRef<HTMLVideoElement>(null);
  const [remoteStreams, setRemoteStreams] = useState<Map<string, MediaStream>>(new Map());
  const callRef = useRef<MeshCall | null>(null);
  const streamRef = useRef<MediaStream | null>(null);

  useEffect(() => {
    if (!getAccessToken()) {
      router.push("/login");
      return;
    }
    const conversationId = params.conversationId;
    const selfId = getUserId() ?? "";
    let ws: WebSocket;
    let cancelled = false;

    (async () => {
      let stream: MediaStream;
      try {
        stream = await navigator.mediaDevices.getUserMedia({ video: wantVideo, audio: true });
      } catch {
        setError("camera/microphone permission denied");
        setStatus("failed");
        return;
      }
      if (cancelled) {
        stream.getTracks().forEach((tr) => tr.stop());
        return;
      }
      streamRef.current = stream;
      if (localRef.current) localRef.current.srcObject = stream;

      ws = new WebSocket(wsURL());
      const call = new MeshCall(
        conversationId,
        selfId,
        (payload: SignalPayload) => {
          if (ws.readyState === WebSocket.OPEN) {
            ws.send(JSON.stringify({ type: "signal", conversation_id: conversationId, signal: payload }));
          }
        },
        stream,
        (peerId, remote) => setRemoteStreams((prev) => new Map(prev).set(peerId, remote)),
        (peerId) => setRemoteStreams((prev) => {
          const next = new Map(prev);
          next.delete(peerId);
          return next;
        })
      );
      callRef.current = call;

      ws.onopen = () => {
        setStatus("in call");
        call.join();
      };
      ws.onmessage = (ev) => {
        try {
          const data = JSON.parse(ev.data as string);
          if (data.type === "signal") {
            call.handleSignal(data.sender_id as string, data.signal as SignalPayload);
          }
        } catch { /* ignore */ }
      };
      ws.onerror = () => setStatus("connection error");
      ws.onclose = () => setStatus("ended");
    })();

    return () => {
      cancelled = true;
      callRef.current?.leave();
      streamRef.current?.getTracks().forEach((tr) => tr.stop());
      ws?.close();
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [params.conversationId]);

  const hangUp = () => {
    callRef.current?.leave();
    streamRef.current?.getTracks().forEach((tr) => tr.stop());
    router.push("/chat");
  };

  return (
    <div className="card">
      <div className="row">
        <h3 style={{ margin: 0 }}>{wantVideo ? t("videoCall") : t("audioCall")}</h3>
        <span className="badge">{status}</span>
        <div className="spacer" />
        <button className="danger" onClick={hangUp}>{t("endCall")}</button>
      </div>
      {error && <div className="error-text" style={{ marginTop: 8 }}>{error}</div>}
      <div className="video-grid" style={{ marginTop: 12 }}>
        <video ref={localRef} autoPlay playsInline muted />
        {Array.from(remoteStreams.entries()).map(([peerId, stream]) => (
          <RemoteVideo key={peerId} stream={stream} />
        ))}
      </div>
    </div>
  );
}

function RemoteVideo({ stream }: { stream: MediaStream }) {
  const ref = useRef<HTMLVideoElement>(null);
  useEffect(() => {
    if (ref.current) ref.current.srcObject = stream;
  }, [stream]);
  return <video ref={ref} autoPlay playsInline />;
}
