#!/usr/bin/env python3
"""Contract tests for the installed fleet-control acceptance proof."""

from pathlib import Path
import ast
import runpy
import subprocess
import unittest
from unittest import mock

from scripts import console_browser_test as browser_proof


REPO = Path(__file__).resolve().parents[1]


class InstalledFleetVerticalContract(unittest.TestCase):
    def test_registration_default_and_restart_precede_runtime_authority(self) -> None:
        proof = (REPO / "scripts" / "verify-installed-fleet-vertical.py").read_text(encoding="utf-8")
        tree = ast.parse(proof)
        fixtures = [node.value for node in ast.walk(tree) if isinstance(node, ast.Assign)
                    and any(isinstance(target, ast.Name) and target.id == "fleet_fixture" for target in node.targets)]
        self.assertEqual(len(fixtures), 1)
        keys = [key.value for node in ast.walk(fixtures[0]) if isinstance(node, ast.Dict)
                for key in node.keys if isinstance(key, ast.Constant)]
        self.assertNotIn("lifecycle", keys, "explicit enabled would not prove defaulting")
        self.assertIn('agent_revision.get("lifecycle") != "enabled"', proof)
        self.assertIn('restarted_revision != agent_revision or gateway_log.exists()', proof)
        self.assertLess(proof.index('browser_phase("registration-readback")'), proof.index('aegis("provision"'))
        browser = (REPO / "scripts" / "console_browser_test.py").read_text(encoding="utf-8")
        self.assertIn("#record-proof-agent .lifecycle.enabled", browser)
        self.assertIn("#agent-inline-detail .detail-title .lifecycle.enabled", browser)
        self.assertIn('refresh_registration_document(devtools)', browser)
        calls = [node for node in ast.walk(ast.parse(browser)) if isinstance(node, ast.Call)
                 and isinstance(node.func, ast.Name) and node.func.id == "verify_enabled_registration"]
        self.assertEqual(len(calls), 2, "registration and restarted browser must both execute readback")

    def test_queue_evidence_inspector_is_opened_before_readback(self) -> None:
        browser = (REPO / "scripts" / "console_browser_test.py").read_text(encoding="utf-8")
        self.assertLess(browser.index('"visible Queue definition evidence"'),
                        browser.index('"Graph to replacement-page Queue evidence, receipt, and disposition chain"'))

    def test_clean_shutdown_rejects_crash_and_forced_cleanup(self) -> None:
        stop = runpy.run_path(str(REPO / "scripts/verify-installed-fleet-vertical.py"))["stop_server"]
        server = mock.Mock()
        server.wait.return_value = 0
        stop(server)
        server.kill.assert_not_called()
        server.wait.return_value = -15
        with self.assertRaises(SystemExit):
            stop(server)
        server.wait.side_effect = [subprocess.TimeoutExpired("aegis", 5), -9]
        with self.assertRaisesRegex(SystemExit, "forced cleanup"):
            stop(server)
        server.kill.assert_called_once()

    def test_browser_identity_rejects_missing_or_malformed_revision(self) -> None:
        devtools = mock.Mock()
        digest = "sha256:" + "a" * 64
        devtools.evaluate.return_value = "r1 @ " + digest
        self.assertEqual(browser_proof.registration_identity(devtools, "Registry revision"), {"revision": 1, "digest": digest})
        for value in [None, "enabled", "r2 @ " + digest, "r1 @ sha256:bad"]:
            devtools.evaluate.return_value = value
            with self.assertRaises(RuntimeError):
                browser_proof.registration_identity(devtools, "Registry revision")

    def test_refresh_requires_new_completed_document(self) -> None:
        devtools = mock.Mock()
        frame = lambda loader: {"frameTree": {"frame": {"loaderId": loader}}}
        devtools.command.side_effect = [frame("old"), {}, frame("new")]
        devtools.evaluate.return_value = True
        browser_proof.refresh_registration_document(devtools)
        devtools.command.reset_mock(side_effect=True)
        devtools.command.return_value = frame("old")
        with self.assertRaisesRegex(RuntimeError, "new document"):
            browser_proof.refresh_registration_document(devtools, timeout=0.001)
        devtools.command.side_effect = [frame("old"), {}, frame("new")]
        devtools.evaluate.return_value = False
        with self.assertRaisesRegex(RuntimeError, "new document"):
            browser_proof.refresh_registration_document(devtools, timeout=0.001)

    def test_readback_checks_identity_before_and_after_refresh(self) -> None:
        expected = {"revision": 1, "digest": "sha256:" + "a" * 64}
        wrong = {"revision": 1, "digest": "sha256:" + "b" * 64}
        for identities in [[expected, expected], [wrong], [expected, wrong]]:
            with self.subTest(identities=identities), mock.patch.object(browser_proof.time, "sleep"), \
                    mock.patch.object(browser_proof, "navigate"), \
                    mock.patch.object(browser_proof, "click"), mock.patch.object(browser_proof, "wait_for") as wait, \
                    mock.patch.object(browser_proof, "refresh_registration_document") as refresh, \
                    mock.patch.object(browser_proof, "registration_identity", side_effect=identities):
                if identities == [expected, expected]:
                    browser_proof.verify_enabled_registration(mock.Mock(), "http://localhost", expected)
                    self.assertEqual(wait.call_count, 3)
                    refresh.assert_called_once()
                else:
                    with self.assertRaisesRegex(RuntimeError, "differs from reviewed revision"):
                        browser_proof.verify_enabled_registration(mock.Mock(), "http://localhost", expected)

    def test_release_shaped_verifier_runs_repository_owned_vertical(self) -> None:
        verifier = (REPO / "scripts" / "verify-installed-mvi.sh").read_text(encoding="utf-8")
        self.assertIn('verify-installed-fleet-vertical.py" "$install/aegis" "$proof/vertical"', verifier)
        self.assertIn(
            'AEGIS_CANDIDATE_ARCHIVE_EXTRACTED=1 \\\n  "$repo/scripts/verify-installed-console.sh" "$install/aegis" "$proof/console"',
            verifier,
        )
        proof = REPO / "scripts" / "verify-installed-fleet-vertical.py"
        self.assertTrue(proof.is_file(), "repository-owned installed fleet vertical is missing")

    def test_vertical_contract_names_every_required_boundary(self) -> None:
        proof = (REPO / "scripts" / "verify-installed-fleet-vertical.py").read_text(encoding="utf-8")
        for marker in (
            "registry",
            "loop",
            "graph",
            "queue",
            "fresh_runtime_admission",
            "hermes_queue_execution",
            "evidence_gated_disposition",
            "durable_rejection",
            "historical_reconstruction",
        ):
            self.assertIn(marker, proof)

    def test_vertical_runs_from_a_real_repository_child_of_its_isolated_home(self) -> None:
        proof = (REPO / "scripts" / "verify-installed-fleet-vertical.py").read_text(encoding="utf-8")
        self.assertIn('repository = home / "repository"', proof)
        self.assertIn("repository.mkdir(mode=0o700)", proof)
        self.assertIn("cwd=repository", proof)

    def test_hermes_fixture_declares_no_credential_scope_and_real_gateway_protocol(self) -> None:
        proof = (REPO / "scripts" / "verify-installed-fleet-vertical.py").read_text(encoding="utf-8")
        self.assertNotIn('"credentials": ["provider:none"]', proof)
        self.assertIn('"credentials": []', proof)
        self.assertIn("gateway.ready", proof)
        self.assertIn("message.complete", proof)
        self.assertIn('\\"session_id\\":\\"installed-hermes-session\\"', proof)
        self.assertIn("bounded Hermes queue path retained a disposable runtime home", proof)
        self.assertIn('"schema_version": "aegis.current-fleet.fixture.v1"', proof)
        self.assertIn('final_item.get("projection", {}).get("state")', proof)

    def test_loop_fixture_proves_publisher_provenance_and_lifecycle(self) -> None:
        proof = (REPO / "scripts" / "verify-installed-fleet-vertical.py").read_text(encoding="utf-8")
        self.assertIn('"publisher": ref(agent_revision, "proof-agent")', proof)
        self.assertIn('loop_view["provenance"]', proof)
        self.assertIn('aegis("loops", "activate", "proof-loop"', proof)
        self.assertIn('aegis("loops", "retire", "proof-loop"', proof)
        self.assertIn('loop_view["lifecycle_history"]', proof)
        self.assertIn('authority_bound_publication_and_append_only_lifecycle', proof)
        self.assertIn('aegis("loops", "show", "proof-loop", "1")["revision"]["digest"]', proof)


if __name__ == "__main__":
    unittest.main()
