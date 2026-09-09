"use client";

import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { api, getAccessToken } from "@/lib/api";

type Plan = {
  id: string;
  name: string;
  price_usd: number;
  features: unknown;
};

export default function PremiumPage() {
  const router = useRouter();
  const [plans, setPlans] = useState<Plan[]>([]);
  const [error, setError] = useState("");
  const [status, setStatus] = useState("");
  const [busy, setBusy] = useState("");

  useEffect(() => {
    if (!getAccessToken()) {
      router.push("/login");
      return;
    }
    api<{ plans: Plan[] }>("/api/premium/plans")
      .then((d) => setPlans(d.plans))
      .catch((e) => setError(e instanceof Error ? e.message : "failed to load plans"));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const subscribe = async (planId: string) => {
    setBusy(planId);
    setStatus("");
    setError("");
    try {
      const d = await api<{ id: string; status: string }>("/api/premium/subscribe", {
        method: "POST",
        body: JSON.stringify({ plan_id: planId }),
      });
      setStatus(`Subscribed — membership ${d.id} is ${d.status}. Your USD wallet was charged for the 30-day period.`);
    } catch (e) {
      setError(e instanceof Error ? e.message : "subscription failed");
    } finally {
      setBusy("");
    }
  };

  return (
    <div className="col">
      <div className="card col" style={{ gap: 8 }}>
        <h2 style={{ margin: 0 }}>💎 Premium</h2>
        <div className="muted">
          Unlock creator tools, larger uploads, reels analytics and priority support with a monthly USD-wallet subscription.
        </div>
        {error && <div className="error-text">{error}</div>}
        {status && <div className="muted" style={{ color: "var(--accent-2)" }}>{status}</div>}
      </div>
      {plans.length === 0 && !error && (
        <div className="card muted">No active plans are currently offered.</div>
      )}
      {plans.map((p) => (
        <div key={p.id} className="card col" style={{ gap: 8 }}>
          <div className="row">
            <strong style={{ fontSize: 18 }}>{p.name}</strong>
            <div className="spacer" />
            <span className="badge green">${p.price_usd.toFixed(2)}/month</span>
          </div>
          <div className="muted" style={{ fontSize: 13, whiteSpace: "pre-wrap" }}>
            {typeof p.features === "string"
              ? p.features
              : JSON.stringify(p.features ?? [], null, 2)}
          </div>
          <div>
            <button className="small" disabled={busy === p.id} onClick={() => subscribe(p.id)}>
              {busy === p.id ? "Processing…" : "Subscribe"}
            </button>
          </div>
        </div>
      ))}
    </div>
  );
}