"""Fail-closed oracles for the real Chrome guided Doer proof."""
import copy
import json
import pathlib
import tempfile
import unittest
from html.parser import HTMLParser
from types import SimpleNamespace
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


class ComposerInputTests(unittest.TestCase):
    def test_native_preview_required_controls_are_filled(self):
        # Read the actual native form, not a duplicate fixture field list.
        class RequiredControls(HTMLParser):
            def __init__(self):
                super().__init__()
                self.names = []

            def handle_starttag(self, tag, attrs):
                attrs = dict(attrs)
                if tag in ("input", "textarea", "select") and "required" in attrs:
                    self.names.append(attrs["name"])

        source = (pathlib.Path(__file__).resolve().parents[1] / "web/console/components.templ").read_text()
        composer = source.split("templ DoerComposerPage(", 1)[1].split("</form>", 1)[0]
        parser = RequiredControls()
        parser.feed(composer)
        self.assertIn("name", parser.names)
        fields = {name: "" for name in parser.names}
        fields.update({name: "server-owned" for name in journey.OWNED})
        browser = mock.Mock()
        browser.evaluate.return_value = "proof-agent"
        submitted = []

        class PreviewReached(Exception):
            pass

        def click(_browser, selector):
            if selector == 'select[name="publisher_id"]':
                fields["publisher_id"] = "proof-agent"
            elif "button[type=submit]" in selector:
                missing = [name for name in parser.names if not fields.get(name)]
                self.assertEqual(missing, [], "native required controls would block preview")
                submitted.append(True)
                raise PreviewReached()

        def replace_text(_browser, selector, value):
            fields[selector.split('"')[1]] = value

        driver = SimpleNamespace(require=require, wait_for=mock.Mock(), navigate=mock.Mock(),
                                 click=click, key=mock.Mock(), replace_text=replace_text)
        fields["assert_text"] = "bytes"
        with tempfile.TemporaryDirectory() as root:
            workspace = pathlib.Path(root) / "installed"
            (workspace / "doer-workspace").mkdir(parents=True)
            with mock.patch.object(journey, "values", side_effect=lambda b, s, names: {n: fields.get(n) for n in names}), \
                 mock.patch.object(journey, "capture", return_value=[]), \
                 mock.patch.object(journey.time, "sleep"), self.assertRaises(PreviewReached):
                journey.run_journey(browser, "http://127.0.0.1", workspace, driver)
        self.assertEqual(submitted, [True])
        self.assertNotEqual(fields["name"], fields["loop_id"])

    def test_retention_allowlist_includes_explicit_loop_name(self):
        # Both return-to-composer and setup retention comparisons use INPUTS.
        self.assertIn("name", journey.INPUTS)
        self.assertNotIn("name", journey.OWNED)


if __name__ == "__main__":
    unittest.main()
