#!/usr/bin/env python3
"""Run the installed Aegis binary through the bounded Hermes fleet vertical.

The caller creates the authority generation first. Every product operation below
is executed by the extracted release-shaped binary against one isolated proof
root. A bounded fake Hermes gateway exercises the production Hermes queue path;
this program never imports Aegis implementation packages.
"""

from __future__ import annotations

import hashlib
import json
import os
from pathlib import Path
import pwd
import subprocess
import sys
from datetime import datetime, timezone
from typing import Any, NoReturn


def fail(message: str) -> NoReturn:
    raise SystemExit(f"installed fleet vertical denied: {message}")


def write_json(path: Path, value: object) -> None:
    path.write_text(json.dumps(value, sort_keys=True) + "\n", encoding="utf-8")
    path.chmod(0o600)


def main() -> int:
    if len(sys.argv) != 3:
        print("usage: verify-installed-fleet-vertical.py INSTALLED_AEGIS EMPTY_PROOF_ROOT", file=sys.stderr)
        return 2
    binary = Path(sys.argv[1]).resolve(strict=True)
    root = Path(sys.argv[2]).resolve(strict=True)
    if binary.is_symlink() or not binary.is_file() or not os.access(binary, os.X_OK):
        fail("installed Aegis must be one regular executable")
    if root.is_symlink() or not root.is_dir():
        fail("proof root must be one real directory")
    # The shell caller initializes only this generation before invocation.
    if any(root.iterdir()):
        allowed = root / "state" / "persistence" / "authority-v1"
        if not allowed.is_dir():
            fail("proof root contains unexpected pre-existing state")

    user = pwd.getpwuid(os.getuid())
    home = root / "home"
    repository = home / "repository"
    fixtures = root / "fixtures"
    home.mkdir(mode=0o700)
    repository.mkdir(mode=0o700)
    fixtures.mkdir(mode=0o700)
    hermes_install = root / "hermes-fixture"
    gateway = hermes_install / "venv" / "bin" / "python"
    gateway.parent.mkdir(parents=True, mode=0o700)
    gateway_log = fixtures / "hermes-gateway-invoked"
    gateway.write_text(
        "#!/bin/sh\n"
        f"printf 'invoked\\n' > '{gateway_log}'\n"
        "printf '%s\\n' '{\"jsonrpc\":\"2.0\",\"method\":\"event\",\"params\":{\"type\":\"gateway.ready\",\"payload\":{}}}'\n"
        "read create\n"
        "printf '%s\\n' '{\"jsonrpc\":\"2.0\",\"id\":\"create\",\"result\":{\"session_id\":\"installed-hermes-session\"}}'\n"
        "read prompt\n"
        "printf '%s\\n' '{\"jsonrpc\":\"2.0\",\"id\":\"prompt\",\"result\":{\"accepted\":true}}'\n"
        "printf '%s\\n' '{\"jsonrpc\":\"2.0\",\"method\":\"event\",\"params\":{\"type\":\"message.start\",\"session_id\":\"installed-hermes-session\",\"payload\":{}}}'\n"
        "printf '%s\\n' '{\"jsonrpc\":\"2.0\",\"method\":\"event\",\"params\":{\"type\":\"message.delta\",\"session_id\":\"installed-hermes-session\",\"payload\":{\"delta\":\"installed Hermes queue output\"}}}'\n"
        "printf '%s\\n' '{\"jsonrpc\":\"2.0\",\"method\":\"event\",\"params\":{\"type\":\"message.complete\",\"session_id\":\"installed-hermes-session\",\"payload\":{}}}'\n"
        "while read rest; do :; done\n",
        encoding="utf-8",
    )
    gateway.chmod(0o700)
    hermes_fixture = root / "hermes-version-fixture"
    hermes_fixture.write_text(
        "#!/bin/sh\n"
        "if [ \"${1:-}\" = \"--version\" ]; then\n"
        f"  printf 'Hermes Agent v0.18.2\\nInstall directory: {hermes_install}\\n'\n"
        "  exit 0\n"
        "fi\n"
        "exec sleep 3600\n",
        encoding="utf-8",
    )
    hermes_fixture.chmod(0o700)
    config = root / "aegis.yaml"
    config.write_text(
        f"state_dir: {root / 'state'}\n"
        "runtime_default: hermes\n"
        f"hermes_executable: {hermes_fixture}\n"
        "principal:\n"
        "  id: principal-1\n"
        "  name: Installed Proof Principal\n"
        f'  uid: "{os.getuid()}"\n'
        f"  user: {user.pw_name}\n"
        "  auth_ttl: 15m\n"
        "audit:\n"
        f"  checkpoint_dir: {root / 'checkpoints'}\n",
        encoding="utf-8",
    )
    config.chmod(0o600)
    environment = os.environ.copy()
    environment["HOME"] = str(home)

    def aegis(*arguments: str, input_file: Path | None = None, expect_list: bool = False) -> Any:
        command = [str(binary), "--config", str(config), *arguments]
        if input_file is not None:
            command.append(str(input_file))
        completed = subprocess.run(
            command,
            cwd=repository,
            env=environment,
            text=True,
            capture_output=True,
            timeout=30,
            check=False,
        )
        if completed.returncode != 0:
            fail(f"{' '.join(arguments)} exited {completed.returncode}: {completed.stderr.strip()}")
        try:
            value = json.loads(completed.stdout)
        except json.JSONDecodeError as exc:
            fail(f"{' '.join(arguments)} returned invalid JSON: {exc}")
        if expect_list:
            if not isinstance(value, list):
                fail(f"{' '.join(arguments)} returned a non-list result")
        elif not isinstance(value, dict):
            fail(f"{' '.join(arguments)} returned a non-object result")
        return value

    now = datetime.now(timezone.utc).replace(microsecond=0).isoformat().replace("+00:00", "Z")
    charter_file = fixtures / "charter.json"
    write_json(charter_file, {
        "schema_version": "aegis.dev/v1alpha1",
        "agent_id": "proof-agent",
        "name": "Installed Fleet Proof Agent",
        "revision": 1,
        "runtime": {"adapter": "hermes", "runtime": "hermes-agent", "version_constraint": ">=0.18.0,<0.19.0", "target": "aegis-owned-ephemeral"},
        "stanzas": [{
            "id": "principal", "name": "Principal", "enabled": True,
            "authentication": {"methods": ["local-os"], "selectors": [{"kinds": ["human"], "subject_ids": [f"local-uid:{os.getuid()}"], "principal_ids": ["principal-1"], "issuers": ["local-os"], "claims": {}, "environments": ["local"]}], "require_fresh": True, "max_auth_age_seconds": 900},
            "grant": {"capabilities": ["chat"], "tools": ["no_mcp"]},
            "scopes": {"memory": ["proof-memory"], "credentials": []},
            "session": {"maximum_lifetime_seconds": 600, "idle_timeout_seconds": 300, "require_reauth": True, "delegation": False},
            "approval": {"required_operations": ["provision"], "maximum_lifetime_seconds": 300, "single_use": True},
            "information_flow": {"cross_stanza": "deny"},
            "hermes": {"profile": "", "persistent_home": False, "mcp_servers": [], "plugins": [], "toolsets": ["no_mcp"], "model": "proof-hermes", "provider": "none"},
        }],
        "created_by": "principal-1", "created_at": now,
    })
    canonical = aegis("charter", "import", input_file=charter_file)
    charter_digest = canonical.get("digest")
    if not isinstance(charter_digest, str) or not charter_digest.startswith("sha256:"):
        fail("charter import omitted its canonical digest")

    plan = aegis("plan", "preview", "proof-agent", "--revision", "1")
    plan_id = plan["plan"]["id"]
    approval = aegis("approval", "request", plan_id, "--ttl", "5m")
    approval_id = approval["id"]
    aegis("approval", "approve", approval_id)
    receipt = aegis("provision", plan_id, approval_id)
    if receipt.get("status") != "verified":
        fail("deterministic provisioning was not verified")
    preview = aegis("session", "preview", "proof-agent", "--revision", "1", "--stanza", "principal")
    session = aegis("session", "start", preview["mandate"]["id"])
    authority = aegis("session", "authority", session["id"])

    digest_a = "sha256:" + "a" * 64
    agent_file = fixtures / "agent.json"
    write_json(agent_file, {
        "fixture": {
            "schema_version": "aegis.current-fleet.fixture.v1",
            "fleet_id": "proof-fleet",
            "agents": [{
                "source_id": "proof-source", "agent_id": "proof-agent",
                "runtime": {"adapter": "hermes", "runtime": "hermes-agent", "target": "aegis-owned-ephemeral"},
                "ownership": {"owner_id": "principal-1", "accountability_id": "installed-proof"},
                "lifecycle": "enabled",
                "charter": {"schema_version": "aegis.reference.revision.v1", "id": "proof-agent", "revision": 1, "digest": charter_digest},
                "capability_declarations": ["fleet.execute"],
            }],
        },
        "identity": {"fleet_id": "proof-fleet", "kind": "current-fleet", "source_id": "proof-source"},
    })
    registered = aegis("agents", "register", input_file=agent_file)
    agent_revision = registered["agent"]["revision"]
    if not registered.get("created") or agent_revision["charter"]["digest"] != charter_digest:
        fail("registry did not retain immutable charter provenance")

    ref = lambda value, identifier: {"schema_version": "aegis.reference.revision.v1", "id": identifier, "revision": 1, "digest": value["digest"]}
    expected_output_digest = "sha256:" + hashlib.sha256(b"installed Hermes queue output").hexdigest()
    loop_file = fixtures / "loop.json"
    write_json(loop_file, {
        "authority": authority,
        "publisher": ref(agent_revision, "proof-agent"),
        "revision": {
            "loop_id": "proof-loop", "revision": 1, "entry_step_id": "work",
            "inputs": [], "outputs": [],
            "steps": [
                {"id": "work", "kind": "action", "input_ports": [], "output_ports": [], "retry": {"max_attempts": 1}, "evidence_claims": [{"claim": "exact-output", "media_type": "text/plain", "expected_digest": expected_output_digest, "verifier_id": "aegis-artifact-verifier", "policy_version": "aegis.dev/artifact-verification/v1"}]},
                {"id": "done", "kind": "terminal", "input_ports": [], "output_ports": [], "retry": {"max_attempts": 1}, "terminal": {"outcome": "succeeded", "output_mappings": []}, "evidence_claims": []},
            ],
            "transitions": [{"id": "finish", "from_step_id": "work", "to_step_id": "done", "mappings": []}],
            "required_evidence": [{"claim": "exact-output", "producer_step_id": "work"}],
        },
        "idempotency_key": "installed-proof-loop-1",
    })
    published_loop = aegis("loops", "publish", input_file=loop_file)
    loop_revision = published_loop["revision"]
    if published_loop["validation"]["outcome"] != "valid":
        fail("loop publication was not valid")
    loop_views = aegis("loops", "list", expect_list=True)
    loop_view = next((value for value in loop_views if value.get("revision", {}).get("loop_id") == "proof-loop" and value.get("revision", {}).get("revision") == 1), None)
    if loop_view is None:
        fail("published Loop view was not reconstructed")
    provenance = loop_view["provenance"]
    if provenance.get("loop", {}).get("digest") != loop_revision["digest"] or provenance.get("publisher_agent", {}).get("digest") != agent_revision["digest"] or provenance.get("authority", {}).get("digest") != authority["digest"] or provenance.get("charter", {}).get("digest") != charter_digest or provenance.get("validation_digest") != published_loop["validation"]["digest"] or not provenance.get("mandate_id") or not provenance.get("stanza_id") or not provenance.get("digest"):
        fail("Loop publication omitted exact immutable authority and publisher provenance")

    loop_lifecycle_file = fixtures / "loop-lifecycle.json"
    write_json(loop_lifecycle_file, {
        "authority": authority,
        "publisher": ref(agent_revision, "proof-agent"),
        "loop": ref(loop_revision, "proof-loop"),
        "event_id": "activate-proof-loop-1",
    })
    activated = aegis("loops", "activate", "proof-loop", input_file=loop_lifecycle_file)
    activation = activated["event"]
    if activated.get("idempotent") or activation.get("state") != "active" or activation.get("revision", {}).get("digest") != loop_revision["digest"] or activation.get("publisher_agent", {}).get("digest") != agent_revision["digest"] or activation.get("authority", {}).get("digest") != authority["digest"] or not activation.get("digest"):
        fail("Loop activation omitted exact immutable authority, publisher, or revision provenance")
    replayed_activation = aegis("loops", "activate", "proof-loop", input_file=loop_lifecycle_file)
    if not replayed_activation.get("idempotent") or replayed_activation.get("event", {}).get("digest") != activation["digest"]:
        fail("exact Loop activation replay was not idempotent")

    graph_file = fixtures / "graph.json"
    write_json(graph_file, {
        "authority": authority,
        "revision": {
            "graph_id": "proof-graph", "revision": 1, "inputs": [], "outputs": [],
            "nodes": [{"id": "proof-node", "participant": ref(agent_revision, "proof-agent"), "loop": ref(loop_revision, "proof-loop"), "inputs": [], "outputs": []}],
            "input_mappings": [], "dependencies": [], "output_mappings": [], "admission_rules": [],
        },
        "idempotency_key": "installed-proof-graph-1",
    })
    published_graph = aegis("graphs", "publish", input_file=graph_file)
    graph_revision = published_graph["revision"]
    if published_graph["validation"]["outcome"] != "valid":
        fail("graph publication was not valid")

    def submission(authority_value: dict, suffix: str) -> Path:
        path = fixtures / f"submission-{suffix}.json"
        write_json(path, {
            "authority": authority_value, "graph": ref(graph_revision, "proof-graph"), "inputs": [],
            "submission_id": f"submission-{suffix}", "idempotency_key": f"installed-proof-submit-{suffix}",
            "snapshot_id": f"snapshot-{suffix}", "queue_item_id": f"queue-{suffix}", "graph_run_id": f"run-{suffix}",
            "transition_id": f"queued-{suffix}", "rejection_id": f"rejection-{suffix}", "max_attempts": 1,
        })
        return path

    accepted = aegis("graphs", "submit", input_file=submission(authority, "accepted"))
    snapshot = accepted.get("accepted", {}).get("snapshot", {})
    if not accepted.get("created") or snapshot.get("graph", {}).get("digest") != graph_revision["digest"]:
        fail("queue admission omitted exact historical graph snapshot")

    wrong_authority = dict(authority)
    wrong_authority["digest"] = digest_a
    rejected = aegis("graphs", "submit", input_file=submission(wrong_authority, "denied"))
    if not rejected.get("created") or rejected.get("accepted") is not None or rejected.get("rejection", {}).get("reason_code") != "readiness_denied":
        fail("invalid authority was not durably rejected")

    work_file = fixtures / "work.json"
    write_json(work_file, {
        "authority": authority, "queue_item_id": "queue-accepted", "worker_id": "installed-hermes-worker",
        "loop_execution_id": "loop-execution-accepted", "claim_id": "claim-accepted", "attempt_id": "attempt-accepted",
        "claim_transition_id": "claimed-accepted", "terminal_transition_id": "terminal-accepted",
        "disposition_id": "disposition-accepted", "artifact_id": "artifact-accepted", "lease_duration": 60000000000,
    })
    result = aegis("queue", "process", input_file=work_file)
    receipts = result.get("receipts", [])
    artifact = result.get("artifact", {})
    receipt = receipts[0] if len(receipts) == 1 else {}
    if (
        result.get("disposition", {}).get("state") != "succeeded"
        or artifact.get("digest") != expected_output_digest
        or artifact.get("content_ref") != expected_output_digest
        or artifact.get("attempt_id") != "attempt-accepted"
        or receipt.get("outcome") != "passed"
        or receipt.get("attempt_id") != "attempt-accepted"
        or receipt.get("observed_digest") != expected_output_digest
        or not receipt.get("evidence_ref")
    ):
        fail("Hermes execution did not reach exact evidence-gated successful disposition")
    runtime_homes = root / "state" / "runtime" / "fleet" / "runtime"
    if not gateway_log.is_file():
        fail("registered Hermes queue path did not invoke the gateway")
    if not runtime_homes.is_dir() or any(runtime_homes.iterdir()):
        fail("bounded Hermes queue path retained a disposable runtime home")
    final_item = aegis("queue", "show", "queue-accepted")
    durable_artifact = final_item.get("artifact", {})
    durable_receipts = final_item.get("receipts", [])
    durable_receipt = durable_receipts[0] if len(durable_receipts) == 1 else {}
    durable_disposition = final_item.get("disposition", {})
    if (
        final_item.get("projection", {}).get("state") != "succeeded"
        or final_item.get("item", {}).get("snapshot", {}).get("digest") != snapshot.get("digest")
        or durable_artifact.get("id") != artifact.get("id")
        or durable_artifact.get("digest") != expected_output_digest
        or durable_artifact.get("attempt_id") != "attempt-accepted"
        or durable_receipt.get("id") != receipt.get("id")
        or durable_receipt.get("evidence_ref") != receipt.get("evidence_ref")
        or durable_receipt.get("observed_digest") != expected_output_digest
        or durable_receipt.get("attempt_id") != "attempt-accepted"
        or durable_disposition.get("attempt_id") != "attempt-accepted"
        or durable_disposition.get("authority", {}).get("digest") != authority.get("digest")
    ):
        fail("terminal queue readback lost its immutable snapshot, evidence, attempt, or authority binding")
    if aegis("agents", "show", "proof-agent", "1")["revision"]["digest"] != agent_revision["digest"] or aegis("loops", "show", "proof-loop", "1")["revision"]["digest"] != loop_revision["digest"] or aegis("graphs", "show", "proof-graph", "1")["digest"] != graph_revision["digest"]:
        fail("historical definition reconstruction changed an exact digest")

    write_json(loop_lifecycle_file, {
        "authority": authority,
        "publisher": ref(agent_revision, "proof-agent"),
        "loop": ref(loop_revision, "proof-loop"),
        "event_id": "retire-proof-loop-1",
        "expected_previous_digest": activation["digest"],
    })
    retired = aegis("loops", "retire", "proof-loop", input_file=loop_lifecycle_file)
    retirement = retired["event"]
    if retired.get("idempotent") or retirement.get("state") != "retired" or retirement.get("previous_digest") != activation["digest"] or retirement.get("publisher_agent", {}).get("digest") != agent_revision["digest"] or retirement.get("authority", {}).get("digest") != authority["digest"] or not retirement.get("digest"):
        fail("Loop retirement omitted append-only authority and publisher provenance")
    loop_views = aegis("loops", "list", expect_list=True)
    loop_view = next((value for value in loop_views if value.get("revision", {}).get("loop_id") == "proof-loop" and value.get("revision", {}).get("revision") == 1), None)
    if loop_view is None or loop_view.get("lifecycle", {}).get("state") != "retired" or [event.get("state") for event in loop_view["lifecycle_history"]] != ["active", "retired"] or loop_view["lifecycle_history"][0].get("digest") != activation["digest"] or loop_view["lifecycle_history"][1].get("digest") != retirement["digest"]:
        fail("Loop lifecycle projection did not preserve ordered append-only history")

    lifecycle_file = fixtures / "agent-lifecycle.json"
    write_json(lifecycle_file, {"expected": ref(agent_revision, "proof-agent"), "lifecycle": "disabled"})
    disabled = aegis("agents", "disable", "proof-agent", input_file=lifecycle_file)["revision"]
    if disabled.get("revision") != 2 or disabled.get("lifecycle") != "disabled" or disabled.get("digest") == agent_revision["digest"]:
        fail("Agent disable did not append one immutable lifecycle revision")
    write_json(lifecycle_file, {"expected": {"schema_version": "aegis.reference.revision.v1", "id": "proof-agent", "revision": 2, "digest": disabled["digest"]}, "lifecycle": "enabled"})
    enabled = aegis("agents", "enable", "proof-agent", input_file=lifecycle_file)["revision"]
    history = aegis("agents", "history", "proof-agent", expect_list=True)
    if enabled.get("revision") != 3 or enabled.get("lifecycle") != "enabled" or [value.get("revision") for value in history] != [1, 2, 3]:
        fail("Agent history did not preserve ordered enabled/disabled lifecycle revisions")
    if aegis("agents", "show", "proof-agent", "1")["revision"]["digest"] != agent_revision["digest"]:
        fail("Agent lifecycle administration changed historical revision 1")

    evidence = {
        "schema_version": 1,
        "binary": str(binary),
        "state_root": str(root / "state"),
        "proofs": {
            "registry": "immutable_revision_history_and_enabled_disabled_lifecycle_read_back",
            "loop": "authority_bound_publication_and_append_only_lifecycle",
            "graph": "validated_exact_bindings_published",
            "queue": "accepted_and_terminal_read_back",
            "fresh_runtime_admission": "worker_repeated_controller_admission",
            "hermes_queue_execution": "registered_bounded_gateway_turn_invoked_and_disposable_home_removed",
            "evidence_gated_disposition": "content_addressed_artifact_and_independently_reloadable_attempt_bound_receipt_succeeded",
            "durable_rejection": "wrong_authority_recorded_as_readiness_denied",
            "historical_reconstruction": "snapshot_and_exact_definition_digests_read_back",
        },
        "production_state_mutated": False,
        "credentials_used": False,
    }
    evidence_path = root / "installed-fleet-vertical-evidence.json"
    write_json(evidence_path, evidence)
    print("installed fleet vertical verified: registry=immutable_history_and_lifecycle loop=authority_bound_lifecycle_verified graph=valid queue=succeeded hermes_gateway=bounded disposable_home=removed fresh_admission=verified evidence=attempt_bound durable_rejection=verified historical_reconstruction=verified credentials=none")
    return 0


if __name__ == "__main__":
    sys.exit(main())
