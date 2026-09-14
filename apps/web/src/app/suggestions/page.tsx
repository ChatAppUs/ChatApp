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

type NearbyPerson = {
  username: string;
  display_name: string;
  distance_km: number;
};

export default function SuggestionsPage() {
  const router = useRouter();
  const [users, setUsers] = useState<Suggestion[]>([]);
  const [following, setFollowing] = useState<Set<string>>(new Set());
  const [error, setError] = useState("");
  const [nearby, setNearby] = useState<NearbyPerson[]>([]);
  const [nearbyError, setNearbyError] = useState("");
  const [discoverable, setDiscoverable] = useState(false);

  useEffect(() => {
    if (!getAccessToken()) {
      router.push("/login");
      return;
    }
    api<{ discoverable: boolean }>("/api/me")
      .then((d) => setDiscoverable(Boolean(d.discoverable)))
      .catch(() => {});
    api<{ suggestions: Suggestion[] }>("/api/me/suggestions")
      .then((d) => setUsers(d.suggestions ?? []))
      .catch((e) => setError(e instanceof Error ? e.message : "failed to load suggestions"));
    api<{ people: NearbyPerson[] }>("/api/nearby")
      .then((d) => setNearby(d.people ?? []))
      .catch((e) =>
        setNearbyError(e instanceof Error ? e.message : "nearby unavailable")
      );
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

  const toggleDiscoverable = async () => {
    const next = !discoverable;
    setDiscoverable(next);
    try {
      await api("/api/me/discoverable", { method: "PUT", body: JSON.stringify({ enabled: next }) });
      setNearby([]);
      setNearbyError("");
      api<{ people: NearbyPerson[] }>("/api/nearby")
        .then((d) => setNearby(d.people ?? []))
        .catch((e) => setNearbyError(e instanceof Error ? e.message : "nearby unavailable"));
    } catch (e) {
      setDiscoverable(!next);
      setNearbyError(e instanceof Error ? e.message : "toggle failed");
    }
  };

  return (
    <div className="col">
      <div className="card col" style={{ gap: 8 }}>
        <h2 style={{ margin: 0 }}>🤝 Suggested for you</h2>
        <div className="muted">People you may know, ranked by mutual connections.</div>
        {error && <div className="error-text">{error}</div>}
      </div>
      <div className="card col" style={{ gap: 8 }}>
        <h2 style={{ margin: 0 }}>📍 People nearby</h2>
        <label className="row" style={{ gap: 8, fontSize: 14 }}>
          <input type="checkbox" checked={discoverable} onChange={toggleDiscoverable} />
          Appear in People nearby (share my discoverability with people within 5 km)
        </label>
        <div className="muted">
          Discoverable people sharing live location within 5 km. Share your live
          location from any chat to appear here and to see others nearby
          (imo/Telegram-style discovery).
        </div>
        {nearbyError && (
          <div className="muted">
            {nearbyError === "start live location first"
              ? "Share your live location in a chat to enable nearby discovery."
              : nearbyError}
          </div>
        )}
        {!nearbyError && nearby.length === 0 && (
          <div className="muted">No discoverable people around right now.</div>
        )}
        {nearby.map((n) => (
          <div key={n.username} className="row" style={{ gap: 10, fontSize: 14 }}>
            <span>🧭 <strong>{n.display_name}</strong></span>
            <span className="muted">@{n.username}</span>
            <div className="spacer" />
            <span className="badge">{n.distance_km} km</span>
          </div>
        ))}
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