"""Adversarial source-contract checks for the reviewed fragment exception."""
import pathlib
import unittest
from console_security_test import bounded_navigation_source


class FragmentContractTest(unittest.TestCase):
    def setUp(self):
        self.source = (pathlib.Path(__file__).resolve().parents[1] / "web/console/navigation.js").read_text()

    def test_exact_reviewed_adapter_only(self):
        remainder = bounded_navigation_source(self.source)
        self.assertNotIn("location.hash", remainder)
        self.assertNotIn("location.replace", remainder)

    def test_unreviewed_fragment_uses_rejected(self):
        for extra in ('location.hash = "#/agents/other";',
                      'location.replace("https://evil.invalid");'):
            with self.subTest(extra=extra), self.assertRaises(AssertionError):
                bounded_navigation_source(self.source + "\n" + extra)

    def test_missing_or_duplicate_adapters_rejected(self):
        for marker in ('  const resolveGraph = () => {',
                       '  const resolveAgentFragment = () => {',
                       '  const reconcileFragment = () => {'):
            for changed in (self.source.replace(marker, "", 1), self.source + marker):
                with self.subTest(marker=marker), self.assertRaises(AssertionError):
                    bounded_navigation_source(changed)

    def test_security_mutations_rejected(self):
        for old, new in [
            ("key.length > 1024", "key.length > 999999"),
            ("location.hash.length > 12302", "false"),
            ('new URL(location.href)', 'new URL("https://evil.invalid")'),
            ('target.searchParams.set("record_key", key)', 'target.searchParams.set("admin", key)'),
            ('location.replace(target.href)', 'document.body.innerHTML = key'),
            ('location.pathname === "/console/credentials"', 'true'),
            ('location.pathname !== "/console/graphs"', 'false'),
            ('{0,127}', '{0,999999}'),
            ('target.pathname = "/console/agents"', 'target.pathname = "/console/credentials"'),
            ('target.searchParams.delete("revision")', 'target.searchParams.delete("other")'),
        ]:
            with self.subTest(mutation=old):
                self.assertIn(old, self.source)
                with self.assertRaises(AssertionError):
                    bounded_navigation_source(self.source.replace(old, new))


if __name__ == "__main__":
    unittest.main()
