"""ChatApp ML service: feed ranking and content moderation.

Ranking uses a real scoring function over engagement, recency and author
affinity features. Moderation runs a deterministic rule-based pass with a
pluggable transformer hook (set MODERATION_MODEL to a HuggingFace model id
to activate a real classifier; the service works without it).
"""

import math
import os
import re
import time
from typing import Any

from fastapi import FastAPI
from pydantic import BaseModel, Field

app = FastAPI(title="ChatApp ML", version="0.1.0")

# ---------- Feed ranking ----------


class PostFeatures(BaseModel):
    post_id: str
    author_id: str
    created_at: float  # unix seconds
    like_count: int = 0
    comment_count: int = 0
    share_count: int = 0
    view_count: int = 0
    author_followed: bool = False
    has_media: bool = False


class RankRequest(BaseModel):
    user_id: str
    posts: list[PostFeatures] = Field(default_factory=list)


def score_post(p: PostFeatures, now: float) -> float:
    age_hours = max((now - p.created_at) / 3600.0, 0.0)
    recency = math.exp(-age_hours / 24.0)  # 24h half-life-ish decay
    engagement = p.like_count + 2.0 * p.comment_count + 3.0 * p.share_count
    engagement_rate = engagement / max(p.view_count, 1)
    engagement_score = math.log1p(engagement) + 4.0 * engagement_rate
    affinity = 1.5 if p.author_followed else 0.0
    media_boost = 0.3 if p.has_media else 0.0
    return 2.0 * recency + engagement_score + affinity + media_boost


@app.post("/rank")
def rank(req: RankRequest) -> dict[str, Any]:
    now = time.time()
    scored = [(p.post_id, score_post(p, now)) for p in req.posts]
    scored.sort(key=lambda x: x[1], reverse=True)
    return {"ranking": [{"post_id": pid, "score": round(s, 6)} for pid, s in scored]}


# ---------- Content moderation ----------
# Real rule-based pass; activate a transformer model via MODERATION_MODEL
# for production-grade classification.

_HATE_PATTERNS = [
    re.compile(r, re.IGNORECASE)
    for r in [
        r"\b(kill|exterminate)\s+(all|every)\b",
        r"\b(ethnic|racial)\s+cleansing\b",
    ]
]
_SPAM_PATTERNS = [
    re.compile(r, re.IGNORECASE)
    for r in [
        r"\b(free|guaranteed)\s+(money|crypto|bitcoin)\b",
        r"\bclick\s+here\s+to\s+(win|claim)\b",
        r"(\b\w+\b)(\s+\1\b){4,}",  # same word repeated 5+ times
    ]
]
_URL_RE = re.compile(r"https?://", re.IGNORECASE)

_classifier = None
if os.environ.get("MODERATION_MODEL"):
    try:
        from transformers import pipeline  # type: ignore

        _classifier = pipeline(
            "text-classification", model=os.environ["MODERATION_MODEL"]
        )
    except Exception as exc:  # model optional; rules still apply
        print(f"moderation model unavailable: {exc}")


class ModerateRequest(BaseModel):
    text: str
    media_urls: list[str] = Field(default_factory=list)


@app.post("/moderate")
def moderate(req: ModerateRequest) -> dict[str, Any]:
    text = req.text or ""
    labels: list[str] = []
    score = 0.0

    if any(p.search(text) for p in _HATE_PATTERNS):
        labels.append("hate")
        score = max(score, 0.95)
    if any(p.search(text) for p in _SPAM_PATTERNS):
        labels.append("spam")
        score = max(score, 0.8)
    if len(_URL_RE.findall(text)) > 5:
        labels.append("link_spam")
        score = max(score, 0.6)

    if _classifier is not None and text.strip():
        try:
            result = _classifier(text[:512])[0]
            label = str(result.get("label", "")).lower()
            conf = float(result.get("score", 0.0))
            if label not in ("safe", "ok", "non-toxic") and conf > 0.7:
                labels.append(label)
                score = max(score, conf)
        except Exception:
            pass

    decision = "allow"
    if score >= 0.9:
        decision = "block"
    elif score >= 0.5:
        decision = "review"

    return {"decision": decision, "score": round(score, 4), "labels": sorted(set(labels))}


