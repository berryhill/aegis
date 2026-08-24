#!/usr/bin/env python3
"""Contract tests for the installed fleet-control acceptance proof."""

from pathlib import Path
import unittest


REPO = Path(__file__).resolve().parents[1]


class InstalledFleetVerticalContract(unittest.TestCase):
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
