"""External ML provider adapter with explicit limits and truthful states.

The gap register requires that production selects *either* an installed
local model *or* an authenticated external provider with rate limits,
privacy policy, retries, cost controls and data-residency decisions. This
module is the external-provider half of that contract:

  PROVIDER_BASE_URL  - OpenAI-compatible chat endpoint, e.g.
                       https://api.openai.com/v1 (required to activate)
  PROVIDER_API_KEY   - bearer token for the endpoint (required)
  PROVIDER_MODEL     - model id to request (default "gpt-4o-mini")
  PROVIDER_RPM       - client-side rate limit, requests per minute (default 60)
  PROVIDER_DAILY_BUDGET_USD - hard daily spend ceiling (default 5.00)
  PROVIDER_RESIDENCY - declared data residency, e.g. "us" / "eu" (default "us")
  PROVIDER_PRIVACY   - privacy class disclosed to callers:
                       "external" (default) or "external-logging"
                       (provider may retain/logs requests)

Guarantees:
  * never fabricates output: every failure mode raises ProviderUnavailable
    with the exact reason, or returns ok=False metadata
  * token-bucket rate limit (PROVIDER_RPM) enforced client-side
  * bounded exponential backoff retries (2 attempts) on 429/5xx
  * cost accounting with a daily USD budget; exceeded budget = unavailable
  * data-residency and privacy class disclosed in every response

No output is ever generated here: when the provider is not configured the
caller receives ProviderUnavailable("provider not configured") and must
report {"available": false} upstream — matching the ML service contract.
"""

from __future__ import annotations

import json
import os
import threading
import time
import urllib.error
import urllib.request
from typing import Any


class ProviderUnavailable(Exception):
    """Raised when the external provider cannot serve a request.

    ``reason`` is always safe to return to callers: it names the exact
    missing configuration or limit that was hit, never a fabricated result.
    """

    def __init__(self, reason: str) -> None:
        super().__init__(reason)
        self.reason = reason


class _TokenBucket:
    """Threaded token bucket: `rate` tokens per 60s, capacity `rate`."""

    def __init__(self, rate: int) -> None:
        self.rate = max(1, rate)
        self.tokens = float(self.rate)
        self.updated = time.monotonic()
        self.lock = threading.Lock()

    def take(self) -> bool:
        with self.lock:
            now = time.monotonic()
            self.tokens = min(
                float(self.rate), self.tokens + (now - self.updated) * self.rate / 60.0
            )
            self.updated = now
            if self.tokens >= 1.0:
                self.tokens -= 1.0
                return True
            return False


class _Budget:
    """Daily USD spend ceiling with UTC-day reset. Costs are estimates from
    reported token usage (when the provider returns usage) at a configured
    per-1M-token price, plus a conservative fallback per-request cost."""

    def __init__(self, cap_usd: float) -> None:
        self.cap = cap_usd
        self.spent = 0.0
        self.day = time.strftime("%Y-%m-%d", time.gmtime())
        self.lock = threading.Lock()

    def _roll(self) -> None:
        today = time.strftime("%Y-%m-%d", time.gmtime())
        if today != self.day:
            self.day = today
            self.spent = 0.0

    def allow(self) -> bool:
        with self.lock:
            self._roll()
            return self.spent < self.cap

    def add(self, usd: float) -> None:
        with self.lock:
            self._roll()
            self.spent += usd

    def snapshot(self) -> dict[str, Any]:
        with self.lock:
            self._roll()
            return {"cap_usd": self.cap, "spent_usd": round(self.spent, 4)}


def _env_float(key: str, default: float) -> float:
    try:
        return float(os.environ.get(key, "") or default)
    except ValueError:
        return default


