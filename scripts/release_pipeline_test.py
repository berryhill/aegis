"""Load-bearing workflow/evidence topology contract; no third-party YAML parser."""
import importlib.util
import json
import os
from pathlib import Path
import re
import unittest
from unittest import mock

ROOT = Path(__file__).resolve().parent.parent


def load(name, filename):
    spec = importlib.util.spec_from_file_location(name, ROOT / "scripts" / filename)
    assert spec is not None and spec.loader is not None
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


class PipelineTests(unittest.TestCase):
    def setUp(self):
        self.ci = (ROOT / ".github/workflows/ci.yml").read_text()
        self.release = (ROOT / ".github/workflows/release.yml").read_text()
        text = self.ci.split("jobs:\n", 1)[1]
        self.jobs = {match[1]: match[2] for match in re.finditer(r"^  ([a-z-]+):\n(.*?)(?=^  [a-z-]+:\n|\Z)", text, re.M | re.S)}

    def test_complete_shards_and_evidence_names_agree(self):
        shard = load("shards", "verify-go-shard.py")
        evidence = load("evidence", "verify-ci-evidence.py")
        matrix = self.jobs["go-shards"]
        rows = re.findall(r'^          - \{mode: (normal|race), group: ([a-z-]+)\}$', matrix, re.M)
        expected = [(mode, group) for mode, lanes in shard.BUNDLES.items() for group in lanes]
        self.assertEqual(rows, expected)
        self.assertEqual(matrix.count('          - '), len(rows))
        # No Cartesian axes, dynamic include/exclude or generated child matrix.
        matrix_body = matrix.split('      matrix:\n', 1)[1].split('    runs-on:', 1)[0]
        self.assertEqual([line for line in matrix_body.splitlines() if line.strip() and not line.lstrip().startswith('#')],
                         ['        include:'] + [f'          - {{mode: {mode}, group: {group}}}' for mode, group in rows])
        self.assertIn('fail-fast: false', matrix)
        self.assertIn('--bundle "$SHARD_GROUP"', matrix)
        names = {re.search(r'^    name: (.+)$', block, re.M)[1] for key, block in self.jobs.items() if key != 'go-shards'}
        names.update(f'Go {mode} / {group}' for mode, group in rows)
        self.assertEqual(names, evidence.REQUIRED_JOBS)
        self.assertEqual(len(names), len(self.jobs) - 1 + len(rows))
        self.assertLessEqual(len(names), 27, 'expanded ordinary CI job ceiling')
        self.assertIn('-count=1', (ROOT / 'scripts/verify-go-shard.py').read_text())

    def test_aggregate_rejects_every_nonsuccess_and_missing_gate(self):
        gate = self.jobs["release-readiness"]
        expected = {"repository-checks", "go-shards", "installed-proof", "vet", "authority-denial-matrix", "vulnerability-scan", "race"}
        needs = set(re.search(r"needs: \[(.+)\]", gate)[1].split(", "))
        self.assertEqual(needs, expected)
        self.assertIn("always()", gate)
        body = gate.split("python3 - <<'PY'\n", 1)[1].rsplit("          PY", 1)[0]
        code = "\n".join(line[10:] for line in body.splitlines())
        success = {name: {"result": "success"} for name in expected}
        with mock.patch.dict(os.environ, NEEDS_JSON=json.dumps(success)):
            exec(code, {})
        for status in ("failure", "cancelled", "skipped", "pending"):
            for name in expected:
                results = {**success, name: {"result": status}}
                with self.subTest(status=status, name=name), mock.patch.dict(os.environ, NEEDS_JSON=json.dumps(results)):
                    with self.assertRaises(AssertionError):
                        exec(code, {})
        success.pop("vet")
        with mock.patch.dict(os.environ, NEEDS_JSON=json.dumps(success)), self.assertRaises(AssertionError):
            exec(code, {})
        success['unknown'] = {'result': 'success'}
        with mock.patch.dict(os.environ, NEEDS_JSON=json.dumps(success)), self.assertRaises(AssertionError):
            exec(code, {})

    def test_race_aggregate_requires_entire_matrix_success(self):
        gate = self.jobs['race']
        self.assertIn('always()', gate)
        self.assertIn('needs: [go-shards]', gate)
        self.assertIn('SHARDS_RESULT: ${{ needs.go-shards.result }}', gate)
        self.assertIn('run: test "$SHARDS_RESULT" = success', gate)

    def test_all_security_tests_are_executed_and_no_source_gate_removed(self):
        make = (ROOT / "Makefile").read_text()
        stages = make.split("verify-repository-stages:\n", 1)[1].split("\nrelease-readiness:", 1)[0]
        for name in ("verify_ci_evidence_test.py", "verify_release_publication_test.py", "fast_release_test.py", "verify_go_shard_test.py", "release_pipeline_test.py"):
            self.assertIn(name, stages)
        full = make.split("verify-stages:\n", 1)[1].split("\nverify-repository:", 1)[0]
        for command in ("verify-repository-stages", "verify-installed-mvi.sh", "python3 scripts/verify-go-shard.py --all", "python3 scripts/verify-go-shard.py --all --race", "go vet ./...", "$(GOVULNCHECK)"):
            self.assertIn(command, full)

    def test_no_duplicate_source_workflow_and_no_privileged_pr_execution(self):
        events = self.ci.split("jobs:\n", 1)[0]
        self.assertIn("push:\n    branches: [main]", events)
        self.assertIn("pull_request:", events)
        self.assertNotIn("tags:", events)
        self.assertNotIn("pull_request_target", self.ci)
        self.assertNotIn("make release-readiness", self.ci)
        for name, block in self.jobs.items():
            if name == "go-shards":
                # Measured slow race groups include setup plus the complete
                # package run. Every other matrix/job allowance remains ten.
                self.assertIn("timeout-minutes: ${{ matrix.mode == 'race' && (matrix.group == 'api-a' || matrix.group == 'api-b') && 16 || 10 }}", block)
            else:
                self.assertIn("timeout-minutes: 10", block)
        self.assertNotIn("MemoryMax=", self.ci)
        self.assertNotIn("CPUQuota=", self.ci)

    def test_all_workflows_triggers_and_children_are_closed(self):
        workflows = {p.name: p.read_text() for p in (ROOT / '.github/workflows').iterdir()
                     if p.suffix in ('.yml', '.yaml')}
        self.assertEqual(set(workflows), {'ci.yml', 'release.yml'},
                         'new workflows require ordinary-event volume/trigger review')
        self.assertEqual(self.ci.split('on:\n', 1)[1].split('\npermissions:', 1)[0].strip(),
                         'push:\n    branches: [main]\n  pull_request:')
        self.assertEqual(self.release.split('on:\n', 1)[1].split('\npermissions:', 1)[0].strip(),
                         'push:\n    tags: ["v*"]')
        for name, text in workflows.items():
            # There are no reusable jobs or composite/local/remote child actions.
            # Reject unknown topology instead of undercounting hidden fan-out.
            uses = re.findall(r'^\s+(?:- )?uses: (.+)$', text, re.M)
            self.assertTrue(uses, name)
            self.assertLessEqual(set(uses), {'actions/checkout@v4', 'actions/setup-go@v6'})
            self.assertNotRegex(text, r'^    uses:', name)
            for child in ('workflow_call', 'workflow_run', 'workflow_dispatch', 'repository_dispatch',
                          'gh workflow', '/dispatches', 'actions/github-script', 'strategy:'):
                if child == 'strategy:' and name == 'ci.yml':
                    self.assertEqual(text.count(child), 1)
                else:
                    self.assertNotIn(child, text, name)
        self.assertEqual(set(self.jobs), {'repository-checks', 'go-shards', 'installed-proof',
                                        'vet', 'authority-denial-matrix', 'vulnerability-scan',
                                        'race', 'release-readiness'})

    def test_tag_publisher_consumes_parent_and_tests_final_bytes_once(self):
        self.assertIn("scripts/release-readiness-mode.py . prepared", self.release)
        self.assertIn('scripts/verify-ci-evidence.py "$(git rev-parse HEAD^)"', self.release)
        self.assertNotIn("make verify", self.release)
        self.assertEqual(self.release.count("./scripts/verify-installed-mvi.sh"), 1)
        self.assertLess(self.release.index("verify-ci-evidence.py"), self.release.index("verify-installed-mvi.sh"))
        self.assertLess(self.release.index("verify-installed-mvi.sh"), self.release.index("gh release create"))
        self.assertLess(self.release.index("gh release create"), self.release.index("verify-release-publication.py"))
        self.assertIn("--verify-tag", self.release)
        self.assertIn("fetch-depth: 2", self.release)
        self.assertIn("timeout-minutes: 10", self.release)


if __name__ == "__main__":
    unittest.main()
