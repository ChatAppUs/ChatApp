"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import { useRouter } from "next/navigation";
import { api, getAccessToken } from "@/lib/api";
import { useI18n } from "@/lib/i18n";
import type { StakingAsset, StakingPosition } from "@/lib/types";

export default function StakingPage() {
  const { t } = useI18n();
  const router = useRouter();
  const [assets, setAssets] = useState<StakingAsset[]>([]);
  const [positions, setPositions] = useState<StakingPosition[]>([]);
  const [assetPick, setAssetPick] = useState("");
  const [durationPick, setDurationPick] = useState<number>(0);
  const [amount, setAmount] = useState("");
  const [err, setErr] = useState("");
  const [msg, setMsg] = useState("");

  const load = useCallback(() => {
    api<{ assets: StakingAsset[] }>("/api/staking/assets")
      .then((d) => setAssets(d.assets || []))
      .catch(() => setAssets([]));
    api<{ positions: StakingPosition[] }>("/api/staking/positions")
      .then((d) => setPositions(d.positions || []))
      .catch(() => setPositions([]));
  }, []);

  useEffect(() => {
    if (!getAccessToken()) {
      router.push("/login");
      return;
    }
    load();
  }, [load, router]);

  const assetObj = useMemo(
    () => assets.find((a) => `${a.asset}/${a.chain}` === assetPick),
    [assets, assetPick],
  );

  useEffect(() => {
    if (!assetPick && assets.length > 0) {
      setAssetPick(`${assets[0].asset}/${assets[0].chain}`);
    }
  }, [assets, assetPick]);

  useEffect(() => {
    if (assetObj) setDurationPick(assetObj.durations[0] ?? 0);
  }, [assetObj]);

  const open = async () => {
    setErr(""); setMsg("");
    if (!assetObj) return;
    if (!assetObj.durations.includes(durationPick)) {
      setErr(t("durationNotAllowed"));
      return;
    }
    try {
      const [asset, chain] = assetPick.split("/");
      await api("/api/staking/positions", {
        method: "POST",
        body: JSON.stringify({ asset, chain, amount, duration_days: durationPick }),
      });
      setMsg(t("positionOpened"));
      setAmount("");
      load();
    } catch (e) {
      setErr(e instanceof Error ? e.message : t("error"));
    }
  };

  const estimateReward = () => {
    if (!assetObj || !amount || Number(amount) <= 0) return null;
    const apy = Number(assetObj.apy) / 100;
    return (Number(amount) * apy * (durationPick / 365)).toFixed(6);
  };

  const unlock = async (pos: StakingPosition) => {
    setErr(""); setMsg("");
    try {
      const r = await api<{ status: string; message?: string }>(
        `/api/staking/positions/${pos.id}/unlock`, { method: "POST" },
      );
      setMsg(r.message || t("unlockQueued"));
      load();
    } catch (e) {
      setErr(e instanceof Error ? e.message : t("error"));
    }
  };

  return (
    <div className="card">
      <h3>{t("staking")}</h3>
      {err && <p className="error">{err}</p>}
      {msg && <p className="ok">{msg}</p>}
      <div className="grid2">
        <label>
          {t("asset")}
          <select value={assetPick} onChange={(e) => setAssetPick(e.target.value)}>
            {assets.map((a) => (
              <option key={`${a.asset}/${a.chain}`} value={`${a.asset}/${a.chain}`}>
                {a.asset} ({a.chain})
              </option>
            ))}
          </select>
        </label>
        <label>
          {t("durationDays")}
          <select value={durationPick} onChange={(e) => setDurationPick(Number(e.target.value))}>
            {(assetObj?.durations ?? []).map((d) => (
              <option key={d} value={d}>{d}</option>
            ))}
          </select>
        </label>
      </div>
      {assetObj && (
        <p className="muted">
          APY {assetObj.apy} · {t("livePrice")}: {assetObj.price_usd ?? "—"} USD ·
          {" "}{t("minStake")} {assetObj.min_amount} · {t("maxStake")} {assetObj.max_amount}
        </p>
      )}
      <div className="row">
        <input
          type="number"
          value={amount}
          placeholder={t("amount")}
          onChange={(e) => setAmount(e.target.value)}
        />
        <button onClick={open} disabled={!assetObj || !amount}>
          {t("stake")}
        </button>
      </div>
      {estimateReward() != null && (
        <p className="muted">{t("estimatedReward")}: {estimateReward()}</p>
      )}
      <h4 style={{ marginTop: "16px" }}>{t("yourPositions")}</h4>
      {positions.length === 0 && <p className="muted">{t("noPositions")}</p>}
      {positions.map((p) => (
        <div key={p.id} className="card row" style={{ justifyContent: "space-between" }}>
          <div>
            <b>{p.asset}/{p.chain}</b> · {p.amount} @ {p.apy} · {p.duration_days}d
            <div className="muted">
              {t("endsAt")}: {p.ends_at.slice(0, 10)} · {t("status")}: {p.status}
              {p.accrued_estimate ? <> · {t("accruedEst")}: {p.accrued_estimate}</> : null}
              {p.reward ? <> · {t("reward")}: {p.reward}</> : null}
            </div>
          </div>
          {p.status === "active" && (
            <button className="secondary small" onClick={() => unlock(p)}>{t("unlock")}</button>
          )}
          {p.status === "unlock_requested" && (
            <span className="muted">{t("unlockQueued")}</span>
          )}
        </div>
      ))}
    </div>
  );
}
