"use client";

import { useCallback, useEffect, useState } from "react";
import { useI18n } from "@/lib/i18n";
import { api } from "@/lib/api";
import {
  cancelSubscription, createTier, earnings, giftCatalog, mySubscriptions,
  myTiers, sendGift, sendTip,
  type Earnings, type Gift, type Subscription, type Tier,
} from "@/lib/features";

interface UserHit { id: string; username: string; display_name: string }

async function findUser(username: string): Promise<UserHit | null> {
  const r = await api<{ users: UserHit[] }>(`/api/users/search?q=${encodeURIComponent(username)}`);
  return r.users.find((u) => u.username === username) ?? r.users[0] ?? null;
}

export default function MonetizePage() {
  const { t } = useI18n();
  const [tiers, setTiers] = useState<Tier[]>([]);
  const [subs, setSubs] = useState<Subscription[]>([]);
  const [gifts, setGifts] = useState<Gift[]>([]);
  const [earns, setEarns] = useState<Earnings | null>(null);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");

  const [name, setName] = useState("");
  const [price, setPrice] = useState("");
  const [perks, setPerks] = useState("");

  const [tipTo, setTipTo] = useState("");
  const [tipAmount, setTipAmount] = useState("");
  const [giftTo, setGiftTo] = useState("");
  const [giftId, setGiftId] = useState("");

  const load = useCallback(async () => {
    try {
      const [ti, su, ea, gc] = await Promise.all([myTiers(), mySubscriptions(), earnings(), giftCatalog()]);
      setTiers(ti.tiers);
      setSubs(su.subscriptions);
      setEarns(ea);
      setGifts(gc.gifts);
      if (!giftId && gc.gifts.length > 0) setGiftId(gc.gifts[0].id);
    } catch (e) {
      setError(e instanceof Error ? e.message : "load failed");
    }
  }, [giftId]);

  useEffect(() => { load(); }, [load]);

  const create = async (e: React.FormEvent) => {
    e.preventDefault();
    setError("");
    try {
      const usd = parseFloat(price);
      if (!Number.isFinite(usd) || usd <= 0) throw new Error("invalid price");
      await createTier(name, perks, usd);
      setName(""); setPrice(""); setPerks("");
      setNotice(t("createTier"));
      load();
    } catch (err) {
      setError(err instanceof Error ? err.message : "create failed");
    }
  };

  const tip = async (e: React.FormEvent) => {
    e.preventDefault();
    try {
      const user = await findUser(tipTo);
      if (!user) throw new Error("user not found");
      await sendTip(user.id, parseFloat(tipAmount), "");
      setTipTo(""); setTipAmount("");
      setNotice(t("sendTip"));
      load();
    } catch (err) {
      setError(err instanceof Error ? err.message : "tip failed");
    }
  };

  const gift = async (e: React.FormEvent) => {
    e.preventDefault();
    try {
      const user = await findUser(giftTo);
      if (!user) throw new Error("user not found");
      await sendGift(user.id, giftId);
      setGiftTo("");
      setNotice(t("sendGift"));
      load();
    } catch (err) {
      setError(err instanceof Error ? err.message : "gift failed");
    }
  };

  const unsub = async (subId: string) => {
    await cancelSubscription(subId).catch((e) => setError(e.message));
    load();
  };

  return (
    <div className="col">
      <h1>{t("monetize")}</h1>
      {error && <div className="error">{error}</div>}
      {notice && <div className="badge green">{notice}</div>}

      <form className="card col" onSubmit={create}>
        <h2>{t("createTier")}</h2>
        <label>{t("eventTitle")}</label>
        <input value={name} onChange={(e) => setName(e.target.value)} required maxLength={60} />
        <label>{t("amount")} (USD)</label>
        <input value={price} onChange={(e) => setPrice(e.target.value)} required placeholder="4.99" />
        <label>{t("benefits")}</label>
        <input value={perks} onChange={(e) => setPerks(e.target.value)} maxLength={512} />
        <button type="submit">{t("createTier")}</button>
      </form>

      <h2>{t("tiers")}</h2>
      {tiers.map((tier) => (
        <div className="card row" key={tier.id}>
          <strong>{tier.name}</strong>
          <span className="badge">${tier.price_usd.toFixed(2)}/mo</span>
          <span className="badge">{tier.subscriber_count}</span>
        </div>
      ))}

      <h2>{t("memberships")}</h2>
      {subs.map((s) => (
        <div className="card col" key={s.id}>
          <div className="row">
            <strong>{s.tier_name}</strong>
            <span className="badge">${s.price_usd.toFixed(2)}</span>
            <span className="badge green">{s.status}</span>
          </div>
          <p className="muted">@{s.creator_username} · until {new Date(s.current_period_end).toLocaleDateString()}</p>
          <button className="secondary" onClick={() => unsub(s.id)}>{t("unsubscribe")}</button>
        </div>
      ))}

      <h2>{t("tips")}</h2>
      <form className="card col" onSubmit={tip}>
        <label>{t("toUsername")}</label>
        <input value={tipTo} onChange={(e) => setTipTo(e.target.value)} required />
        <label>{t("amount")} (USD)</label>
        <input value={tipAmount} onChange={(e) => setTipAmount(e.target.value)} required placeholder="5.00" />
        <button type="submit">{t("sendTip")}</button>
      </form>

      <h2>{t("gifts")}</h2>
      <form className="card col" onSubmit={gift}>
        <label>{t("toUsername")}</label>
        <input value={giftTo} onChange={(e) => setGiftTo(e.target.value)} required />
        <label>{t("gifts")}</label>
        <select value={giftId} onChange={(e) => setGiftId(e.target.value)}>
          {gifts.map((g) => (
            <option key={g.id} value={g.id}>{g.name} · ${g.price_usd.toFixed(2)}</option>
          ))}
        </select>
        <button type="submit">{t("sendGift")}</button>
      </form>

      <h2>{t("earnings")}</h2>
      {earns && (
        <div className="card col">
          <div className="row">
            <span className="badge green">{t("earnings")} ${earns.earned.toFixed(2)}</span>
            <span className="badge">{t("earnings")} available ${earns.available.toFixed(2)}</span>
            <span className="badge">paid out ${earns.paid_out.toFixed(2)}</span>
          </div>
        </div>
      )}
    </div>
  );
}
