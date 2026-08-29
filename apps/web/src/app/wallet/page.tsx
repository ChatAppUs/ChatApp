"use client";

import { useCallback, useEffect, useState } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { api, getAccessToken } from "@/lib/api";
import { useI18n } from "@/lib/i18n";
import type { WalletAccount, LedgerEntry } from "@/lib/types";

export default function WalletPage() {
  const { t } = useI18n();
  const router = useRouter();
  const [accounts, setAccounts] = useState<WalletAccount[]>([]);
  const [history, setHistory] = useState<LedgerEntry[]>([]);
  const [assets, setAssets] = useState<Record<string, string[]>>({});
  const [kyc, setKyc] = useState("");
  const [newAsset, setNewAsset] = useState("USDT");
  const [newChain, setNewChain] = useState("ethereum");
  const [transfer, setTransfer] = useState({ to: "", asset: "USDT", chain: "ethereum", amount: "", memo: "" });
  const [msg, setMsg] = useState("");
  const [err, setErr] = useState("");

  const load = useCallback(() => {
    api<{ accounts: WalletAccount[] }>("/api/wallet/accounts").then((d) => setAccounts(d.accounts)).catch(() => {});
    api<{ entries: LedgerEntry[] }>("/api/wallet/history").then((d) => setHistory(d.entries)).catch(() => {});
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

  return (
    <>
      {kyc !== "verified" && (
        <div className="card">
          <span className="badge yellow">{t("kycStatus")}: {kyc || "none"}</span>{" "}
          <Link href="/kyc">{t("kyc")} →</Link>
        </div>
      )}
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
            {Object.keys(assets).map((a) => <option key={a} value={a}>{a}</option>)}
          </select>
          <select value={newChain} onChange={(e) => setNewChain(e.target.value)} style={{ width: "auto" }}>
            {(assets[newAsset] ?? []).map((c) => <option key={c} value={c}>{c}</option>)}
          </select>
          <button className="secondary small" onClick={createAccount}>{t("createAccount")}</button>
        </div>
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
              {Object.keys(assets).filter((a) => a !== "USD").map((a) => <option key={a} value={a}>{a}</option>)}
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
