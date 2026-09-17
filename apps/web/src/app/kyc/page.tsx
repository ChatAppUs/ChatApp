"use client";

import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { api, getAccessToken, uploadMedia } from "@/lib/api";
import { useI18n } from "@/lib/i18n";

// Identity spec §7 — KYC verification: progress indicator, document front AND
// back (§7.2), selfie with document, and random instruction-based live
// verification (§7.3). The ML pipeline scores everything server-side; admins
// review whatever does not auto-verify.
export default function KYCPage() {
  const { t } = useI18n();
  const router = useRouter();
  const [status, setStatus] = useState("");
  const [form, setForm] = useState({ full_name: "", country: "", doc_type: "passport", doc_number: "" });
  const [docFrontURL, setDocFrontURL] = useState("");
  const [docBackURL, setDocBackURL] = useState("");
  const [selfieURL, setSelfieURL] = useState("");
  const [liveness, setLiveness] = useState<{ id: string; instruction: string } | null>(null);
  const [busy, setBusy] = useState(false);
  const [msg, setMsg] = useState("");
  const [err, setErr] = useState("");

  useEffect(() => {
    if (!getAccessToken()) {
      router.push("/login");
      return;
    }
    api<{ status: string }>("/api/kyc/status").then((d) => setStatus(d.status)).catch(() => {});
  }, [router]);

  const requestLiveness = async () => {
    setErr("");
    try {
      const res = await api<{ challenge_id: string; instruction: string; expires_in_seconds: number }>(
        "/api/kyc/liveness/challenge",
        { method: "POST" }
      );
      setLiveness({
        id: res.challenge_id,
        instruction: res.instruction.replace(/_/g, " "),
      });
      setSelfieURL("");
    } catch (e) {
      setErr(e instanceof Error ? e.message : "could not start live verification");
    }
  };

  const uploadTo = async (file: File, setter: (url: string) => void) => {
    setErr("");
    try {
      setter(await uploadMedia(file));
    } catch (e) {
      setErr(e instanceof Error ? e.message : "upload failed");
    }
  };

  const FileField = ({
    label,
    url,
    onFile,
  }: {
    label: string;
    url: string;
    onFile: (f: File) => void;
  }) => (
    <label className="secondary" style={{ cursor: "pointer", display: "block" }}>
      {url ? `✓ ${label} attached` : `Upload ${label}`}
      <input
        type="file"
        accept="image/*"
        style={{ display: "none" }}
        onChange={(e) => {
          const f = e.target.files?.[0];
          if (f) onFile(f);
        }}
      />
    </label>
  );

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setErr("");
    setMsg("");
    if (!docFrontURL || !docBackURL || !selfieURL || !liveness) {
      setErr("Attach both document sides and complete the live check first");
      return;
    }
    setBusy(true);
    try {
      const res = await api<{ status: string }>("/api/kyc/submit", {
        method: "POST",
        body: JSON.stringify({
          ...form,
          doc_image_url: docFrontURL,
          doc_back_url: docBackURL,
          selfie_url: selfieURL,
          liveness_challenge_id: liveness.id,
        }),
      });
      setStatus(res.status);
      setMsg(
        res.status === "verified"
          ? "Verified automatically — you're good to go"
          : "Submitted — we'll notify you once review completes"
      );
    } catch (e2) {
      setErr(e2 instanceof Error ? e2.message : t("error"));
    } finally {
      setBusy(false);
    }
  };

  const steps = ["Details", "Documents", "Live check", "Review"];
  const stepIndex = status === "verified" ? 4 : status === "pending" ? 3 : form.full_name ? 1 : 0;

  return (
    <div className="card" style={{ maxWidth: 480, margin: "24px auto" }}>
      <h2>{t("kyc")}</h2>
      <p>
        {t("kycStatus")}:{" "}
        <span className={`badge ${status === "verified" ? "green" : status === "rejected" ? "red" : "yellow"}`}>
          {status || "none"}
        </span>
      </p>

      <ol style={{ display: "flex", gap: 8, listStyle: "none", padding: 0, fontSize: 13 }}>
        {steps.map((s, i) => (
          <li key={s} style={{ color: i <= stepIndex ? "var(--accent, #6366f1)" : "var(--muted, #888)" }}>
            {i + 1}. {s}
          </li>
        ))}
      </ol>

      {status !== "verified" && status !== "pending" && (
        <form onSubmit={submit} className="col">
          <div>
            <label>{t("fullName")}</label>
            <input value={form.full_name} onChange={(e) => setForm({ ...form, full_name: e.target.value })} required />
          </div>
          <div>
            <label>{t("country")} (ISO)</label>
            <input
              value={form.country}
              onChange={(e) => setForm({ ...form, country: e.target.value.toUpperCase() })}
              required
              maxLength={2}
              placeholder="US"
            />
          </div>
          <div>
            <label>Document</label>
            <select value={form.doc_type} onChange={(e) => setForm({ ...form, doc_type: e.target.value })}>
              <option value="passport">Passport</option>
              <option value="national_id">National ID</option>
              <option value="driving_license">Driving licence</option>
            </select>
          </div>
          <div>
            <label>{t("docNumber")}</label>
            <input value={form.doc_number} onChange={(e) => setForm({ ...form, doc_number: e.target.value })} required />
          </div>

          <FileField label="document FRONT" url={docFrontURL} onFile={(f) => uploadTo(f, setDocFrontURL)} />
          <FileField label="document BACK" url={docBackURL} onFile={(f) => uploadTo(f, setDocBackURL)} />

          <div className="col" style={{ gap: 8 }}>
            {liveness ? (
              <>
                <p className="badge yellow">Live instruction: {liveness.instruction}</p>
                <FileField
                  label={`selfie ("${liveness.instruction}")`}
                  url={selfieURL}
                  onFile={(f) => uploadTo(f, setSelfieURL)}
                />
              </>
            ) : (
              <button type="button" className="secondary" onClick={requestLiveness}>
                Start live verification
              </button>
            )}
          </div>

          {err && <div className="error-text">{err}</div>}
          {msg && <div className="success-text">{msg}</div>}
          <button type="submit" disabled={busy || !docFrontURL || !docBackURL || !selfieURL || !liveness}>
            {busy ? t("loading") : t("kycSubmit")}
          </button>
        </form>
      )}
    </div>
  );
}
