"use client";

import { useCallback, useEffect, useState } from "react";
import { api, getAccessToken } from "@/lib/api";

// AI creator tools — master plan §23. Dubbing and clip generation are
// provider-backed; when a model is not configured the API reports the reason
// and this page shows it plainly instead of a fake result.

type Dub = {
  id: string;
  media_url: string;
  target_lang: string;
  status: string;
  audio_url: string;
  label: string;
  reason: string;
};
type Clip = {
  id: string;
  start_s: string;
  end_s: string;
  score: string;
  reason: string;
  title: string;
  approved: boolean;
};
type ClipJob = {
  id: string;
  media_url: string;
  duration_s: string;
  status: string;
  reason: string;
  clips: Clip[] | null;
};

export default function AiStudioPage() {
  const [authed, setAuthed] = useState(false);
  const [tab, setTab] = useState<"dub" | "clips">("dub");
  const [dubs, setDubs] = useState<Dub[]>([]);
  const [jobs, setJobs] = useState<ClipJob[]>([]);
  const [mediaURL, setMediaURL] = useState("");
  const [target, setTarget] = useState("es");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [note, setNote] = useState("");

  const load = useCallback(async () => {
    try {
      const [d, c] = await Promise.all([
        api<{ dubs: Dub[] }>("/api/ai/dubs"),
        api<{ jobs: ClipJob[] }>("/api/ai/clips"),
      ]);
      setDubs(d.dubs ?? []);
      setJobs(c.jobs ?? []);
    } catch (e) {
      setError(String(e));
    }
  }, []);

  useEffect(() => {
    setAuthed(!!getAccessToken());
    if (getAccessToken()) void load();
  }, [load]);

  const startDub = async () => {
    setBusy(true);
    setNote("");
    setError("");
    try {
      const r = await api<{ status: string; reason: string; label: string }>("/api/ai/dub", {
        method: "POST",
        body: JSON.stringify({ media_url: mediaURL, target_lang: target }),
      });
      setNote(
        r.status === "ready"
          ? `Dubbed audio ready — labelled "${r.label}".`
          : `Not available: ${r.reason}`
      );
      await load();
    } catch (e) {
      setError(String(e));
    } finally {
      setBusy(false);
    }
  };

  const analyze = async () => {
    setBusy(true);
    setNote("");
    setError("");
    try {
      const r = await api<{ status: string; reason: string; clips: Clip[] }>(
        "/api/ai/clips/analyze",
        { method: "POST", body: JSON.stringify({ media_url: mediaURL }) }
      );
      setNote(
        r.status === "analyzed"
          ? `Found ${r.clips.length} candidate clip(s).`
          : `Not available: ${r.reason}`
      );
      await load();
    } catch (e) {
      setError(String(e));
    } finally {
      setBusy(false);
    }
  };

  const decide = async (clip: Clip, approve: boolean) => {
    try {
      await api(`/api/ai/clips/${clip.id}/approve`, {
        method: "POST",
        body: JSON.stringify({ approve }),
      });
      await load();
    } catch (e) {
      setError(String(e));
    }
  };

  if (!authed) {
    return (
      <main className="container">
        <h1>AI Studio</h1>
        <p>Sign in to use dubbing and clip generation.</p>
        <a className="btn" href="/login">Sign in</a>
      </main>
    );
  }

  return (
    <main className="container">
      <h1>AI Studio</h1>
      {error && <p className="error">{error}</p>}
      {note && <p className="note">{note}</p>}

      <section className="card">
        <input
          placeholder="Media URL (https://…)"
          value={mediaURL}
          onChange={(e) => setMediaURL(e.target.value)}
        />
        <div className="row">
          <button className={tab === "dub" ? "btn" : "btn ghost"} onClick={() => setTab("dub")}>
            Dubbing
          </button>
          <button className={tab === "clips" ? "btn" : "btn ghost"} onClick={() => setTab("clips")}>
            Clips
          </button>
        </div>
        {tab === "dub" ? (
          <div className="row">
            <input
              placeholder="Target language (es, fr, hi…)"
              value={target}
              onChange={(e) => setTarget(e.target.value)}
            />
            <button className="btn" onClick={startDub} disabled={busy || !mediaURL}>
              {busy ? "Working…" : "Dub this media"}
            </button>
          </div>
        ) : (
          <button className="btn" onClick={analyze} disabled={busy || !mediaURL}>
            {busy ? "Analyzing…" : "Find short clips"}
          </button>
        )}
      </section>

      {tab === "dub" && (
        <section className="card">
          <h2>Dubbing jobs</h2>
          <ul className="list">
            {dubs.map((d) => (
              <li key={d.id}>
                <div>
                  <strong>{d.status}</strong> → {d.target_lang}{" "}
                  <span className="muted">{d.media_url}</span>
                </div>
                {d.status === "ready" ? (
                  <>
                    <div className="muted">Label: {d.label}</div>
                    <audio controls src={d.audio_url} />
                  </>
                ) : (
                  <div className="muted">Reason: {d.reason}</div>
                )}
              </li>
            ))}
            {dubs.length === 0 && <li className="muted">No dubbing jobs yet.</li>}
          </ul>
        </section>
      )}

      {tab === "clips" && (
        <section className="card">
          <h2>Clip jobs</h2>
          {jobs.map((j) => (
            <div key={j.id} className="card">
              <div>
                <strong>{j.status}</strong>{" "}
                <span className="muted">
                  {j.duration_s}s · {j.media_url}
                </span>
              </div>
              {j.status !== "analyzed" && <div className="muted">Reason: {j.reason}</div>}
              <ul className="list">
                {(j.clips ?? []).map((c) => (
                  <li key={c.id}>
                    <div>
                      <strong>{c.title}</strong>{" "}
                      <span className="muted">
                        {c.start_s}s–{c.end_s}s · score {c.score}
                      </span>
                    </div>
                    <div className="muted">{c.reason}</div>
                    <button className="btn small" onClick={() => decide(c, !c.approved)}>
                      {c.approved ? "Approved — revoke" : "Approve for publishing"}
                    </button>
                  </li>
                ))}
              </ul>
            </div>
          ))}
          {jobs.length === 0 && <p className="muted">No clip jobs yet.</p>}
        </section>
      )}
    </main>
  );
}
