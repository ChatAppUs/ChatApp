"use client";

import { useCallback, useEffect, useState } from "react";
import { api } from "@/lib/api";
import { useI18n } from "@/lib/i18n";

type ScreenTimeState = {
  limit_minutes: number;
  used_minutes: number;
  exceeded: boolean;
};

// Screen-time limits (TikTok/Facebook): daily limit + live usage tracking.
// The client pings active minutes while this tab is visible; the server
// accumulates per-day usage and reports when the limit is exceeded.
// App lock (Telegram): requires the account password to toggle.
export default function ScreenTimePanel() {
  const { t } = useI18n();
  const [state, setState] = useState<ScreenTimeState | null>(null);
  const [limit, setLimit] = useState("");
  const [appLock, setAppLock] = useState(false);
  const [lockPassword, setLockPassword] = useState("");
  const [showLockForm, setShowLockForm] = useState(false);
  const [error, setError] = useState("");
  const [status, setStatus] = useState("");

  const load = useCallback(() => {
    api<ScreenTimeState>("/api/me/screen-time").then(setState).catch(() => {});
    api<{ app_lock_enabled?: boolean }>("/api/me")
      .then((d) => setAppLock(Boolean(d.app_lock_enabled)))
      .catch(() => {});
  }, []);

  useEffect(load, [load]);

  // Accumulate ~1 minute per minute while this tab is visible.
  useEffect(() => {
    const iv = setInterval(() => {
      if (document.hidden) return;
      api<ScreenTimeState>("/api/me/screen-time/ping", {
        method: "POST",
        body: JSON.stringify({ minutes: 1 }),
      }).then(setState).catch(() => {});
    }, 60_000);
    return () => clearInterval(iv);
  }, []);

  const saveLimit = async () => {
    setError("");
    setStatus("");
    try {
      await api("/api/me/screen-time", {
        method: "PUT",
        body: JSON.stringify({ limit_minutes: Number(limit) || 0 }),
      });
      setStatus(t("saved"));
      load();
    } catch (e) {
      setError(e instanceof Error ? e.message : t("error"));
    }
  };

  const toggleLock = async () => {
    setError("");
    setStatus("");
    try {
      await api("/api/auth/verify-password", {
        method: "POST",
        body: JSON.stringify({ password: lockPassword }),
      });
      const next = !appLock;
      await api("/api/me/app-lock", {
        method: "PUT",
        body: JSON.stringify({ enabled: next }),
      });
      setAppLock(next);
      setLockPassword("");
      setShowLockForm(false);
      setStatus(t("saved"));
    } catch {
      setError(t("wrongPassword"));
    }
  };

  return (
    <div className="card col" style={{ gap: 8 }}>
      <strong>⏱️ {t("screenTime")}</strong>
      {state && (
        <div className="muted" style={{ fontSize: 13 }}>
          {t("usedToday")}: {Math.round(state.used_minutes)} / {state.limit_minutes || "∞"} min
          {state.exceeded && <span className="error-text"> · {t("limitExceeded")}</span>}
        </div>
      )}
      <div className="row" style={{ gap: 6 }}>
        <input type="number" min={0} max={1440} value={limit}
          placeholder={t("dailyLimit")} onChange={(e) => setLimit(e.target.value)}
          style={{ width: 140 }} />
        <button className="secondary" onClick={saveLimit}>{t("save")}</button>
      </div>
      <strong>🔒 {t("appLock")}</strong>
      <div className="muted" style={{ fontSize: 13 }}>{t("appLockHint")}</div>
      {!showLockForm ? (
        <div className="row" style={{ gap: 6 }}>
          <button className="secondary" onClick={() => setShowLockForm(true)}>
            {appLock ? t("disable") : t("enable")}
          </button>
          <span className="muted" style={{ fontSize: 13 }}>{appLock ? "🔒 on" : "off"}</span>
        </div>
      ) : (
        <div className="row" style={{ gap: 6 }}>
          <input type="password" value={lockPassword} placeholder={t("password")}
            onChange={(e) => setLockPassword(e.target.value)} />
          <button onClick={toggleLock} disabled={!lockPassword}>{t("verifyPassword")}</button>
          <button className="secondary" onClick={() => setShowLockForm(false)}>{t("cancel")}</button>
        </div>
      )}
      {status && <div className="muted">{status}</div>}
      {error && <div className="error">{error}</div>}
    </div>
  );
}
