"use client";

import { useCallback, useEffect, useState } from "react";
import Link from "next/link";
import { useI18n } from "@/lib/i18n";
import { createPage, followPage, listPages, type Page } from "@/lib/features";

export default function PagesPage() {
  const { t } = useI18n();
  const [pages, setPages] = useState<Page[]>([]);
  const [query, setQuery] = useState("");
  const [name, setName] = useState("");
  const [category, setCategory] = useState("");
  const [description, setDescription] = useState("");
  const [error, setError] = useState("");

  const load = useCallback(async (q = "") => {
    try {
      const r = await listPages(q);
      setPages(r.pages);
    } catch (e) {
      setError(e instanceof Error ? e.message : "load failed");
    }
  }, []);

  useEffect(() => { load(); }, [load]);

  const create = async (e: React.FormEvent) => {
    e.preventDefault();
    setError("");
    try {
      await createPage(name, category, description);
      setName(""); setCategory(""); setDescription("");
      load(query);
    } catch (err) {
      setError(err instanceof Error ? err.message : "create failed");
    }
  };

  const follow = async (id: string) => {
    try {
      await followPage(id);
      load(query);
    } catch (e) {
      setError(e instanceof Error ? e.message : "follow failed");
    }
  };

  const search = (e: React.FormEvent) => {
    e.preventDefault();
    load(query);
  };

  return (
    <div className="col">
      <h1>{t("pages")}</h1>
      {error && <div className="error">{error}</div>}

      <form className="card col" onSubmit={create}>
        <h2>{t("createPage")}</h2>
        <label>{t("pageName")}</label>
        <input value={name} onChange={(e) => setName(e.target.value)} required maxLength={100} />
        <label>{t("category")}</label>
        <input value={category} onChange={(e) => setCategory(e.target.value)} maxLength={64} />
        <label>{t("description") ?? t("groupTopic")}</label>
        <input value={description} onChange={(e) => setDescription(e.target.value)} maxLength={512} />
        <button type="submit">{t("createPage")}</button>
      </form>

      <form className="card row" onSubmit={search}>
        <input placeholder={t("searchOrDiscover")} value={query} onChange={(e) => setQuery(e.target.value)} />
        <button type="submit">{t("searchOrDiscover")}</button>
      </form>

      {pages.map((p) => (
        <div className="card col" key={p.id}>
          <div className="row">
            <Link href={`/pages/${p.id}`}><strong>{p.name}</strong></Link>
            <span className="badge">{p.category}</span>
            <span className="badge">{p.follower_count} {t("followers")}</span>
          </div>
          {p.description && <p className="muted">{p.description}</p>}
          <button onClick={() => follow(p.id)}>{t("follow")}</button>
        </div>
      ))}
      {pages.length === 0 && <p className="muted">—</p>}
    </div>
  );
}
