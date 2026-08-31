"use client";

import { useEffect, useRef, useState } from "react";
import { useParams, useRouter, useSearchParams } from "next/navigation";
import { api, getAccessToken, getUserId, uploadMedia, wsURL } from "@/lib/api";
import { useI18n } from "@/lib/i18n";
import { MeshCall, SignalPayload, VideoFilter, VIDEO_FILTERS } from "@/lib/webrtc";

type Recording = { id: string; username: string; media_url: string; duration_s: number; created_at: string };

export default function CallPage() {
  const { t } = useI18n();
  const router = useRouter();
  const params = useParams<{ conversationId: string }>();
  const searchParams = useSearchParams();
  const wantVideo = searchParams.get("video") !== "0";
  const roomId = `${params.conversationId}-meeting`;

  const [status, setStatus] = useState("connecting");
  const [error, setError] = useState("");
  const [sharing, setSharing] = useState(false);
  const [recording, setRecording] = useState(false);
  const [recSecs, setRecSecs] = useState(0);
  const [recordings, setRecordings] = useState<Recording[]>([]);
  const localRef = useRef<HTMLVideoElement>(null);
  const [remoteStreams, setRemoteStreams] = useState<Map<string, MediaStream>>(new Map());
  const callRef = useRef<MeshCall | null>(null);
  const streamRef = useRef<MediaStream | null>(null);
  const screenRef = useRef<MediaStream | null>(null);
  const recRef = useRef<MediaRecorder | null>(null);
  const recChunks = useRef<Blob[]>([]);
  const recStart = useRef(0);
  const filterRef = useRef<VideoFilter | null>(null);
  const [filter, setFilter] = useState("none");

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
    recRef.current?.state === "recording" && recRef.current.stop();
    callRef.current?.leave();
    screenRef.current?.getTracks().forEach((tr) => tr.stop());
    streamRef.current?.getTracks().forEach((tr) => tr.stop());
    router.push("/chat");
  };

  const loadRecordings = () => {
    api<{ recordings: Recording[] }>(`/api/calls/rooms/${roomId}/recordings`)
      .then((d) => setRecordings(d.recordings)).catch(() => {});
  };

  useEffect(loadRecordings, [roomId]);

  const toggleShare = async () => {
    setError("");
    if (sharing) {
      screenRef.current?.getTracks().forEach((tr) => tr.stop());
      screenRef.current = null;
      const cam = streamRef.current?.getVideoTracks()[0] ?? null;
      callRef.current?.replaceVideoTrack(cam);
      if (localRef.current && streamRef.current) localRef.current.srcObject = streamRef.current;
      setSharing(false);
      api(`/api/calls/rooms/${roomId}/screenshare`, { method: "POST", body: JSON.stringify({ on: false }) }).catch(() => {});
      return;
    }
    try {
      const screen = await navigator.mediaDevices.getDisplayMedia({ video: true });
      screenRef.current = screen;
      const track = screen.getVideoTracks()[0];
      track.onended = () => { void toggleShare(); };
      callRef.current?.replaceVideoTrack(track);
      const mixed = new MediaStream([track, ...(streamRef.current?.getAudioTracks() ?? [])]);
      if (localRef.current) localRef.current.srcObject = mixed;
      setSharing(true);
      api(`/api/calls/rooms/${roomId}/screenshare`, { method: "POST", body: JSON.stringify({ on: true }) }).catch(() => {});
    } catch {
      setError("screen share cancelled or unavailable");
    }
  };

  const startRecording = () => {
    setError("");
    try {
      const ctx = new AudioContext();
      const dest = ctx.createMediaStreamDestination();
      if (streamRef.current) ctx.createMediaStreamSource(streamRef.current).connect(dest);
      remoteStreams.forEach((s) => { if (s.getAudioTracks().length) ctx.createMediaStreamSource(s).connect(dest); });
      const videoTrack = streamRef.current?.getVideoTracks()[0];
      const combined = new MediaStream([
        ...(videoTrack ? [videoTrack] : []),
        ...dest.stream.getAudioTracks(),
      ]);
      const rec = new MediaRecorder(combined, { mimeType: "video/webm" });
      recChunks.current = [];
      rec.ondataavailable = (e) => e.data.size && recChunks.current.push(e.data);
      recStart.current = Date.now();
      const tick = setInterval(() => setRecSecs(Math.round((Date.now() - recStart.current) / 1000)), 1000);
      rec.onstop = async () => {
        clearInterval(tick);
        const secs = Math.max(1, Math.round((Date.now() - recStart.current) / 1000));
        const blob = new Blob(recChunks.current, { type: "video/webm" });
        try {
          await api("/api/calls/rooms", { method: "POST",
            body: JSON.stringify({ conversation_id: params.conversationId, mode: "meeting" }) });
          const url = await uploadMedia(new File([blob], "recording.webm", { type: "video/webm" }));
          await api(`/api/calls/rooms/${roomId}/recordings`, {
            method: "POST", body: JSON.stringify({ media_url: url, duration_s: secs }),
          });
          loadRecordings();
        } catch (e) {
          setError(e instanceof Error ? e.message : "failed to save recording");
        }
        void ctx.close();
      };
      recRef.current = rec;
      rec.start(500);
      setRecording(true);
    } catch (e) {
      setError(e instanceof Error ? e.message : "recording unavailable");
    }
  };

  const stopRecording = () => {
    recRef.current?.stop();
    setRecording(false);
    setRecSecs(0);
  };

  // AR/color effects: process the camera through a canvas and publish the
  // filtered track, so remote participants see the effect too.
  const applyFilter = (name: string) => {
    setFilter(name);
    const camTrack = streamRef.current?.getVideoTracks()[0];
    if (!camTrack || sharing) return;
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

  const deleteRecording = async (id: string) => {
    await api(`/api/calls/recordings/${id}`, { method: "DELETE" }).catch(() => {});
    loadRecordings();
  };

  return (
    <div className="card">
      <div className="row">
        <h3 style={{ margin: 0 }}>{wantVideo ? t("videoCall") : t("audioCall")}</h3>
        <span className="badge">{status}</span>
        {recording && <span className="badge">⏺ {recSecs}s</span>}
        <div className="spacer" />
        {wantVideo && (
          <button className={sharing ? "small" : "secondary"} onClick={toggleShare}>
            {sharing ? t("stopScreenShare") : `🖥 ${t("screenShare")}`}
          </button>
        )}
        {wantVideo && (
          <select className="secondary" value={filter} title="AR effect"
            onChange={(e) => applyFilter(e.target.value)}>
            {Object.keys(VIDEO_FILTERS).map((f) => (
              <option key={f} value={f}>✨ {f}</option>
            ))}
          </select>
        )}
        <button className={recording ? "danger" : "secondary"}
          onClick={recording ? stopRecording : startRecording}>
          {recording ? `■ ${t("stopRecording")}` : `⏺ ${t("startRecording")}`}
        </button>
        <button className="danger" onClick={hangUp}>{t("endCall")}</button>
      </div>
      {error && <div className="error-text" style={{ marginTop: 8 }}>{error}</div>}
      <div className="video-grid" style={{ marginTop: 12 }}>
        <video ref={localRef} autoPlay playsInline muted />
        {Array.from(remoteStreams.entries()).map(([peerId, stream]) => (
          <RemoteVideo key={peerId} stream={stream} />
        ))}
      </div>
      {recordings.length > 0 && (
        <div className="col" style={{ marginTop: 12, gap: 6 }}>
          <h4 style={{ margin: 0 }}>{t("recordings")}</h4>
          {recordings.map((r) => (
            <div key={r.id} className="row" style={{ alignItems: "center", gap: 6 }}>
              <a href={r.media_url} target="_blank" rel="noreferrer">
                ⏺ {r.duration_s}s — @{r.username}
              </a>
              <span className="muted" style={{ fontSize: 12 }}>
                {new Date(r.created_at).toLocaleString()}
              </span>
              <div className="spacer" />
              <button className="danger small" onClick={() => deleteRecording(r.id)}>{t("delete")}</button>
            </div>
          ))}
        </div>
      )}
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
