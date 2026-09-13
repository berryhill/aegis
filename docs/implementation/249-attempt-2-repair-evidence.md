# Issue 249 — implementation-loop attempt 2 repair evidence

Task: `t_a08be60`; session: `sess_5680390a45d8`; step: `implement_minimally`.
Baseline candidate: `4f50cd611ca96e0914081a49639b77a992fd92bf` on `task-t_a08be60-249`.
Previous packet: `tflp_442f207e8226445f`, SHA-256 `519406301cedb46a42730ac4125221e7c5cf43c080a714702b5d5983c6d976ab`.

## Contract and evidence custody

The domain obligation is truthful, exact-digest-reviewed enabled-by-default registration without runtime authority. The technical repair is limited to current documentation/specifications, bundled advisory skills and their integrity manifest, and the manager's deterministic import-review response. Existing registration decoding, approval checks, canonical revision validation, legacy disabled import recognition, and disabled/retired admission denial remain unchanged from the baseline candidate.

Authenticated profile-scoped FlowStore readback with in-memory fallback disabled succeeded in this attempt. It confirmed the same task/session, ACTIVE implementation cursor, implementation-loop attempt 2, and the exact previous packet ID/hash. Complete canonical outputs were read for diagnosis, security review, launch review, final verification, reporting, publication, and CI. The earlier inability to retrieve them is historical; the adverse findings below are not cleared by successful readback.

## Repaired source findings

- Current README, SECURITY, quickstart, architecture, threat model, slash guidance, and Unreleased changelog no longer promise disabled new imports.
- Manager/MVP/end-to-end specifications and Registry/onboarding skills now describe enabled new imports. The canonical-domain specification explicitly limits defaulting to omitted registration input and preserves strict persisted revisions, explicit disabled/retired values, and existing records.
- Three bundled onboarding evaluation signals were corrected without removing automatic-import, activation, or authority-inheritance denial. Skill file sizes, file digests, per-skill digests, and the bundle digest were recomputed and validated.
- Independent read-only review found stale manager import-review prose in addition to the bundled-skill issues. The response now renders the actual proposal lifecycle and explicitly separates eligibility from runtime readiness/authority, retaining exact confirmation and non-mutating preparation.
- `internal/manager/expertise.go` was inspected: its projection contains no disabled-default promise. No expertise version/digest or certification contract change was required.
- Historical gap wording in the original issue contract is labeled as historical rather than presented as current behavior. Legitimate disabled/retired denial and historical evidence were not replaced.

## Verification performed in this step

- RED: `go test ./internal/managergateway -run TestDegradedManagerTurnServesAuthoritativeProfileIntentsWithoutModel -count=1` failed on the stale disabled message while the returned proposal was enabled.
- GREEN: focused registry, app, command, and managergateway suites passed after the response repair.
- The first skillbundle run correctly failed on changed-file digest mismatch. After recomputing the exact manifest, `PATH=/home/silas/go/bin:$PATH make skillbundle-verify` passed validation and all 101 structural evaluation cases.
- `/home/silas/go/bin/go test ./...` passed after the manifest repair; some package results were cached.
- `git diff --check` passed. A targeted stale-new-import wording scan returned zero matches across current launch prose, specifications, and shipped skills/evaluations.
- Independent reviewer session `20260913_144246_0d600d` performed no writes or tests. Its findings were checked against source and repaired by the parent; its summary is not substitute test evidence.

## Launch-asset impact and carried acceptance gaps

Changed assets: README, SECURITY, CHANGELOG, quickstart, architecture prose, threat model, and detailed slash guidance. Specifications and shipped skills were synchronized in the same repair.

Reviewed without registration-specific edits: LICENSE, CONTRIBUTING, CODE_OF_CONDUCT, the architecture diagram, no-key demonstration's stated scope, historical recording's stated scope, and repository-local contributor issue backlog. No release workflow, provenance guard, browser verifier, demo script, recording, or remote asset was changed. New skill bytes require newly built source-bound release archives; prior archive/checksum success does not cover this repair.

The following complete canonical prior-review findings remain OPEN, not silently superseded:

| Finding | Evidence recovered | Required acceptance |
| --- | --- | --- |
| LA249-02 / installed browser proof | Exact-candidate installed verifier previously exited 1: Chrome did not become ready; reproduced by final verification | Diagnose Chrome startup and rerun real authenticated extracted-binary registration, refresh/restart and lifecycle readback against the repaired clean candidate; no fixture substitution |
| LA249-03 / no-key demonstration | `./scripts/demo-no-key.sh` previously exited 1: unsupported Hermes identity output | Locate a supported executable and repeat the actual documented demonstration without normal-profile mutation, provider/model changes, or weakened identity checks |
| LA249-04 / remaining executable verification | Prior launch review could not find govulncheck and did not run every documented workflow | Run the pinned vulnerability tool and applicable executable workflows; repeat broad/race/vet, packaging, checksums, and installed proof for the repaired candidate |
| Owner security reporting route | SECURITY retains an owner-controlled private reporting-route requirement | Repository-owner decision and verification; not a local source-repair prerequisite |
| Owner conduct reporting route | CODE_OF_CONDUCT explicitly records missing private conduct-reporting route | Repository-owner decision before community launch; do not invent a contact |
| Publication and CI | Prior publish/CI outputs were DEFERRED_LOCAL_FAILURE | Discover/reuse the task-owned PR only after local gates pass, then verify exact published-head CI; no publication or merge evidence exists for this repair |

This is implementation-step evidence, not final acceptance. No commit, push, PR write, release, runtime provisioning/activation, model change, task creation, or merge was performed by this step. Subsequent conductor gates must retain the open findings and establish their own fresh evidence.
