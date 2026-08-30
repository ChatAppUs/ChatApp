"use client";

import { FormEvent, useCallback, useEffect, useState } from "react";
import { adminApi } from "@/lib/api";

// Admin finance plane: withdrawal signing queue, dynamic role management,
// convert rates and P2P dispute resolution.

interface Withdrawal {
  id: string;
  username: string;
  asset: string;
  chain: string;
  to_address: string;
  amount: string;
  fee: string;
  status: string;
  risk_score: number;
  risk_flags: string[];
  auto_approved: boolean;
  approved_by: string;
  tx_hash: string;
  created_at: string;
}

interface RoleDef {
  name: string;
  description: string;
  permissions: string[];
  built_in: boolean;
  created_at: string;
}

interface Dispute {
  id: string;
  buyer_username: string;
  seller_username: string;
  asset: string;
  chain: string;
  crypto_amount: string;
  fiat_amount: string;
  fiat_currency: string;
  payment_method: string;
  status: string;
}

type Act = (fn: () => Promise<unknown>) => void;

export function WithdrawalsTab({ act }: { act: Act }) {
  const [items, setItems] = useState<Withdrawal[]>([]);
  const [filter, setFilter] = useState("pending_review");

  const load = useCallback(() => {
    adminApi<{ withdrawals: Withdrawal[] }>(`/api/admin/withdrawals?status=${filter}`)
      .then((d) => setItems(d.withdrawals))
      .catch(() => setItems([]));
  }, [filter]);

  useEffect(() => { load(); }, [load]);

  return (
    <div className="card">
      <div className="row" style={{ marginBottom: 10 }}>
        <select value={filter} onChange={(e) => setFilter(e.target.value)} style={{ width: "auto" }}>
          {["pending_review", "approved", "signed", "completed", "rejected", "failed"].map((s) => (
            <option key={s} value={s}>{s}</option>
          ))}
        </select>
        <button className="secondary small" onClick={load}>Refresh</button>
      </div>
      <table className="table">
        <thead>
          <tr><th>user</th><th>amount</th><th>destination</th><th>risk</th><th>status</th><th></th></tr>
        </thead>
        <tbody>
          {items.map((w) => (
            <tr key={w.id}>
              <td>@{w.username}</td>
              <td>{w.amount} {w.asset} <span className="muted">{w.chain} · fee {w.fee}</span></td>
              <td className="muted" style={{ maxWidth: 220, overflow: "hidden", textOverflow: "ellipsis" }}>{w.to_address}</td>
              <td>
                <span className={`badge ${w.risk_score >= 100 ? "red" : w.risk_score > 0 ? "yellow" : "green"}`}>{w.risk_score}</span>{" "}
                <span className="muted">{w.risk_flags.join(", ")}</span>
              </td>
              <td>
                <span className={`badge ${w.status === "completed" ? "green" : w.status === "rejected" || w.status === "failed" ? "red" : "yellow"}`}>
                  {w.status}{w.auto_approved ? " ⚡" : ""}
                </span>
              </td>
              <td>
                {w.status === "pending_review" && (
                  <div className="row">
                    <button className="success small" onClick={() => act(() => adminApi(`/api/admin/withdrawals/${w.id}/review`, { method: "POST", body: JSON.stringify({ decision: "approve" }) }).then(load))}>
                      Sign &amp; approve
                    </button>
                    <button className="danger small" onClick={() => act(() => adminApi(`/api/admin/withdrawals/${w.id}/review`, { method: "POST", body: JSON.stringify({ decision: "reject" }) }).then(load))}>
                      Reject &amp; refund
                    </button>
                  </div>
                )}
                {w.tx_hash && <span className="muted">{w.tx_hash.slice(0, 18)}…</span>}
              </td>
            </tr>
          ))}
          {items.length === 0 && <tr><td colSpan={6} className="muted">Queue empty</td></tr>}
        </tbody>
      </table>
    </div>
  );
}

