"use client";

import { useEffect, useRef, useState } from "react";
import { useParams, useRouter } from "next/navigation";
import { api, getAccessToken } from "@/lib/api";
import { SfuCall, SfuSession, VideoFilter, VIDEO_FILTERS } from "@/lib/webrtc";

type RoomInfo = {
  slug: string;
  title: string;
  host_username: string;
  created_at: string;
  ended: boolean;
  link: string;
};

// Drop-in rooms (Messenger Rooms parity): a persistent, shareable link anyone
// signed-in can join — media runs through the self-built SFU.
export default function RoomPage() {
  const router = useRouter();
  const params = useParams<{ slug: string }>();
  const [info, setInfo] = useState<RoomInfo | null>(null);
  const [status, setStatus] = useState("preview");
  const [error, setError] = useState("");
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

  useEffect(() => {
    if (!getAccessToken()) {
      router.push("/login");
      return;
    }
    api<RoomInfo>(`/api/rooms/${params.slug}`)
      .then(setInfo)
      .catch((e) => setError(e instanceof Error ? e.message : "room not found"));
    return () => {
      callRef.current?.leave();
      streamRef.current?.getTracks().forEach((t) => t.stop());
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [params.slug]);

  const join = async () => {
    setError("");
    setStatus("connecting");
    let stream: MediaStream;
    try {
      stream = await navigator.mediaDevices.getUserMedia({ video: true, audio: true });
    } catch {
      setError("camera/microphone permission denied");
      setStatus("preview");
      return;
    }
    streamRef.current = stream;
    if (localRef.current) localRef.current.srcObject = stream;
    try {
      const session = await api<SfuSession>(`/api/rooms/${params.slug}/join`, {
        method: "POST",
        body: "{}",
      });
      const call = new SfuCall(
        session,
        stream,
        (peerId, remote) => setRemoteStreams((prev) => new Map(prev).set(peerId, remote)),
        (peerId) =>
          setRemoteStreams((prev) => {
            const next = new Map(prev);
            next.delete(peerId);
            return next;
          })
      );
      callRef.current = call;
      await call.join();
      setStatus("in room");
    } catch (e) {
      setError(e instanceof Error ? e.message : "failed to join room");
      setStatus("preview");
    }
  };

  const leave = () => {
    callRef.current?.leave();
    streamRef.current?.getTracks().forEach((t) => t.stop());
    router.push("/chat");
  };

  return (
    <div className="card">
      <div className="row">
        <h3 style={{ margin: 0 }}>{info?.title ?? "Room"}</h3>
        <span className="badge">{status}</span>
        <div className="spacer" />
        {status === "in room" && (
          <select className="secondary" value={filter} title="AR effect"
            onChange={(e) => applyFilter(e.target.value)}>
            {Object.keys(VIDEO_FILTERS).map((f) => (
              <option key={f} value={f}>✨ {f}</option>
            ))}
          </select>
        )}
        {status === "in room" ? (
          <button className="danger" onClick={leave}>
            Leave
          </button>
        ) : (
          <button className="secondary" onClick={() => router.push("/chat")}>
            Back
          </button>
        )}
      </div>
      {info && (
        <p className="muted" style={{ margin: "8px 0" }}>
          Hosted by @{info.host_username} · link {info.link}
        </p>
      )}
      {error && (
        <div className="error-text" style={{ marginTop: 8 }}>
          {error}
        </div>
      )}
      {info?.ended && (
        <p className="muted" style={{ marginTop: 8 }}>
          This room has ended.
        </p>
      )}
      {status === "preview" && info && !info.ended && (
        <button style={{ marginTop: 8 }} onClick={join}>
          Join room
        </button>
      )}
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
