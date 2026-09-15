"""Regression tests for installed browser lifetime evidence; no real credentials."""
import unittest
from unittest import mock

from scripts import console_browser_test as browser


class BrowserLifetimeEvidenceTest(unittest.TestCase):
    def cookie(self, expires=4600):
        return {"name": "aegis-console", "expires": expires, "httpOnly": True,
                "sameSite": "Strict", "path": "/console", "secure": False}

    def inspect(self, cookies):
        devtools = mock.Mock()
        devtools.command.return_value = {"cookies": cookies}
        return browser.verify_default_session_cookie(devtools, "http://127.0.0.1:8443", 1000, 1002)

    def test_accepts_one_hour_and_returns_only_deadline(self):
        self.assertEqual(self.inspect([self.cookie()]), 4600)

    def test_rejects_short_clamped_or_oversized_lifetimes(self):
        for deadline in (1300, 1900, 4598, 4603, 8200):
            with self.subTest(deadline=deadline), self.assertRaises(RuntimeError):
                self.inspect([self.cookie(deadline)])

    def test_missing_ambiguous_or_weakened_cookie_denies(self):
        for cookies in ([], [self.cookie(), self.cookie()],
                        [dict(self.cookie(), httpOnly=False)],
                        [dict(self.cookie(), sameSite="Lax")],
                        [dict(self.cookie(), path="/")]):
            with self.subTest(cookies=cookies), self.assertRaises(RuntimeError):
                self.inspect(cookies)

    def test_https_requires_secure_cookie(self):
        devtools = mock.Mock()
        devtools.command.return_value = {"cookies": [self.cookie()]}
        with self.assertRaises(RuntimeError):
            browser.verify_default_session_cookie(devtools, "https://localhost", 1000, 1002)


if __name__ == "__main__":
    unittest.main()
