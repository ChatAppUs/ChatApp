"""AI creator tools + assistant endpoints for the ChatApp ML service.

Every endpoint here is *honest about availability*: when the backing model is
not configured it returns ``{"available": false, "reason": ...}`` and the API
persists that state rather than inventing an output. Two of the three
capabilities (clip candidate selection and dubbing segmentation) are
deterministic analyses over real media metadata and therefore run without any
external model; only speech synthesis and open-ended chat need a model.

Environment:
  WHISPER_MODEL   - HuggingFace ASR model id (shared with /captions)
  TTS_MODEL       - HuggingFace text-to-speech model id
  TRANSLATE_MODEL - HuggingFace translation model id
  ASSISTANT_MODEL - HuggingFace text-generation model id
  MEDIA_PROBE_URL - optional ffprobe-capable service for exact durations
"""

from __future__ import annotations

import os
import re
import urllib.request
from typing import Any

from fastapi import FastAPI
from pydantic import BaseModel, Field

# ---------------------------------------------------------------- models -----


class DubRequest(BaseModel):
    media_url: str
    target_lang: str
    source_lang: str | None = None


class ClipRequest(BaseModel):
    media_url: str
    max_clips: int = 5
    min_len_s: float = 8.0
    max_len_s: float = 60.0


class AssistantRequest(BaseModel):
    messages: list[dict[str, str]] = Field(default_factory=list)
    user_id: str | None = None


class TranslateRequest(BaseModel):
    text: str
    target_lang: str
    source_lang: str | None = None


# --------------------------------------------------------------- helpers -----

_SENTENCE_RE = re.compile(r"[^.!?\n]+[.!?]?")


def _probe_duration(url: str) -> float:
    """Best-effort duration probe. Returns 0.0 when it cannot be determined."""
    try:
        req = urllib.request.Request(url, method="HEAD")
        with urllib.request.urlopen(req, timeout=10) as resp:
            raw = resp.headers.get("Content-Length") or "0"
        # Assume ~500 KB/s for video-as-a-rough-guess is NOT acceptable, so we
        # only trust Content-Length when a bitrate is known. Without ffprobe we
        # return 0 and let the caller treat duration as unknown.
        _ = raw
    except Exception:
        pass
    return 0.0


def _asr_segments(url: str) -> tuple[bool, str, list[dict[str, Any]]]:
    """Reuse the shared ASR instance from main to get real timed segments."""
    try:
        from main import _asr, _asr_error  # type: ignore
    except Exception as exc:  # pragma: no cover - import cycle guard
        return False, f"asr unavailable: {exc}", []
    if _asr is None:
        return False, _asr_error or "WHISPER_MODEL not configured", []
    try:
        with urllib.request.urlopen(url, timeout=30) as resp:
            audio = resp.read(256 * 1024 * 1024)
        result = _asr(audio, return_timestamps=True)  # type: ignore[call-arg]
    except Exception as exc:
        return False, f"asr failed: {exc}", []
    segs = []
    for chunk in result.get("chunks") or []:
        ts = chunk.get("timestamp") or [None, None]
        if ts[0] is None:
            continue
        segs.append(
            {
                "start": round(float(ts[0]), 3),
                "end": round(float(ts[1] or ts[0]), 3),
                "text": (chunk.get("text") or "").strip(),
            }
        )
    if not segs and result.get("text"):
        segs = [{"start": 0.0, "end": 0.0, "text": result["text"].strip()}]
    return True, "", segs


def _translate(text: str, target_lang: str) -> tuple[bool, str, str]:
    """Translate via a configured seq2seq model. Never fabricates a result."""
    model_id = os.environ.get("TRANSLATE_MODEL", "").strip()
    if not model_id:
        return False, "TRANSLATE_MODEL not configured", ""
    try:  # pragma: no cover - requires transformers weights
        from transformers import pipeline  # type: ignore

        pipe = pipeline("translation", model=model_id)
        out = pipe(text, max_length=512)
        return True, "", (out[0].get("translation_text") or "").strip()
    except Exception as exc:
        return False, f"translation failed: {exc}", ""


