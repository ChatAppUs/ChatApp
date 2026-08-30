"use client";

import { useCallback, useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { api, getAccessToken } from "@/lib/api";
import { useI18n } from "@/lib/i18n";
import type { ConvertRate, Conversion } from "@/lib/types";

export default function ConvertPage() {
  const { t } = useI18n();
  const router = useRouter();
  const [rates, setRates] = useState<ConvertRate[]>([]);
  const [history, setHistory] = useState<Conversion[]>([]);
  const [from, setFrom] = useState("USD/internal");
  const [to, setTo] = useState("USDT/tron");
  const [amount, setAmount] = useState("");
  const [quote, setQuote] = useState<{ to_amount: string; rate: string } | null>(null);
  const [msg, setMsg] = useState("");
  const [err, setErr] = useState("");

  const load = useCallback(() => {
    api<{ rates: ConvertRate[] }>("/api/convert/rates").then((d) => setRates(d.rates)).catch(() => {});
    api<{ conversions: Conversion[] }>("/api/convert/history").then((d) => setHistory(d.conversions)).catch(() => {});
  }, []);

  useEffect(() => {
    if (!getAccessToken()) {
      router.push("/login");
      return;
    }
    load();
  }, [load, router]);

  const pairs = rates.map((r) => `${r.asset}/${r.chain}`);

  const getQuote = useCallback(async (fromPair: string, toPair: string, amt: string) => {
    if (!amt || Number(amt) <= 0) {
      setQuote(null);
      return;
    }
    const [fa, fc] = fromPair.split("/");
    const [ta, tc] = toPair.split("/");
    try {
      const q = await api<{ to_amount: string; rate: string }>(
        `/api/convert/quote?from_asset=${fa}&from_chain=${fc}&to_asset=${ta}&to_chain=${tc}&amount=${encodeURIComponent(amt)}`,
      );
      setQuote(q);
    } catch {
      setQuote(null);
    }
  }, []);

  useEffect(() => {
    const timer = setTimeout(() => getQuote(from, to, amount), 350);
    return () => clearTimeout(timer);
  }, [from, to, amount, getQuote]);

  const convertNow = async () => {
    setErr(""); setMsg("");
    const [fa, fc] = from.split("/");
    const [ta, tc] = to.split("/");
    try {
      const res = await api<{ id: string; to_amount: string }>("/api/convert", {
        method: "POST",
        body: JSON.stringify({ from_asset: fa, from_chain: fc, to_asset: ta, to_chain: tc, amount }),
      });
      setMsg(`${t("youReceive")}: ${res.to_amount} ${ta}`);
      setAmount("");
      setQuote(null);
      load();
    } catch (e) {
      setErr(e instanceof Error ? e.message : t("error"));
    }
  };

  return (
    <>
      <div className="card">
        <h3>{t("convert")}</h3>
        <div className="grid2">
          <div>
            <label>{t("convertFrom")}</label>
            <select value={from} onChange={(e) => setFrom(e.target.value)}>
              {pairs.map((p) => <option key={p} value={p}>{p}</option>)}
            </select>
          </div>
          <div>
            <label>{t("convertTo")}</label>
            <select value={to} onChange={(e) => setTo(e.target.value)}>
              {pairs.filter((p) => p !== from).map((p) => <option key={p} value={p}>{p}</option>)}
            </select>
          </div>
          <div>
            <label>{t("amount")}</label>
            <input value={amount} onChange={(e) => setAmount(e.target.value)} inputMode="decimal" />
          </div>
          <div>
            <label>{t("quote")}</label>
            <div className="card" style={{ margin: 0, padding: 10 }}>
              {quote ? (
                <>
                  <div>{t("youReceive")}: <strong>{quote.to_amount}</strong> {to.split("/")[0]}</div>
                  <div className="muted">{t("rate")}: {quote.rate}</div>
                </>
              ) : (
                <span className="muted">—</span>
              )}
            </div>
          </div>
        </div>
        <div className="row" style={{ marginTop: 10 }}>
          <button onClick={convertNow} disabled={!quote}>{t("convertNow")}</button>
          {msg && <span className="success-text">{msg}</span>}
          {err && <span className="error-text">{err}</span>}
        </div>
      </div>

      <div className="card">
        <h3>{t("conversions")}</h3>
        <table className="table">
          <thead>
            <tr><th>{t("convertFrom")}</th><th>{t("convertTo")}</th><th>{t("rate")}</th><th></th></tr>
          </thead>
          <tbody>
            {history.map((c) => (
              <tr key={c.id}>
                <td>{c.from_amount} {c.from_asset} <span className="muted">{c.from_chain}</span></td>
                <td>{c.to_amount} {c.to_asset} <span className="muted">{c.to_chain}</span></td>
                <td className="muted">{c.rate}</td>
                <td className="muted">{new Date(c.created_at).toLocaleString()}</td>
              </tr>
            ))}
            {history.length === 0 && (
              <tr><td colSpan={4} className="muted">{t("noResults")}</td></tr>
            )}
          </tbody>
        </table>
      </div>
    </>
  );
}
