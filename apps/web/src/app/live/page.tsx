"use client";

import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { api, getAccessToken } from "@/lib/api";

interface LiveRoom {
  room_id: string;
  conversation_id: string;
  title: string;
  viewers: number;
}

export default function LiveDiscoveryPage() {
  const router = useRouter();
  const [rooms, setRooms] = useState<LiveRoom[]>([]);
  const [error, setError] = useState("");

  useEffect(() => {
    if (!getAccessToken()) {
      router.push("/login");
      return;
    }
    const load = () =>
      api<{ live: LiveRoom[] }>("/api/live")
        .then((d) => setRooms(d.live))
        .catch((e) => setError(e.message));
    load();
    const iv = setInterval(load, 10000);
    return () => clearInterval(iv);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  return (
    <div className="card">
      <div className="row">
        <h3 style={{ margin: 0 }}>Live now</h3>
        <div className="spacer" />
        <button className="secondary small" onClick={() => router.push("/chat")}>Back</button>
      </div>
      {error && <div className="error-text" style={{ marginTop: 8 }}>{error}</div>}
      {rooms.length === 0 && !error && (
        <p className="muted" style={{ marginTop: 12 }}>No live broadcasts right now.</p>
      )}
      <div style={{ marginTop: 12, display: "grid", gap: 8 }}>
        {rooms.map((r) => (
          <div key={r.room_id} className="row card" style={{ margin: 0 }}>
            <span>🔴 {r.title || "Live broadcast"}</span>
            <span className="muted">{r.viewers} watching</span>
            <div className="spacer" />
            <button
              className="small"
              onClick={() => router.push(`/live/${r.room_id}?conv=${r.conversation_id}`)}
            >
              Watch
            </button>
          </div>
        ))}
      </div>
    </div>
  );
}
