"use client";

import { useRef, useState } from "react";
import { composite, Overlay, TEMPLATE_PRESETS } from "@/lib/videoEditor";
import { useI18n } from "@/lib/i18n";

/** TikTok-style editor panel: applies text overlays w/ timing, green-screen
 *  (chroma key), templates, and an optional voiceover mix into the clip.
 *  Emits the composited .webm file once. */
export default function VideoEditor({ source, onExported }: { source: File; onExported: (f: File) => void }) {
  const { t } = useI18n();
  const [busy, setBusy] = useState(false);
  const [progress, setProgress] = useState(0);
  const [template, setTemplate] = useState("None");
  const [title, setTitle] = useState("");
  const [duration, setDuration] = useState(0);
  const [green, setGreen] = useState(false);
  const [voice, setVoice] = useState(false);
  const bgRef = useRef<HTMLImageElement | undefined>(undefined);

  const run = async () => {
    setBusy(true); setProgress(0);
    try {
      const made = TEMPLATE_PRESETS.find((p) => p.name === template)!;
      const patched: Overlay[] = made.make(title || " ", Math.max(1, duration));
      const bg = bgRef.current;
      if (!duration) {
        const v = document.createElement("video");
        v.src = URL.createObjectURL(source); await new Promise((r) => v.onloadedmetadata = r);
        setDuration(v.duration); URL.revokeObjectURL(v.src);
      }
      const out = await composite(source, { overlays: title ? patched : [], greenScreen: green,
        voiceover: voice, background: green ? (bg ?? null) : null }, (p) => setProgress(p));
      onExported(out);
    } catch (e) {
      setBusy(false);
    }
  };

  const pickBg = (e: React.ChangeEvent<HTMLInputElement>) => {
    const f = e.target.files?.[0];
    if (!f) return;
    const img = new Image();
    img.onload = () => { bgRef.current = img; };
    img.src = URL.createObjectURL(f);
  };

  return (
    <div className="col" style={{ gap: 8, border: "1px solid var(--border, #444)", borderRadius: 8, padding: 8 }}>
      <div className="row" style={{ gap: 8 }}>
        <select className="secondary" value={template} onChange={(e) => setTemplate(e.target.value)}>
          {TEMPLATE_PRESETS.map((p) => <option key={p.name} value={p.name}>{t("template")}: {p.name}</option>)}
        </select>
        {template !== "None" && (
          <input className="secondary" placeholder={t("overlayText")} value={title}
                 onChange={(e) => setTitle(e.target.value)} style={{ width: 160 }} />
        )}
      </div>
      <div className="row" style={{ gap: 10 }}>
        <label className="small"><input type="checkbox" checked={green} onChange={(e) => setGreen(e.target.checked)} /> {t("greenScreen")}</label>
        {green && <input type="file" accept="image/*" onChange={pickBg} className="secondary small" />}
      </div>
      <label className="small"><input type="checkbox" checked={voice} onChange={(e) => setVoice(e.target.checked)} /> {t("voiceover")}</label>
      {busy ? (
        <span className="muted small" role="progressbar" aria-valuenow={Math.round(progress)}>
          ⏳ {Math.round(progress)}%
        </span>
      ) : (
        <button className="secondary small" onClick={run}>{t("exportClip")}</button>
      )}
    </div>
  );
}
