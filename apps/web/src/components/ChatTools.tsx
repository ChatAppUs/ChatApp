"use client";

import { useEffect, useRef, useState } from "react";
import { api, uploadMedia } from "@/lib/api";
import type { LiveLocation } from "@/lib/types";

// ---------- Poll composer ----------

export function PollModal({ convId, onClose }: { convId: string; onClose: () => void }) {
  const [question, setQuestion] = useState("");
  const [options, setOptions] = useState(["", ""]);
  const [multi, setMulti] = useState(false);
  const [error, setError] = useState("");

  const submit = async () => {
    setError("");
    try {
      await api(`/api/conversations/${convId}/polls`, {
        method: "POST",
        body: JSON.stringify({ question, options: options.filter((o) => o.trim()), multi }),
      });
      onClose();
    } catch (e) {
      setError(e instanceof Error ? e.message : "failed");
    }
  };

  return (
    <div className="card col" style={{ gap: 8 }}>
      <h4 style={{ margin: 0 }}>Create poll</h4>
      <input placeholder="Question" value={question} maxLength={300}
        onChange={(e) => setQuestion(e.target.value)} />
      {options.map((o, i) => (
        <div key={i} className="row" style={{ gap: 6 }}>
          <input placeholder={`Option ${i + 1}`} value={o} maxLength={100}
            onChange={(e) => setOptions(options.map((x, j) => (j === i ? e.target.value : x)))} />
          {options.length > 2 && (
            <button className="danger small" onClick={() => setOptions(options.filter((_, j) => j !== i))}>×</button>
          )}
        </div>
      ))}
      {options.length < 10 && (
        <button className="secondary small" onClick={() => setOptions([...options, ""])}>Add option</button>
      )}
      <label className="row" style={{ gap: 6 }}>
        <input type="checkbox" checked={multi} onChange={(e) => setMulti(e.target.checked)} style={{ width: "auto" }} />
        <span className="muted">Allow multiple answers</span>
      </label>
      {error && <div className="error">{error}</div>}
      <div className="row" style={{ gap: 6 }}>
        <button onClick={submit}>Create</button>
        <button className="secondary" onClick={onClose}>Cancel</button>
      </div>
    </div>
  );
}

// ---------- Video note recorder ----------

export function VideoNoteButton({ convId, onSent }: { convId: string; onSent: () => void }) {
  const [recording, setRecording] = useState(false);
  const [error, setError] = useState("");
  const recRef = useRef<MediaRecorder | null>(null);
  const streamRef = useRef<MediaStream | null>(null);
  const chunksRef = useRef<Blob[]>([]);
  const startedRef = useRef(0);

  const stop = () => recRef.current?.stop();

  const start = async () => {
    setError("");
    try {
      const stream = await navigator.mediaDevices.getUserMedia({ video: true, audio: true });
      streamRef.current = stream;
      const rec = new MediaRecorder(stream, { mimeType: "video/webm" });
      chunksRef.current = [];
      startedRef.current = Date.now();
      rec.ondataavailable = (e) => e.data.size && chunksRef.current.push(e.data);
      rec.onstop = async () => {
        stream.getTracks().forEach((t) => t.stop());
        const secs = Math.max(1, Math.round((Date.now() - startedRef.current) / 1000));
        const blob = new Blob(chunksRef.current, { type: "video/webm" });
        try {
          const url = await uploadMedia(new File([blob], "note.webm", { type: "video/webm" }));
          await api(`/api/conversations/${convId}/video-note`, {
            method: "POST",
            body: JSON.stringify({ media_url: url, duration_s: secs }),
          });
          onSent();
        } catch (e) {
          setError(e instanceof Error ? e.message : "upload failed");
        }
      };
      recRef.current = rec;
      rec.start(250);
      setRecording(true);
    } catch (e) {
      setError(e instanceof Error ? e.message : "camera unavailable");
    }
  };

  return (
    <>
      <button className={recording ? "danger small" : "secondary small"}
        onClick={recording ? stop : start} title="Video note">
        {recording ? "■ send note" : "◉ note"}
      </button>
      {error && <span className="error" style={{ fontSize: 12 }}>{error}</span>}
    </>
  );
}

// ---------- Live location ----------

