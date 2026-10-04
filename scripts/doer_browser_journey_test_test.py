"""Fail-closed oracles for the real Chrome guided Doer proof."""
import copy
import json
import unittest
from unittest import mock
from scripts import doer_browser_journey_test as journey
from scripts.console_browser_test import require


class ReceiptTests(unittest.TestCase):
    def setUp(self):
        self.form = {"revision": "1", "digest": "sha256:exact", "idempotency_key": "retained-run"}
        self.receipt = {"outcome": "blocked", "facts": {"Reason": journey.BLOCKER, "Operation": "reserved-request"}, "form": self.form.copy(), "queueLinks": 0}

    def test_exact_reserved_blocker(self):
        journey.validate_blocked_receipt(self.receipt, self.form, require)

    def test_rejects_success_denial_missing_reservation_and_changed_keys(self):
        cases = [("outcome", "succeeded"), ("outcome", "rejected"), ("queueLinks", 1)]
        for field, value in cases:
            with self.subTest(field=field, value=value):
                bad = copy.deepcopy(self.receipt)
                bad[field] = value
                with self.assertRaises(RuntimeError):
                    journey.validate_blocked_receipt(bad, self.form, require)
        for field, value in (("Reason", "another_blocker"), ("Operation", "")):
            bad = copy.deepcopy(self.receipt)
            bad["facts"][field] = value
            with self.assertRaises(RuntimeError):
                journey.validate_blocked_receipt(bad, self.form, require)
        for name in self.form:
            bad = copy.deepcopy(self.receipt)
            bad["form"][name] += "-changed"
            with self.subTest(name=name), self.assertRaises(RuntimeError):
                journey.validate_blocked_receipt(bad, self.form, require)

    def test_form_evidence_explicit_allowlist_no_csrf(self):
        browser = mock.Mock()
        browser.evaluate.return_value = self.form
        self.assertEqual(journey.values(browser, "form", self.form), self.form)
        expression = browser.evaluate.call_args.args[0]
        self.assertIn(json.dumps(list(self.form)), expression)
        self.assertNotIn("csrf", expression)
        self.assertNotIn("FormData", expression)


if __name__ == "__main__":
    unittest.main()
