"use client";

import { useCallback, useEffect, useState } from "react";
import { adminApi } from "@/lib/api";

interface Draw {
  id: string;
  title: string;
  frequency: string;
  sales_open_at: string;
  sales_close_at: string;
  status: string;
  ticket_price_usd: string;
  prize_alloc_pct: string;
  operator_fee_pct: string;
  max_tickets_per_user: number;
  unique_winner: boolean;
  min_age: number;
  allowed_countries: string[];
  disabled: boolean;
  prize_pool: string;
  winner_count: number;
  tickets_sold: number;
}

interface Price {
  id: string;
  draw_id: string;
  label: string;
  amount_usd: string;
  status: string;
}

export function LuckyDrawTab({ act }: { act: (fn: () => Promise<unknown>) => void }) {
  const [draws, setDraws] = useState<Draw[]>([]);
  const [prices, setPrices] = useState<Price[]>([]);
  const [err, setErr] = useState("");
  const [form, setForm] = useState({
    title: "",
    frequency: "daily",
    ticket_price_usd: "1.00",
    prize_alloc_pct: "80",
    operator_fee_pct: "10",
    max_tickets_per_user: 10,
    unique_winner: true,
    min_age: 18,
    allowed_countries: "",
    sales_open_at: "",
    sales_close_at: "",
  });

  const load = useCallback(() => {
    adminApi<{ draws: Draw[] }>("/api/admin/luckydraw")
      .then((d) => setDraws(d.draws || []))
      .catch(() => setDraws([]));
    adminApi<{ prices: Price[] }>("/api/admin/luckydraw/prices")
      .then((d) => setPrices(d.prices || []))
      .catch(() => setPrices([]));
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  const create = () => {
    setErr("");
    act(() =>
      adminApi("/api/admin/luckydraw", {
        method: "POST",
        body: JSON.stringify({
          ...form,
          allowed_countries: form.allowed_countries
            ? form.allowed_countries.split(",").map((s) => s.trim()).filter(Boolean)
            : [],
        }),
      }).then(load)
    ).catch((e) => setErr(e instanceof Error ? e.message : "error"));
  };

  const actOn = (id: string, action: string, extra: Record<string, unknown> = {}) => {
    setErr("");
    act(() =>
      adminApi(`/api/admin/luckydraw/${id}/${action}`, {
        method: "POST",
        body: JSON.stringify(extra),
      }).then(load)
    ).catch((e) => setErr(e instanceof Error ? e.message : "error"));
  };

  return (
    <div>
      <h3>🎰 Lucky Draw</h3>
      {err && <div className="error-text" style={{ marginBottom: 8 }}>{err}</div>}

      <div className="card" style={{ marginBottom: 12 }}>
        <h4>Create draw</h4>
        <div className="grid2">
          <label>Title
            <input value={form.title} onChange={(e) => setForm({ ...form, title: e.target.value })} />
          </label>
          <label>Frequency
            <select value={form.frequency} onChange={(e) => setForm({ ...form, frequency: e.target.value })}>
              <option value="daily">daily</option>
              <option value="weekly">weekly</option>
              <option value="monthly">monthly</option>
            </select>
          </label>
          <label>Ticket price (USD)
            <input value={form.ticket_price_usd} onChange={(e) => setForm({ ...form, ticket_price_usd: e.target.value })} />
          </label>
          <label>Prize alloc %
            <input value={form.prize_alloc_pct} onChange={(e) => setForm({ ...form, prize_alloc_pct: e.target.value })} />
          </label>
          <label>Operator fee %
            <input value={form.operator_fee_pct} onChange={(e) => setForm({ ...form, operator_fee_pct: e.target.value })} />
          </label>
          <label>Max tickets / user
            <input type="number" value={form.max_tickets_per_user} onChange={(e) => setForm({ ...form, max_tickets_per_user: Number(e.target.value) })} />
          </label>
          <label>Min age
            <input type="number" value={form.min_age} onChange={(e) => setForm({ ...form, min_age: Number(e.target.value) })} />
          </label>
          <label>Allowed countries (comma ISO)
            <input value={form.allowed_countries} onChange={(e) => setForm({ ...form, allowed_countries: e.target.value })} />
          </label>
          <label>Sales open (ISO)
            <input value={form.sales_open_at} onChange={(e) => setForm({ ...form, sales_open_at: e.target.value })} />
          </label>
          <label>Sales close (ISO)
            <input value={form.sales_close_at} onChange={(e) => setForm({ ...form, sales_close_at: e.target.value })} />
          </label>
          <label className="row">
            <input type="checkbox" checked={form.unique_winner} onChange={(e) => setForm({ ...form, unique_winner: e.target.checked })} />
            Unique-user winner
          </label>
        </div>
        <button onClick={create}>Create</button>
      </div>

      <table className="table">
        <thead>
          <tr>
            <th>Title</th><th>Freq</th><th>Status</th><th>Price</th><th>Pool</th>
            <th>Tickets</th><th>Winners</th><th>Actions</th>
          </tr>
        </thead>
        <tbody>
          {draws.map((d) => (
            <tr key={d.id}>
              <td>{d.title}</td>
              <td>{d.frequency}</td>
              <td>{d.status}{d.disabled ? " (disabled)" : ""}</td>
              <td>${d.ticket_price_usd}</td>
              <td>${d.prize_pool}</td>
              <td>{d.tickets_sold}</td>
              <td>{d.winner_count}</td>
              <td>
                <div className="row" style={{ gap: 4, flexWrap: "wrap" }}>
                  {d.status === "draft" && <button className="small" onClick={() => actOn(d.id, "open")}>Open sales</button>}
                  {d.status === "open" && <button className="small" onClick={() => actOn(d.id, "close")}>Close sales</button>}
                  {d.status === "sales_closed" && <button className="small" onClick={() => actOn(d.id, "draw")}>Run draw</button>}
                  {d.status === "drawn" && <button className="small" onClick={() => actOn(d.id, "settle")}>Settle</button>}
                  <button className="secondary small" onClick={() => actOn(d.id, "disable", { disabled: !d.disabled })}>
                    {d.disabled ? "Enable" : "Disable"}
                  </button>
                </div>
              </td>
            </tr>
          ))}
        </tbody>
      </table>

      <h4 style={{ marginTop: 12 }}>Prize prices</h4>
      <table className="table">
        <thead><tr><th>Label</th><th>Amount</th><th>Status</th></tr></thead>
        <tbody>
          {prices.map((p) => (
            <tr key={p.id}>
              <td>{p.label}</td>
              <td>${p.amount_usd}</td>
              <td>{p.status}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
