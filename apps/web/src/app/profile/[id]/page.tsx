"use client";

import { useCallback, useEffect, useState } from "react";
import { useParams, useRouter } from "next/navigation";
import { api, getAccessToken, getUserId } from "@/lib/api";
import { useI18n } from "@/lib/i18n";
import type { Post, PublicUser } from "@/lib/types";
import PostCard from "@/components/PostCard";

export default function ProfilePage() {
  const { t } = useI18n();
  const router = useRouter();
  const params = useParams<{ id: string }>();
  const [user, setUser] = useState<PublicUser | null>(null);
  const [followers, setFollowers] = useState(0);
  const [following, setFollowing] = useState(0);
  const [posts, setPosts] = useState<Post[]>([]);
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
        </div>
      </div>
      {posts.map((p) => (
        <PostCard key={p.id} post={p} onChanged={load} />
      ))}
    </>
  );
}
