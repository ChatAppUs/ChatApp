"use client";

import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import Link from "next/link";
import { api, getAccessToken } from "@/lib/api";

type Visitor = {
  id: string;
  username: string;
  display_name: string;
  avatar_url: string;
  viewed_at: string;
};

export default function VisitorsPage() {
  const router = useRouter();
  const [visitors, setVisitors] = useState<Visitor[]>([]);
  const [error, setError] = useState("");

  useEffect(() => {
    if (!getAccessToken()) {
      router.push("/login");
      return;
    }
    api<{ visitors: Visitor[] }>("/api/me/profile-visitors")
      .then((d) => setVisitors(d.visitors ?? []))
      .catch((e) => setError(e instanceof Error ? e.message : "failed to load visitors"));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  return (
    <div className="col">
      <div className="card col" style={{ gap: 8 }}>
        <h2 style={{ margin: 0 }}>👀 Profile visitors</h2>
        <div className="muted">People who recently viewed your profile (only shown when they visited with privacy settings on).</div>
        {error && <div className="error-text">{error}</div>}
      </div>
      {visitors.length === 0 && !error && <div className="card muted">No one has set foot on your doorstep yet — or visitors kept their visit private.</div>}
      {visitors.map((v) => (
        <Link key={v.id} href={`/profile/${v.id}`} style={{ textDecoration: "none", color: "inherit" }}>
          <div className="card row" style={{ gap: 10, alignItems: "center" }}>
            {v.avatar_url ? (
              // eslint-disable-next-line @next/next/no-img-element
              <img src={v.avatar_url} alt="" style={{ width: 42, height: 42, borderRadius: "50%" }} />
            ) : (
              <div style={{ width: 42, height: 42, borderRadius: "50%", background: "var(--surface2)" }} />
            )}
            <div style={{ flex: 1 }}>
              <strong>{v.display_name}</strong>
              <div className="muted" style={{ fontSize: 12 }}>@{v.username} · visited {new Date(v.viewed_at).toLocaleString()}</div>
            </div>
          </div>
        </Link>
      ))}
    </div>
  );
}