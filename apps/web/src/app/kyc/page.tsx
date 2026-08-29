"use client";

import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { api, getAccessToken } from "@/lib/api";
import { useI18n } from "@/lib/i18n";

export default function KYCPage() {
  const { t } = useI18n();
  const router = useRouter();
  const [status, setStatus] = useState("");
  const [form, setForm] = useState({ full_name: "", country: "", doc_type: "passport", doc_number: "" });
  const [msg, setMsg] = useState("");
  const [err, setErr] = useState("");

  useEffect(() => {
    if (!getAccessToken()) {
      router.push("/login");
      return;
    }
    api<{ status: string }>("/api/kyc/status").then((d) => setStatus(d.status)).catch(() => {});
  }, [router]);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setErr(""); setMsg("");
    try {
      const res = await api<{ status: string }>("/api/kyc/submit", {
        method: "POST",
        body: JSON.stringify(form),
      });
      setStatus(res.status);
      setMsg(res.status);
    } catch (e2) {
      setErr(e2 instanceof Error ? e2.message : t("error"));
    }
  };

  return (
    <div className="card" style={{ maxWidth: 480, margin: "24px auto" }}>
      <h2>{t("kyc")}</h2>
      <p>
        {t("kycStatus")}: <span className={`badge ${status === "verified" ? "green" : status === "rejected" ? "red" : "yellow"}`}>{status || "none"}</span>
      </p>
      {status !== "verified" && status !== "pending" && (
        <form onSubmit={submit} className="col">
          <div>
            <label>{t("fullName")}</label>
            <input value={form.full_name} onChange={(e) => setForm({ ...form, full_name: e.target.value })} required />
          </div>
          <div>
            <label>{t("country")} (ISO)</label>
            <input value={form.country} onChange={(e) => setForm({ ...form, country: e.target.value.toUpperCase() })} required maxLength={2} placeholder="US" />
          </div>
          <div>
            <label>Document</label>
            <select value={form.doc_type} onChange={(e) => setForm({ ...form, doc_type: e.target.value })}>
              <option value="passport">Passport</option>
              <option value="national_id">National ID</option>
              <option value="driving_license">Driving license</option>
            </select>
          </div>
          <div>
            <label>{t("docNumber")}</label>
            <input value={form.doc_number} onChange={(e) => setForm({ ...form, doc_number: e.target.value })} required />
          </div>
          {err && <div className="error-text">{err}</div>}
          {msg && <div className="success-text">{msg}</div>}
          <button type="submit">{t("kycSubmit")}</button>
        </form>
      )}
    </div>
  );
}
