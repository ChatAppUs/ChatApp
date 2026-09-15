"use client";

import { useCallback, useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { api, getAccessToken } from "@/lib/api";
import { encryptFor, publishIdentityKey } from "@/lib/e2e";

interface MeshStatus {
  mesh: string;
  queued: number;
  delivered: number;
  expired: number;
}

interface MeshPacket {
  packet_id: string;
  payload: string;
  ttl: number;
  hops: number;
  created_at: string;
}

// Device key persists per browser so the mesh relay can attribute packets to
// this device across reloads (same header contract as Android/iOS clients).
const DEVICE_KEY = "chatapp.meshDeviceKey";
function deviceKey(): string {
  try {
    let k = localStorage.getItem(DEVICE_KEY);
    if (!k) {
      k = "web_" + crypto.randomUUID();
      localStorage.setItem(DEVICE_KEY, k);
    }
    return k;
  } catch {
    return "web_anonymous";
  }
}

export default function MeshPage() {
  const router = useRouter();
  const [status, setStatus] = useState<MeshStatus | null>(null);
  const [packets, setPackets] = useState<MeshPacket[]>([]);
  const [dest, setDest] = useState("");
  const [message, setMessage] = useState("");
  const [notice, setNotice] = useState("");
  const [error, setError] = useState("");
  const key = deviceKey();

  const register = useCallback(async () => {
    await api("/api/mesh/register", {
      method: "POST",
      headers: { "X-Mesh-Device": key },
      body: JSON.stringify({ device_key: key, transport: "internet" }),
    });
  }, [key]);

  const load = useCallback(async () => {
    try {
      await register();
      const s = await api<MeshStatus>("/api/mesh/status", {
        headers: { "X-Mesh-Device": key },
      });
      setStatus(s);
      setError("");
    } catch (e) {
      setError(e instanceof Error ? e.message : "status failed");
    }
  }, [key, register]);

  const poll = useCallback(async () => {
    try {
      const d = await api<{ packets: MeshPacket[] }>("/api/mesh/poll", {
        headers: { "X-Mesh-Device": key },
      });
      setPackets(d.packets ?? []);
    } catch (e) {
      setError(e instanceof Error ? e.message : "poll failed");
    }
  }, [key]);

  useEffect(() => {
    if (!getAccessToken()) {
      router.push("/login");
      return;
    }
    load();
  }, [load, router]);

  const send = async (e: React.FormEvent) => {
    e.preventDefault();
    setError("");
    try {
      await publishIdentityKey();
      const encrypted = await encryptFor(dest, message);
      const res = await api<{ status: string; packet_id: string }>("/api/mesh/send", {
        method: "POST",
        headers: { "X-Mesh-Device": key },
        body: JSON.stringify({
          packet_id: crypto.randomUUID(),
          dest_device_key: dest,
          payload: btoa(unescape(encodeURIComponent(encrypted))),
          ttl: 8,
        }),
      });
      setNotice(`Packet ${res.packet_id} ${res.status}`);
      setMessage("");
      load();
    } catch (err) {
      setError(err instanceof Error ? err.message : "send failed");
    }
  };

  return (
    <main>
      <h2>Offline Mesh</h2>
      <p className="muted">
        Store-and-forward relay for offline messaging. Packets are queued server-side
        and delivered when the destination device polls. Your device key:{" "}
        <code>{key}</code>
      </p>
      {error && <p className="error">{error}</p>}
      {notice && <p className="muted">{notice}</p>}

      {status && (
        <section className="card">
          <h3>Relay status</h3>
          <p>
            Mesh: {status.mesh} · queued: {status.queued} · delivered:{" "}
            {status.delivered} · expired: {status.expired}
          </p>
          <button className="secondary" onClick={load}>Refresh</button>
        </section>
      )}

      <section className="card">
        <h3>Queue an offline packet</h3>
        <form onSubmit={send}>
          <input
            value={dest}
            onChange={(e) => setDest(e.target.value)}
            placeholder="Destination account ID (must have an E2E identity key)"
            required
          />
          <input
            value={message}
            onChange={(e) => setMessage(e.target.value)}
            placeholder="Message (encrypted client-side by the mesh engine)"
            required
          />
          <button type="submit" disabled={!dest || !message}>Queue packet</button>
        </form>
      </section>

      <section className="card">
        <h3>Delivered packets</h3>
        <button className="secondary" onClick={poll}>Poll now</button>
        {packets.length === 0 ? (
          <p className="muted">No packets waiting for this device.</p>
        ) : (
          <ul>
            {packets.map((p) => (
              <li key={p.packet_id}>
                <code>{p.packet_id}</code> · hops {p.hops} · ttl {p.ttl} ·{" "}
                {p.created_at}
              </li>
            ))}
          </ul>
        )}
      </section>
    </main>
  );
}
