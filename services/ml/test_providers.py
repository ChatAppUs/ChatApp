"""Tests for the external ML provider adapter (no network access)."""

import importlib
import os
import sys
import unittest
from unittest import mock

sys.path.insert(0, os.path.dirname(__file__))

import providers  # noqa: E402


class ProviderAdapterTests(unittest.TestCase):
    def _client(self, **env):
        base = {
            "PROVIDER_BASE_URL": "https://example.com/v1",
            "PROVIDER_API_KEY": "k" * 20,
            "PROVIDER_RPM": "60",
            "PROVIDER_DAILY_BUDGET_USD": "0.01",
            "PROVIDER_RESIDENCY": "eu",
        }
        base.update(env)
        with mock.patch.dict(os.environ, base, clear=False):
            return providers.ProviderClient()

    def test_unconfigured_reports_reason(self):
        with mock.patch.dict(os.environ, {"PROVIDER_BASE_URL": "", "PROVIDER_API_KEY": ""}):
            c = providers.ProviderClient()
            with self.assertRaises(providers.ProviderUnavailable) as ctx:
                c.chat([{"role": "user", "content": "hi"}])
            self.assertIn("not configured", ctx.exception.reason)

    def test_rate_limit_blocks(self):
        c = self._client(PROVIDER_RPM="1")
        good = mock.Mock()
        good.read = lambda n=-1: b'{"choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":10,"completion_tokens":5}}'
        ctx = mock.Mock(__enter__=mock.Mock(return_value=good), __exit__=mock.Mock(return_value=False))
        with mock.patch("providers.urllib.request.urlopen", return_value=ctx):
            out = c.chat([{"role": "user", "content": "hi"}])
            self.assertEqual(out["text"], "ok")
        with self.assertRaises(providers.ProviderUnavailable) as ctx2:
            c.chat([{"role": "user", "content": "hi"}])
        self.assertIn("rate limit", ctx2.exception.reason)

    def test_budget_exhausted_is_unavailable(self):
        c = self._client(PROVIDER_DAILY_BUDGET_USD="0.000001", PROVIDER_PRICE_IN_PER_1M="10000")
        good = mock.Mock()
        good.read = lambda n=-1: b'{"choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":1000000,"completion_tokens":0}}'
        ctx = mock.Mock(__enter__=mock.Mock(return_value=good), __exit__=mock.Mock(return_value=False))
        with mock.patch("providers.urllib.request.urlopen", return_value=ctx):
            c.chat([{"role": "user", "content": "hi"}])
        with self.assertRaises(providers.ProviderUnavailable) as ctx2:
            c.chat([{"role": "user", "content": "hi"}])
        self.assertIn("budget", ctx2.exception.reason)

    def test_retries_on_500_then_gives_truthful_reason(self):
        c = self._client()
        err = mock.Mock()
        err.code = 500
        with mock.patch("providers.urllib.request.urlopen", side_effect=providers.urllib.error.HTTPError("u", 500, "e", None, None)):
            with self.assertRaises(providers.ProviderUnavailable) as ctx:
                c.chat([{"role": "user", "content": "hi"}])
            self.assertIn("500", ctx.exception.reason)

    def test_metadata_discloses_residency(self):
        c = self._client()
        good = mock.Mock()
        good.read = lambda n=-1: b'{"choices":[{"message":{"content":"bonjour"}}],"usage":{"prompt_tokens":10,"completion_tokens":5}}'
        ctx = mock.Mock(__enter__=mock.Mock(return_value=good), __exit__=mock.Mock(return_value=False))
        with mock.patch("providers.urllib.request.urlopen", return_value=ctx):
            out = c.chat([{"role": "user", "content": "hi"}])
        self.assertEqual(out["data_residency"], "eu")
        self.assertIn("privacy_class", out)
        self.assertGreater(out["cost_usd"], 0)

    def test_status_shape(self):
        s = self._client().status()
        for key in ("configured", "rate_limit_rpm", "budget", "data_residency", "privacy_class"):
            self.assertIn(key, s)
        self.assertTrue(s["configured"])

    def test_module_entry_reads_env(self):
        with mock.patch.dict(os.environ, {"PROVIDER_BASE_URL": "", "PROVIDER_API_KEY": ""}):
            providers._client = None  # force re-read
            with self.assertRaises(providers.ProviderUnavailable) as ctx:
                providers.provider_chat([{"role": "user", "content": "hi"}])
            self.assertIn("not configured", ctx.exception.reason)
        providers._client = None


if __name__ == "__main__":
    unittest.main()
