"use client";

import { useEffect, useState } from "react";
import { registerWebPush, unregisterWebPush, vapidPublicKey } from "@/lib/features";
import { useI18n } from "@/lib/i18n";

function b64ToUint8(base64Url: string): Uint8Array {
  const padding = "=".repeat((4 - (base64Url.length % 4)) % 4);
  const base64 = (base64Url + padding).replace(/-/g, "+").replace(/_/g, "/");
  const raw = atob(base64);
  return Uint8Array.from(raw, (c) => c.charCodeAt(0));
}

export default function PushSetup() {
  const { t } = useI18n();
  const [supported, setSupported] = useState(false);
  const [enabled, setEnabled] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    const ok =
      typeof window !== "undefined" &&
      "serviceWorker" in navigator &&
      "PushManager" in window &&
      "Notification" in window;
    setSupported(ok);
    if (!ok) return;
    navigator.serviceWorker
      .register("/sw.js")
      .then((reg) => reg.pushManager.getSubscription())
      .then((sub) => setEnabled(!!sub))
      .catch(() => setEnabled(false));
  }, []);

  if (!supported) return null;

  const enable = async () => {
    setBusy(true);
    setError("");
    try {
      const permission = await Notification.requestPermission();
      if (permission !== "granted") {
        setError("notification permission denied");
        return;
      }
      const { key } = await vapidPublicKey();
      if (!key) throw new Error("push not configured on server");
      const reg = await navigator.serviceWorker.ready;
      const sub = await reg.pushManager.subscribe({
        userVisibleOnly: true,
        applicationServerKey: b64ToUint8(key) as BufferSource,
      });
      const subJSON = sub.toJSON();
      await registerWebPush(
        { endpoint: subJSON.endpoint ?? sub.endpoint, keys: subJSON.keys },
        navigator.userAgent
      );
      setEnabled(true);
    } catch (e) {
      setError(e instanceof Error ? e.message : "push setup failed");
    } finally {
      setBusy(false);
    }
  };

  const disable = async () => {
    setBusy(true);
    try {
      const reg = await navigator.serviceWorker.ready;
      const sub = await reg.pushManager.getSubscription();
      if (sub) {
        await unregisterWebPush(sub.endpoint).catch(() => {});
        await sub.unsubscribe();
      }
      setEnabled(false);
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="card col">
      <h2>{t("pushNotifications")}</h2>
      {error && <div className="error">{error}</div>}
      {enabled ? (
        <div className="row">
          <span className="badge green">{t("pushEnabled")}</span>
          <button className="secondary" onClick={disable} disabled={busy}>
            {t("disable")}
          </button>
        </div>
      ) : (
        <button onClick={enable} disabled={busy}>
          {busy ? t("loading") : t("enablePush")}
        </button>
      )}
    </div>
  );
}
