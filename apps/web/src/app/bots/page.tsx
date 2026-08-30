"use client";

import { useCallback, useEffect, useState } from "react";
import { useI18n } from "@/lib/i18n";
import { createBot, deleteBot, myBots, setMiniApp, setWebhook, type Bot } from "@/lib/features";

export default function BotsPage() {
  const { t } = useI18n();
  const [bots, setBots] = useState<Bot[]>([]);
  const [username, setUsername] = useState("");
  const [displayName, setDisplayName] = useState("");
  const [description, setDescription] = useState("");
  const [newToken, setNewToken] = useState("");
  const [error, setError] = useState("");
  const [webhookUrl, setWebhookUrl] = useState<Record<string, string>>({});
  const [miniUrl, setMiniUrl] = useState<Record<string, string>>({});

  const load = useCallback(async () => {
    try {
      const data = await myBots();
      setBots(data.bots);
    } catch (e) {
      setError(e instanceof Error ? e.message : "load failed");
    }
  }, []);

  useEffect(() => { load(); }, [load]);

  const create = async (e: React.FormEvent) => {
    e.preventDefault();
    setError("");
    try {
      const bot = await createBot(username, displayName, description);
      setNewToken(bot.token);
      setUsername(""); setDisplayName(""); setDescription("");
      load();
    } catch (err) {
      setError(err instanceof Error ? err.message : "create failed");
    }
  };

  const hook = async (id: string) => {
    try {
      await setWebhook(id, webhookUrl[id] ?? "");
      load();
    } catch (e) {
      setError(e instanceof Error ? e.message : "webhook failed");
    }
  };

  const mini = async (id: string) => {
    try {
      await setMiniApp(id, "Mini-app", miniUrl[id] ?? "");
      load();
    } catch (e) {
      setError(e instanceof Error ? e.message : "mini-app failed");
    }
  };

  const remove = async (id: string) => {
    await deleteBot(id).catch((e) => setError(e.message));
    load();
  };

  return (
    <div className="col">
      <h1>{t("bots")}</h1>
      {error && <div className="error">{error}</div>}
      {newToken && (
        <div className="card col">
          <strong>{t("botToken")}</strong>
          <code style={{ wordBreak: "break-all" }}>{newToken}</code>
        </div>
      )}

      <form className="card col" onSubmit={create}>
        <h2>{t("createBot")}</h2>
        <label>{t("botUsername")}</label>
        <input value={username} onChange={(e) => setUsername(e.target.value)} required maxLength={32} />
        <label>{t("displayName")}</label>
        <input value={displayName} onChange={(e) => setDisplayName(e.target.value)} required maxLength={64} />
        <label>{t("groupTopic")}</label>
        <input value={description} onChange={(e) => setDescription(e.target.value)} maxLength={256} />
        <button type="submit">{t("createBot")}</button>
      </form>

      {bots.map((b) => (
        <div className="card col" key={b.id}>
          <div className="row">
            <strong>@{b.username}</strong>
            <span className={`badge ${b.active ? "green" : ""}`}>{b.active ? "active" : "inactive"}</span>
            {b.has_webhook && <span className="badge">{t("webhook")}</span>}
          </div>
          {b.description && <p className="muted">{b.description}</p>}
          <label>{t("webhook")}</label>
          <div className="row">
            <input
              value={webhookUrl[b.id] ?? ""}
              onChange={(e) => setWebhookUrl((m) => ({ ...m, [b.id]: e.target.value }))}
              placeholder="https://bot.example.com/webhook"
            />
            <button className="secondary" onClick={() => hook(b.id)}>{t("save")}</button>
          </div>
          <label>{t("miniApp")}</label>
          <div className="row">
            <input
              value={miniUrl[b.id] ?? b.mini_app_url}
              onChange={(e) => setMiniUrl((m) => ({ ...m, [b.id]: e.target.value }))}
              placeholder="https://mini.example.com"
            />
            <button className="secondary" onClick={() => mini(b.id)}>{t("save")}</button>
          </div>
          <button className="danger" onClick={() => remove(b.id)}>{t("delete")}</button>
        </div>
      ))}
    </div>
  );
}
