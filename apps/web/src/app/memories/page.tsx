"use client";

import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { api, getAccessToken } from "@/lib/api";

type Memory = {
  id: string;
  kind: string;
  note: string;
  on_date: string;
  source: {
    id: string;
    body: string;
    media_url: string;
  };
};

export default function MemoriesPage() {
  const router = useRouter();
  const [all, setAll] = useState(false);
  const [memories, setMemories] = useState<Memory[]>([]);
  const [error, setError] = useState("");
  const [status, setStatus] = useState("");

  const load = (includeAll: boolean) =>
    api<{ memories: Memory[] }>(`/api/me/memories${includeAll ? "?all=1" : ""}`)
      .then((d) => setMemories(d.memories ?? []))
      .catch((e) => setError(e instanceof Error ? e.message : "failed to load memories"));

  useEffect(() => {
    if (!getAccessToken()) {
      router.push("/login");
      return;
    }
    load(false);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const dismiss = async (id: string) => {
    setError("");
    try {
      await api(`/api/me/memories/${id}`, { method: "DELETE" });
      setMemories((prev) => prev.filter((m) => m.id !== id));
    } catch (e) {
      setError(e instanceof Error ? e.message : "dismiss failed");
    }
  };

  return (
    <div className="col">
      <div className="card col" style={{ gap: 8 }}>
        <div className="row">
          <h2 style={{ margin: 0 }}>🕰️ Memories</h2>
          <div className="spacer" />
          <button className={all ? "small" : "secondary small"} onClick={() => { setAll(!all); load(!all); }}>
            {all ? "Today ↑" : "Show all"}
          </button>
        </div>
        <div className="muted">“On this day” — your archived stories and posts from this date in past years. Dismiss one and it will not reappear.</div>
        {error && <div className="error-text">{error}</div>}
        {status && <div className="muted" style={{ color: "var(--accent-2)" }}>{status}</div>}
      </div>
      {memories.length === 0 && !error && <div className="card muted">No memories for this date — keep posting and archiving stories to see them here.</div>}
      {memories.map((m) => (
        <div key={m.id} className="card col" style={{ gap: 8 }}>
          <div className="row">
            <span className="badge">{m.kind}</span>
            <span className="muted" style={{ fontSize: 12 }}>{new Date(m.on_date).toLocaleDateString()}</span>
            <div className="spacer" />
            <button className="secondary small" onClick={() => dismiss(m.id)}>Dismiss</button>
          </div>
          {m.note && <div className="muted">{m.note}</div>}
          {m.source.media_url && (
            // eslint-disable-next-line @next/next/no-img-element
            <img src={m.source.media_url} alt="" style={{ maxWidth: "100%", borderRadius: 12 }} />
          )}
          {m.source.body && <div>{m.source.body}</div>}
        </div>
      ))}
    </div>
  );
}