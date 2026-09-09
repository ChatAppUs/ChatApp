"use client";

import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { api, getAccessToken } from "@/lib/api";

type LevelInfo = {
  xp: number;
  level: number;
  next_level_xp: number;
};

export default function LevelPage() {
  const router = useRouter();
  const [info, setInfo] = useState<LevelInfo | null>(null);
  const [error, setError] = useState("");

  useEffect(() => {
    if (!getAccessToken()) {
      router.push("/login");
      return;
    }
    api<LevelInfo>("/api/me/level")
      .then(setInfo)
      .catch((e) => setError(e instanceof Error ? e.message : "failed to load level"));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const progress = info && info.next_level_xp > 0 ? Math.min(100, Math.round((info.xp / info.next_level_xp) * 100)) : 0;
  const currentBase = info ? (info.level - 1) * (info.level - 1) * 100 : 0;
  const gain = info ? Math.max(0, info.xp - currentBase) : 0;

  return (
    <div className="col">
      <div className="card col" style={{ gap: 8 }}>
        <div className="row">
          <h2 style={{ margin: 0 }}>🏅 Level</h2>
          {info && <span className="badge green">Lv {info.level}</span>}
        </div>
        {error && <div className="error-text">{error}</div>}
        {info ? (
          <>
            <div className="muted">Total XP: {info.xp.toLocaleString()}</div>
            <div style={{ height: 10, borderRadius: 999, background: "var(--surface2)", overflow: "hidden" }}>
              <div style={{ width: `${progress}%`, height: "100%", background: "var(--accent-2)", transition: "width .3s" }} />
            </div>
            <div className="muted" style={{ fontSize: 12 }}>
              {gain.toLocaleString()} XP into level {info.level} — {Math.max(0, info.next_level_xp - info.xp).toLocaleString()} XP to level {info.level + 1}. Earn XP by posting, reacting and being active in ChatApp.
            </div>
          </>
        ) : (
          !error && <div className="muted">Loading…</div>
        )}
      </div>
      <div className="card muted">
        Level up, unlock perks — higher upload caps, early access to creator tools, and a badge next to your name on every platform.
      </div>
    </div>
  );
}