"use client";

import { FormEvent, useCallback, useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { adminApi, clearAdminSession, getAdminToken } from "@/lib/api";
import { WithdrawalsTab, RolesTab, RatesTab, DisputesTab, MerchantsTab, CardsTab, TransfersTab, StakingTab, PricesTab } from "@/components/FinanceTabs";
import { SafetyTab, DerivedRatesTab } from "@/components/SafetyTabs";

interface Stats {
  users: number;
  posts: number;
  open_reports: number;
  pending_kyc: number;
  active_ads: number;
}

interface AdminUser {
  id: string;
  username: string;
  display_name: string;
  email: string | null;
  phone: string | null;
  status: string;
  kyc_status: string;
  created_at: string;
}

interface Report {
  id: string;
  reporter: string;
  target_type: string;
  target_id: string;
  reason: string;
  created_at: string;
}

interface KYCItem {
  id: string;
  user_id: string;
  username: string;
  display_name: string;
  created_at: string;
  auto_score: number | null;
  auto_checks: { score?: number; checks?: Record<string, boolean | string | object> } | null;
}

interface AdItem {
  id: string;
  name: string;
  advertiser: string;
  objective: string;
  total_budget: string;
  currency: string;
}

interface PlatformToken {
  id: string;
  symbol: string;
  name: string;
  chain: string;
  contract_address: string | null;
  decimals: number;
  is_native: boolean;
  enabled: boolean;
  deposit_enabled: boolean;
  withdraw_enabled: boolean;
  p2p_enabled: boolean;
  convert_enabled: boolean;
  min_withdraw: string;
  withdraw_fee: string;
  created_at: string;
}

type Tab = "stats" | "users" | "reports" | "kyc" | "ads" | "tokens" | "withdrawals" | "roles" | "rates" | "disputes" | "merchants" | "cards" | "transfers" | "staking" | "prices" | "safety" | "derived-rates" | "moments";

export default function DashboardPage() {
  const router = useRouter();
  const [stats, setStats] = useState<Stats | null>(null);
  const [users, setUsers] = useState<AdminUser[]>([]);
  const [reports, setReports] = useState<Report[]>([]);
  const [kycQueue, setKycQueue] = useState<KYCItem[]>([]);
  const [adQueue, setAdQueue] = useState<AdItem[]>([]);
  const [tokens, setTokens] = useState<PlatformToken[]>([]);
  const [userQuery, setUserQuery] = useState("");
  const [tab, setTab] = useState<Tab>("stats");
  const [error, setError] = useState("");

  const logout = () => {
    clearAdminSession();
    router.push("/");
  };

  const load = useCallback(async () => {
    try {
      const s = await adminApi<Stats>("/api/admin/stats");
      setStats(s);
      const [u, r, k, a, tk] = await Promise.allSettled([
        adminApi<{ users: AdminUser[] }>("/api/admin/users"),
        adminApi<{ reports: Report[] }>("/api/admin/reports"),
        adminApi<{ submissions: KYCItem[] }>("/api/admin/kyc"),
        adminApi<{ campaigns: AdItem[] }>("/api/admin/ads"),
        adminApi<{ tokens: PlatformToken[] }>("/api/admin/wallet/tokens"),
      ]);
      if (u.status === "fulfilled") setUsers(u.value.users);
      if (r.status === "fulfilled") setReports(r.value.reports);
      if (k.status === "fulfilled") setKycQueue(k.value.submissions);
      if (a.status === "fulfilled") setAdQueue(a.value.campaigns);
      if (tk.status === "fulfilled") setTokens(tk.value.tokens);
    } catch (e) {
      const err = e as Error & { status?: number };
      if (err.status === 401) logout();
      else setError(err.message);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  useEffect(() => {
    if (!getAdminToken()) {
      router.push("/");
      return;
    }
    load();
  }, [load, router]);

  const act = (fn: () => Promise<unknown>) =>
    fn().then(load).catch((e) => setError((e as Error).message));

  const searchUsers = () =>
    adminApi<{ users: AdminUser[] }>(`/api/admin/users?q=${encodeURIComponent(userQuery)}`)
      .then((d) => setUsers(d.users))
      .catch(() => {});

  return (
    <>
      <div className="row" style={{ marginBottom: 12, flexWrap: "wrap" }}>
        {(["stats", "users", "reports", "kyc", "ads", "tokens", "withdrawals", "roles", "rates", "disputes", "merchants", "cards", "transfers", "staking", "prices", "safety", "derived-rates", "moments"] as const).map((k) => (
          <button key={k} className={tab === k ? "small" : "secondary small"} onClick={() => setTab(k)}>
            {k}
          </button>
        ))}
        <div className="spacer" />
        <button className="secondary small" onClick={logout}>Sign out</button>
      </div>
      {error && <div className="error-text" style={{ marginBottom: 8 }}>{error}</div>}

      {tab === "stats" && stats && (
        <div className="grid2">
          {Object.entries(stats).map(([k, v]) => (
            <div key={k} className="card" style={{ textAlign: "center" }}>
              <div style={{ fontSize: 32, fontWeight: 800 }}>{v}</div>
              <div className="muted">{k.replace(/_/g, " ")}</div>
            </div>
          ))}
        </div>
      )}

      {tab === "users" && (
        <div className="card">
          <div className="row" style={{ marginBottom: 10 }}>
            <input placeholder="Search users" value={userQuery} onChange={(e) => setUserQuery(e.target.value)} />
            <button className="small" onClick={searchUsers}>Search</button>
          </div>
          <table className="table">
            <thead>
              <tr><th>username</th><th>email</th><th>status</th><th>KYC</th><th></th></tr>
            </thead>
            <tbody>
              {users.map((u) => (
                <tr key={u.id}>
                  <td>@{u.username}</td>
                  <td className="muted">{u.email ?? u.phone ?? "—"}</td>
                  <td><span className={`badge ${u.status === "active" ? "green" : "red"}`}>{u.status}</span></td>
                  <td>{u.kyc_status}</td>
                  <td>
                    {u.status === "active" ? (
                      <button className="danger small" onClick={() => act(() => adminApi(`/api/admin/users/${u.id}/status`, { method: "POST", body: JSON.stringify({ status: "suspended" }) }))}>
                        Suspend
                      </button>
                    ) : (
                      <button className="success small" onClick={() => act(() => adminApi(`/api/admin/users/${u.id}/status`, { method: "POST", body: JSON.stringify({ status: "active" }) }))}>
                        Activate
                      </button>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {tab === "reports" && (
        <div className="card">
          <table className="table">
            <thead>
              <tr><th>reporter</th><th>type</th><th>reason</th><th></th></tr>
            </thead>
            <tbody>
              {reports.map((r) => (
                <tr key={r.id}>
                  <td>@{r.reporter}</td>
                  <td>{r.target_type}</td>
                  <td className="muted">{r.reason}</td>
                  <td>
                    <div className="row">
                      <button className="success small" onClick={() => act(() => adminApi(`/api/admin/reports/${r.id}/resolve`, { method: "POST", body: JSON.stringify({ resolution: "resolved" }) }))}>
                        Resolve
                      </button>
                      <button className="secondary small" onClick={() => act(() => adminApi(`/api/admin/reports/${r.id}/resolve`, { method: "POST", body: JSON.stringify({ resolution: "dismissed" }) }))}>
                        Dismiss
                      </button>
                    </div>
                  </td>
                </tr>
              ))}
              {reports.length === 0 && <tr><td colSpan={4} className="muted">No open reports</td></tr>}
            </tbody>
          </table>
        </div>
      )}

      {tab === "kyc" && (
        <div className="card">
          <table className="table">
            <thead>
              <tr><th>username</th><th>name</th><th>ML auto-check</th><th></th></tr>
            </thead>
            <tbody>
              {kycQueue.map((k) => (
                <tr key={k.id}>
                  <td>@{k.username}</td>
                  <td>{k.display_name}</td>
                  <td>
                    {k.auto_score != null ? (
                      <span title={JSON.stringify(k.auto_checks?.checks ?? {}, null, 2)}>
                        score {k.auto_score.toFixed(2)}
                        {(() => {
                          const failed = Object.entries(k.auto_checks?.checks ?? {})
                            .filter(([, v]) => v === false).map(([n]) => n);
                          return failed.length > 0 && (
                            <span className="muted" style={{ fontSize: 12 }}> — failed: {failed.join(", ")}</span>
                          );
                        })()}
                      </span>
                    ) : (
                      <span className="muted">no evidence</span>
                    )}
                  </td>
                  <td>
                    <div className="row">
                      <button className="success small" onClick={() => act(() => adminApi(`/api/admin/kyc/${k.id}/review`, { method: "POST", body: JSON.stringify({ decision: "verified" }) }))}>
                        Approve
                      </button>
                      <button className="danger small" onClick={() => act(() => adminApi(`/api/admin/kyc/${k.id}/review`, { method: "POST", body: JSON.stringify({ decision: "rejected" }) }))}>
                        Reject
                      </button>
                    </div>
                  </td>
                </tr>
              ))}
              {kycQueue.length === 0 && <tr><td colSpan={4} className="muted">Queue empty</td></tr>}
            </tbody>
          </table>
        </div>
      )}

      {tab === "ads" && (
        <div className="card">
          <table className="table">
            <thead>
              <tr><th>campaign</th><th>advertiser</th><th>budget</th><th></th></tr>
            </thead>
            <tbody>
              {adQueue.map((a) => (
                <tr key={a.id}>
                  <td>{a.name}</td>
                  <td>@{a.advertiser}</td>
                  <td>{a.total_budget} {a.currency}</td>
                  <td>
                    <div className="row">
                      <button className="success small" onClick={() => act(() => adminApi(`/api/admin/ads/${a.id}/review`, { method: "POST", body: JSON.stringify({ decision: "active" }) }))}>
                        Approve
                      </button>
                      <button className="danger small" onClick={() => act(() => adminApi(`/api/admin/ads/${a.id}/review`, { method: "POST", body: JSON.stringify({ decision: "rejected" }) }))}>
                        Reject
                      </button>
                    </div>
                  </td>
                </tr>
              ))}
              {adQueue.length === 0 && <tr><td colSpan={4} className="muted">No pending campaigns</td></tr>}
            </tbody>
          </table>
        </div>
      )}

      {tab === "tokens" && <TokensTab tokens={tokens} act={act} />}
      {tab === "withdrawals" && <WithdrawalsTab act={act} />}
      {tab === "roles" && <RolesTab act={act} />}
      {tab === "rates" && <RatesTab act={act} />}
      {tab === "disputes" && <DisputesTab act={act} />}
      {tab === "merchants" && <MerchantsTab act={act} />}
      {tab === "cards" && <CardsTab act={act} />}
      {tab === "transfers" && <TransfersTab act={act} />}
      {tab === "staking" && <StakingTab act={act} />}
      {tab === "prices" && <PricesTab act={act} />}
      {tab === "safety" && <SafetyTab />}
      {tab === "derived-rates" && <DerivedRatesTab />}
      {tab === "moments" && <MomentsTab act={act} />}
    </>
  );
}

// X Moments curation: moderators assemble curated collections of posts and
// publish them to the /moments discovery surface.
interface AdminMoment {
  id: string;
  title: string;
  summary: string;
  cover_url: string;
  published_at: string | null;
  created_at: string;
  item_count: number;
}

function MomentsTab({ act }: { act: (fn: () => Promise<unknown>) => void }) {
  const [moments, setMoments] = useState<AdminMoment[]>([]);
  const [title, setTitle] = useState("");
  const [summary, setSummary] = useState("");
  const [cover, setCover] = useState("");
  const [postId, setPostId] = useState<Record<string, string>>({});
  const [formError, setFormError] = useState("");

  const load = useCallback(
    () =>
      adminApi<{ moments: AdminMoment[] }>("/api/admin/moments")
        .then((d) => setMoments(d.moments))
        .catch(() => {}),
    []
  );

  useEffect(() => {
    load();
  }, [load]);

  const create = (e: FormEvent) => {
    e.preventDefault();
    setFormError("");
    act(async () => {
      await adminApi("/api/admin/moments", {
        method: "POST",
        body: JSON.stringify({ title, summary, cover_url: cover }),
      });
      setTitle("");
      setSummary("");
      setCover("");
      load();
    });
  };

  return (
    <div className="card">
      <h3 style={{ marginTop: 0 }}>Moments</h3>
      <form onSubmit={create} className="row" style={{ flexWrap: "wrap", gap: 6, marginBottom: 12 }}>
        <input placeholder="Title" value={title} maxLength={200}
          onChange={(e) => setTitle(e.target.value)} style={{ minWidth: 220 }} />
        <input placeholder="Summary (optional)" value={summary} maxLength={2000}
          onChange={(e) => setSummary(e.target.value)} style={{ minWidth: 220 }} />
        <input placeholder="Cover URL (optional)" value={cover}
          onChange={(e) => setCover(e.target.value)} style={{ minWidth: 220 }} />
        <button type="submit">Create draft</button>
      </form>
      {formError && <div className="error-text">{formError}</div>}
      <table className="table">
        <thead>
          <tr><th>title</th><th>items</th><th>status</th><th>add post</th><th></th></tr>
        </thead>
        <tbody>
          {moments.map((m) => (
            <tr key={m.id}>
              <td>{m.title}</td>
              <td>{m.item_count}</td>
              <td>{m.published_at ? "published" : "draft"}</td>
              <td>
                <div className="row" style={{ gap: 4 }}>
                  <input placeholder="post id" value={postId[m.id] ?? ""} style={{ width: 260 }}
                    onChange={(e) => setPostId({ ...postId, [m.id]: e.target.value })} />
                  <button className="small" onClick={() =>
                    act(async () => {
                      await adminApi(`/api/admin/moments/${m.id}/items`, {
                        method: "POST",
                        body: JSON.stringify({ post_id: (postId[m.id] ?? "").trim() }),
                      });
                      load();
                    })
                  }>Add</button>
                </div>
              </td>
              <td>
                <div className="row">
                  {!m.published_at ? (
                    <button className="success small" onClick={() =>
                      act(async () => {
                        await adminApi(`/api/admin/moments/${m.id}/publish`, { method: "POST", body: "{}" });
                        load();
                      })
                    }>Publish</button>
                  ) : (
                    <button className="secondary small" onClick={() =>
                      act(async () => {
                        await adminApi(`/api/admin/moments/${m.id}/publish`, { method: "POST", body: JSON.stringify({ publish: false }) });
                        load();
                      })
                    }>Unpublish</button>
                  )}
                  <button className="danger small" onClick={() =>
                    act(async () => {
                      await adminApi(`/api/admin/moments/${m.id}`, { method: "DELETE" });
                      load();
                    })
                  }>Delete</button>
                </div>
              </td>
            </tr>
          ))}
          {moments.length === 0 && <tr><td colSpan={5} className="muted">No moments yet</td></tr>}
        </tbody>
      </table>
    </div>
  );
}

// Wallet platform-token management (superadmin/finance): which tokens the
// built-in multichain wallet offers to users.
function TokensTab({ tokens, act }: { tokens: PlatformToken[]; act: (fn: () => Promise<unknown>) => void }) {
  const [symbol, setSymbol] = useState("");
  const [name, setName] = useState("");
  const [chain, setChain] = useState("");
  const [contract, setContract] = useState("");
  const [decimals, setDecimals] = useState("18");
  const [isNative, setIsNative] = useState(false);
  const [formError, setFormError] = useState("");

  const add = (e: FormEvent) => {
    e.preventDefault();
    setFormError("");
    act(async () => {
      await adminApi("/api/admin/wallet/tokens", {
        method: "POST",
        body: JSON.stringify({
          symbol, name, chain,
          contract_address: contract,
          decimals: parseInt(decimals, 10),
          is_native: isNative,
        }),
      });
      setSymbol(""); setName(""); setChain(""); setContract("");
    });
  };

  return (
    <>
      <div className="card" style={{ marginBottom: 12 }}>
        <h4 style={{ marginTop: 0 }}>Add platform token</h4>
        <form onSubmit={add}>
          <div className="row" style={{ flexWrap: "wrap", gap: 8 }}>
            <input placeholder="Symbol (e.g. CHAT)" value={symbol} onChange={(e) => setSymbol(e.target.value)} required style={{ width: 140 }} />
            <input placeholder="Name" value={name} onChange={(e) => setName(e.target.value)} required />
            <input placeholder="Chain (e.g. ethereum)" value={chain} onChange={(e) => setChain(e.target.value)} required style={{ width: 160 }} />
            <input placeholder="Decimals" value={decimals} onChange={(e) => setDecimals(e.target.value)} style={{ width: 90 }} />
          </div>
          <div className="row" style={{ marginTop: 8, flexWrap: "wrap", gap: 8 }}>
            <label className="muted">
              <input type="checkbox" checked={isNative} onChange={(e) => setIsNative(e.target.checked)} style={{ width: "auto" }} /> native coin
            </label>
            {!isNative && (
              <input placeholder="Contract address" value={contract} onChange={(e) => setContract(e.target.value)} required style={{ flex: 1 }} />
            )}
            <button className="small" type="submit">Add / update</button>
          </div>
        </form>
        {formError && <div className="error-text">{formError}</div>}
      </div>
      <div className="card">
        <table className="table">
          <thead>
            <tr><th>symbol</th><th>name</th><th>chain</th><th>contract</th><th>rails</th><th>status</th><th></th></tr>
          </thead>
          <tbody>
            {tokens.map((t) => (
              <tr key={t.id}>
                <td><strong>{t.symbol}</strong></td>
                <td>{t.name}</td>
                <td>{t.chain}</td>
                <td className="muted" style={{ maxWidth: 180, overflow: "hidden", textOverflow: "ellipsis" }}>
                  {t.is_native ? "native" : t.contract_address ?? "—"}
                </td>
                <td>
                  <div className="row" style={{ gap: 4 }}>
                    {([
                      ["deposit_enabled", "dep", t.deposit_enabled],
                      ["withdraw_enabled", "wd", t.withdraw_enabled],
                      ["p2p_enabled", "p2p", t.p2p_enabled],
                      ["convert_enabled", "conv", t.convert_enabled],
                    ] as const).map(([field, label, on]) => (
                      <button
                        key={field}
                        className={on ? "success small" : "secondary small"}
                        title={field}
                        onClick={() =>
                          act(() =>
                            adminApi(`/api/admin/wallet/tokens/${t.id}/features`, {
                              method: "POST",
                              body: JSON.stringify({ [field]: !on }),
                            }),
                          )
                        }
                      >
                        {label}
                      </button>
                    ))}
                  </div>
                </td>
                <td><span className={`badge ${t.enabled ? "green" : "red"}`}>{t.enabled ? "enabled" : "disabled"}</span></td>
                <td>
                  <div className="row">
                    <button
                      className="secondary small"
                      onClick={() => act(() => adminApi(`/api/admin/wallet/tokens/${t.id}/status`, { method: "POST", body: JSON.stringify({ enabled: !t.enabled }) }))}
                    >
                      {t.enabled ? "Disable" : "Enable"}
                    </button>
                    <button
                      className="danger small"
                      onClick={() => act(() => adminApi(`/api/admin/wallet/tokens/${t.id}`, { method: "DELETE" }))}
                    >
                      Delete
                    </button>
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </>
  );
}
