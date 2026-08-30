"use client";

import { useCallback, useEffect, useState } from "react";
import Link from "next/link";
import { useI18n } from "@/lib/i18n";
import { createGroup, joinGroup, listGroups, type Group } from "@/lib/features";

export default function GroupsPage() {
  const { t } = useI18n();
  const [groups, setGroups] = useState<Group[]>([]);
  const [query, setQuery] = useState("");
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [isPrivate, setIsPrivate] = useState(false);
  const [error, setError] = useState("");

  const load = useCallback(async (q = "") => {
    try {
      const r = await listGroups(q);
      setGroups(r.groups);
    } catch (e) {
      setError(e instanceof Error ? e.message : "load failed");
    }
  }, []);

  useEffect(() => { load(); }, [load]);

  const create = async (e: React.FormEvent) => {
    e.preventDefault();
    setError("");
    try {
      await createGroup(name, description, isPrivate);
      setName("");
      setDescription("");
      setIsPrivate(false);
      load(query);
    } catch (err) {
      setError(err instanceof Error ? err.message : "create failed");
    }
  };

  const join = async (id: string) => {
    try {
      await joinGroup(id);
      load(query);
    } catch (e) {
      setError(e instanceof Error ? e.message : "join failed");
    }
  };

  const search = (e: React.FormEvent) => {
    e.preventDefault();
    load(query);
  };

  return (
    <div className="col">
      <h1>{t("groups")}</h1>
      {error && <div className="error">{error}</div>}

      <form className="card col" onSubmit={create}>
        <h2>{t("createGroup")}</h2>
        <label>{t("groupName")}</label>
        <input value={name} onChange={(e) => setName(e.target.value)} required maxLength={100} />
        <label>{t("groupTopic")}</label>
        <input value={description} onChange={(e) => setDescription(e.target.value)} maxLength={512} />
        <label className="row">
          <input type="checkbox" checked={isPrivate} onChange={(e) => setIsPrivate(e.target.checked)} />
          {t("profileLock")}
        </label>
        <button type="submit">{t("createGroup")}</button>
      </form>

      <form className="card row" onSubmit={search}>
        <input placeholder={t("searchOrDiscover")} value={query} onChange={(e) => setQuery(e.target.value)} />
        <button type="submit">{t("searchOrDiscover")}</button>
      </form>

      {groups.map((g) => (
        <div className="card col" key={g.id}>
          <div className="row">
            <Link href={`/groups/${g.id}`}><strong>{g.name}</strong></Link>
            <span className="badge">{g.member_count} · {g.privacy}</span>
          </div>
          {g.description && <p className="muted">{g.description}</p>}
          {g.privacy === "public" && (
            <button onClick={() => join(g.id)}>{t("joinGroup")}</button>
          )}
        </div>
      ))}
      {groups.length === 0 && <p className="muted">—</p>}
    </div>
  );
}
