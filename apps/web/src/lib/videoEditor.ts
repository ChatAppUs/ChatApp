/** Client-side TikTok-style video editor: timed text overlays, optional
 *  green-screen (chroma key) replacement with an image, and a voiceover mix
 *  (mic recorded during playback). Renders into a canvas and re-records with
 *  MediaRecorder into a single .webm File — zero upload until publish. */

export interface Overlay {
  text: string;
  start: number; // seconds
  end: number;   // seconds
  template: "title" | "lower-thirds" | "caption" | "inline";
  color: string;
}

export interface EditorOptions {
  overlays: Overlay[];
  greenScreen: boolean;
  voiceover: boolean; // mix mic audio into the output
  background?: HTMLImageElement | null;
}

function drawOverlay(ctx: CanvasRenderingContext2D, w: number, h: number, ov: Overlay) {
  ctx.font = ov.template === "title" ? "900 56px system-ui" : ov.template === "lower-thirds" ? "700 28px system-ui" : "500 28px system-ui";
  ctx.textBaseline = "middle";
  ctx.fillStyle = ov.color;
  const tw = ctx.measureText(ov.text).width;
  if (ov.template === "title") {
    ctx.fillText(ov.text, (w - tw) / 2, h * 0.18);
  } else if (ov.template === "lower-thirds") {
    ctx.fillStyle = "rgba(0,0,0,0.6)";
    ctx.fillRect(0, h - 64, w, 44);
    ctx.fillStyle = ov.color;
    ctx.fillText(ov.text, 24, h - 42);
  } else if (ov.template === "caption") {
    ctx.fillText(ov.text, (w - tw) / 2, h * 0.8);
  } else {
    ctx.fillText(ov.text, 16, h - 24);
  }
}

export function chromaKey(ctx: CanvasRenderingContext2D, w: number, h: number) {
  const img = ctx.getImageData(0, 0, w, h);
  for (let i = 0; i < img.data.length; i += 4) {
    const r = img.data[i], g = img.data[i + 1], b = img.data[i + 2];
    if (g > 100 && g > r * 1.4 && g > b * 1.4) img.data[i + 3] = 0;
  }
  ctx.putImageData(img, 0, 0);
}

/** Processes a video clip through the editor pipeline and resolves with the
 *  composited webm File. */
export async function composite(src: File, opts: EditorOptions, onProgress: (pct: number) => void): Promise<File> {
  const url = URL.createObjectURL(src);
  const video = document.createElement("video");
  video.playsInline = true;
  video.src = url;

  const canvas = document.createElement("canvas");
  canvas.width = video.videoWidth || 1280;
  canvas.height = video.videoHeight || 720;
  const ctx = canvas.getContext("2d")!;

  let mic: MediaStream | undefined;
  let audioCtx: AudioContext | undefined;
  let combined: MediaStream | undefined;

  if (audioSvcSupported()) {
    combined = canvas.captureStream(30);
    if (opts.voiceover) {
      mic = await navigator.mediaDevices.getUserMedia({ audio: { noiseSuppression: true, echoCancellation: true } });
      audioCtx = new AudioContext();
      const dest = audioCtx.createMediaStreamDestination();
      try {
        const vs = audioCtx.createMediaElementSource(video);
        vs.connect(dest);
        vs.connect(audioCtx.destination);
      } catch { /* video has no audible track — voiceover only */ }
      audioCtx.createMediaStreamSource(mic).connect(dest);
      const out = combined!;
      dest.stream.getAudioTracks().forEach((tr) => out.addTrack(tr));
    }
  }

  const chunks: Blob[] = [];
  const mime = MediaRecorder.isTypeSupported("video/webm;codecs=vp9,opus") ? "video/webm;codecs=vp9,opus" : "video/webm";
  const rec = new MediaRecorder(combined ?? canvas.captureStream(30), { mimeType: mime });
  rec.ondataavailable = (e) => e.data.size && chunks.push(e.data);

  const draw = () => {
    ctx.drawImage(video, 0, 0, canvas.width, canvas.height);
    if (opts.greenScreen && opts.background) {
      chromaKey(ctx, canvas.width, canvas.height);
      ctx.globalCompositeOperation = "destination-over";
      ctx.drawImage(opts.background, 0, 0, canvas.width, canvas.height);
      ctx.globalCompositeOperation = "source-over";
    }
    const t = video.currentTime;
    for (const ov of opts.overlays) {
      if (t >= ov.start && t <= ov.end) drawOverlay(ctx, canvas.width, canvas.height, ov);
    }
    onProgress((video.currentTime / video.duration) * 100);
  };

  return new Promise((resolve, reject) => {
    video.onended = () => {
      rec.stop();
      mic?.getTracks().forEach((t) => t.stop());
      URL.revokeObjectURL(url);
      resolve(new File(chunks, src.name.replace(/\.[^.]+$/i, "") + "-cut.webm", { type: "video/webm" }));
    };
    const tick = () => { if (!video.ended) { draw(); requestAnimationFrame(tick); } };
    rec.start();
    void video.play();
    tick();
    setTimeout(() => reject(new Error("editor timeout")), 10 * 60 * 1000);
  });
}

function audioSvcSupported() {
  return typeof Navigator !== "undefined" && "mediaDevices" in navigator && !!navigator.mediaDevices.getUserMedia;
}

/** Preset overlay packs ("Templates" row in the TikTok gap list). */
export const TEMPLATE_PRESETS: { name: string; make: (title: string, duration: number) => Overlay[] }[] = [
  { name: "Title card", make: (t, d) => [{ text: t, start: 0, end: Math.min(2, d), template: "title", color: "#fff" }] },
  { name: "Lower thirds", make: (t, d) => [{ text: t, start: 0, end: Math.min(5, d), template: "lower-thirds", color: "#fff" }] },
  { name: "Auto caption", make: (t, d) => [{ text: t, start: 0, end: d, template: "caption", color: "#fff" }] },
  { name: "None", make: () => [] },
];
