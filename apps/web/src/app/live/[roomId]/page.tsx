"use client";

import { useEffect, useRef, useState } from "react";
import { useParams, useRouter, useSearchParams } from "next/navigation";
import { api, getAccessToken } from "@/lib/api";
import { SfuCall, SfuSession } from "@/lib/webrtc";

// Live broadcasting on the ChatApp SFU: publishers capture and publish;
// viewers subscribe receive-only.
export default function LiveRoomPage() {
  const router = useRouter();
  const params = useParams<{ roomId: string }>();
  const searchParams = useSearchParams();
  const isPublisher = searchParams.get("publish") === "1";
  const convId = searchParams.get("conv") ?? "";

  const [status, setStatus] = useState("connecting");
  const [error, setError] = useState("");
  const localRef = useRef<HTMLVideoElement>(null);
  const remoteRef = useRef<HTMLVideoElement>(null);
  const callRef = useRef<SfuCall | null>(null);
  const streamRef = useRef<MediaStream | null>(null);

  useEffect(() => {
    if (!getAccessToken()) {
      router.push("/login");
      return;
    }
    let cancelled = false;
    (async () => {
      let stream: MediaStream | null = null;
      if (isPublisher) {
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
      }
      try {
        const session = isPublisher
          ? await api<SfuSession>("/api/calls/rooms", {
              method: "POST",
              body: JSON.stringify({ conversation_id: convId, mode: "live" }),
            })
          : await api<SfuSession>(
              `/api/calls/rooms/${encodeURIComponent(params.roomId)}/join`,
              { method: "POST", body: "{}" }
            );
        const call = new SfuCall(
          session,
          stream,
          (_peerId, remote) => {
            if (remoteRef.current) remoteRef.current.srcObject = remote;
          },
          () => setStatus("ended")
        );
        callRef.current = call;
        await call.join();
        setStatus(isPublisher ? "live" : "watching");
      } catch (e) {
        setError(e instanceof Error ? e.message : "failed to join");
        setStatus("failed");
      }
    })();
    return () => {
      cancelled = true;
      callRef.current?.leave();
      streamRef.current?.getTracks().forEach((t) => t.stop());
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [params.roomId]);

  const leave = () => {
    callRef.current?.leave();
    streamRef.current?.getTracks().forEach((t) => t.stop());
    router.push("/live");
  };

  return (
    <div className="card">
      <div className="row">
        <h3 style={{ margin: 0 }}>{isPublisher ? "🔴 Live broadcast" : "Live"}</h3>
        <span className="badge">{status}</span>
        <div className="spacer" />
        <button className="danger" onClick={leave}>{isPublisher ? "End broadcast" : "Leave"}</button>
      </div>
      {error && <div className="error-text" style={{ marginTop: 8 }}>{error}</div>}
      <div style={{ marginTop: 12 }}>
        {isPublisher ? (
          <video ref={localRef} autoPlay playsInline muted style={{ width: "100%", borderRadius: 8 }} />
        ) : (
          <video ref={remoteRef} autoPlay playsInline style={{ width: "100%", borderRadius: 8 }} />
        )}
      </div>
    </div>
  );
}
