"use client";

import { useCallback, useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { api, getAccessToken } from "@/lib/api";
import { useI18n } from "@/lib/i18n";

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
}

interface AdItem {
  id: string;
  name: string;
  advertiser: string;
  objective: string;
  total_budget: string;
  currency: string;
}

export default function AdminPage() {
  const { t } = useI18n();
  const router = useRouter();
  const [forbidden, setForbidden] = useState(false);
  const [stats, setStats] = useState<Stats | null>(null);
  const [users, setUsers] = useState<AdminUser[]>([]);
  const [reports, setReports] = useState<Report[]>([]);
  const [kycQueue, setKycQueue] = useState<KYCItem[]>([]);
  const [adQueue, setAdQueue] = useState<AdItem[]>([]);
  const [userQuery, setUserQuery] = useState("");
  const [tab, setTab] = useState<"stats" | "users" | "reports" | "kyc" | "ads">("stats");

  const load = useCallback(async () => {
    try {
      const s = await api<Stats>("/api/admin/stats");
      setStats(s);
      const [u, r, k, a] = await Promise.allSettled([
        api<{ users: AdminUser[] }>("/api/admin/users"),
        api<{ reports: Report[] }>("/api/admin/reports"),
        api<{ submissions: KYCItem[] }>("/api/admin/kyc"),
        api<{ campaigns: AdItem[] }>("/api/admin/ads"),
      ]);
      if (u.status === "fulfilled") setUsers(u.value.users);
      if (r.status === "fulfilled") setReports(r.value.reports);
      if (k.status === "fulfilled") setKycQueue(k.value.submissions);
      if (a.status === "fulfilled") setAdQueue(a.value.campaigns);
    } catch {
      setForbidden(true);
    }
  }, []);

  useEffect(() => {
    if (!getAccessToken()) {
      router.push("/login");
      return;
    }
    load();
  }, [load, router]);

  if (forbidden) {
    return <div className="card"><p className="error-text">403 — admin role required</p></div>;
  }

  const act = (fn: () => Promise<unknown>) => fn().then(load).catch(() => {});

  const searchUsers = () =>
    api<{ users: AdminUser[] }>(`/api/admin/users?q=${encodeURIComponent(userQuery)}`)
      .then((d) => setUsers(d.users))
      .catch(() => {});

  return (
    <>
      <div className="row" style={{ marginBottom: 12, flexWrap: "wrap" }}>
        {(["stats", "users", "reports", "kyc", "ads"] as const).map((k) => (
          <button key={k} className={tab === k ? "small" : "secondary small"} onClick={() => setTab(k)}>
            {t(k === "kyc" ? "kycQueue" : k === "ads" ? "adReview" : k)}
          </button>
        ))}
      </div>

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
            <input placeholder={t("searchUsers")} value={userQuery} onChange={(e) => setUserQuery(e.target.value)} />
            <button className="small" onClick={searchUsers}>{t("searchUsers")}</button>
          </div>
          <table className="table">
            <thead>
              <tr><th>{t("username")}</th><th>{t("email")}</th><th>status</th><th>KYC</th><th></th></tr>
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
                      <button className="danger small" onClick={() => act(() => api(`/api/admin/users/${u.id}/status`, { method: "POST", body: JSON.stringify({ status: "suspended" }) }))}>
                        {t("suspend")}
                      </button>
                    ) : (
                      <button className="success small" onClick={() => act(() => api(`/api/admin/users/${u.id}/status`, { method: "POST", body: JSON.stringify({ status: "active" }) }))}>
                        {t("activate")}
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
              <tr><th>{t("reports")}</th><th>type</th><th>reason</th><th></th></tr>
            </thead>
            <tbody>
              {reports.map((r) => (
                <tr key={r.id}>
                  <td>@{r.reporter}</td>
                  <td>{r.target_type}</td>
                  <td className="muted">{r.reason}</td>
                  <td>
                    <div className="row">
                      <button className="success small" onClick={() => act(() => api(`/api/admin/reports/${r.id}/resolve`, { method: "POST", body: JSON.stringify({ resolution: "resolved" }) }))}>
                        {t("resolve")}
                      </button>
                      <button className="secondary small" onClick={() => act(() => api(`/api/admin/reports/${r.id}/resolve`, { method: "POST", body: JSON.stringify({ resolution: "dismissed" }) }))}>
                        {t("dismiss")}
                      </button>
                    </div>
                  </td>
                </tr>
              ))}
              {reports.length === 0 && <tr><td colSpan={4} className="muted">{t("noResults")}</td></tr>}
            </tbody>
          </table>
        </div>
      )}

      {tab === "kyc" && (
        <div className="card">
          <table className="table">
            <thead>
              <tr><th>{t("username")}</th><th>{t("displayName")}</th><th></th></tr>
            </thead>
            <tbody>
              {kycQueue.map((k) => (
                <tr key={k.id}>
                  <td>@{k.username}</td>
                  <td>{k.display_name}</td>
                  <td>
                    <div className="row">
                      <button className="success small" onClick={() => act(() => api(`/api/admin/kyc/${k.id}/review`, { method: "POST", body: JSON.stringify({ decision: "verified" }) }))}>
                        {t("approve")}
                      </button>
                      <button className="danger small" onClick={() => act(() => api(`/api/admin/kyc/${k.id}/review`, { method: "POST", body: JSON.stringify({ decision: "rejected" }) }))}>
                        {t("reject")}
                      </button>
                    </div>
                  </td>
                </tr>
              ))}
              {kycQueue.length === 0 && <tr><td colSpan={3} className="muted">{t("noResults")}</td></tr>}
            </tbody>
          </table>
        </div>
      )}

      {tab === "ads" && (
        <div className="card">
          <table className="table">
            <thead>
              <tr><th>{t("campaignName")}</th><th>advertiser</th><th>{t("budget")}</th><th></th></tr>
            </thead>
            <tbody>
              {adQueue.map((a) => (
                <tr key={a.id}>
                  <td>{a.name}</td>
                  <td>@{a.advertiser}</td>
                  <td>{a.total_budget} {a.currency}</td>
                  <td>
                    <div className="row">
                      <button className="success small" onClick={() => act(() => api(`/api/admin/ads/${a.id}/review`, { method: "POST", body: JSON.stringify({ decision: "active" }) }))}>
                        {t("approve")}
                      </button>
                      <button className="danger small" onClick={() => act(() => api(`/api/admin/ads/${a.id}/review`, { method: "POST", body: JSON.stringify({ decision: "rejected" }) }))}>
                        {t("reject")}
                      </button>
                    </div>
                  </td>
                </tr>
              ))}
              {adQueue.length === 0 && <tr><td colSpan={4} className="muted">{t("noResults")}</td></tr>}
            </tbody>
          </table>
        </div>
      )}
    </>
  );
}
