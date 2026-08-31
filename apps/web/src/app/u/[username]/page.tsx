"use client";

import { useEffect, useState } from "react";
import { useParams, useRouter } from "next/navigation";
import { api, getAccessToken } from "@/lib/api";
import { useI18n } from "@/lib/i18n";
import type { PublicUser } from "@/lib/types";

// Username profile links (t.me/<username> parity): /u/<username> resolves
// the public handle and forwards to the full profile page.
export default function UsernameProfilePage() {
  const { t } = useI18n();
  const router = useRouter();
  const params = useParams<{ username: string }>();
  const [error, setError] = useState("");

  useEffect(() => {
    if (!getAccessToken()) {
      router.push("/login");
      return;
    }
    api<{ user: PublicUser }>(
      `/api/u/${encodeURIComponent(params.username)}`
    )
      .then((d) => router.replace(`/profile/${d.user.id}`))
      .catch((e) => setError(e instanceof Error ? e.message : t("error")));
  }, [params.username, router, t]);

  if (error) return <div className="card error-text">{error}</div>;
  return <div className="card muted">{t("loading")}</div>;
}
