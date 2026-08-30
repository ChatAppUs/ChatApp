"use client";

import { useCallback, useEffect, useState } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { api, getAccessToken } from "@/lib/api";
import type { Card, CardTransaction } from "@/lib/types";

interface IssuedCard extends Card {
  card_number?: string;
  cvv?: string;
}

export default function CardsPage() {
  const router = useRouter();
  const [cards, setCards] = useState<Card[]>([]);
  const [txns, setTxns] = useState<CardTransaction[]>([]);
  const [txCard, setTxCard] = useState("");
  const [issued, setIssued] = useState<IssuedCard | null>(null);
  const [label, setLabel] = useState("");
  const [accounts, setAccounts] = useState<{ id: string; asset: string; chain: string; balance: string }[]>([]);
  const [topup, setTopup] = useState({ cardId: "", asset: "USDT", chain: "tron", amount: "" });
  const [limits, setLimits] = useState({ cardId: "", daily: "", monthly: "" });
  const [msg, setMsg] = useState("");
  const [err, setErr] = useState("");

  const load = useCallback(() => {
    api<{ cards: Card[] }>("/api/cards").then((d) => setCards(d.cards)).catch(() => {});
    api<{ accounts: { id: string; asset: string; chain: string; balance: string }[] }>("/api/wallet/accounts")
      .then((d) => setAccounts(d.accounts.filter((a) => a.asset !== "USD"))).catch(() => {});
  }, []);

  const loadStatement = async (cardId: string) => {
    if (txCard === cardId) {
      setTxCard("");
      return;
    }
    const d = await api<{ transactions: CardTransaction[] }>(`/api/cards/${cardId}/transactions`)
      .catch(() => null);
    if (d) {
      setTxns(d.transactions);
      setTxCard(cardId);
    }
  };

  useEffect(() => {
    if (!getAccessToken()) {
      router.push("/login");
      return;
    }
    load();
  }, [load, router]);

  const issue = async () => {
    setErr(""); setMsg("");
    try {
      const c = await api<IssuedCard>("/api/cards", {
        method: "POST",
        body: JSON.stringify({ label }),
      });
      setIssued(c);
      setLabel("");
      load();
    } catch (e) {
      setErr(e instanceof Error ? e.message : "failed to issue card");
    }
  };

  const doTopup = async () => {
    setErr(""); setMsg("");
    try {
      const r = await api<{ usd_amount: string }>(`/api/cards/${topup.cardId}/topup`, {
        method: "POST",
        body: JSON.stringify({ asset: topup.asset, chain: topup.chain, amount: topup.amount }),
      });
      setMsg(`Topped up $${r.usd_amount}`);
      setTopup({ ...topup, amount: "" });
      load();
    } catch (e) {
      setErr(e instanceof Error ? e.message : "top-up failed");
    }
  };

  const setStatus = async (id: string, status: string) => {
    await api(`/api/cards/${id}/status`, { method: "POST", body: JSON.stringify({ status }) }).catch(() => {});
    load();
  };

  const saveLimits = async () => {
    setErr(""); setMsg("");
    try {
      await api(`/api/cards/${limits.cardId}/limits`, {
        method: "PUT",
        body: JSON.stringify({ daily_usd: limits.daily, monthly_usd: limits.monthly }),
      });
      setMsg("Limits updated");
      setLimits({ cardId: "", daily: "", monthly: "" });
      load();
    } catch (e) {
      setErr(e instanceof Error ? e.message : "failed to update limits");
    }
  };

  const fmtUsd = (s: string) => (parseFloat(s) || 0).toFixed(2);

  return (
    <main className="col">
      <div className="row">
        <h2>💳 Cards</h2>
        <div className="spacer" />
        <Link href="/wallet">← Wallet</Link>
      </div>

      {issued?.card_number && (
        <div className="card" style={{ borderColor: "var(--accent)" }}>
          <strong>Your new card — copy these details now, they are never shown again:</strong>
          <div style={{ fontSize: 20, letterSpacing: 2, margin: "8px 0" }}>{issued.card_number}</div>
          <div className="muted">CVV {issued.cvv} · Expires {String(issued.expiry_month).padStart(2, "0")}/{issued.expiry_year}</div>
          <button className="secondary small" style={{ marginTop: 8 }} onClick={() => {
            navigator.clipboard?.writeText(`${issued.card_number} ${issued.cvv} ${issued.expiry_month}/${issued.expiry_year}`);
          }}>Copy</button>
          <button className="secondary small" style={{ marginInlineStart: 8 }} onClick={() => setIssued(null)}>Done</button>
        </div>
      )}

      <div className="card">
        <div className="row">
          <input placeholder="Card label (optional)" value={label} onChange={(e) => setLabel(e.target.value)} />
          <button onClick={issue}>Issue virtual card</button>
        </div>
      </div>

      {cards.map((c) => (
        <div className="card" key={c.id}>
          <div className="row">
            <strong>{c.label || "Card"} ···· {c.last4}</strong>
            <span className="badge">{c.status}</span>
            <div className="spacer" />
            <strong>${fmtUsd(c.balance_usd)}</strong>
          </div>
          <div className="muted" style={{ fontSize: 12 }}>
            Expires {String(c.expiry_month).padStart(2, "0")}/{c.expiry_year} ·
            daily ${fmtUsd(c.daily_limit_usd)} · monthly ${fmtUsd(c.monthly_limit_usd)}
          </div>
          <div className="row" style={{ marginTop: 8, flexWrap: "wrap" }}>
            {c.status === "active" ? (
              <button className="secondary small" onClick={() => setStatus(c.id, "frozen")}>❄️ Freeze</button>
            ) : c.status === "frozen" ? (
              <button className="secondary small" onClick={() => setStatus(c.id, "active")}>▶️ Unfreeze</button>
            ) : null}
            {c.status !== "terminated" && (
              <button className="secondary small" onClick={() => {
                if (confirm("Permanently close this card?")) setStatus(c.id, "terminated");
              }}>✕ Close</button>
            )}
            <button className="secondary small"
              onClick={() => setTopup({ ...topup, cardId: topup.cardId === c.id ? "" : c.id })}>
              ➕ Top up
            </button>
            <button className="secondary small"
              onClick={() => setLimits(limits.cardId === c.id
                ? { cardId: "", daily: "", monthly: "" }
                : { cardId: c.id, daily: c.daily_limit_usd, monthly: c.monthly_limit_usd })}>
              ⚙️ Limits
            </button>
            <button className="secondary small" onClick={() => loadStatement(c.id)}>📄 Statement</button>
          </div>

          {txCard === c.id && (
            <div className="col" style={{ marginTop: 8 }}>
              {txns.length === 0 && <span className="muted" style={{ fontSize: 12 }}>No transactions yet.</span>}
              {txns.map((x) => (
                <div className="row" key={x.id} style={{ borderBottom: "1px solid var(--border)", padding: "4px 0", fontSize: 13 }}>
                  <span>{x.merchant}</span>
                  <span className="muted">{x.kind}{x.status !== "captured" ? ` · ${x.status}` : ""}</span>
                  <div className="spacer" />
                  <span>${fmtUsd(x.amount_usd)}</span>
                  <span className="muted" style={{ fontSize: 12 }}>{new Date(x.created_at).toLocaleString()}</span>
                </div>
              ))}
            </div>
          )}

          {topup.cardId === c.id && (
            <div className="col" style={{ marginTop: 8 }}>
              <div className="row">
                <select value={`${topup.asset}:${topup.chain}`} onChange={(e) => {
                  const [a, ch] = e.target.value.split(":");
                  setTopup({ ...topup, asset: a, chain: ch });
                }}>
                  {accounts.map((a) => (
                    <option key={a.id} value={`${a.asset}:${a.chain}`}>
                      {a.asset} ({a.chain}) — {parseFloat(a.balance || "0")}
                    </option>
                  ))}
                </select>
                <input placeholder="Amount" value={topup.amount}
                  onChange={(e) => setTopup({ ...topup, amount: e.target.value })} />
                <button className="small" onClick={doTopup}>Top up</button>
              </div>
              <span className="muted" style={{ fontSize: 12 }}>Converted to USD at the current admin rate.</span>
            </div>
          )}

          {limits.cardId === c.id && (
            <div className="row" style={{ marginTop: 8 }}>
              <input placeholder="Daily USD" value={limits.daily}
                onChange={(e) => setLimits({ ...limits, daily: e.target.value })} />
              <input placeholder="Monthly USD" value={limits.monthly}
                onChange={(e) => setLimits({ ...limits, monthly: e.target.value })} />
              <button className="small" onClick={saveLimits}>Save</button>
            </div>
          )}
        </div>
      ))}
      {cards.length === 0 && <div className="muted">No cards yet — issue one above.</div>}

      {msg && <div className="card" style={{ borderColor: "var(--accent)" }}>{msg}</div>}
      {err && <div className="error-text">{err}</div>}
    </main>
  );
}
