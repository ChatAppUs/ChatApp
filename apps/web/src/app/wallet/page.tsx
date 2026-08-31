"use client";

import { useCallback, useEffect, useState } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { api, getAccessToken } from "@/lib/api";
import { useI18n } from "@/lib/i18n";
import QRDisplay from "@/components/QRDisplay";
import QRScanModal from "@/components/QRScanModal";
import type { WalletAccount, LedgerEntry, DepositAddress, Withdrawal, TokenPrice } from "@/lib/types";

function PricesTable() {
  const [prices, setPrices] = useState<TokenPrice[]>([]);
  useEffect(() => {
    api<{ prices: TokenPrice[] }>("/api/prices")
      .then((d) => setPrices(d.prices || []))
      .catch(() => setPrices([]));
  }, []);
  if (prices.length === 0) return <p className="muted">—</p>;
  return (
    <table className="table">
      <thead>
        <tr><th>asset</th><th>USD</th><th>source</th></tr>
      </thead>
      <tbody>
        {prices.map((p) => (
          <tr key={`${p.asset}/${p.chain}`}>
            <td>{p.asset} <span className="muted">{p.chain}</span></td>
            <td>{p.usd ?? "—"}</td>
            <td className="muted">{p.source ?? "—"}</td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}

export default function WalletPage() {
  const { t } = useI18n();
  const router = useRouter();
  const [accounts, setAccounts] = useState<WalletAccount[]>([]);
  const [history, setHistory] = useState<LedgerEntry[]>([]);
  const [withdrawals, setWithdrawals] = useState<Withdrawal[]>([]);
  const [assets, setAssets] = useState<Record<string, string[]>>({});
  const [kyc, setKyc] = useState("");
  const [newAsset, setNewAsset] = useState("USDT");
  const [newChain, setNewChain] = useState("ethereum");
  const [transfer, setTransfer] = useState({ to: "", asset: "USDT", chain: "ethereum", amount: "", memo: "" });
  const [deposit, setDeposit] = useState({ asset: "USDT", chain: "ethereum" });
  const [depositAddr, setDepositAddr] = useState<DepositAddress | null>(null);
  const [wd, setWd] = useState({ asset: "USDT", chain: "ethereum", to: "", amount: "" });
  const [copied, setCopied] = useState(false);
  const [scanning, setScanning] = useState(false);
  const [msg, setMsg] = useState("");
  const [err, setErr] = useState("");

  const load = useCallback(() => {
    api<{ accounts: WalletAccount[] }>("/api/wallet/accounts").then((d) => setAccounts(d.accounts)).catch(() => {});
    api<{ entries: LedgerEntry[] }>("/api/wallet/history").then((d) => setHistory(d.entries)).catch(() => {});
    api<{ withdrawals: Withdrawal[] }>("/api/wallet/withdrawals").then((d) => setWithdrawals(d.withdrawals)).catch(() => {});
    api<{ assets: Record<string, string[]> }>("/api/wallet/assets").then((d) => setAssets(d.assets)).catch(() => {});
    api<{ status: string }>("/api/kyc/status").then((d) => setKyc(d.status)).catch(() => {});
  }, []);

  useEffect(() => {
    if (!getAccessToken()) {
      router.push("/login");
      return;
    }
    load();
  }, [load, router]);

  const assetNames = Object.keys(assets).filter((a) => a !== "USD");
  const allAssetNames = Object.keys(assets);

  const createAccount = async () => {
    setErr(""); setMsg("");
    try {
      await api("/api/wallet/accounts", {
        method: "POST",
        body: JSON.stringify({ asset: newAsset, chain: newChain }),
      });
      load();
    } catch (e) {
      setErr(e instanceof Error ? e.message : t("error"));
    }
  };

  const sendTransfer = async () => {
    setErr(""); setMsg("");
    try {
      const res = await api<{ tx_id: string }>("/api/wallet/transfer", {
        method: "POST",
        body: JSON.stringify({
          to_username: transfer.to,
          asset: transfer.asset,
          chain: transfer.chain,
          amount: transfer.amount,
          memo: transfer.memo,
        }),
      });
      setMsg(`tx ${res.tx_id}`);
      setTransfer({ ...transfer, to: "", amount: "", memo: "" });
      load();
    } catch (e) {
      setErr(e instanceof Error ? e.message : t("error"));
    }
  };

  const loadDepositAddress = async () => {
    setErr("");
    try {
      const d = await api<DepositAddress>("/api/wallet/deposit-address", {
        method: "POST",
        body: JSON.stringify({ asset: deposit.asset, chain: deposit.chain }),
      });
      setDepositAddr(d);
      setCopied(false);
    } catch (e) {
      setDepositAddr(null);
      setErr(e instanceof Error ? e.message : t("error"));
    }
  };

  const copyAddress = async () => {
    if (!depositAddr) return;
    try {
      await navigator.clipboard.writeText(depositAddr.address);
      setCopied(true);
    } catch {
      /* clipboard unavailable */
    }
  };

  const pasteAddress = async () => {
    try {
      const text = await navigator.clipboard.readText();
      if (text) setWd({ ...wd, to: text.trim() });
    } catch {
      /* clipboard unavailable */
    }
  };

  const submitWithdraw = async () => {
    setErr(""); setMsg("");
    try {
      const res = await api<{ id: string; status: string; auto_approved: boolean; approved_in_ms: number }>(
        "/api/wallet/withdraw",
        {
          method: "POST",
          body: JSON.stringify({ asset: wd.asset, chain: wd.chain, to_address: wd.to, amount: wd.amount }),
        },
      );
      setMsg(
        res.auto_approved
          ? `${t("withdraw")} ${res.status} — auto-approved in ${res.approved_in_ms}ms`
          : `${t("withdraw")} ${res.status} — ${t("pendingReview")}`,
      );
      setWd({ ...wd, to: "", amount: "" });
      load();
    } catch (e) {
      setErr(e instanceof Error ? e.message : t("error"));
    }
  };

  return (
    <>
      {kyc !== "verified" && (
        <div className="card">
          <span className="badge yellow">{t("kycStatus")}: {kyc || "none"}</span>{" "}
          <Link href="/kyc">{t("kyc")} →</Link>
        </div>
      )}
      <div className="card">
        <div className="row">
          <Link href="/cards">💳 Cards →</Link>
          <Link href="/staking">{t("staking")} →</Link>
        </div>
      </div>
      <div className="card">
        <h3>{t("prices")}</h3>
        <PricesTable />
      </div>
      <div className="card">
        <h3>{t("balance")}</h3>
        <table className="table">
          <thead>
            <tr><th>{t("asset")}</th><th>{t("chain")}</th><th>{t("balance")}</th></tr>
          </thead>
          <tbody>
            {accounts.map((a) => (
              <tr key={a.id}>
                <td>{a.asset}</td>
                <td>{a.chain}</td>
                <td>{a.balance}</td>
              </tr>
            ))}
            {accounts.length === 0 && (
              <tr><td colSpan={3} className="muted">{t("noResults")}</td></tr>
            )}
          </tbody>
        </table>
        <div className="row" style={{ marginTop: 10 }}>
          <select value={newAsset} onChange={(e) => {
            setNewAsset(e.target.value);
            setNewChain((assets[e.target.value] ?? [""])[0]);
          }} style={{ width: "auto" }}>
            {allAssetNames.map((a) => <option key={a} value={a}>{a}</option>)}
          </select>
          <select value={newChain} onChange={(e) => setNewChain(e.target.value)} style={{ width: "auto" }}>
            {(assets[newAsset] ?? []).map((c) => <option key={c} value={c}>{c}</option>)}
          </select>
          <button className="secondary small" onClick={createAccount}>{t("createAccount")}</button>
        </div>
      </div>

      <div className="card">
        <h3>{t("deposit")}</h3>
        <p className="muted">{t("depositHint")}</p>
        <div className="row">
          <select value={deposit.asset} onChange={(e) => {
            const a = e.target.value;
            setDeposit({ asset: a, chain: (assets[a] ?? [""])[0] });
            setDepositAddr(null);
          }} style={{ width: "auto" }}>
            {assetNames.map((a) => <option key={a} value={a}>{a}</option>)}
          </select>
          <select value={deposit.chain} onChange={(e) => { setDeposit({ ...deposit, chain: e.target.value }); setDepositAddr(null); }} style={{ width: "auto" }}>
            {(assets[deposit.asset] ?? []).map((c) => <option key={c} value={c}>{c}</option>)}
          </select>
          <button className="secondary small" onClick={loadDepositAddress}>{t("depositAddress")}</button>
        </div>
        {depositAddr && (
          <div style={{ marginTop: 12, display: "flex", gap: 16, alignItems: "flex-start", flexWrap: "wrap" }}>
            <QRDisplay value={depositAddr.uri} />
            <div style={{ flex: 1, minWidth: 220 }}>
              <label>{t("depositAddress")}</label>
              <input readOnly value={depositAddr.address} onFocus={(e) => e.target.select()} />
              <div className="row" style={{ marginTop: 8 }}>
                <button className="secondary small" onClick={copyAddress}>
                  {copied ? `✓ ${t("copied")}` : t("copy")}
                </button>
              </div>
              <p className="muted" style={{ marginTop: 8 }}>{depositAddr.asset} · {depositAddr.chain}</p>
            </div>
          </div>
        )}
      </div>

      <div className="card">
        <h3>{t("withdraw")}</h3>
        <p className="muted">{t("withdrawHint")}</p>
        <div className="grid2">
          <div>
            <label>{t("asset")}</label>
            <select value={wd.asset} onChange={(e) => {
              const a = e.target.value;
              setWd({ ...wd, asset: a, chain: (assets[a] ?? [""])[0] });
            }}>
              {assetNames.map((a) => <option key={a} value={a}>{a}</option>)}
            </select>
          </div>
          <div>
            <label>{t("chain")}</label>
            <select value={wd.chain} onChange={(e) => setWd({ ...wd, chain: e.target.value })}>
              {(assets[wd.asset] ?? []).map((c) => <option key={c} value={c}>{c}</option>)}
            </select>
          </div>
          <div>
            <label>{t("toAddress")}</label>
            <div className="row">
              <input value={wd.to} onChange={(e) => setWd({ ...wd, to: e.target.value })} style={{ flex: 1 }} />
              <button className="secondary small" onClick={pasteAddress}>{t("paste")}</button>
              <button className="secondary small" onClick={() => setScanning(!scanning)}>{t("scanQR")}</button>
            </div>
          </div>
          <div>
            <label>{t("amount")}</label>
            <input value={wd.amount} onChange={(e) => setWd({ ...wd, amount: e.target.value })} inputMode="decimal" />
          </div>
        </div>
        {scanning && (
          <QRScanModal
            onResult={(addr) => { setWd({ ...wd, to: addr }); setScanning(false); }}
            onClose={() => setScanning(false)}
          />
        )}
        <div className="row" style={{ marginTop: 10 }}>
          <button onClick={submitWithdraw} disabled={!wd.to || !wd.amount}>{t("withdraw")}</button>
        </div>
        {withdrawals.length > 0 && (
          <table className="table" style={{ marginTop: 12 }}>
            <thead>
              <tr><th>{t("asset")}</th><th>{t("amount")}</th><th>{t("fee")}</th><th>{t("status")}</th><th></th></tr>
            </thead>
            <tbody>
              {withdrawals.map((x) => (
                <tr key={x.id}>
                  <td>{x.asset} <span className="muted">{x.chain}</span></td>
                  <td>{x.amount}</td>
                  <td className="muted">{x.fee}</td>
                  <td>
                    <span className={`badge ${x.status === "completed" ? "green" : x.status === "rejected" || x.status === "failed" ? "red" : "yellow"}`}>
                      {x.status}
                    </span>
                    {x.auto_approved && <span className="muted"> ⚡</span>}
                  </td>
                  <td className="muted">{new Date(x.created_at).toLocaleString()}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>

      <div className="card">
        <h3>{t("transfer")}</h3>
        <div className="grid2">
          <div>
            <label>{t("toUsername")}</label>
            <input value={transfer.to} onChange={(e) => setTransfer({ ...transfer, to: e.target.value })} />
          </div>
          <div>
            <label>{t("amount")}</label>
            <input value={transfer.amount} onChange={(e) => setTransfer({ ...transfer, amount: e.target.value })} inputMode="decimal" />
          </div>
          <div>
            <label>{t("asset")}</label>
            <select value={transfer.asset} onChange={(e) => {
              const a = e.target.value;
              setTransfer({ ...transfer, asset: a, chain: (assets[a] ?? [""])[0] });
            }}>
              {assetNames.map((a) => <option key={a} value={a}>{a}</option>)}
            </select>
          </div>
          <div>
            <label>{t("chain")}</label>
            <select value={transfer.chain} onChange={(e) => setTransfer({ ...transfer, chain: e.target.value })}>
              {(assets[transfer.asset] ?? []).map((c) => <option key={c} value={c}>{c}</option>)}
            </select>
          </div>
        </div>
        <div style={{ marginTop: 8 }}>
          <label>{t("memo")}</label>
          <input value={transfer.memo} onChange={(e) => setTransfer({ ...transfer, memo: e.target.value })} />
        </div>
        <div className="row" style={{ marginTop: 10 }}>
          <button onClick={sendTransfer}>{t("send")}</button>
          {msg && <span className="success-text">{msg}</span>}
          {err && <span className="error-text">{err}</span>}
        </div>
      </div>

      <div className="card">
        <h3>{t("history")}</h3>
        <table className="table">
          <thead>
            <tr><th>{t("asset")}</th><th>{t("amount")}</th><th>kind</th><th>{t("memo")}</th><th></th></tr>
          </thead>
          <tbody>
            {history.map((h, i) => (
              <tr key={i}>
                <td>{h.asset}</td>
                <td style={{ color: h.amount.startsWith("-") ? "var(--danger)" : "var(--accent-2)" }}>{h.amount}</td>
                <td>{h.kind}</td>
                <td className="muted">{h.memo}</td>
                <td className="muted">{new Date(h.created_at).toLocaleString()}</td>
              </tr>
            ))}
            {history.length === 0 && (
              <tr><td colSpan={5} className="muted">{t("noResults")}</td></tr>
            )}
          </tbody>
        </table>
      </div>
    </>
  );
}
