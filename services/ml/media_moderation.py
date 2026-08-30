"""ChatApp ML media moderation: known-bad content matching.

Fetches media over the internal network, computes an exact SHA-256 and a
64-bit perceptual dHash (Pillow when installed), and matches against the
blocked-hash list supplied by the API. dHash matching catches re-encoded or
resized copies of blocked media; SHA-256 catches byte-identical reuploads.
Pure-stdlib fallback (SHA-256 only) keeps the endpoint functional without
Pillow.
"""

import hashlib
import urllib.request

from fastapi import FastAPI
from pydantic import BaseModel, Field

try:
    from PIL import Image  # type: ignore

    _HAS_PIL = True
except Exception:
    _HAS_PIL = False


def dhash64(data: bytes) -> str:
    """64-bit difference hash; empty string when Pillow is unavailable."""
    if not _HAS_PIL:
        return ""
    import io

    try:
        img = Image.open(io.BytesIO(data)).convert("L").resize((9, 8))
        px = list(img.getdata())
    except Exception:
        return ""
    bits = 0
    for row in range(8):
        for col in range(8):
            bits = (bits << 1) | (1 if px[row * 9 + col] > px[row * 9 + col + 1] else 0)
    return f"{bits:016x}"


def hamming_hex(a: str, b: str) -> int:
    if len(a) != 16 or len(b) != 16:
        return 64
    return bin(int(a, 16) ^ int(b, 16)).count("1")


def register_media_moderation(app: FastAPI) -> None:
    class MediaModerateRequest(BaseModel):
        media_urls: list[str] = Field(default_factory=list, max_length=10)
        blocked_sha256: list[str] = Field(default_factory=list)
        blocked_dhash: list[str] = Field(default_factory=list)
        dhash_threshold: int = 10  # hamming distance; 64-bit hash

    @app.post("/moderate-media")
    def moderate_media(req: MediaModerateRequest) -> dict[str, object]:
        blocked_sha = set(req.blocked_sha256)
        blocked_dh = [h for h in req.blocked_dhash if len(h) == 16]
        results = []
        decision = "allow"
        for url in req.media_urls:
            entry: dict[str, object] = {"url": url, "sha256": "", "dhash": "", "match": ""}
            try:
                with urllib.request.urlopen(url, timeout=15) as resp:
                    data = resp.read(64 * 1024 * 1024)
            except Exception as exc:
                entry["match"] = f"fetch_failed: {exc}"
                results.append(entry)
                continue
            sha = hashlib.sha256(data).hexdigest()
            dh = dhash64(data)
            entry["sha256"] = sha
            entry["dhash"] = dh
            if sha in blocked_sha:
                entry["match"] = "sha256"
                decision = "block"
            elif dh:
                for bad in blocked_dh:
                    if hamming_hex(dh, bad) <= req.dhash_threshold:
                        entry["match"] = f"dhash:{bad}"
                        decision = "block"
                        break
            results.append(entry)
        return {"decision": decision, "results": results}