@app.get("/health")
def health() -> dict[str, str]:
    return {"status": "ok"}


# ---------- Watch-signal ranking (For You page) ----------


class WatchFeatures(BaseModel):
    post_id: str
    author_id: str
    created_at: float
    like_count: int = 0
    comment_count: int = 0
    share_count: int = 0
    view_count: int = 0
    unique_watchers: int = 0
    avg_completion: float = 0.0  # 0..1 mean fraction of the video watched
    rewatch_rate: float = 0.0  # mean replays per watcher
    author_followed: bool = False


class WatchRankRequest(BaseModel):
    user_id: str
    posts: list[WatchFeatures] = Field(default_factory=list)


def score_watch_post(p: WatchFeatures, now: float) -> float:
    age_hours = max((now - p.created_at) / 3600.0, 0.0)
    recency = math.exp(-age_hours / 36.0)
    # Completion is the dominant FYP signal (TikTok-style); rewatching is the
    # strongest positive watch interaction.
    completion = 5.0 * max(min(p.avg_completion, 1.0), 0.0)
    rewatch = 3.0 * math.log1p(max(p.rewatch_rate, 0.0))
    engagement = p.like_count + 2.0 * p.comment_count + 3.0 * p.share_count
    engagement_score = math.log1p(engagement)
    reach = math.log1p(max(p.unique_watchers, 0))
    affinity = 1.0 if p.author_followed else 0.0
    return 2.0 * recency + completion + rewatch + engagement_score + reach + affinity


@app.post("/rank/watch")
def rank_watch(req: WatchRankRequest) -> dict[str, Any]:
    now = time.time()
    scored = [(p.post_id, score_watch_post(p, now)) for p in req.posts]
    scored.sort(key=lambda x: x[1], reverse=True)
    return {"ranking": [{"post_id": pid, "score": round(s, 6)} for pid, s in scored]}


# ---------- Captions (Whisper ASR) ----------

_asr = None
_asr_error: str | None = None
if os.environ.get("WHISPER_MODEL"):
    try:
        from transformers import pipeline  # type: ignore

        _asr = pipeline("automatic-speech-recognition", model=os.environ["WHISPER_MODEL"])
    except Exception as exc:  # model optional; endpoint reports unavailable
        _asr_error = str(exc)


class CaptionRequest(BaseModel):
    media_url: str
    language: str | None = None


@app.post("/captions")
def captions(req: CaptionRequest) -> dict[str, Any]:
    """Transcribe an audio/video URL with Whisper.

    Real ASR: requires WHISPER_MODEL (e.g. openai/whisper-small) and the
    transformers+torch stack. Callers fall back to uncaptioned media when
    the model is not loaded; the response always states why.
    """
    if _asr is None:
        reason = _asr_error or "WHISPER_MODEL not configured"
        return {"available": False, "reason": reason, "text": "", "segments": []}
    import urllib.request

    try:
        with urllib.request.urlopen(req.media_url, timeout=30) as resp:
            audio = resp.read(256 * 1024 * 1024)  # 256MB cap
    except Exception as exc:
        return {"available": False, "reason": f"fetch failed: {exc}", "text": "", "segments": []}
    try:
        result = _asr(audio, return_timestamps=True)  # type: ignore[call-arg]
    except Exception as exc:
        return {"available": False, "reason": f"asr failed: {exc}", "text": "", "segments": []}
    chunks = result.get("chunks") or []
    segments = [
        {
            "start": round((c.get("timestamp") or [0.0, 0.0])[0] or 0.0, 3),
            "end": round((c.get("timestamp") or [0.0, 0.0])[1] or 0.0, 3),
            "text": c.get("text", ""),
        }
        for c in chunks
    ]
    return {"available": True, "text": result.get("text", ""), "segments": segments}