export function LiveLocationPanel({ convId }: { convId: string }) {
  const [locations, setLocations] = useState<LiveLocation[]>([]);
  const [sharing, setSharing] = useState(false);
  const [error, setError] = useState("");

  const load = () => {
    api<{ locations: LiveLocation[] }>(`/api/conversations/${convId}/live-location`)
      .then((d) => setLocations(d.locations))
      .catch(() => {});
  };

  useEffect(() => {
    load();
    const t = setInterval(load, 30000);
    return () => clearInterval(t);
  }, [convId]);

  const share = () => {
    setError("");
    navigator.geolocation.getCurrentPosition(
      async (pos) => {
        try {
          await api(`/api/conversations/${convId}/live-location`, {
            method: "PUT",
            body: JSON.stringify({ lat: pos.coords.latitude, lng: pos.coords.longitude, duration_minutes: 60 }),
          });
          setSharing(true);
          load();
        } catch (e) {
          setError(e instanceof Error ? e.message : "failed");
        }
      },
      () => setError("location permission denied")
    );
  };

  const stopSharing = async () => {
    await api(`/api/conversations/${convId}/live-location`, { method: "DELETE" }).catch(() => {});
    setSharing(false);
    load();
  };

  return (
    <div className="col" style={{ gap: 6 }}>
      {locations.map((l) => (
        <div key={l.user_id} className="row" style={{ gap: 6, fontSize: 13 }}>
          <span>📍 <strong>@{l.username}</strong> {l.lat.toFixed(5)}, {l.lng.toFixed(5)}</span>
          <a className="muted" target="_blank" rel="noreferrer"
            href={`https://www.openstreetmap.org/?mlat=${l.lat}&mlon=${l.lng}#map=16/${l.lat}/${l.lng}`}>
            map
          </a>
          <span className="muted">until {new Date(l.expires_at).toLocaleTimeString()}</span>
        </div>
      ))}
      <div className="row" style={{ gap: 6 }}>
        {sharing ? (
          <button className="danger small" onClick={stopSharing}>Stop sharing location</button>
        ) : (
          <button className="secondary small" onClick={share}>📍 Share live location (1h)</button>
        )}
      </div>
      {error && <span className="error" style={{ fontSize: 12 }}>{error}</span>}
    </div>
  );
}

// ---------- Pay in chat ----------

export function PayModal({ convId, toUserId, onClose }: { convId: string; toUserId: string; onClose: () => void }) {
  const [asset, setAsset] = useState("USDT");
  const [chain, setChain] = useState("tron");
  const [amount, setAmount] = useState("");
  const [note, setNote] = useState("");
  const [error, setError] = useState("");
  const [done, setDone] = useState(false);

  const submit = async () => {
    setError("");
    try {
      await api(`/api/conversations/${convId}/pay`, {
        method: "POST",
        body: JSON.stringify({ to_user_id: toUserId, asset, chain, amount, note }),
      });
      setDone(true);
      setTimeout(onClose, 1200);
    } catch (e) {
      setError(e instanceof Error ? e.message : "payment failed");
    }
  };

  return (
    <div className="card col" style={{ gap: 8 }}>
      <h4 style={{ margin: 0 }}>Send crypto in chat</h4>
      <div className="row" style={{ gap: 6 }}>
        <select value={`${asset}:${chain}`} onChange={(e) => {
          const [a, c] = e.target.value.split(":");
          setAsset(a); setChain(c);
        }}>
          <option value="USDT:tron">USDT (TRON)</option>
          <option value="USDT:ethereum">USDT (ERC-20)</option>
          <option value="BTC:bitcoin">BTC</option>
          <option value="ETH:ethereum">ETH</option>
          <option value="SOL:solana">SOL</option>
        </select>
        <input placeholder="Amount" value={amount} inputMode="decimal"
          onChange={(e) => setAmount(e.target.value)} />
      </div>
      <input placeholder="Note (optional)" value={note} maxLength={200}
        onChange={(e) => setNote(e.target.value)} />
      {error && <div className="error">{error}</div>}
      {done && <div className="badge green">Sent!</div>}
      <div className="row" style={{ gap: 6 }}>
        <button onClick={submit}>Send</button>
        <button className="secondary" onClick={onClose}>Cancel</button>
      </div>
    </div>
  );
}
