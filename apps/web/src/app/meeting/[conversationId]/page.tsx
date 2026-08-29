"use client";

import { useEffect, useRef, useState } from "react";
import { useParams, useRouter } from "next/navigation";
import { api, getAccessToken } from "@/lib/api";
import { SfuCall, SfuSession } from "@/lib/webrtc";

// Large group calls / meetings run through the ChatApp SFU (self-built,
// services/sfu) — no external media kit involved.
export default function MeetingPage() {
  const router = useRouter();
  const params = useParams<{ conversationId: string }>();
  const [status, setStatus] = useState("connecting");
  const [error, setError] = useState("");
  const localRef = useRef<HTMLVideoElement>(null);
  const [remoteStreams, setRemoteStreams] = useState<Map<string, MediaStream>>(new Map());
  const callRef = useRef<SfuCall | null>(null);
  const streamRef = useRef<MediaStream | null>(null);

  useEffect(() => {
    if (!getAccessToken()) {
      router.push("/login");
      return;
    }
    let cancelled = false;
    (async () => {
      let stream: MediaStream;
      try {
        stream = await navigator.mediaDevices.getUserMedia({ video: true, audio: true });
      } catch {
        setError("camera/microphone permission denied");
        setStatus("failed");
        return;
      }
      if (cancelled) {
        stream.getTracks().forEach((t) => t.stop());
        return;
      }
      streamRef.current = stream;
      if (localRef.current) localRef.current.srcObject = stream;
      try {
        const session = await api<SfuSession>("/api/calls/rooms", {
          method: "POST",
          body: JSON.stringify({ conversation_id: params.conversationId, mode: "meeting" }),
        });
        const call = new SfuCall(
          session,
          stream,
          (peerId, remote) => setRemoteStreams((prev) => new Map(prev).set(peerId, remote)),
          (peerId) => setRemoteStreams((prev) => {
            const next = new Map(prev);
            next.delete(peerId);
            return next;
          })
        );
        callRef.current = call;
        await call.join();
        setStatus("in meeting");
      } catch (e) {
        setError(e instanceof Error ? e.message : "failed to join meeting");
        setStatus("failed");
      }
    })();
    return () => {
      cancelled = true;
      callRef.current?.leave();
      streamRef.current?.getTracks().forEach((t) => t.stop());
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [params.conversationId]);

  const hangUp = () => {
    callRef.current?.leave();
    streamRef.current?.getTracks().forEach((t) => t.stop());
    router.push("/chat");
  };

  return (
    <div className="card">
      <div className="row">
        <h3 style={{ margin: 0 }}>Meeting</h3>
        <span className="badge">{status}</span>
        <div className="spacer" />
        <button className="danger" onClick={hangUp}>Leave</button>
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
