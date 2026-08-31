"use client";

import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import Link from "next/link";
import { api, getAccessToken } from "@/lib/api";
import { useI18n } from "@/lib/i18n";

interface Moment {
  id: string;
  title: string;
  summary: string;
  cover_url: string;
  published_at: string;
  item_count: number;
  curator_name: string;
  curator_username: string;
}

// X Moments parity: curated story collections published by moderators.
export default function MomentsPage() {
  const { t } = useI18n();
  const router = useRouter();
  const [moments, setMoments] = useState<Moment[]>([]);
  const [error, setError] = useState("");

  useEffect(() => {
    if (!getAccessToken()) {
      router.push("/login");
      return;
    }
    api<{ moments: Moment[] }>("/api/moments")
      .then((d) => setMoments(d.moments))
      .catch((e) => setError(e instanceof Error ? e.message : t("error")));
  }, [router, t]);

  return (
    <div className="col">
      <div className="card">
        <h2 style={{ marginTop: 0 }}>{t("moments")}</h2>
        {error && <div className="error">{error}</div>}
        {moments.length === 0 && !error && (
          <span className="muted">{t("noMoments")}</span>
        )}
      </div>
      {moments.map((m) => (
        <Link key={m.id} href={`/moments/${m.id}`} style={{ textDecoration: "none", color: "inherit" }}>
          <div className="card" style={{ cursor: "pointer" }}>
            {m.cover_url && (
              // eslint-disable-next-line @next/next/no-img-element
              <img src={m.cover_url} alt="" style={{ width: "100%", borderRadius: 8, maxHeight: 220, objectFit: "cover" }} />
            )}
            <h3 style={{ marginBottom: 4 }}>{m.title}</h3>
            {m.summary && <p className="muted" style={{ marginTop: 0 }}>{m.summary}</p>}
            <span className="muted" style={{ fontSize: 12 }}>
              {m.item_count} posts · {t("curatedBy")} {m.curator_name || m.curator_username} ·{" "}
              {new Date(m.published_at).toLocaleDateString()}
            </span>
          </div>
        </Link>
      ))}
    </div>
  );
}
