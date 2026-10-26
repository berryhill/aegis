#!/usr/bin/env python3
"""Contract tests for the installed fleet-control acceptance proof."""

from pathlib import Path
import ast
import json
import runpy
import subprocess
import sys
import tempfile
import unittest
from unittest import mock

from scripts import console_browser_test as browser_proof


REPO = Path(__file__).resolve().parents[1]


class InstalledFleetVerticalContract(unittest.TestCase):
    def exercise_console_port(self, supplied, *, occupied=False, restart=False):
        """Run real main setup, stopping before any product operation."""
        proof = runpy.run_path(str(REPO / "scripts/verify-installed-fleet-vertical.py"))

        class ProductBoundary(Exception):
            pass

        environment = {"AEGIS_INSTALLED_CONSOLE_BROWSER": "mock-browser",
                       "AEGIS_INSTALLED_CONSOLE_REPO": str(REPO)}
        if supplied is not None:
            environment["AEGIS_ACCEPTANCE_PORT"] = supplied
        with tempfile.TemporaryDirectory(dir=REPO) as directory, \
                mock.patch.dict("os.environ", environment, clear=True), \
                mock.patch.object(sys, "argv", ["proof", sys.executable, directory]), \
                mock.patch("socket.socket") as sockets, \
                mock.patch("subprocess.run") as run, \
                mock.patch("subprocess.Popen") as popen:
            probe = sockets.return_value.__enter__.return_value
            probe.getsockname.return_value = ("127.0.0.1", 47965)
            if occupied:
                probe.bind.side_effect = OSError("address already in use")
            # First call is mocked auth initialization; next is charter validation.
            run.side_effect = [mock.Mock(), ProductBoundary()]
            try:
                proof["main"]()
            except SystemExit:
                run.assert_not_called()
                popen.assert_not_called()
                if occupied:
                    assert supplied is not None
                    probe.bind.assert_called_once_with(("127.0.0.1", int(supplied)))
                    probe.setsockopt.assert_not_called()
                    probe.getsockname.assert_not_called()
                else:
                    sockets.assert_not_called()
                raise
            except ProductBoundary as exc:
                trace = exc.__traceback__
                while trace is not None and trace.tb_frame.f_code.co_name != "main":
                    trace = trace.tb_next
                assert trace is not None
                state = trace.tb_frame.f_locals
            else:
                self.fail("main did not reach the product boundary")
            config = Path(directory, "aegis.yaml").read_text()
            if restart:
                server = popen.return_value
                server.poll.return_value = None
                server.wait.return_value = 0
                with mock.patch("urllib.request.urlopen") as urlopen, \
                        mock.patch.dict(state["browser_phase"].__globals__,
                                        {"run_owned": mock.Mock(return_value=mock.Mock(returncode=0, stdout=b""))}), \
                        mock.patch.object(sys, "stdout"):
                    state["browser_phase"]("registration-readback")
                    state["browser_phase"]("reconstruction")
                self.assertEqual(urlopen.call_args_list,
                                 [mock.call(state["console_origin"] + "/console", timeout=1)] * 2)
                self.assertEqual(popen.call_count, 2)
                for call in popen.call_args_list:
                    self.assertEqual(call.args[0][-1], "serve")
                    self.assertEqual(call.args[0][2], str(Path(directory, "aegis.yaml")))
                self.assertEqual(server.terminate.call_count, 2)
                self.assertEqual(server.wait.call_count, 2)
                server.kill.assert_not_called()
            return state["console_port"], config, probe.bind.call_args_list

    def test_helper_port_boundaries_wire_main_config_and_restart(self) -> None:
        for port in (8450, 8469):
            with self.subTest(port=port):
                selected, config, binds = self.exercise_console_port(str(port), restart=True)
                self.assertEqual(selected, port)
                self.assertIn(f"listen: 127.0.0.1:{port}\n", config)
                self.assertIn(f"origin: http://127.0.0.1:{port}\n", config)
                self.assertIn(f".installed-console-{port}.sock", config)
                self.assertEqual(binds, [mock.call(("127.0.0.1", port))])

    def test_absent_helper_port_preserves_hermetic_ephemeral_fallback(self) -> None:
        selected, config, binds = self.exercise_console_port(None)
        self.assertEqual(selected, 47965)
        self.assertIn("listen: 127.0.0.1:47965\n", config)
        self.assertEqual(binds, [mock.call(("127.0.0.1", 0))])

    def test_invalid_helper_port_denied_before_socket_or_process(self) -> None:
        for value in ("", "8448", "8098", "8449", "8470", "0", "65536", "-8450",
                      "+8450", "08450", " 8450", "8450 ", "8450\n", "8450.0",
                      "８４５０", "٨٤٥٠", "8_450", "abc"):
            with self.subTest(value=value):
                with self.assertRaisesRegex(SystemExit, "AEGIS_ACCEPTANCE_PORT"):
                    self.exercise_console_port(value)

    def test_occupied_helper_port_fails_without_fallback_or_takeover(self) -> None:
        with self.assertRaisesRegex(SystemExit, "console port unavailable"):
            self.exercise_console_port("8450", occupied=True)

    def test_gateway_answers_empty_tools_preflight_before_session(self) -> None:
        tree = ast.parse((REPO / "scripts/verify-installed-fleet-vertical.py").read_text())
        gateway_source = next(node.args[0] for node in ast.walk(tree)
                              if isinstance(node, ast.Call)
                              and isinstance(node.func, ast.Attribute)
                              and isinstance(node.func.value, ast.Name)
                              and node.func.value.id == "gateway"
                              and node.func.attr == "write_text")
        with tempfile.TemporaryDirectory(dir=REPO) as directory:
            root = Path(directory)
            source = eval(compile(ast.Expression(gateway_source), "gateway-fixture", "eval"),
                          {"gateway_log": root / "invoked"})
            gateway = root / "gateway"
            gateway.write_text(source)
            requests = [
                {"jsonrpc": "2.0", "id": "aegis-tools", "method": "tools.show", "params": {}},
                {"jsonrpc": "2.0", "id": "create", "method": "session.create", "params": {}},
                {"jsonrpc": "2.0", "id": "prompt", "method": "prompt.submit", "params": {}},
            ]
            result = subprocess.run(["sh", str(gateway)],
                                    input="".join(json.dumps(r, separators=(",", ":")) + "\n" for r in requests),
                                    text=True, capture_output=True, timeout=5)
            self.assertEqual(result.returncode, 0, result.stderr)
            messages = [json.loads(line) for line in result.stdout.splitlines()]
            self.assertEqual(messages[1], {"jsonrpc": "2.0", "id": "aegis-tools",
                                          "result": {"total": 0, "sections": []}})
            self.assertEqual(messages[2]["id"], "create")
            self.assertEqual(messages[3]["id"], "prompt")
            self.assertEqual(messages[-1]["params"]["type"], "message.complete")

    def test_runpy_loads_owned_sibling_from_unrelated_cwd(self) -> None:
        # A fresh isolated interpreter excludes repository/PYTHONPATH imports.
        script = REPO / "scripts/verify-installed-fleet-vertical.py"
        with tempfile.TemporaryDirectory() as cwd:
            result = subprocess.run(
                [sys.executable, "-I", "-c",
                 "import runpy, sys; before = list(sys.path); "
                 "proof = runpy.run_path(sys.argv[1]); "
                 "assert callable(proof['run_owned']); "
                 "assert proof['geometry_revision'](2)['revision'] == 1; "
                 "assert sys.path == before", str(script)],
                cwd=cwd, capture_output=True, text=True, timeout=10,
            )
        self.assertEqual(result.returncode, 0, result.stderr)

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
        self.assertLess(browser.index('"Definition details opened visible Queue evidence inspector"'),
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
