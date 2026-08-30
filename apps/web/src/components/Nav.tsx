"use client";

import Link from "next/link";
import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { useI18n, LOCALES, LocaleCode } from "@/lib/i18n";
import { clearTokens, getAccessToken } from "@/lib/api";

export default function Nav() {
  const { t, locale, setLocale } = useI18n();
  const router = useRouter();
  const [authed, setAuthed] = useState(false);

  useEffect(() => {
    setAuthed(!!getAccessToken());
  }, []);

  const logout = () => {
    clearTokens();
    setAuthed(false);
    router.push("/login");
  };

  return (
    <nav className="topnav">
      <Link href="/" className="brand">{t("appName")}</Link>
      {authed && (
        <>
          <Link className="navlink" href="/">{t("feed")}</Link>
          <Link className="navlink" href="/fyp">{t("forYou")}</Link>
          <Link className="navlink" href="/reels">{t("reels")}</Link>
          <Link className="navlink" href="/chat">{t("chat")}</Link>
          <Link className="navlink" href="/notifications">🔔</Link>
          <Link className="navlink" href="/channels">📢</Link>
          <Link className="navlink" href="/groups">{t("groups")}</Link>
          <Link className="navlink" href="/pages">{t("pages")}</Link>
          <Link className="navlink" href="/trending">#</Link>
          <Link className="navlink" href="/bookmarks">★</Link>
          <Link className="navlink" href="/creator">{t("creator")}</Link>
          <Link className="navlink" href="/monetize">{t("monetize")}</Link>
          <Link className="navlink" href="/bots">{t("bots")}</Link>
          <Link className="navlink" href="/privacy">{t("privacy")}</Link>
          <Link className="navlink" href="/wallet">{t("wallet")}</Link>
          <Link className="navlink" href="/convert">{t("convert")}</Link>
          <Link className="navlink" href="/p2p">{t("p2p")}</Link>
          <Link className="navlink" href="/ads">{t("ads")}</Link>
          <Link className="navlink" href="/scan">▦</Link>
          <Link className="navlink" href="/settings">⚙</Link>
        </>
      )}
      <div className="spacer" />
      <button
        className="secondary"
        aria-label="toggle theme"
        onClick={() => {
          const next =
            document.documentElement.dataset.theme === "light" ? "dark" : "light";
          document.documentElement.dataset.theme = next;
          try { localStorage.setItem("chatapp.theme", next); } catch { /* ignore */ }
        }}
      >
        ◐
      </button>
      <select
        aria-label="language"
        value={locale}
        onChange={(e) => setLocale(e.target.value as LocaleCode)}
        style={{ width: "auto", padding: "5px 8px" }}
      >
        {LOCALES.map((l) => (
          <option key={l.code} value={l.code}>{l.name}</option>
        ))}
      </select>
      {authed ? (
        <button className="secondary small" onClick={logout}>{t("logout")}</button>
      ) : (
        <Link className="navlink" href="/login">{t("login")}</Link>
      )}
    </nav>
  );
}
