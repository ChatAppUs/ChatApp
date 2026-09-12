"use client";

import { useCallback, useEffect, useState } from "react";
import { api } from "@/lib/api";

interface Earnings {
  earned: number;
  paid_out: number;
  available: number;
  currency: string;
}

interface Payout {
  id: string;
  amount: number;
  asset: string;
  status: string;
  destination: string;
  created_at: string;
}

interface CreatorInsights {
  daily: Array<{ day: string; reach: number; impressions: number; watch_time_s: number; new_followers: number; top_sound: string }>;
  totals: { reach: number; impressions: number; watch_time_s: number; new_followers: number; top_sound: string };
}

export default function CreatorPage() {
  const [earnings, setEarnings] = useState<Earnings | null>(null);
  const [payouts, setPayouts] = useState<Payout[]>([]);
  const [insights, setInsights] = useState<CreatorInsights | null>(null);
  const [amount, setAmount] = useState("");
  const [destination, setDestination] = useState("");
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");

  const load = useCallback(() => {
    api<Earnings>("/api/creator/earnings").then(setEarnings).catch(() => {});
    api<{ payouts: Payout[] }>("/api/creator/payouts")
      .then((d) => setPayouts(d.payouts))
      .catch(() => {});
    api<CreatorInsights>("/api/creator/insights?days=14")
      .then(setInsights)
      .catch(() => {});
  }, []);

  useEffect(load, [load]);

  const requestPayout = async () => {
    setError("");
    setNotice("");
    try {
      await api("/api/creator/payouts", {
        method: "POST",
        body: JSON.stringify({ amount: parseFloat(amount), asset: "USD", destination }),
      });
      setNotice("Payout requested — finance team will review it.");
      setAmount("");
      setDestination("");
      load();
    } catch (e) {
      setError(e instanceof Error ? e.message : "payout failed");
    }
  };

  return (
    <div className="col">
      <div className="card">
        <h2 style={{ marginTop: 0 }}>Creator studio</h2>
        {earnings && (
          <div className="row" style={{ flexWrap: "wrap" }}>
            <span className="badge green">Earned: ${earnings.earned.toFixed(2)}</span>
            <span className="badge">Paid out: ${earnings.paid_out.toFixed(2)}</span>
            <span className="badge yellow">Available: ${earnings.available.toFixed(2)}</span>
          </div>
        )}
        <p className="muted">
          Earnings accrue from views on your posts and reels. KYC verification is required before payouts.
        </p>
      </div>
      <div className="card col">
        <h3 style={{ marginTop: 0 }}>Reach and engagement</h3>
        {insights ? (
          <>
            <div className="row" style={{ flexWrap: "wrap" }}>
              <span className="badge">Reach: {insights.totals.reach.toLocaleString()}</span>
              <span className="badge">Impressions: {insights.totals.impressions.toLocaleString()}</span>
              <span className="badge">Watch time: {Math.round(insights.totals.watch_time_s / 60)} min</span>
              <span className="badge green">New followers: {insights.totals.new_followers.toLocaleString()}</span>
            </div>
            <p className="muted">Top sound: {insights.totals.top_sound || "No sound data yet"}</p>
            <div className="col" style={{ gap: 6 }}>
              {insights.daily.map((row) => (
                <div key={row.day} className="row" style={{ fontSize: 13 }}>
                  <span>{new Date(row.day).toLocaleDateString()}</span>
                  <span className="muted">reach {row.reach.toLocaleString()} · impressions {row.impressions.toLocaleString()} · followers +{row.new_followers.toLocaleString()}</span>
                </div>
              ))}
            </div>
            {insights.daily.length === 0 && <span className="muted">Analytics will appear after your content receives activity.</span>}
          </>
        ) : (
          <span className="muted">Loading analytics…</span>
        )}
      </div>
      <div className="card col">
        <h3 style={{ marginTop: 0 }}>Request payout</h3>
        <input type="number" min="1" step="0.01" placeholder="Amount (USD)" value={amount}
          onChange={(e) => setAmount(e.target.value)} />
        <input placeholder="Destination (bank / wallet address)" value={destination}
          onChange={(e) => setDestination(e.target.value)} />
        {error && <div className="error">{error}</div>}
        {notice && <div className="badge green">{notice}</div>}
        <button onClick={requestPayout}>Request payout</button>
      </div>
      <div className="card col">
        <h3 style={{ marginTop: 0 }}>Payout history</h3>
        {payouts.map((p) => (
          <div key={p.id} className="row">
            <span>${p.amount.toFixed(2)} {p.asset}</span>
            <span className="muted">{p.destination}</span>
            <div className="spacer" />
            <span className={`badge ${p.status === "paid" ? "green" : p.status === "rejected" ? "red" : "yellow"}`}>
              {p.status}
            </span>
          </div>
        ))}
        {payouts.length === 0 && <span className="muted">No payouts yet.</span>}
      </div>
    </div>
  );
}
