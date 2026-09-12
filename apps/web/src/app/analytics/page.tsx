"use client";

import { useCallback, useEffect, useState } from "react";
import { api } from "@/lib/api";

interface ProAnalytics {
  posts: number;
  followers: number;
  total_likes: number;
  total_comments: number;
  total_views: number;
  shares_7d: number;
  earnings_usd: number;
}

const metrics: Array<[keyof ProAnalytics, string]> = [
  ["posts", "Posts"],
  ["followers", "Followers"],
  ["total_likes", "Likes"],
  ["total_comments", "Comments"],
  ["total_views", "Views"],
  ["shares_7d", "Shares in 7 days"],
];

export default function AnalyticsPage() {
  const [data, setData] = useState<ProAnalytics | null>(null);
  const [error, setError] = useState("");

  const load = useCallback(() => {
    setError("");
    api<ProAnalytics>("/api/me/analytics")
      .then(setData)
      .catch((e) => setError(e instanceof Error ? e.message : "Unable to load analytics"));
  }, []);

  useEffect(load, [load]);

  return (
    <div className="col">
      <div className="card">
        <h2 style={{ marginTop: 0 }}>Professional dashboard</h2>
        <p className="muted">Account-level performance across your posts, audience, and creator activity.</p>
        {error && <div className="error">{error}</div>}
        {!data && !error && <span className="muted">Loading analytics…</span>}
        {data && (
          <>
            <div className="grid2">
              {metrics.map(([key, label]) => (
                <div className="card" key={key} style={{ textAlign: "center" }}>
                  <div style={{ fontSize: 28, fontWeight: 800 }}>{data[key].toLocaleString()}</div>
                  <div className="muted">{label}</div>
                </div>
              ))}
            </div>
            <div className="card" style={{ marginTop: 12 }}>
              <strong>Earnings received</strong>
              <div style={{ fontSize: 24, marginTop: 4 }}>${data.earnings_usd.toFixed(2)} USD</div>
            </div>
          </>
        )}
      </div>
    </div>
  );
}
