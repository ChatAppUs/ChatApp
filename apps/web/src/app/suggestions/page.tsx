"use client";

import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import Link from "next/link";
import { api, getAccessToken } from "@/lib/api";

type Suggestion = {
  id: string;
  username: string;
  display_name: string;
  avatar_url: string;
  mutual_follows: number;
};

export default function SuggestionsPage() {
  const router = useRouter();
  const [users, setUsers] = useState<Suggestion[]>([]);
  const [following, setFollowing] = useState<Set<string>>(new Set());
  const [error, setError] = useState("");

  useEffect(() => {
    if (!getAccessToken()) {
      router.push("/login");
      return;
    }
    api<{ suggestions: Suggestion[] }>("/api/me/suggestions")
      .then((d) => setUsers(d.suggestions ?? []))
      .catch((e) => setError(e instanceof Error ? e.message : "failed to load suggestions"));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const follow = async (id: string) => {
    setError("");
    try {
      await api(`/api/users/${id}/follow`, { method: "POST", body: "{}" });
      setFollowing((prev) => {
        const s = new Set(prev);
        s.add(id);
        return s;
      });
    } catch (e) {
      setError(e instanceof Error ? e.message : "follow failed");
    }
  };

  return (
    <div className="col">
      <div className="card col" style={{ gap: 8 }}>
        <h2 style={{ margin: 0 }}>🤝 Suggested for you</h2>
        <div className="muted">People you may know, ranked by mutual connections.</div>
        {error && <div className="error-text">{error}</div>}
      </div>
      {users.length === 0 && !error && <div className="card muted">Nothing to suggest right now — check back soon.</div>}
      {users.map((u) => {
        const isFollowed = following.has(u.id);
        return (
          <div key={u.id} className="card row" style={{ gap: 10, alignItems: "center" }}>
            <Link href={`/profile/${u.id}`} style={{ textDecoration: "none", color: "inherit", display: "flex", alignItems: "center", gap: 10 }}>
              {u.avatar_url ? (
                // eslint-disable-next-line @next/next/no-img-element
                <img src={u.avatar_url} alt="" style={{ width: 42, height: 42, borderRadius: "50%" }} />
              ) : (
                <div style={{ width: 42, height: 42, borderRadius: "50%", background: "var(--surface2)" }} />
              )}
              <div>
                <strong>{u.display_name}</strong>
                <div className="muted" style={{ fontSize: 12 }}>@{u.username} · {u.mutual_follows} mutual{u.mutual_follows === 1 ? "" : "s"}</div>
              </div>
            </Link>
            <div className="spacer" />
            <button className={isFollowed ? "secondary small" : "small"} disabled={isFollowed} onClick={() => follow(u.id)}>
              {isFollowed ? "Following ✓" : "Follow"}
            </button>
          </div>
        );
      })}
    </div>
  );
}