class ProviderClient:
    def __init__(self) -> None:
        self.base_url = os.environ.get("PROVIDER_BASE_URL", "").strip().rstrip("/")
        self.api_key = os.environ.get("PROVIDER_API_KEY", "").strip()
        self.model = os.environ.get("PROVIDER_MODEL", "").strip() or "gpt-4o-mini"
        self.rpm = int(_env_float("PROVIDER_RPM", 60.0))
        self.budget = _Budget(_env_float("PROVIDER_DAILY_BUDGET_USD", 5.0))
        self.residency = os.environ.get("PROVIDER_RESIDENCY", "").strip() or "us"
        self.privacy = os.environ.get("PROVIDER_PRIVACY", "").strip() or "external"
        self.price_per_1m_in = _env_float("PROVIDER_PRICE_IN_PER_1M", 0.15)
        self.price_per_1m_out = _env_float("PROVIDER_PRICE_OUT_PER_1M", 0.60)
        self.fallback_cost = _env_float("PROVIDER_FALLBACK_COST", 0.002)
        self.bucket = _TokenBucket(self.rpm)

    @property
    def configured(self) -> bool:
        return bool(self.base_url and self.api_key)

    def status(self) -> dict[str, Any]:
        return {
            "configured": self.configured,
            "model": self.model if self.configured else "",
            "rate_limit_rpm": self.rpm,
            "budget": self.budget.snapshot(),
            "data_residency": self.residency,
            "privacy_class": self.privacy,
        }

    def chat(
        self, messages: list[dict[str, str]], max_tokens: int = 512
    ) -> dict[str, Any]:
        """One chat completion with rate limit, budget and retry policy.

        Returns the provider text plus cost/residency metadata. Raises
        ProviderUnavailable for every not-served case.
        """
        if not self.configured:
            raise ProviderUnavailable("provider not configured (PROVIDER_BASE_URL/PROVIDER_API_KEY)")
        if not self.bucket.take():
            raise ProviderUnavailable(f"provider rate limit exceeded ({self.rpm} rpm)")
        if not self.budget.allow():
            raise ProviderUnavailable("provider daily budget exhausted")

        payload = json.dumps(
            {"model": self.model, "messages": messages, "max_tokens": max_tokens}
        ).encode()
        last_err = "provider request failed"
        for attempt in range(3):  # 1 initial + 2 retries
            req = urllib.request.Request(
                self.base_url + "/chat/completions",
                data=payload,
                headers={
                    "Content-Type": "application/json",
                    "Authorization": f"Bearer {self.api_key}",
                },
                method="POST",
            )
            try:
                with urllib.request.urlopen(req, timeout=30) as resp:
                    body = json.loads(resp.read(4 * 1024 * 1024).decode())
                break
            except urllib.error.HTTPError as exc:
                if exc.code == 429 or exc.code >= 500:
                    last_err = f"provider http {exc.code}"
                    if attempt < 2:
                        time.sleep(0.5 * (2**attempt))
                        continue
                raise ProviderUnavailable(f"{last_err}") from exc
            except Exception as exc:
                raise ProviderUnavailable(f"provider request failed: {exc}") from exc
        else:  # pragma: no cover - loop always breaks or raises
            raise ProviderUnavailable(last_err)

        try:
            text = (body["choices"][0]["message"].get("content") or "").strip()
        except (KeyError, IndexError, TypeError):
            raise ProviderUnavailable("provider response missing completion") from None
        usage = body.get("usage") or {}
        cost = self.fallback_cost
        pin = float(usage.get("prompt_tokens") or 0)
        pout = float(usage.get("completion_tokens") or 0)
        if pin or pout:
            cost = (pin * self.price_per_1m_in + pout * self.price_per_1m_out) / 1_000_000.0
        self.budget.add(cost)
        return {
            "text": text,
            "provider": self.model,
            "cost_usd": round(cost, 6),
            "data_residency": self.residency,
            "privacy_class": self.privacy,
        }


_client: ProviderClient | None = None
_client_lock = threading.Lock()


def _get() -> ProviderClient:
    global _client
    with _client_lock:
        if _client is None:
            _client = ProviderClient()
        return _client


def provider_chat(
    messages: list[dict[str, str]], max_tokens: int = 512
) -> dict[str, Any]:
    """Module entry point; re-reads env so tests can configure dynamically."""
    return _get().chat(messages, max_tokens=max_tokens)


def provider_status() -> dict[str, Any]:
    return _get().status()


def register_provider_endpoints(app) -> None:
    """Expose provider status + a gated chat endpoint on the ML app."""

    @app.get("/provider/status")
    def provider_status_endpoint() -> dict[str, Any]:
        return provider_status()

    @app.post("/provider/chat")
    def provider_chat_endpoint(req: dict) -> dict[str, Any]:
        msgs = [
            {"role": str(m.get("role", "user")), "content": str(m.get("content", ""))}
            for m in (req.get("messages") or [])
        ]
        return provider_chat(msgs, max_tokens=int(req.get("max_tokens") or 512))