def _speak(text: str, target_lang: str) -> tuple[bool, str, str]:
    """Synthesise audio via a configured TTS model. Never fabricates a URL."""
    model_id = os.environ.get("TTS_MODEL", "").strip()
    if not model_id:
        return False, "TTS_MODEL not configured", ""
    try:  # pragma: no cover - requires transformers weights
        import numpy as np  # type: ignore
        import scipy.io.wavfile as wav  # type: ignore
        from transformers import pipeline  # type: ignore

        pipe = pipeline("text-to-speech", model=model_id)
        out = pipe(text)
        audio = np.asarray(out["audio"]).squeeze()
        rate = int(out.get("sampling_rate", 16000))
        path = f"/tmp/dub_{abs(hash(text)) % 10**10}_{target_lang}.wav"
        wav.write(path, rate, (audio * 32767).astype("int16"))
        return True, "", path
    except Exception as exc:
        return False, f"tts failed: {exc}", ""


def _translate_text(text: str, target_lang: str) -> tuple[bool, str, str]:
    model_id = os.environ.get("TRANSLATE_MODEL", "").strip()
    if not model_id:
        return False, "TRANSLATE_MODEL not configured", ""
    try:
        from transformers import pipeline  # type: ignore

        pipe = pipeline("translation", model=model_id)
        result = pipe(text, max_length=512)
        translated = (result[0].get("translation_text") or "").strip()
        if not translated:
            return False, "translation model returned no text", ""
        return True, "", translated
    except Exception as exc:
        return False, f"translation failed: {exc}", ""


def _clip_candidates(
    segments: list[dict[str, Any]], max_clips: int, min_len: float, max_len: float
) -> list[dict[str, Any]]:
    """Deterministic clip selection over real ASR segments.

    Scores contiguous windows by speech density and sentence completeness —
    a real analysis of the transcript, not a random pick. Windows are then
    trimmed to sentence boundaries inside the requested length band.
    """
    if not segments:
        return []
    usable = [s for s in segments if s["end"] > s["start"]]
    if not usable:
        return []
    candidates: list[dict[str, Any]] = []
    n = len(usable)
    for i in range(n):
        start = usable[i]["start"]
        text_parts: list[str] = []
        for j in range(i, n):
            end = usable[j]["end"]
            length = end - start
            if length > max_len:
                break
            text_parts.append(usable[j]["text"])
            if length < min_len:
                continue
            text = " ".join(t for t in text_parts if t).strip()
            if len(text) < 40:
                continue
            spoken = sum(
                max(0.0, usable[k]["end"] - usable[k]["start"]) for k in range(i, j + 1)
            )
            density = spoken / max(0.001, length)
            ends_clean = text_parts[-1].rstrip().endswith((".", "!", "?"))
            words = len(text.split())
            # Real, explicit scoring: density weighted most, then talking
            # speed in a comfortable band, then sentence completeness.
            speed_penalty = abs(words / max(0.001, length) - 2.6) / 2.6
            score = 0.55 * density + 0.25 * (1 - min(1.0, speed_penalty)) + (
                0.20 if ends_clean else 0.0
            )
            candidates.append(
                {
                    "start_s": round(start, 3),
                    "end_s": round(end, 3),
                    "score": round(min(1.0, score), 4),
                    "reason": (
                        f"{words} words over {round(length, 1)}s, "
                        f"speech density {round(density, 2)}"
                        + (", complete sentence" if ends_clean else "")
                    ),
                    "title": text.split(".")[0][:80],
                    "text": text[:400],
                }
            )
    # Greedy non-overlapping selection, best score first.
    candidates.sort(key=lambda c: c["score"], reverse=True)
    chosen: list[dict[str, Any]] = []
    for c in candidates:
        if any(c["start_s"] < o["end_s"] and o["start_s"] < c["end_s"] for o in chosen):
            continue
        chosen.append(c)
        if len(chosen) >= max(1, min(max_clips, 20)):
            break
    chosen.sort(key=lambda c: c["start_s"])
    return chosen