export function RolesTab({ act }: { act: Act }) {
  const [roles, setRoles] = useState<RoleDef[]>([]);
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [permissions, setPermissions] = useState("");
  const [grantUser, setGrantUser] = useState("");
  const [grantRole, setGrantRole] = useState("");
  const [error, setError] = useState("");

  const load = useCallback(() => {
    adminApi<{ roles: RoleDef[] }>("/api/admin/role-defs")
      .then((d) => setRoles(d.roles))
      .catch(() => setRoles([]));
  }, []);

  useEffect(() => { load(); }, [load]);

  const create = (e: FormEvent) => {
    e.preventDefault();
    setError("");
    act(async () => {
      try {
        await adminApi("/api/admin/role-defs", {
          method: "POST",
          body: JSON.stringify({
            name,
            description,
            permissions: permissions.split(",").map((p) => p.trim()).filter(Boolean),
          }),
        });
        setName(""); setDescription(""); setPermissions("");
        load();
      } catch (err) {
        setError((err as Error).message);
      }
    });
  };

  const grant = (e: FormEvent) => {
    e.preventDefault();
    setError("");
    act(async () => {
      try {
        await adminApi("/api/admin/roles", {
          method: "POST",
          body: JSON.stringify({ user_id: grantUser, role: grantRole }),
        });
        setGrantUser("");
      } catch (err) {
        setError((err as Error).message);
      }
    });
  };

  const revoke = () => {
    setError("");
    act(async () => {
      try {
        await adminApi("/api/admin/roles", {
          method: "DELETE",
          body: JSON.stringify({ user_id: grantUser, role: grantRole }),
        });
        setGrantUser("");
      } catch (err) {
        setError((err as Error).message);
      }
    });
  };

  return (
    <>
      <div className="card" style={{ marginBottom: 12 }}>
        <h4 style={{ marginTop: 0 }}>Create admin role</h4>
        <form onSubmit={create}>
          <div className="row" style={{ flexWrap: "wrap", gap: 8 }}>
            <input placeholder="role_name" value={name} onChange={(e) => setName(e.target.value)} required style={{ width: 180 }} />
            <input placeholder="Description" value={description} onChange={(e) => setDescription(e.target.value)} style={{ flex: 1 }} />
            <input
              placeholder="permissions, comma-separated (e.g. p2p.resolve, convert.manage, withdrawals.review)"
              value={permissions}
              onChange={(e) => setPermissions(e.target.value)}
              required
              style={{ flex: 2, minWidth: 280 }}
            />
            <button className="small" type="submit">Create role</button>
          </div>
        </form>
        <h4>Grant / revoke role</h4>
        <form onSubmit={grant}>
          <div className="row" style={{ flexWrap: "wrap", gap: 8 }}>
            <input placeholder="User ID" value={grantUser} onChange={(e) => setGrantUser(e.target.value)} required style={{ flex: 1, minWidth: 280 }} />
            <select value={grantRole} onChange={(e) => setGrantRole(e.target.value)} required style={{ width: "auto" }}>
              <option value="">role…</option>
              {roles.map((r) => <option key={r.name} value={r.name}>{r.name}</option>)}
            </select>
            <button className="small" type="submit">Grant</button>
            <button className="danger small" type="button" onClick={revoke}>Revoke</button>
          </div>
        </form>
        {error && <div className="error-text">{error}</div>}
      </div>
      <div className="card">
        <table className="table">
          <thead>
            <tr><th>role</th><th>permissions</th><th>description</th><th></th></tr>
          </thead>
          <tbody>
            {roles.map((r) => (
              <tr key={r.name}>
                <td><strong>{r.name}</strong>{r.built_in && <span className="badge">built-in</span>}</td>
                <td className="muted">{r.permissions.join(", ") || "—"}</td>
                <td className="muted">{r.description}</td>
                <td>
                  {!r.built_in && (
                    <button className="danger small" onClick={() => act(() => adminApi(`/api/admin/role-defs/${r.name}`, { method: "DELETE" }).then(load))}>
                      Delete
                    </button>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </>
  );
}

export function RatesTab({ act }: { act: Act }) {
  const [asset, setAsset] = useState("");
  const [chain, setChain] = useState("");
  const [rate, setRate] = useState("");

  const save = (e: FormEvent) => {
    e.preventDefault();
    act(async () => {
      await adminApi("/api/admin/convert/rates", {
        method: "POST",
        body: JSON.stringify({ asset, chain, usd_rate: rate }),
      });
      setAsset(""); setChain(""); setRate("");
    });
  };

  return (
    <div className="card">
      <h4 style={{ marginTop: 0 }}>Set convert rate (USD per unit)</h4>
      <form onSubmit={save}>
        <div className="row" style={{ flexWrap: "wrap", gap: 8 }}>
          <input placeholder="Symbol (e.g. BTC)" value={asset} onChange={(e) => setAsset(e.target.value)} required style={{ width: 140 }} />
          <input placeholder="Chain (e.g. bitcoin)" value={chain} onChange={(e) => setChain(e.target.value)} required style={{ width: 160 }} />
          <input placeholder="USD rate" value={rate} onChange={(e) => setRate(e.target.value)} required inputMode="decimal" style={{ width: 160 }} />
          <button className="small" type="submit">Set rate</button>
        </div>
      </form>
      <p className="muted">Rates drive the user convert engine; every change is audit-logged.</p>
    </div>
  );
}

export function DisputesTab({ act }: { act: Act }) {
  const [disputes, setDisputes] = useState<Dispute[]>([]);

  const load = useCallback(() => {
    adminApi<{ trades: Dispute[] }>("/api/admin/p2p/disputes")
      .then((d) => setDisputes(d.trades))
      .catch(() => setDisputes([]));
  }, []);

  useEffect(() => { load(); }, [load]);

  return (
    <div className="card">
      <table className="table">
        <thead>
          <tr><th>trade</th><th>buyer</th><th>seller</th><th>amount</th><th>payment</th><th></th></tr>
        </thead>
        <tbody>
          {disputes.map((d) => (
            <tr key={d.id}>
              <td className="muted">{d.id.slice(0, 8)}…</td>
              <td>@{d.buyer_username}</td>
              <td>@{d.seller_username}</td>
              <td>{d.crypto_amount} {d.asset} <span className="muted">({d.fiat_amount} {d.fiat_currency})</span></td>
              <td className="muted">{d.payment_method}</td>
              <td>
                <div className="row">
                  <button className="success small" onClick={() => act(() => adminApi(`/api/admin/p2p/trades/${d.id}/resolve`, { method: "POST", body: JSON.stringify({ to: "buyer" }) }).then(load))}>
                    Pay buyer
                  </button>
                  <button className="secondary small" onClick={() => act(() => adminApi(`/api/admin/p2p/trades/${d.id}/resolve`, { method: "POST", body: JSON.stringify({ to: "seller" }) }).then(load))}>
                    Refund seller
                  </button>
                </div>
              </td>
            </tr>
          ))}
          {disputes.length === 0 && <tr><td colSpan={6} className="muted">No open disputes</td></tr>}
        </tbody>
      </table>
    </div>
  );
}

interface AdminMerchant {
  user_id: string;
  username: string;
  business_name: string;
  status: string;
  tier: number;
  tier_name: string;
  note: string;
  applied_at: string;
}

export function MerchantsTab({ act }: { act: (fn: () => Promise<unknown>) => void }) {
  const [merchants, setMerchants] = useState<AdminMerchant[]>([]);
  const [status, setStatus] = useState("pending");
  const load = useCallback(() => {
    adminApi<{ merchants: AdminMerchant[] }>(`/api/admin/p2p/merchants?status=${status}`)
      .then((d) => setMerchants(d.merchants)).catch(() => {});
  }, [status]);
  useEffect(() => { load(); }, [load]);

  const review = (uid: string, approve: boolean) => {
    const tier = approve ? Number(prompt("Tier level (1-3):", "1") || "1") : 0;
    act(() => adminApi(`/api/admin/p2p/merchants/${uid}/review`, {
      method: "POST", body: JSON.stringify({ approve, tier }),
    }).then(load));
  };

  return (
    <div className="card">
      <div className="row" style={{ marginBottom: 8 }}>
        {["pending", "verified", "rejected", "revoked", ""].map((s) => (
          <button key={s || "all"} className={status === s ? "small" : "secondary small"}
            onClick={() => setStatus(s)}>
            {s || "all"}
          </button>
        ))}
      </div>
      <table className="table">
        <thead>
          <tr><th>Business</th><th>User</th><th>Status</th><th>Tier</th><th>Note</th><th>Actions</th></tr>
        </thead>
        <tbody>
          {merchants.map((m) => (
            <tr key={m.user_id}>
              <td>{m.business_name}</td>
              <td>@{m.username}</td>
              <td><span className="badge">{m.status}</span></td>
              <td>{m.tier > 0 ? `T${m.tier} ${m.tier_name}` : "—"}</td>
              <td className="muted">{m.note}</td>
              <td>
                <div className="row">
                  {m.status === "pending" && (
                    <>
                      <button className="success small" onClick={() => review(m.user_id, true)}>Approve</button>
                      <button className="secondary small" onClick={() => review(m.user_id, false)}>Reject</button>
                    </>
                  )}
                  {m.status === "verified" && (
                    <>
                      <button className="secondary small" onClick={() => {
                        const tier = Number(prompt("New tier level:", String(m.tier)) || "");
                        if (tier) act(() => adminApi(`/api/admin/p2p/merchants/${m.user_id}/tier`, {
                          method: "POST", body: JSON.stringify({ tier }),
                        }).then(load));
                      }}>Set tier</button>
                      <button className="secondary small" onClick={() =>
                        act(() => adminApi(`/api/admin/p2p/merchants/${m.user_id}/revoke`, { method: "POST" }).then(load))}>
                        Revoke
                      </button>
                    </>
                  )}
                </div>
              </td>
            </tr>
          ))}
          {merchants.length === 0 && <tr><td colSpan={6} className="muted">No merchants with this status</td></tr>}
        </tbody>
      </table>
    </div>
  );
}

interface AdminCard {
  id: string;
  username: string;
  label: string;
  last4: string;
  status: string;
  balance_usd: string;
  daily_limit_usd: string;
  monthly_limit_usd: string;
  created_at: string;
}

export function CardsTab({ act }: { act: (fn: () => Promise<unknown>) => void }) {
  const [cards, setCards] = useState<AdminCard[]>([]);
  const load = useCallback(() => {
    adminApi<{ cards: AdminCard[] }>("/api/admin/cards")
      .then((d) => setCards(d.cards)).catch(() => {});
  }, []);
  useEffect(() => { load(); }, [load]);

  return (
    <div className="card">
      <table className="table">
        <thead>
          <tr><th>Card</th><th>User</th><th>Status</th><th>Balance</th><th>Limits (d/m)</th><th>Actions</th></tr>
        </thead>
        <tbody>
          {cards.map((c) => (
            <tr key={c.id}>
              <td>{c.label || "Card"} ···· {c.last4}</td>
              <td>@{c.username}</td>
              <td><span className="badge">{c.status}</span></td>
              <td>${c.balance_usd}</td>
              <td className="muted">${c.daily_limit_usd} / ${c.monthly_limit_usd}</td>
              <td>
                <div className="row">
                  {c.status !== "frozen" && c.status !== "terminated" && (
                    <button className="secondary small" onClick={() =>
                      act(() => adminApi(`/api/admin/cards/${c.id}/status`, { method: "POST", body: JSON.stringify({ status: "frozen" }) }).then(load))}>
                      Freeze
                    </button>
                  )}
                  {c.status === "frozen" && (
                    <button className="success small" onClick={() =>
                      act(() => adminApi(`/api/admin/cards/${c.id}/status`, { method: "POST", body: JSON.stringify({ status: "active" }) }).then(load))}>
                      Unfreeze
                    </button>
                  )}
                  {c.status !== "terminated" && (
                    <button className="secondary small" onClick={() => {
                      if (confirm("Terminate this card permanently?"))
                        act(() => adminApi(`/api/admin/cards/${c.id}/status`, { method: "POST", body: JSON.stringify({ status: "terminated" }) }).then(load));
                    }}>
                      Close
                    </button>
                  )}
                </div>
              </td>
            </tr>
          ))}
          {cards.length === 0 && <tr><td colSpan={6} className="muted">No cards issued</td></tr>}
        </tbody>
      </table>
    </div>
  );
}

interface AdminTransfer {
  tx_id: string;
  from_username: string;
  to_username: string;
  asset: string;
  chain: string;
  amount: string;
  memo: string;
  reversed: boolean;
  created_at: string;
}

export function TransfersTab({ act }: { act: (fn: () => Promise<unknown>) => void }) {
  const [transfers, setTransfers] = useState<AdminTransfer[]>([]);
  const load = useCallback(() => {
    adminApi<{ transfers: AdminTransfer[] }>("/api/admin/transfers")
      .then((d) => setTransfers(d.transfers)).catch(() => {});
  }, []);
  useEffect(() => { load(); }, [load]);

  return (
    <div className="card">
      <table className="table">
        <thead>
          <tr><th>When</th><th>From</th><th>To</th><th>Amount</th><th>Memo</th><th>Actions</th></tr>
        </thead>
        <tbody>
          {transfers.map((x) => (
            <tr key={x.tx_id} style={x.reversed ? { opacity: 0.55 } : undefined}>
              <td className="muted">{new Date(x.created_at).toLocaleString()}</td>
              <td>@{x.from_username}</td>
              <td>@{x.to_username}</td>
              <td>{x.amount} {x.asset} <span className="muted">({x.chain})</span></td>
              <td className="muted">{x.memo}</td>
              <td>
                {x.reversed
                  ? <span className="badge">reversed</span>
                  : <button className="secondary small" onClick={() => {
                      if (confirm(`Reverse ${x.amount} ${x.asset} from @${x.from_username} to @${x.to_username}?`))
                        act(() => adminApi(`/api/admin/transfers/${x.tx_id}/reverse`, { method: "POST" }).then(load));
                    }}>
                      Reverse
                    </button>}
              </td>
            </tr>
          ))}
          {transfers.length === 0 && <tr><td colSpan={6} className="muted">No user-to-user transfers yet</td></tr>}
        </tbody>
      </table>
    </div>
  );
}
