"use client";

import { useCallback, useEffect, useState } from "react";
import { adminApi } from "@/lib/api";

type ModEntry = {
  id: string;
  media_url: string;
  sha256: string;
  dhash: string;
  decision: string;
  reason: string;
  created_at: string;
};

type BlockedHash = {
  id: string;
  sha256: string;
  dhash: string;
  reason: string;
  created_at: string;
};

type DerivedRate = { from_asset: string; to_asset: string; rate: number; trades: number };

export function SafetyTab() {
  const [sha, setSha] = useState("");
  const [reason, setReason] = useState("");
  const [entries, setEntries] = useState<ModEntry[]>([]);
  const [hashes, setHashes] = useState<BlockedHash[]>([]);
  const [stats, setStats] = useState<{ total: number; by_source?: Record<string, number> } | null>(null);
  const [csv, setCsv] = useState("");
  const [msg, setMsg] = useState("");
  const [error, setError] = useState("");

  const load = useCallback(async () => {
    try {
      const m = await adminApi<{ entries: ModEntry[] }>("/api/admin/moderation/media");
      setEntries(m.entries);
      const h = await adminApi<{ hashes: BlockedHash[] }>("/api/admin/moderation/blocked-hashes");
      setHashes(h.hashes);
      const s = await adminApi<{ total: number; by_source?: Record<string, number> }>("/api/admin/sanctions/stats");
      setStats(s);
    } catch (e) {
      setError(e instanceof Error ? e.message : "failed to load");
    }
  }, []);

  useEffect(() => { load(); }, [load]);

  const blockHash = async () => {
    setError(""); setMsg("");
    try {
      await adminApi("/api/admin/moderation/block-hash", {
        method: "POST",
        body: JSON.stringify({ sha256: sha.trim(), reason: reason.trim() }),
      });
      setSha(""); setReason("");
      setMsg("Hash blocked");
      load();
    } catch (e) {
      setError(e instanceof Error ? e.message : "failed");
    }
  };

  const unblock = async (id: string) => {
    await adminApi(`/api/admin/moderation/block-hash/${id}`, { method: "DELETE" }).catch(() => {});
    load();
  };

  const importCsv = async () => {
    setError(""); setMsg("");
    try {
      const r = await adminApi<{ imported: number; skipped: number }>("/api/admin/sanctions/import", {
        method: "POST",
        headers: { "Content-Type": "text/csv" },
        body: csv,
      });
      setMsg(`Imported ${r.imported}, skipped ${r.skipped}`);
      setCsv("");
      load();
    } catch (e) {
      setError(e instanceof Error ? e.message : "import failed");
    }
  };

  return (
    <div className="col" style={{ gap: 16 }}>
      <section className="card">
        <h3>Blocked media hashes</h3>
        <div className="row">
          <input placeholder="sha256 (64 hex)" value={sha} maxLength={64}
            onChange={(e) => setSha(e.target.value)} />
          <input placeholder="reason" value={reason} maxLength={200}
            onChange={(e) => setReason(e.target.value)} />
          <button onClick={blockHash} disabled={!/^[0-9a-f]{64}$/i.test(sha.trim())}>Block</button>
        </div>
        <table className="table">
          <thead><tr><th>SHA-256</th><th>dHash</th><th>Reason</th><th>Added</th><th /></tr></thead>
          <tbody>
            {hashes.map((h) => (
              <tr key={h.id}>
                <td style={{ fontFamily: "monospace" }}>{h.sha256 ? `${h.sha256.slice(0, 16)}…` : "—"}</td>
                <td style={{ fontFamily: "monospace" }}>{h.dhash || "—"}</td>
                <td>{h.reason}</td>
                <td>{new Date(h.created_at).toLocaleString()}</td>
                <td><button className="danger small" onClick={() => unblock(h.id)}>Unblock</button></td>
              </tr>
            ))}
            {hashes.length === 0 && <tr><td colSpan={5} className="muted">No blocked hashes</td></tr>}
          </tbody>
        </table>
      </section>

      <section className="card">
        <h3>Media moderation log</h3>
        <table className="table">
          <thead><tr><th>Media</th><th>Decision</th><th>Reason</th><th>At</th></tr></thead>
          <tbody>
            {entries.map((e) => (
              <tr key={e.id}>
                <td style={{ fontFamily: "monospace" }}>
                  {e.media_url ? <a href={e.media_url} target="_blank" rel="noreferrer">view</a> : (e.sha256 || e.dhash || "—")}
                </td>
                <td>{e.decision}</td>
                <td>{e.reason}</td>
                <td>{new Date(e.created_at).toLocaleString()}</td>
              </tr>
            ))}
            {entries.length === 0 && <tr><td colSpan={4} className="muted">No moderation entries</td></tr>}
          </tbody>
        </table>
      </section>

      <section className="card">
        <h3>Sanctions list</h3>
        {stats && (
          <p className="muted">
            {stats.total} entries
            {stats.by_source && Object.entries(stats.by_source).map(([k, v]) => ` · ${k}: ${v}`)}
          </p>
        )}
        <textarea rows={4} placeholder={"source,name,program\nofac,John Doe,SDN"}
          value={csv} onChange={(e) => setCsv(e.target.value)} />
        <div className="row">
          <button onClick={importCsv} disabled={!csv.trim()}>Import CSV</button>
        </div>
      </section>

      {msg && <div className="ok">{msg}</div>}
      {error && <div className="error">{error}</div>}
    </div>
  );
}

export function DerivedRatesTab() {
  const [derived, setDerived] = useState<DerivedRate[]>([]);
  const [msg, setMsg] = useState("");
  const [error, setError] = useState("");

  const load = useCallback(async () => {
    try {
      const r = await adminApi<{ derived: DerivedRate[] }>("/api/admin/convert/rates/derived");
      setDerived(r.derived);
    } catch (e) {
      setError(e instanceof Error ? e.message : "failed");
    }
  }, []);

  useEffect(() => { load(); }, [load]);

  const apply = async () => {
    setMsg(""); setError("");
    try {
      const r = await adminApi<{ applied: number }>("/api/admin/convert/rates/apply-derived", {
        method: "POST", body: "{}",
      });
      setMsg(`Applied ${r.applied} rates from the P2P order book`);
      load();
    } catch (e) {
      setError(e instanceof Error ? e.message : "apply failed");
    }
  };

  return (
    <section className="card">
      <h3>Rates derived from P2P order book</h3>
      <p className="muted">Prices discovered from completed P2P trades on this platform.</p>
      <div className="row" style={{ marginBottom: 8 }}>
        <button className="secondary" onClick={load}>Refresh</button>
        <button onClick={apply}>Apply as convert rates</button>
      </div>
      <table className="table">
        <thead><tr><th>Pair</th><th>Rate</th><th>Trades</th></tr></thead>
        <tbody>
          {derived.map((d) => (
            <tr key={`${d.from_asset}-${d.to_asset}`}>
              <td>{d.from_asset} → {d.to_asset}</td>
              <td>{d.rate}</td>
              <td>{d.trades}</td>
            </tr>
          ))}
        </tbody>
      </table>
      {msg && <div className="ok">{msg}</div>}
      {error && <div className="error">{error}</div>}
    </section>
  );
}
