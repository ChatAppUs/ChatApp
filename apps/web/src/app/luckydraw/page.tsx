"use client";

import { useCallback, useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { api, getAccessToken } from "@/lib/api";
import { useI18n } from "@/lib/i18n";
import type { LuckyDraw, LuckyDrawTicket } from "@/lib/types";

export default function LuckyDrawPage() {
  const { t } = useI18n();
  const router = useRouter();
  const [draws, setDraws] = useState<LuckyDraw[]>([]);
  const [tickets, setTickets] = useState<LuckyDrawTicket[]>([]);
  const [counts, setCounts] = useState<Record<string, string>>({});
  const [err, setErr] = useState("");
  const [msg, setMsg] = useState("");
  const [winners, setWinners] = useState<Record<string, { prize_usd: string; username: string }[]>>({});

  const statusLabel = (s: string) => {
    switch (s) {
      case "open": return "🟢";
      case "sales_closed": return "🔴";
      case "settled": return "🏆";
      default: return "⏳";
    }
  };

  const load = useCallback(() => {
    api<{ draws: LuckyDraw[] }>("/api/luckydraw")
      .then((d) => setDraws(d.draws || []))
      .catch(() => setDraws([]));
    api<{ tickets: LuckyDrawTicket[] }>("/api/luckydraw/mine")
      .then((d) => setTickets(d.tickets || []))
      .catch(() => setTickets([]));
  }, []);

  useEffect(() => {
    if (!getAccessToken()) {
      router.push("/login");
      return;
    }
    load();
  }, [load, router]);

  useEffect(() => {
    draws.filter((d) => d.status === "settled").forEach((d) => {
      api<{ winners: { prize_usd: string; username: string }[] }>(`/api/luckydraw/${d.id}/winners`)
        .then((w) => setWinners((prev) => ({ ...prev, [d.id]: w.winners || [] })))
        .catch(() => {});
    });
  }, [draws]);

  const buy = async (draw: LuckyDraw) => {
    setErr(""); setMsg("");
    try {
      const count = Number(counts[draw.id] || 1);
      await api("/api/luckydraw/tickets", {
        method: "POST",
        body: JSON.stringify({ draw_id: draw.id, count }),
      });
      setMsg(`${t("buyTickets")}: ${count}`);
      load();
    } catch (e) {
      setErr(e instanceof Error ? e.message : t("purchaseError"));
    }
  };

  const openDraws = draws.filter((d) => d.status === "open");
  const otherDraws = draws.filter((d) => d.status !== "open");

  return (
    <div className="card">
      <h3>🎰 {t("luckyDraw")}</h3>
      {err && <p className="error">{err}</p>}
      {msg && <p className="ok">{msg}</p>}

      <h4 style={{ marginTop: "12px" }}>{t("openDraws")}</h4>
      {openDraws.length === 0 && <p className="muted">{t("drawNotOpen")}</p>}
      {openDraws.map((d) => (
        <div key={d.id} className="card" style={{ marginBottom: 8 }}>
          <div className="row" style={{ justifyContent: "space-between" }}>
            <div>
              <b>{d.title}</b> <span className="muted">({d.frequency})</span>
              <div className="muted">
                {t("ticketPrice")}: ${d.ticket_price_usd} · {t("prizePool")}: ${d.prize_pool} ·{" "}
                {t("tickets")}: {d.tickets_sold} · {t("maxPerUser")}: {d.max_tickets_per_user}
              </div>
              <div className="muted">
                {t("prizeAlloc")}: {d.prize_alloc_pct}% · {t("operatorFee")}: {d.operator_fee_pct}% ·{" "}
                {t("uniqueWinner")}: {d.unique_winner ? "✓" : "✗"} · {t("minAge")}: {d.min_age}+
                {d.my_tickets ? <> · {t("myTickets")}: {d.my_tickets}</> : null}
              </div>
              <div className="muted">
                {t("salesClose")}: {new Date(d.sales_close_at).toLocaleString()}
              </div>
            </div>
            <div className="row">
              <input
                type="number"
                min={1}
                max={d.max_tickets_per_user}
                value={counts[d.id] ?? 1}
                placeholder="1"
                style={{ width: 60 }}
                onChange={(e) => setCounts((prev) => ({ ...prev, [d.id]: e.target.value }))}
              />
              <button onClick={() => buy(d)}>{t("buyTickets")}</button>
            </div>
          </div>
        </div>
      ))}

      <h4 style={{ marginTop: "16px" }}>{t("myTickets")}</h4>
      {tickets.length === 0 && <p className="muted">{t("noTicketsYet")}</p>}
      {tickets.map((tk) => (
        <div key={tk.id} className="card row" style={{ justifyContent: "space-between" }}>
          <div>
            <b>{tk.ticket_number}</b> · {tk.draw_title} · ${tk.price_usd}
            <div className="muted">
              {new Date(tk.created_at).toLocaleString()} · {t("drawStatus")}: {statusLabel(tk.draw_status)} {tk.draw_status}
            </div>
          </div>
        </div>
      ))}

      <h4 style={{ marginTop: "16px" }}>{t("pastDraws")}</h4>
      {otherDraws.map((d) => (
        <div key={d.id} className="card" style={{ marginBottom: 8 }}>
          <div className="row" style={{ justifyContent: "space-between" }}>
            <div>
              <b>{d.title}</b> <span className="muted">({d.frequency})</span> · {statusLabel(d.status)} {d.status}
              <div className="muted">
                {t("prizePool")}: ${d.prize_pool} · {t("tickets")}: {d.tickets_sold}
              </div>
            </div>
          </div>
          {d.status === "settled" && (
            <div className="muted" style={{ marginTop: 6 }}>
              {t("drawWinners")}:
              {(winners[d.id] || []).map((w, i) => (
                <span key={i} style={{ marginRight: 10 }}>
                  🏆 @{w.username} — ${w.prize_usd}
                </span>
              ))}
            </div>
          )}
        </div>
      ))}
    </div>
  );
}