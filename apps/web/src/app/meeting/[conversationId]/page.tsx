"use client";

import { useEffect, useRef, useState } from "react";
import { useParams, useRouter } from "next/navigation";
import { api, getAccessToken } from "@/lib/api";
import { SfuCall, SfuSession, VideoFilter, VIDEO_FILTERS } from "@/lib/webrtc";

// Large group calls / meetings run through the ChatApp SFU (self-built,
// services/sfu) — no external media kit involved.
export default function MeetingPage() {
  const router = useRouter();
  const params = useParams<{ conversationId: string }>();
  const [status, setStatus] = useState("connecting");
  const [error, setError] = useState("");
  const [quality, setQuality] = useState<"good" | "fair" | "poor" | "unknown">("unknown");
  const [audioOnly, setAudioOnly] = useState(false);
  const localRef = useRef<HTMLVideoElement>(null);
  const [remoteStreams, setRemoteStreams] = useState<Map<string, MediaStream>>(new Map());
  const callRef = useRef<SfuCall | null>(null);
  const streamRef = useRef<MediaStream | null>(null);
  const filterRef = useRef<VideoFilter | null>(null);
  const [filter, setFilter] = useState("none");

  const applyFilter = (name: string) => {
    setFilter(name);
    const camTrack = streamRef.current?.getVideoTracks()[0];
    if (!camTrack) return;
    if (name === "none") {
      filterRef.current?.stop();
      filterRef.current = null;
      callRef.current?.replaceVideoTrack(camTrack);
      return;
    }
    if (!filterRef.current) filterRef.current = new VideoFilter(camTrack);
    filterRef.current.setFilter(name);
    callRef.current?.replaceVideoTrack(filterRef.current.track);
  };

  useEffect(() => () => filterRef.current?.stop(), []);

  // Poll RCTP stats every 3s. When quality is poor, automatically drop local
  // video to audio-only (imo-style adaptive downgrade); restore when it recovers.
  useEffect(() => {
    const call = () => callRef.current;
    const timer = setInterval(async () => {
      const q = await call()?.networkQuality();
      if (!q) return;
      setQuality(q);
      if (q === "poor") { call()?.setAudioOnly(true); setAudioOnly(true); }
      else if (audioOnly) { call()?.setAudioOnly(false); setAudioOnly(false); }
    }, 3000);
    return () => clearInterval(timer);
  }, [audioOnly]);

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
        <span className={`badge ${quality === "poor" ? "danger" : ""}`} title="Network quality (RTCP)">
          {quality === "good" ? "📶 good" : quality === "fair" ? "📶 fair" : quality === "poor" ? "📶 poor" : "📶"}
        </span>
        <div className="spacer" />
        <select className="secondary" value={filter} title="AR effect"
          onChange={(e) => applyFilter(e.target.value)}>
          {Object.keys(VIDEO_FILTERS).map((f) => (
            <option key={f} value={f}>✨ {f}</option>
          ))}
        </select>
        <button className={audioOnly ? "secondary" : ""} title="Audio-only"
          onClick={() => { callRef.current?.setAudioOnly(!audioOnly); setAudioOnly(!audioOnly); }}>
          {audioOnly ? "🔇 audio-only" : "🎥 video"}
        </button>
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
