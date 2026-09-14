"use client";

// FlagsTab — feature flags, experiments and media telemetry (§72-§75).
import { useCallback, useEffect, useState } from "react";
import { adminApi } from "@/lib/api";

interface Flag {
  key: string;
  description: string;
  enabled: boolean;
  rollout_pct: number;
  regions: string[];
  platforms: string[];
}

interface Experiment {
  key: string;
  flag_key: string;
  description: string;
}

interface FlagSummary {
  key: string;
  enabled: boolean;
  rollout_pct: number;
}

interface ExperimentResults {
  experiment: string;
  flag: string;
  variants: Array<{
    variant: string;
    users: number;
    reports: number;
    completion_rate: number;
    avg_watch_ms: number;
  }>;
}

interface QoESummary {
  window: string;
  events: number;
  startup_ms_p50: number;
  startup_ms_p95: number;
  buffer_ms_p50: number;
  playback_failure_rate: number;
  completion_rate: number;
  avg_bitrate_kbps: number;
}

interface CallQoESummary {
  window: string;
  events: number;
  packet_loss_pct_p50: number;
  jitter_ms_p50: number;
  rtt_ms_p50: number;
  bitrate_kbps_p50: number;
  frame_rate_p50: number;
}

export function FlagsTab({ act }: { act: (fn: () => Promise<unknown>) => void }) {
  const [flags, setFlags] = useState<Flag[]>([]);
  const [experiments, setExperiments] = useState<Experiment[]>([]);
  const [results, setResults] = useState<ExperimentResults | null>(null);
  const [qoe, setQoe] = useState<QoESummary | null>(null);
  const [calls, setCalls] = useState<CallQoESummary | null>(null);
  const [newFlag, setNewFlag] = useState({ key: "", description: "", rollout_pct: 0, regions: "", platforms: "" });
  const [newExp, setNewExp] = useState({ key: "", flag_key: "", description: "" });
  const [msg, setMsg] = useState("");
  const [error, setError] = useState("");

  const load = useCallback(() => {
    adminApi<{ flags: Flag[] }>("/api/admin/flags").then((d) => setFlags(d.flags)).catch(() => {});
    adminApi<{ experiments: Experiment[] }>("/api/admin/experiments").then((d) => setExperiments(d.experiments)).catch(() => {});
    adminApi<QoESummary>("/api/admin/qoe/summary").then(setQoe).catch(() => {});
    adminApi<CallQoESummary>("/api/admin/call-quality/summary").then(setCalls).catch(() => {});
  }, []);
  useEffect(() => { load(); }, [load]);

  const saveFlag = () => {
    setMsg(""); setError("");
    act(async () => {
      await adminApi("/api/admin/flags", {
        method: "POST",
        body: JSON.stringify({
          key: newFlag.key.trim(),
          description: newFlag.description,
          rollout_pct: Number(newFlag.rollout_pct) || 0,
          regions: newFlag.regions.split(",").map((s) => s.trim()).filter(Boolean),
          platforms: newFlag.platforms.split(",").map((s) => s.trim()).filter(Boolean),
        }),
      });
      setMsg(`Flag ${newFlag.key.trim()} saved`);
      setNewFlag({ key: "", description: "", rollout_pct: 0, regions: "", platforms: "" });
      load();
    });
  };

  const toggleFlag = (f: FlagSummary) => {
    act(async () => {
      await adminApi(`/api/admin/flags/${encodeURIComponent(f.key)}`, {
        method: "PUT",
        body: JSON.stringify({ enabled: !f.enabled }),
      });
      load();
    });
  };

  const deleteFlag = (key: string) => {
    act(async () => {
      await adminApi(`/api/admin/flags/${encodeURIComponent(key)}`, { method: "DELETE" });
      load();
    });
  };

  const createExperiment = () => {
    setMsg(""); setError("");
    act(async () => {
      await adminApi("/api/admin/experiments", {
        method: "POST",
        body: JSON.stringify({ key: newExp.key.trim(), flag_key: newExp.flag_key.trim(), description: newExp.description }),
      });
      setMsg(`Experiment ${newExp.key.trim()} created`);
      setNewExp({ key: "", flag_key: "", description: "" });
      load();
    });
  };

  const loadResults = (key: string) => {
    adminApi<ExperimentResults>(`/api/admin/experiments/${encodeURIComponent(key)}/results`)
      .then(setResults).catch(() => {});
  };

  return (
    <div className="col" style={{ gap: 16 }}>
      <section className="card">
        <h3>Feature flags</h3>
        <p className="muted">Percentage, region and platform rollouts with stable per-user buckets.</p>
        <div className="row" style={{ marginBottom: 8 }}>
          <button className="secondary" onClick={load}>Refresh</button>
        </div>
        <table className="table">
          <thead><tr><th>Key</th><th>Rollout</th><th>Regions</th><th>Platforms</th><th>Enabled</th><th></th></tr></thead>
          <tbody>
            {flags.map((f) => (
              <tr key={f.key}>
                <td>{f.key}</td>
                <td>{f.rollout_pct}%</td>
                <td>{f.regions.length ? f.regions.join(", ") : "all"}</td>
                <td>{f.platforms.length ? f.platforms.join(", ") : "all"}</td>
                <td>
                  <button className={f.enabled ? "small" : "secondary small"} onClick={() => toggleFlag(f)}>
                    {f.enabled ? "on" : "off"}
                  </button>
                </td>
                <td><button className="secondary small" onClick={() => deleteFlag(f.key)}>delete</button></td>
              </tr>
            ))}
            {flags.length === 0 && <tr><td colSpan={6} className="muted">No flags</td></tr>}
          </tbody>
        </table>
        <div className="row" style={{ flexWrap: "wrap", marginTop: 8 }}>
          <input placeholder="flag key" value={newFlag.key} style={{ minWidth: 160 }}
            onChange={(e) => setNewFlag({ ...newFlag, key: e.target.value })} />
          <input placeholder="description" value={newFlag.description} style={{ minWidth: 200 }}
            onChange={(e) => setNewFlag({ ...newFlag, description: e.target.value })} />
          <input placeholder="rollout %" type="number" min={0} max={100} value={newFlag.rollout_pct} style={{ width: 110 }}
            onChange={(e) => setNewFlag({ ...newFlag, rollout_pct: Number(e.target.value) })} />
          <input placeholder="regions (csv)" value={newFlag.regions} style={{ width: 140 }}
            onChange={(e) => setNewFlag({ ...newFlag, regions: e.target.value })} />
          <input placeholder="platforms (csv)" value={newFlag.platforms} style={{ width: 140 }}
            onChange={(e) => setNewFlag({ ...newFlag, platforms: e.target.value })} />
          <button onClick={saveFlag}>Save flag</button>
        </div>
      </section>

      <section className="card">
        <h3>Experiments</h3>
        <p className="muted">Attach an experiment to a flag to compare on/off variants across completion, watch time, reports and unwatched time.</p>
        <table className="table">
          <thead><tr><th>Key</th><th>Flag</th><th>Description</th><th></th></tr></thead>
          <tbody>
            {experiments.map((e) => (
              <tr key={e.key}>
                <td>{e.key}</td><td>{e.flag_key}</td><td>{e.description}</td>
                <td><button className="secondary small" onClick={() => loadResults(e.key)}>results</button></td>
              </tr>
            ))}
            {experiments.length === 0 && <tr><td colSpan={4} className="muted">No experiments</td></tr>}
          </tbody>
        </table>
        <div className="row" style={{ flexWrap: "wrap", marginTop: 8 }}>
          <input placeholder="experiment key" value={newExp.key} style={{ minWidth: 160 }}
            onChange={(e) => setNewExp({ ...newExp, key: e.target.value })} />
          <input placeholder="flag key" value={newExp.flag_key} style={{ minWidth: 160 }}
            onChange={(e) => setNewExp({ ...newExp, flag_key: e.target.value })} />
          <input placeholder="description" value={newExp.description} style={{ minWidth: 200 }}
            onChange={(e) => setNewExp({ ...newExp, description: e.target.value })} />
          <button onClick={createExperiment}>Create experiment</button>
        </div>
        {results && (
          <table className="table" style={{ marginTop: 8 }}>
            <thead><tr><th>Variant</th><th>Users</th><th>Reports</th><th>Completion</th><th>Avg watch (ms)</th></tr></thead>
            <tbody>
              {results.variants.map((v) => (
                <tr key={v.variant}>
                  <td>{v.variant}</td><td>{v.users}</td><td>{v.reports}</td>
                  <td>{(v.completion_rate * 100).toFixed(1)}%</td><td>{v.avg_watch_ms}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </section>

      <section className="card">
        <h3>Video QoE (24h)</h3>
        {qoe ? (
          <div className="row" style={{ flexWrap: "wrap", gap: 16 }}>
            <div>events: <strong>{qoe.events}</strong></div>
            <div>startup p50: <strong>{qoe.startup_ms_p50} ms</strong></div>
            <div>startup p95: <strong>{qoe.startup_ms_p95} ms</strong></div>
            <div>buffer p50: <strong>{qoe.buffer_ms_p50} ms</strong></div>
            <div>failures: <strong>{(qoe.playback_failure_rate * 100).toFixed(2)}%</strong></div>
            <div>completion: <strong>{(qoe.completion_rate * 100).toFixed(1)}%</strong></div>
            <div>bitrate avg: <strong>{qoe.avg_bitrate_kbps} kbps</strong></div>
          </div>
        ) : <p className="muted">No QoE data yet.</p>}
      </section>

      <section className="card">
        <h3>Call quality (24h)</h3>
        {calls ? (
          <div className="row" style={{ flexWrap: "wrap", gap: 16 }}>
            <div>events: <strong>{calls.events}</strong></div>
            <div>packet loss p50: <strong>{calls.packet_loss_pct_p50}%</strong></div>
            <div>jitter p50: <strong>{calls.jitter_ms_p50} ms</strong></div>
            <div>rtt p50: <strong>{calls.rtt_ms_p50} ms</strong></div>
            <div>bitrate p50: <strong>{calls.bitrate_kbps_p50} kbps</strong></div>
            <div>fps p50: <strong>{calls.frame_rate_p50}</strong></div>
          </div>
        ) : <p className="muted">No call-quality data yet.</p>}
      </section>

      {msg && <div className="ok-text">{msg}</div>}
      {error && <div className="error-text">{error}</div>}
    </div>
  );
}
