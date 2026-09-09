"use client";

import { FormEvent, useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { api, getAccessToken } from "@/lib/api";

type HandleLookup = {
  id: string;
  title: string;
  description: string;
  is_group: boolean;
  is_channel: boolean;
  member_count: number;
};

export default function HandlesPage() {
  const router = useRouter();
  const [handle, setHandle] = useState("");
  const [result, setResult] = useState<HandleLookup | null>(null);
  const [joined, setJoined] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    if (!getAccessToken()) router.push("/login");
  }, [router]);

  const lookUp = async (e: FormEvent) => {
    e.preventDefault();
    setError("");
    setResult(null);
    setJoined("");
    const h = handle.trim().replace(/^@/, "");
    if (!h) return;
    try {
      const d = await api<HandleLookup>(`/api/handles/${encodeURIComponent(h)}`);
      setResult(d);
    } catch (e) {
      setError(e instanceof Error ? e.message : "handle not found");
    }
  };

  const join = async () => {
    if (!result) return;
    setBusy(true);
    setError("");
    setJoined("");
    try {
      const d = await api<{ conversation_id: string }>(`/api/handles/${encodeURIComponent(handle.trim().replace(/^@/, ""))}/join`, {
        method: "POST",
        body: "{}",
      });
      setJoined(`Joined — conversation ${d.conversation_id}. Open the Chat tab and it will appear in your conversations.`);
    } catch (e) {
      setError(e instanceof Error ? e.message : "failed to join");
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="col">
      <div className="card col" style={{ gap: 8 }}>
        <h2 style={{ margin: 0 }}>🪪 Handles</h2>
        <div className="muted">Anyone can get a public @handle for a group or channel. Look one up and join — no invite needed for public rooms.</div>
        {error && <div className="error-text">{error}</div>}
        <form onSubmit={lookUp} className="row" style={{ gap: 6 }}>
          <input value={handle} onChange={(e) => setHandle(e.target.value)} placeholder="@handlename" style={{ flex: 1 }} />
          <button type="submit" disabled={!handle.trim()}>Look up</button>
        </form>
      </div>
      {result && (
        <div className="card col" style={{ gap: 8 }}>
          <div className="row">
            <strong style={{ fontSize: 18 }}>{result.title}</strong>
            <span className="badge green">{result.is_channel ? "Channel" : result.is_group ? "Group" : "Chat"}</span>
          </div>
          {result.description && <div className="muted">{result.description}</div>}
          <div className="muted">{result.member_count} member{result.member_count === 1 ? "" : "s"}</div>
          {joined ? (
            <div className="muted" style={{ color: "var(--accent-2)" }}>{joined}</div>
          ) : (
            <div>
              <button className="small" disabled={busy} onClick={join}>Join @{handle.trim().replace(/^@/, "")}</button>
            </div>
          )}
        </div>
      )}
    </div>
  );
}