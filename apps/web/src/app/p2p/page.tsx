"use client";

import { useCallback, useEffect, useState } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { api, getAccessToken } from "@/lib/api";
import { useI18n } from "@/lib/i18n";
import type { P2POffer, P2PTrade, P2PPaymentMethod, Country, Merchant, MerchantTier } from "@/lib/types";

export default function P2PPage() {
  const { t } = useI18n();
  const router = useRouter();
  const [tab, setTab] = useState<"market" | "myOffers" | "myTrades" | "create" | "merchant">("market");
  const [merchant, setMerchant] = useState<Merchant | null>(null);
  const [tiers, setTiers] = useState<MerchantTier[]>([]);
  const [bizName, setBizName] = useState("");
  const [bizNote, setBizNote] = useState("");
  const [offers, setOffers] = useState<P2POffer[]>([]);
  const [myOffers, setMyOffers] = useState<P2POffer[]>([]);
  const [trades, setTrades] = useState<P2PTrade[]>([]);
  const [methods, setMethods] = useState<P2PPaymentMethod[]>([]);
  const [countries, setCountries] = useState<Country[]>([]);
  const [assets, setAssets] = useState<Record<string, string[]>>({});
  const [kyc, setKyc] = useState("");
  const [side, setSide] = useState<"buy" | "sell">("sell");
  const [asset, setAsset] = useState("USDT");
  const [chain, setChain] = useState("tron");
  const [country, setCountry] = useState("US");
  const [form, setForm] = useState({
    side: "sell", asset: "USDT", chain: "tron", fiat: "USD", country: "US",
    price: "", min: "", max: "", terms: "",
  });
  const [formMethods, setFormMethods] = useState<string[]>([]);
  const [tradeFor, setTradeFor] = useState<P2POffer | null>(null);
  const [tradeAmount, setTradeAmount] = useState("");
  const [tradeMethod, setTradeMethod] = useState("");
  const [msg, setMsg] = useState("");
  const [err, setErr] = useState("");

  const load = useCallback(() => {
    api<{ assets: Record<string, string[]> }>("/api/wallet/assets").then((d) => setAssets(d.assets)).catch(() => {});
    api<{ status: string }>("/api/kyc/status").then((d) => setKyc(d.status)).catch(() => {});
    api<{ countries: Country[] }>("/api/countries").then((d) => setCountries(d.countries)).catch(() => {});
    api<{ trades: P2PTrade[] }>("/api/p2p/trades").then((d) => setTrades(d.trades)).catch(() => {});
    api<{ offers: P2POffer[] }>("/api/p2p/offers/mine").then((d) => setMyOffers(d.offers)).catch(() => {});
    api<{ merchant: Merchant | null }>("/api/p2p/merchant/status")
      .then((d) => setMerchant(d.merchant)).catch(() => {});
    api<{ tiers: MerchantTier[] }>("/api/p2p/merchant/tiers")
      .then((d) => setTiers(d.tiers)).catch(() => {});
  }, []);

  const applyMerchant = async () => {
    setErr(""); setMsg("");
    try {
      await api("/api/p2p/merchant/apply", {
        method: "POST",
        body: JSON.stringify({ business_name: bizName, note: bizNote }),
      });
      setMsg("Merchant application submitted");
      setBizName(""); setBizNote("");
      const d = await api<{ merchant: Merchant | null }>("/api/p2p/merchant/status");
      setMerchant(d.merchant);
    } catch (e) {
      setErr(e instanceof Error ? e.message : "application failed");
    }
  };

  const loadOffers = useCallback(() => {
    const params = new URLSearchParams({ side, asset, chain, country });
    api<{ offers: P2POffer[] }>(`/api/p2p/offers?${params}`).then((d) => setOffers(d.offers)).catch(() => {});
  }, [side, asset, chain, country]);

  useEffect(() => {
    if (!getAccessToken()) {
      router.push("/login");
      return;
    }
    load();
  }, [load, router]);

  useEffect(() => { loadOffers(); }, [loadOffers]);

  useEffect(() => {
    api<{ methods: P2PPaymentMethod[] }>(`/api/p2p/payment-methods?country=${form.country}`)
      .then((d) => setMethods(d.methods)).catch(() => setMethods([]));
  }, [form.country]);

  const createOffer = async () => {
    setErr(""); setMsg("");
    try {
      await api("/api/p2p/offers", {
        method: "POST",
        body: JSON.stringify({
          side: form.side, asset: form.asset, chain: form.chain,
          fiat_currency: form.fiat, country_iso: form.country,
          price: form.price, min_amount: form.min, max_amount: form.max,
          payment_methods: formMethods, terms: form.terms,
        }),
      });
      setMsg(t("createOffer") + " ✓");
      setForm({ ...form, price: "", min: "", max: "", terms: "" });
      setFormMethods([]);
      load();
    } catch (e) {
      setErr(e instanceof Error ? e.message : t("error"));
    }
  };

  const openTrade = async () => {
    if (!tradeFor) return;
    setErr(""); setMsg("");
    try {
      await api("/api/p2p/trades", {
        method: "POST",
        body: JSON.stringify({
          offer_id: tradeFor.id,
          crypto_amount: tradeAmount,
          payment_method: tradeMethod || tradeFor.payment_methods[0],
        }),
      });
      setMsg(t("trade") + " ✓");
      setTradeFor(null);
      setTradeAmount("");
      load();
    } catch (e) {
      setErr(e instanceof Error ? e.message : t("error"));
    }
  };

  const tradeAction = async (id: string, action: string) => {
    setErr(""); setMsg("");
    try {
      await api(`/api/p2p/trades/${id}/${action}`, { method: "POST", body: "{}" });
      load();
    } catch (e) {
      setErr(e instanceof Error ? e.message : t("error"));
    }
  };

  const toggleOffer = async (id: string, active: boolean) => {
    try {
      await api(`/api/p2p/offers/${id}/status`, { method: "POST", body: JSON.stringify({ active: !active }) });
      load();
    } catch {
      /* surfaced on next load */
    }
  };

  const assetNames = Object.keys(assets).filter((a) => a !== "USD");

  if (kyc && kyc !== "verified") {
    return (
      <div className="card">
        <h3>{t("p2p")}</h3>
        <p>{t("kycRequiredP2P")}</p>
        <Link href="/kyc">{t("kyc")} →</Link>
      </div>
    );
  }

  return (
    <>
      <div className="card">
        <div className="row">
          <h3 style={{ marginRight: "auto" }}>{t("p2pMarket")}</h3>
          <button className={`secondary small ${tab === "market" ? "active" : ""}`} onClick={() => setTab("market")}>{t("p2pMarket")}</button>
          <button className={`secondary small ${tab === "myOffers" ? "active" : ""}`} onClick={() => setTab("myOffers")}>{t("myOffers")}</button>
          <button className={`secondary small ${tab === "myTrades" ? "active" : ""}`} onClick={() => setTab("myTrades")}>{t("myTrades")}</button>
          <button className={`secondary small ${tab === "create" ? "active" : ""}`} onClick={() => setTab("create")}>{t("createOffer")}</button>
          <button className={`secondary small ${tab === "merchant" ? "active" : ""}`} onClick={() => setTab("merchant")}>🏪 Merchant</button>
        </div>
        {(msg || err) && (
          <div className="row" style={{ marginTop: 8 }}>
            {msg && <span className="success-text">{msg}</span>}
            {err && <span className="error-text">{err}</span>}
          </div>
        )}
      </div>

      {tab === "market" && (
        <div className="card">
          <div className="row">
            <select value={side} onChange={(e) => setSide(e.target.value as "buy" | "sell")} style={{ width: "auto" }}>
              <option value="sell">{t("buy")} {t("asset")}</option>
              <option value="buy">{t("sell")} {t("asset")}</option>
            </select>
            <select value={asset} onChange={(e) => { setAsset(e.target.value); setChain((assets[e.target.value] ?? [""])[0]); }} style={{ width: "auto" }}>
              {assetNames.map((a) => <option key={a} value={a}>{a}</option>)}
            </select>
            <select value={chain} onChange={(e) => setChain(e.target.value)} style={{ width: "auto" }}>
              {(assets[asset] ?? []).map((c) => <option key={c} value={c}>{c}</option>)}
            </select>
            <select value={country} onChange={(e) => setCountry(e.target.value)} style={{ width: "auto" }}>
              {countries.map((c) => <option key={c.iso} value={c.iso}>{c.flag} {c.name}</option>)}
            </select>
          </div>
          <p className="muted" style={{ marginTop: 8 }}>{t("escrowNote")}</p>
          <table className="table" style={{ marginTop: 8 }}>
            <thead>
              <tr><th>{t("seller")}/{t("buyer")}</th><th>{t("price")}</th><th>{t("limits")}</th><th>{t("paymentMethods")}</th><th></th></tr>
            </thead>
            <tbody>
              {offers.map((o) => (
                <tr key={o.id}>
                  <td>
                    {o.owner_username}
                    {o.owner_is_merchant && (
                      <span className="badge" title={`Verified merchant · tier ${o.owner_merchant_tier}`}>
                        🏪 T{o.owner_merchant_tier}
                      </span>
                    )}
                  </td>
                  <td>{o.price} {o.fiat_currency}</td>
                  <td className="muted">{o.min_amount}–{o.max_amount} {o.asset}</td>
                  <td className="muted">{o.payment_methods.join(", ")}</td>
                  <td><button className="small" onClick={() => { setTradeFor(o); setTradeMethod(o.payment_methods[0] ?? ""); }}>{t("openTrade")}</button></td>
                </tr>
              ))}
              {offers.length === 0 && (
                <tr><td colSpan={5} className="muted">{t("noResults")}</td></tr>
              )}
            </tbody>
          </table>
          {tradeFor && (
            <div className="card" style={{ border: "1px solid var(--accent)", marginTop: 12 }}>
              <h4>{t("openTrade")} — {tradeFor.asset} @ {tradeFor.price} {tradeFor.fiat_currency}</h4>
              <div className="grid2">
                <div>
                  <label>{t("amount")} ({tradeFor.min_amount}–{tradeFor.max_amount} {tradeFor.asset})</label>
                  <input value={tradeAmount} onChange={(e) => setTradeAmount(e.target.value)} inputMode="decimal" />
                </div>
                <div>
                  <label>{t("paymentMethods")}</label>
                  <select value={tradeMethod} onChange={(e) => setTradeMethod(e.target.value)}>
                    {tradeFor.payment_methods.map((m) => <option key={m} value={m}>{m}</option>)}
                  </select>
                </div>
              </div>
              {tradeFor.terms && <p className="muted">{tradeFor.terms}</p>}
              <div className="row" style={{ marginTop: 8 }}>
                <button onClick={openTrade} disabled={!tradeAmount}>{t("openTrade")}</button>
                <button className="secondary" onClick={() => setTradeFor(null)}>{t("cancel")}</button>
              </div>
            </div>
          )}
        </div>
      )}

      {tab === "myOffers" && (
        <div className="card">
          <table className="table">
            <thead>
              <tr><th>{t("asset")}</th><th>{t("price")}</th><th>{t("limits")}</th><th>{t("status")}</th><th></th></tr>
            </thead>
            <tbody>
              {myOffers.map((o) => (
                <tr key={o.id}>
                  <td>{o.side} {o.asset} <span className="muted">{o.chain}</span></td>
                  <td>{o.price} {o.fiat_currency}</td>
                  <td className="muted">{o.min_amount}–{o.max_amount}</td>
                  <td><span className={`badge ${o.active ? "green" : "yellow"}`}>{o.active ? t("enable") : t("disable")}</span></td>
                  <td><button className="secondary small" onClick={() => toggleOffer(o.id, o.active)}>{o.active ? t("disable") : t("enable")}</button></td>
                </tr>
              ))}
              {myOffers.length === 0 && (
                <tr><td colSpan={5} className="muted">{t("noResults")}</td></tr>
              )}
            </tbody>
          </table>
        </div>
      )}

      {tab === "myTrades" && (
        <div className="card">
          <table className="table">
            <thead>
              <tr><th>{t("asset")}</th><th>{t("amount")}</th><th>{t("price")}</th><th>{t("paymentMethods")}</th><th>{t("status")}</th><th></th></tr>
            </thead>
            <tbody>
              {trades.map((tr) => (
                <tr key={tr.id}>
                  <td>{tr.asset} <span className="muted">{tr.chain}</span></td>
                  <td>{tr.crypto_amount}</td>
                  <td>{tr.fiat_amount} {tr.fiat_currency}</td>
                  <td className="muted">{tr.payment_method}</td>
                  <td><span className={`badge ${tr.status === "completed" ? "green" : tr.status === "disputed" ? "red" : "yellow"}`}>{tr.status}</span></td>
                  <td className="row">
                    {tr.status === "open" && (
                      <>
                        <button className="small" onClick={() => tradeAction(tr.id, "paid")}>{t("markPaid")}</button>
                        <button className="secondary small" onClick={() => tradeAction(tr.id, "cancel")}>{t("cancelTrade")}</button>
                      </>
                    )}
                    {tr.status === "paid" && (
                      <>
                        <button className="small" onClick={() => tradeAction(tr.id, "release")}>{t("releaseCrypto")}</button>
                        <button className="secondary small" onClick={() => tradeAction(tr.id, "dispute")}>{t("openDispute")}</button>
                      </>
                    )}
                  </td>
                </tr>
              ))}
              {trades.length === 0 && (
                <tr><td colSpan={6} className="muted">{t("noResults")}</td></tr>
              )}
            </tbody>
          </table>
        </div>
      )}

      {tab === "create" && (
        <div className="card">
          <h4>{t("createOffer")}</h4>
          <div className="grid2">
            <div>
              <label>{t("buy")}/{t("sell")}</label>
              <select value={form.side} onChange={(e) => setForm({ ...form, side: e.target.value })}>
                <option value="sell">{t("sell")}</option>
                <option value="buy">{t("buy")}</option>
              </select>
            </div>
            <div>
              <label>{t("asset")}</label>
              <select value={form.asset} onChange={(e) => setForm({ ...form, asset: e.target.value, chain: (assets[e.target.value] ?? [""])[0] })}>
                {assetNames.map((a) => <option key={a} value={a}>{a}</option>)}
              </select>
            </div>
            <div>
              <label>{t("chain")}</label>
              <select value={form.chain} onChange={(e) => setForm({ ...form, chain: e.target.value })}>
                {(assets[form.asset] ?? []).map((c) => <option key={c} value={c}>{c}</option>)}
              </select>
            </div>
            <div>
              <label>{t("fiatCurrency")}</label>
              <input value={form.fiat} maxLength={3} onChange={(e) => setForm({ ...form, fiat: e.target.value.toUpperCase() })} />
            </div>
            <div>
              <label>{t("country")}</label>
              <select value={form.country} onChange={(e) => setForm({ ...form, country: e.target.value })}>
                {countries.map((c) => <option key={c.iso} value={c.iso}>{c.flag} {c.name}</option>)}
              </select>
            </div>
            <div>
              <label>{t("price")} ({t("fiatCurrency")})</label>
              <input value={form.price} onChange={(e) => setForm({ ...form, price: e.target.value })} inputMode="decimal" />
            </div>
            <div>
              <label>Min {t("amount")}</label>
              <input value={form.min} onChange={(e) => setForm({ ...form, min: e.target.value })} inputMode="decimal" />
            </div>
            <div>
              <label>Max {t("amount")}</label>
              <input value={form.max} onChange={(e) => setForm({ ...form, max: e.target.value })} inputMode="decimal" />
            </div>
          </div>
          <div style={{ marginTop: 8 }}>
            <label>{t("paymentMethods")}</label>
            <div className="row" style={{ flexWrap: "wrap", gap: 8 }}>
              {methods.map((m) => (
                <label key={m.name} className="badge" style={{ cursor: "pointer" }}>
                  <input
                    type="checkbox"
                    checked={formMethods.includes(m.name)}
                    onChange={(e) =>
                      setFormMethods(e.target.checked
                        ? [...formMethods, m.name]
                        : formMethods.filter((x) => x !== m.name))
                    }
                  />{" "}
                  {m.name} <span className="muted">({m.kind})</span>
                </label>
              ))}
            </div>
          </div>
          <div style={{ marginTop: 8 }}>
            <label>{t("terms")}</label>
            <input value={form.terms} onChange={(e) => setForm({ ...form, terms: e.target.value })} />
          </div>
          <div className="row" style={{ marginTop: 10 }}>
            <button onClick={createOffer} disabled={!form.price || !form.max || formMethods.length === 0}>{t("createOffer")}</button>
          </div>
        </div>
      )}
      {tab === "merchant" && (
        <div className="card">
          <h3>🏪 Verified merchant program</h3>
          {merchant ? (
            <div className="col">
              <div className="row">
                <span className={`badge ${merchant.status === "verified" ? "green" : "yellow"}`}>{merchant.status}</span>
                <strong>{merchant.business_name}</strong>
                {merchant.status === "verified" && <span className="badge">Tier {merchant.tier} · {merchant.tier_name}</span>}
              </div>
              {merchant.status === "rejected" && (
                <button className="secondary small" onClick={() => setMerchant(null)}>Re-apply</button>
              )}
              {merchant.status === "verified" && (
                <span className="muted" style={{ fontSize: 12 }}>
                  Your offers carry the 🏪 merchant badge. Tier limits apply per trade and per day.
                </span>
              )}
            </div>
          ) : (
            <div className="col">
              <p className="muted" style={{ fontSize: 13 }}>
                Merchants get a verified badge on their offers and higher trust from buyers.
                An admin reviews every application.
              </p>
              <input placeholder="Business name" value={bizName} maxLength={120}
                onChange={(e) => setBizName(e.target.value)} />
              <input placeholder="Note for the reviewer (optional)" value={bizNote} maxLength={500}
                onChange={(e) => setBizNote(e.target.value)} />
              <button onClick={applyMerchant} disabled={!bizName.trim()}>Apply</button>
            </div>
          )}
          {tiers.length > 0 && (
            <table className="table" style={{ marginTop: 12 }}>
              <thead>
                <tr><th>Tier</th><th>Name</th><th>Max trade</th><th>Daily volume</th><th>Unlocks at</th></tr>
              </thead>
              <tbody>
                {tiers.map((t) => (
                  <tr key={t.level}>
                    <td>T{t.level}</td>
                    <td>{t.name}</td>
                    <td>${parseFloat(t.max_trade_usd).toLocaleString()}</td>
                    <td>${parseFloat(t.daily_volume_usd).toLocaleString()}</td>
                    <td className="muted">{t.min_completed_trades} trades · {parseFloat(t.min_completion_rate) * 100}% completion</td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </div>
      )}
    </>
  );
}
