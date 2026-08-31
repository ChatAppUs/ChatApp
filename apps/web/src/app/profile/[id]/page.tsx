"use client";

import { useCallback, useEffect, useState } from "react";
import Link from "next/link";
import { useParams, useRouter } from "next/navigation";
import { api, getAccessToken, getUserId } from "@/lib/api";
import { useI18n } from "@/lib/i18n";
import type { Album, Post, PublicUser } from "@/lib/types";
import PostCard from "@/components/PostCard";
import ProfileQA from "@/components/ProfileQA";

export default function ProfilePage() {
  const { t } = useI18n();
  const router = useRouter();
  const params = useParams<{ id: string }>();
  const [user, setUser] = useState<PublicUser | null>(null);
  const [followers, setFollowers] = useState(0);
  const [following, setFollowing] = useState(0);
  const [posts, setPosts] = useState<Post[]>([]);
  const [albums, setAlbums] = useState<Album[]>([]);
  const [scheduled, setScheduled] = useState<Post[]>([]);
  const [error, setError] = useState("");

  const load = useCallback(async () => {
    try {
      const data = await api<{ user: PublicUser; followers: number; following: number }>(
        `/api/users/${params.id}`
      );
      setUser(data.user);
      setFollowers(data.followers);
      setFollowing(data.following);
      const p = await api<{ posts: Post[] }>(`/api/users/${params.id}/posts`);
      setPosts(p.posts);
      api<{ albums: Album[] }>(`/api/users/${params.id}/albums`)
        .then((d) => setAlbums(d.albums)).catch(() => {});
      if (data.user.id === getUserId()) {
        api<{ posts: Post[] }>("/api/me/scheduled-posts")
          .then((d) => setScheduled(d.posts)).catch(() => {});
      }
    } catch (e) {
      setError(e instanceof Error ? e.message : t("error"));
    }
  }, [params.id, t]);

  useEffect(() => {
    if (!getAccessToken()) {
      router.push("/login");
      return;
    }
    load();
  }, [load, router]);

  if (error) return <div className="card error-text">{error}</div>;
  if (!user) return <div className="card muted">{t("loading")}</div>;

  const isMe = user.id === getUserId();
  const sorted = [...posts].sort((a, b) =>
    a.id === user.pinned_post_id ? -1 : b.id === user.pinned_post_id ? 1 : 0
  );

  return (
    <>
      <div className="card">
        <div className="row">
          {user.avatar_url ? <img className="avatar" src={user.avatar_url} alt="" /> : <div className="avatar" />}
          <div>
            <h3 style={{ margin: 0 }}>
              {user.display_name} {user.is_verified && <span className="badge green">✓</span>}
            </h3>
            <div className="muted">@{user.username}</div>
          </div>
          <div className="spacer" />
          {!isMe && (
            <>
              <button className="small" onClick={() => api(`/api/users/${user.id}/follow`, { method: "POST" }).then(load)}>
                {t("follow")}
              </button>
              <button className="secondary small" onClick={() => api(`/api/users/${user.id}/follow`, { method: "DELETE" }).then(load)}>
                {t("unfollow")}
              </button>
            </>
          )}
        </div>
        {user.bio && <p>{user.bio}</p>}
        <div className="row muted">
          <span>{t("followers")}: {followers}</span>
          <span>{t("following")}: {following}</span>
          {isMe && <Link href="/albums">🖼️ My albums</Link>}
        </div>
      </div>
      <ProfileQA userId={user.id} isMe={isMe} />
      {albums.length > 0 && (
        <div className="card">
          <strong>Albums</strong>
          <div className="row" style={{ flexWrap: "wrap", gap: 10, marginTop: 8 }}>
            {albums.map((a) => (
              <div key={a.id} style={{ width: 110, textAlign: "center" }}>
                {a.cover_url
                  ? <img src={a.cover_url} alt="" style={{ width: 110, height: 110, borderRadius: 8, objectFit: "cover" }} />
                  : <div style={{ width: 110, height: 110, borderRadius: 8, background: "var(--border)", display: "grid", placeItems: "center", fontSize: 24 }}>🖼️</div>}
                <div style={{ fontSize: 12 }}>{a.title}</div>
                <div className="muted" style={{ fontSize: 11 }}>{a.item_count} items</div>
              </div>
            ))}
          </div>
        </div>
      )}
      {isMe && scheduled.length > 0 && (
        <div className="card">
          <strong>🕒 Scheduled posts</strong>
          {scheduled.map((p) => (
            <div key={p.id} className="row" style={{ marginTop: 6 }}>
              <span style={{ fontSize: 13 }}>{p.body?.slice(0, 60) || p.type}</span>
              <span className="muted" style={{ fontSize: 12 }}>
                {p.publish_at ? new Date(p.publish_at).toLocaleString() : ""}
              </span>
              <div className="spacer" />
              <button className="secondary small" onClick={() =>
                api(`/api/scheduled-posts/${p.id}`, { method: "DELETE" }).then(load)}>
                Cancel
              </button>
            </div>
          ))}
        </div>
      )}
      {sorted.map((p) => (
        <div key={p.id}>
          {p.id === user.pinned_post_id && (
            <div className="muted" style={{ fontSize: 12, marginBottom: 2 }}>📌 Pinned post</div>
          )}
          <PostCard post={p} onChanged={load} />
        </div>
      ))}
    </>
  );
}
