"use client";

import { useCallback, useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { api, getAccessToken } from "@/lib/api";
import { useI18n } from "@/lib/i18n";
import type { Campaign } from "@/lib/types";

export default function AdsPage() {
  const { t } = useI18n();
  const router = useRouter();
  const [campaigns, setCampaigns] = useState<Campaign[]>([]);
  const [form, setForm] = useState({ name: "", objective: "reach", total_budget: "", daily_budget: "", countries: "" });
  const [creative, setCreative] = useState({ campaignId: "", title: "", body: "", cta_url: "" });
  const [fundAmount, setFundAmount] = useState<Record<string, string>>({});
  const [err, setErr] = useState("");
  const [msg, setMsg] = useState("");

  const load = useCallback(() => {
    api<{ campaigns: Campaign[] }>("/api/ads/campaigns")
      .then((d) => setCampaigns(d.campaigns))
      .catch(() => {});
  }, []);

  useEffect(() => {
    if (!getAccessToken()) {
      router.push("/login");
      return;
    }
    load();
  }, [load, router]);

  const run = async (fn: () => Promise<unknown>) => {
    setErr(""); setMsg("");
    try {
      await fn();
      load();
    } catch (e) {
      setErr(e instanceof Error ? e.message : t("error"));
    }
  };

  const createCampaign = () =>
    run(async () => {
      await api("/api/ads/campaigns", {
        method: "POST",
        body: JSON.stringify({
          name: form.name,
          objective: form.objective,
          total_budget: form.total_budget || "0",
          daily_budget: form.daily_budget || "0",
          target_countries: form.countries
            ? form.countries.split(",").map((c) => c.trim().toUpperCase()).filter(Boolean)
            : [],
        }),
      });
      setMsg("created");
      setForm({ name: "", objective: "reach", total_budget: "", daily_budget: "", countries: "" });
    });

  return (
    <>
      <div className="card">
        <h3>{t("newCampaign")}</h3>
        <div className="grid2">
          <div>
            <label>{t("campaignName")}</label>
            <input value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} />
          </div>
          <div>
            <label>Objective</label>
            <select value={form.objective} onChange={(e) => setForm({ ...form, objective: e.target.value })}>
              <option value="reach">Reach</option>
              <option value="traffic">Traffic</option>
              <option value="conversions">Conversions</option>
            </select>
          </div>
          <div>
            <label>{t("budget")} (total USD)</label>
            <input value={form.total_budget} onChange={(e) => setForm({ ...form, total_budget: e.target.value })} inputMode="decimal" />
          </div>
          <div>
            <label>{t("budget")} (daily USD)</label>
            <input value={form.daily_budget} onChange={(e) => setForm({ ...form, daily_budget: e.target.value })} inputMode="decimal" />
          </div>
          <div style={{ gridColumn: "1 / -1" }}>
            <label>{t("targetCountries")} (ISO, comma separated; empty = global)</label>
            <input value={form.countries} onChange={(e) => setForm({ ...form, countries: e.target.value })} placeholder="US, BR, IN" />
          </div>
        </div>
        <div className="row" style={{ marginTop: 10 }}>
          <button onClick={createCampaign} disabled={!form.name.trim()}>{t("newCampaign")}</button>
          {msg && <span className="success-text">{msg}</span>}
          {err && <span className="error-text">{err}</span>}
        </div>
      </div>

      <div className="card">
        <h3>{t("campaigns")}</h3>
        <table className="table">
          <thead>
            <tr>
              <th>{t("campaignName")}</th><th>status</th><th>{t("budget")}</th><th>spent</th><th></th>
            </tr>
          </thead>
          <tbody>
            {campaigns.map((c) => (
              <tr key={c.id}>
                <td>{c.name}</td>
                <td><span className={`badge ${c.status === "active" ? "green" : c.status === "rejected" ? "red" : "yellow"}`}>{c.status}</span></td>
                <td>{c.total_budget} {c.currency}</td>
                <td>{c.spent}</td>
                <td>
                  <div className="row">
                    {(c.status === "draft" || c.status === "rejected") && (
                      <button className="small" onClick={() => run(() => api(`/api/ads/campaigns/${c.id}/submit`, { method: "POST" }))}>
                        {t("submitForReview")}
                      </button>
                    )}
                    <input
                      style={{ width: 80 }}
                      placeholder="fund $"
                      value={fundAmount[c.id] ?? ""}
                      onChange={(e) => setFundAmount({ ...fundAmount, [c.id]: e.target.value })}
                    />
                    <button
                      className="secondary small"
                      onClick={() => run(() => api(`/api/ads/campaigns/${c.id}/fund`, {
                        method: "POST",
                        body: JSON.stringify({ amount: fundAmount[c.id] ?? "0" }),
                      }))}
                    >
                      {t("fund")}
                    </button>
                  </div>
                </td>
              </tr>
            ))}
            {campaigns.length === 0 && (
              <tr><td colSpan={5} className="muted">{t("noResults")}</td></tr>
            )}
          </tbody>
        </table>
      </div>

      <div className="card">
        <h3>{t("addCreative")}</h3>
        <div className="grid2">
          <div>
            <label>{t("campaigns")}</label>
            <select value={creative.campaignId} onChange={(e) => setCreative({ ...creative, campaignId: e.target.value })}>
              <option value="">—</option>
              {campaigns.map((c) => <option key={c.id} value={c.id}>{c.name}</option>)}
            </select>
          </div>
          <div>
            <label>Title</label>
            <input value={creative.title} onChange={(e) => setCreative({ ...creative, title: e.target.value })} />
          </div>
          <div>
            <label>Body</label>
            <input value={creative.body} onChange={(e) => setCreative({ ...creative, body: e.target.value })} />
          </div>
          <div>
            <label>CTA URL</label>
            <input value={creative.cta_url} onChange={(e) => setCreative({ ...creative, cta_url: e.target.value })} placeholder="https://…" />
          </div>
        </div>
        <button
          style={{ marginTop: 10 }}
          disabled={!creative.campaignId || !creative.title.trim()}
          onClick={() => run(() => api(`/api/ads/campaigns/${creative.campaignId}/creatives`, {
            method: "POST",
            body: JSON.stringify({ title: creative.title, body: creative.body, cta_url: creative.cta_url }),
          }))}
        >
          {t("addCreative")}
        </button>
      </div>
    </>
  );
}
