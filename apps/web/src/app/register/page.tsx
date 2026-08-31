"use client";

import { useState } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { api, saveTokens, Tokens } from "@/lib/api";
import { useI18n } from "@/lib/i18n";
import CountryPicker from "@/components/CountryPicker";
import GoogleSignIn from "@/components/GoogleSignIn";

export default function RegisterPage() {
  const { t } = useI18n();
  const router = useRouter();
  const [form, setForm] = useState({
    username: "",
    display_name: "",
    email: "",
    password: "",
  });
  const [dial, setDial] = useState("+1");
  const [countryISO, setCountryISO] = useState("US");
  const [phoneLocal, setPhoneLocal] = useState("");
  const [usePhone, setUsePhone] = useState(false);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  const set = (k: keyof typeof form) => (e: React.ChangeEvent<HTMLInputElement>) =>
    setForm({ ...form, [k]: e.target.value });

  const submit = async (ev: React.FormEvent) => {
    ev.preventDefault();
    setBusy(true);
    setError("");
    try {
      const phone = usePhone && phoneLocal ? `${dial}${phoneLocal.replace(/\D/g, "")}` : "";
      const tokens = await api<Tokens>(
        "/api/auth/register",
        {
          method: "POST",
          body: JSON.stringify({
            username: form.username,
            display_name: form.display_name,
            email: usePhone ? "" : form.email,
            phone,
            phone_country: countryISO,
            password: form.password,
          }),
        },
        false
      );
      saveTokens(tokens, form.username);
      router.push("/");
      router.refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : t("error"));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="card" style={{ maxWidth: 460, margin: "40px auto" }}>
      <h2>{t("register")}</h2>
      <form onSubmit={submit} className="col">
        <div>
          <label>{t("username")}</label>
          <input value={form.username} onChange={set("username")} required pattern="[a-zA-Z0-9_]{3,30}" />
        </div>
        <div>
          <label>{t("displayName")}</label>
          <input value={form.display_name} onChange={set("display_name")} />
        </div>
        <div className="row">
          <label style={{ margin: 0 }}>
            <input
              type="checkbox"
              style={{ width: "auto", marginInlineEnd: 6 }}
              checked={usePhone}
              onChange={(e) => setUsePhone(e.target.checked)}
            />
            {t("phone")}
          </label>
        </div>
        {usePhone ? (
          <>
            <CountryPicker value={dial} onChange={(d, iso) => { setDial(d); setCountryISO(iso); }} />
            <div>
              <label>{t("phone")}</label>
              <div className="row">
                <span className="badge">{dial}</span>
                <input
                  value={phoneLocal}
                  onChange={(e) => setPhoneLocal(e.target.value)}
                  placeholder="4155552671"
                  inputMode="tel"
                />
              </div>
            </div>
          </>
        ) : (
          <div>
            <label>{t("email")}</label>
            <input type="email" value={form.email} onChange={set("email")} required={!usePhone} />
          </div>
        )}
        <div>
          <label>{t("password")}</label>
          <input type="password" value={form.password} onChange={set("password")} required minLength={8} />
        </div>
        {error && <div className="error-text">{error}</div>}
        <button type="submit" disabled={busy}>{busy ? t("loading") : t("register")}</button>
      </form>
      <div style={{ marginTop: 12 }}>
        <GoogleSignIn />
      </div>
      <p className="muted">
        <Link href="/login">{t("login")}</Link>
      </p>
    </div>
  );
}