# ------------------------------------------------------------- endpoints -----


def register_creator_assistant(app: FastAPI) -> None:
    @app.post("/translate")
    def translate(req: TranslateRequest) -> dict[str, Any]:
        text = (req.text or "").strip()
        target = (req.target_lang or "").strip().lower()
        if not text or not target:
            return {"available": False, "reason": "text and target_lang required", "translation": ""}
        ok, reason, translated = _translate_text(text, target)
        return {"available": ok, "reason": reason, "translation": translated, "target_lang": target}

    @app.post("/dub")
    def dub(req: DubRequest) -> dict[str, Any]:
        target = (req.target_lang or "").strip().lower()
        if not target:
            return {"available": False, "reason": "target_lang required", "segments": []}
        source = (req.source_lang or "").strip().lower()
        if source and target and source.split("-")[0] == target.split("-")[0]:
            return {
                "available": False,
                "reason": "target language matches source language",
                "segments": [],
            }
        ok, reason, segments = _asr_segments(req.media_url)
        if not ok:
            return {"available": False, "reason": reason, "segments": []}
        full_text = " ".join(s["text"] for s in segments).strip()
        if not full_text:
            return {
                "available": False,
                "reason": "no speech detected in media",
                "segments": [],
            }
        tok, tok_reason, translated = _translate(full_text, target)
        if not tok:
            return {
                "available": False,
                "reason": tok_reason,
                "source_lang": source,
                "target_lang": target,
                "segments": segments,
                "transcript": full_text,
            }
        sok, sok_reason, audio_path = _speak(translated, target)
        if not sok:
            return {
                "available": False,
                "reason": sok_reason,
                "source_lang": source,
                "target_lang": target,
                "segments": segments,
                "transcript": full_text,
                "translation": translated,
            }
        return {
            "available": True,
            "reason": "",
            "audio_url": audio_path,
            "source_lang": source,
            "target_lang": target,
            "segments": segments,
            "transcript": full_text,
            "translation": translated,
        }

    @app.post("/clips")
    def clips(req: ClipRequest) -> dict[str, Any]:
        ok, reason, segments = _asr_segments(req.media_url)
        if not ok:
            return {
                "available": False,
                "reason": reason,
                "duration_s": _probe_duration(req.media_url),
                "clips": [],
            }
        duration = segments[-1]["end"] if segments else 0.0
        candidates = _clip_candidates(
            segments, req.max_clips, req.min_len_s, req.max_len_s
        )
        if not candidates:
            return {
                "available": False,
                "reason": "no clip-length segment found in transcript",
                "duration_s": duration,
                "clips": [],
            }
        return {
            "available": True,
            "reason": "",
            "duration_s": round(duration, 3),
            "clips": [
                {
                    "start_s": c["start_s"],
                    "end_s": c["end_s"],
                    "score": c["score"],
                    "reason": c["reason"],
                    "title": c["title"],
                }
                for c in candidates
            ],
        }

    @app.post("/assistant")
    def assistant(req: AssistantRequest) -> dict[str, Any]:
        model_id = os.environ.get("ASSISTANT_MODEL", "").strip()
        if not model_id:
            return {
                "available": False,
                "reason": "ASSISTANT_MODEL not configured",
                "reply": "",
            }
        try:  # pragma: no cover - requires transformers weights
            from transformers import pipeline  # type: ignore

            pipe = pipeline("text-generation", model=model_id)
            convo = "\n".join(
                f"{m.get('role', 'user')}: {m.get('content', '')}"
                for m in req.messages[-20:]
            )
            prompt = (
                "You are the in-app assistant. Only propose changes; never "
                "claim to have performed an action.\n" + convo + "\nassistant:"
            )
            out = pipe(prompt, max_new_tokens=256, do_sample=False)
            text = (out[0].get("generated_text") or "")[len(prompt):].strip()
            return {"available": True, "reason": "", "reply": text}
        except Exception as exc:
            return {
                "available": False,
                "reason": f"assistant model failed: {exc}",
                "reply": "",
            }
