"use client";

import { FormEvent, useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import Link from "next/link";
import { api, getAccessToken } from "@/lib/api";
import { useI18n } from "@/lib/i18n";
import PostCard from "@/components/PostCard";
import type { Post, PublicUser } from "@/lib/types";

// Advanced search (X parity): supports operators —
//   from:<username>  since:YYYY-MM-DD  until:YYYY-MM-DD
//   filter:reels|media|links|posts   has:media|link
// plus plain full-text terms.
export default function SearchPage() {
  const { t } = useI18n();
  const router = useRouter();
  const [q, setQ] = useState("");
  const [tab, setTab] = useState<"posts" | "people">("posts");
  const [posts, setPosts] = useState<Post[]>([]);
  const [users, setUsers] = useState<PublicUser[]>([]);
  const [searched, setSearched] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    if (!getAccessToken()) router.push("/login");
  }, [router]);

  const search = async (e?: FormEvent) => {
    e?.preventDefault();
    setError("");
    setSearched(true);
    try {
      if (tab === "posts") {
        const d = await api<{ posts: Post[] }>(
          `/api/search/posts?q=${encodeURIComponent(q)}`
        );
        setPosts(d.posts);
      } else {
        const d = await api<{ users: PublicUser[] }>(
          `/api/users/search?q=${encodeURIComponent(q)}`
        );
        setUsers(d.users);
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : t("error"));
    }
  };

  return (
    <div className="col">
      <div className="card col" style={{ gap: 8 }}>
        <h2 style={{ marginTop: 0 }}>{t("search")}</h2>
        <form onSubmit={search} className="row" style={{ gap: 6 }}>
          <input
            value={q}
            onChange={(e) => setQ(e.target.value)}
            placeholder={t("searchPlaceholder")}
            style={{ flex: 1 }}
          />
          <button type="submit">🔍</button>
        </form>
        <div className="muted" style={{ fontSize: 12 }}>
          from:user · since:2026-01-01 · until:2026-12-31 · filter:reels · has:media
        </div>
        <div className="row" style={{ gap: 6 }}>
          <button
            className={tab === "posts" ? "small" : "secondary small"}
            onClick={() => setTab("posts")}
          >
            {t("posts")}
          </button>
          <button
            className={tab === "people" ? "small" : "secondary small"}
            onClick={() => setTab("people")}
          >
            {t("people")}
          </button>
        </div>
        {error && <div className="error">{error}</div>}
      </div>
      {tab === "posts" && searched && posts.length === 0 && !error && (
        <div className="card muted">{t("noResults")}</div>
      )}
      {tab === "posts" &&
        posts.map((p) => <PostCard key={p.id} post={p} onChanged={() => search()} />)}
      {tab === "people" &&
        users.map((u) => (
          <Link key={u.id} href={`/profile/${u.id}`} style={{ textDecoration: "none", color: "inherit" }}>
            <div className="card row" style={{ gap: 10, alignItems: "center", cursor: "pointer" }}>
              {u.avatar_url ? (
                // eslint-disable-next-line @next/next/no-img-element
                <img src={u.avatar_url} alt="" style={{ width: 40, height: 40, borderRadius: "50%" }} />
              ) : (
                <div style={{ width: 40, height: 40, borderRadius: "50%", background: "var(--surface2)" }} />
              )}
              <div>
                <strong>{u.display_name}</strong>
                <div className="muted" style={{ fontSize: 12 }}>@{u.username}</div>
              </div>
            </div>
          </Link>
        ))}
    </div>
  );
}
