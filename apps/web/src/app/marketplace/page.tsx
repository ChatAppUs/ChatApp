"use client";

import { useCallback, useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { api, getAccessToken } from "@/lib/api";
import { useI18n } from "@/lib/i18n";

type Listing = {
  id: string;
  title: string;
  description: string;
  price_usd: string;
  status: string;
  sold_count?: number;
  seller?: string;
};

type Order = {
  id: string;
  listing_id: string;
  title: string;
  amount_usd: string;
  status: string;
  created_at: string;
};

// TikTok Shop / Facebook Marketplace: wallet-native checkout with optional
// affiliate attribution to the post that referred the buyer.
export default function MarketplacePage() {
  const { t } = useI18n();
  const router = useRouter();
  const [listings, setListings] = useState<Listing[]>([]);
  const [purchases, setPurchases] = useState<Order[]>([]);
  const [sales, setSales] = useState<Order[]>([]);
  const [tab, setTab] = useState<"browse" | "purchases" | "sales">("browse");
  const [title, setTitle] = useState("");
  const [description, setDescription] = useState("");
  const [price, setPrice] = useState("");
  const [error, setError] = useState("");
  const [status, setStatus] = useState("");

  const load = useCallback(async () => {
    try {
      const d = await api<{ listings: Listing[] }>("/api/marketplace");
      setListings(d.listings);
      const p = await api<{ orders: Order[] }>("/api/me/orders");
      setPurchases(p.orders);
      const s = await api<{ orders: Order[] }>("/api/me/orders?as=seller");
      setSales(s.orders);
    } catch (e) {
      setError(e instanceof Error ? e.message : t("error"));
    }
  }, [t]);

  useEffect(() => {
    if (!getAccessToken()) {
      router.push("/login");
      return;
    }
    load();
  }, [load, router]);

  const createListing = async () => {
    setError("");
    setStatus("");
    try {
      await api("/api/marketplace", {
        method: "POST",
        body: JSON.stringify({ title, description, price_usd: Number(price) }),
      });
      setTitle("");
      setDescription("");
      setPrice("");
      setStatus(t("listingCreated"));
      load();
    } catch (e) {
      setError(e instanceof Error ? e.message : t("error"));
    }
  };

  const buy = async (id: string) => {
    setError("");
    setStatus("");
    try {
      const r = await api<{ order_id: string; status: string }>(`/api/marketplace/${id}/buy`, {
        method: "POST",
        body: JSON.stringify({}),
      });
      setStatus(`${t("orderPlaced")} (${r.status})`);
      load();
    } catch (e) {
      setError(e instanceof Error ? e.message : t("error"));
    }
  };

  return (
    <>
      <h2>🛍️ {t("marketplace")}</h2>
      <div className="card col" style={{ gap: 8 }}>
        <strong>{t("createListing")}</strong>
        <input value={title} maxLength={120} placeholder={t("title")} onChange={(e) => setTitle(e.target.value)} />
        <textarea value={description} maxLength={2000} rows={2}
          placeholder={t("description")} onChange={(e) => setDescription(e.target.value)} />
        <div className="row" style={{ gap: 6 }}>
          <input type="number" min={1} step="0.01" value={price}
            placeholder={t("priceUSD")} onChange={(e) => setPrice(e.target.value)} style={{ width: 140 }} />
          <button onClick={createListing} disabled={!title.trim() || !price}>{t("create")}</button>
        </div>
      </div>
      {status && <div className="card muted">{status}</div>}
      {error && <div className="card error-text">{error}</div>}
      <div className="row" style={{ gap: 6, margin: "10px 0" }}>
        <button className={tab === "browse" ? "small" : "secondary small"} onClick={() => setTab("browse")}>
          {t("browse")}
        </button>
        <button className={tab === "purchases" ? "small" : "secondary small"} onClick={() => setTab("purchases")}>
          {t("myPurchases")} · {purchases.length}
        </button>
        <button className={tab === "sales" ? "small" : "secondary small"} onClick={() => setTab("sales")}>
          {t("mySales")} · {sales.length}
        </button>
      </div>
      {tab === "browse" && listings.map((l) => (
        <div className="card row" key={l.id} style={{ alignItems: "center" }}>
          <div style={{ flex: 1 }}>
            <strong>{l.title}</strong>
            {l.seller && <span className="muted" style={{ fontSize: 12 }}> · @{l.seller}</span>}
            <div className="muted" style={{ fontSize: 13 }}>{l.description}</div>
          </div>
          <div style={{ textAlign: "right" }}>
            <div><strong>${l.price_usd}</strong></div>
            {l.status === "active" && (
              <button className="small" onClick={() => buy(l.id)}>{t("buy")}</button>
            )}
          </div>
        </div>
      ))}
      {tab !== "browse" && (tab === "purchases" ? purchases : sales).map((o) => (
        <div className="card row" key={o.id}>
          <span style={{ flex: 1 }}>{o.title}</span>
          <span className="muted">${o.amount_usd}</span>
          <span className="muted" style={{ fontSize: 12 }}>
            {o.status} · {new Date(o.created_at).toLocaleDateString()}
          </span>
        </div>
      ))}
      {tab !== "browse" && (tab === "purchases" ? purchases : sales).length === 0 && (
        <div className="card muted">{t("noOrders")}</div>
      )}
    </>
  );
}
