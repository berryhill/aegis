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

## Attempt 4 correction — LA249-05

Input packet: `tflp_37f87cbb0c4b4502`, hash `e47517adec88aded8a480c563dde5ce104eec1cb0d71d3e593f18f3aa71bf9a3`; task/session unchanged. Pre-edit HEAD was `bd075cb5f677c413987adce58b4b08b79137f989`, clean on `task-t_a08be60-249`, matching the packet.

Contract: correct the threat-model diagram to show enabled initial Registry eligibility after separate exact approval, without implying runtime readiness, activation, or execution authority. Preserve all registration code, explicit disabled/retired inputs, historical records, malformed-input denial and runtime admission. This is a documentation-only repair, not an authority change.

Correction to the historical claims at lines 15 and 28: those claims were incomplete. Direct pre-edit readback confirmed `DefaultImport[Disabled local-default Agent revision 1]` in the diagram. LA249-05 is now source-resolved by replacing that node with enabled Registry-only eligibility and adding an explicit no-runtime-authority edge. The separate review/approval and exact-digest readback edges remain intact. This entry supersedes only the stale-diagram finding and the overbroad historical wording claims; historical evidence above is retained unchanged.

Fresh verification in this activity:

- Diagram assertions passed for enabled eligibility, separate exact approval, absence of the old disabled node, and explicit no-runtime-authority edge.
- `GOMAXPROCS=2 GOFLAGS="${GOFLAGS:+$GOFLAGS }-p=1" GOMEMLIMIT=2GiB /home/silas/go/bin/go test ./internal/registry ./internal/app -parallel=2 -run 'TestRegistrationLifecycleDefaultsOnlyWhenOmitted|TestExecutableResolutionFailsClosedForDigestLifecycleAndRetirement|TestBootstrapLocalHermesImport' -count=1` passed both packages. Toolchain: `go1.26.6 linux/amd64`. This is fresh focused success/denial evidence, not reuse of the previous go1.26.8 installed proof.
- Targeted current Markdown scan found no remaining disabled-new-import promise outside the repaired diagram. Remaining matches were historical evidence or preservation of explicit disabled inputs. This is a targeted source scan, not exhaustive semantic or executable acceptance.
- Launch-impact scan inspected README, LICENSE, SECURITY, CONTRIBUTING, CODE_OF_CONDUCT, CHANGELOG, threat model, architecture, quickstart, no-key demonstration, recording guidance and retained recording/timing, and contributor backlog for this wording defect. Only the threat-model diagram requires a product-asset edit in this activity; this audit receives the correction. Release archives/checksums were not requalified, recorded commands were not replayed, and external issues/releases were not inspected or mutated.
- `git diff --check` passed before this evidence append; final diff readback is required afterward.

Carried ledger (not cleared by this repair):

- LA249-04 — candidate_acceptance; incomplete required verification; repair_kind infrastructure: pinned scanner qualification, applicable documented workflows, and broad/race/vet remain unresolved. Final verification owns broad acceptance. Changed source identity requires new packaging/checksum and installed acceptance qualification; no old archive is accepted for the repaired revision.
- LA249-02 and LA249-03 — retain the packet's historical attempt-3 resolutions only. Current-candidate installed/browser and supported-Hermes evidence must undergo source, dependency, toolchain and environment validity checks; they were not rerun here.
- Publication/exact-head CI — candidate_acceptance; pending_ci: still deferred, not successful. Publish only at its canonical step after local gates.
- Private security/conduct reporting routes — community_release; owner_decision: retained unwaived, no invented contacts or release approval.

No flow completion or continuation is claimed here: flow-step tools are unavailable in this worker; the identity-bound stdout completion envelope must be processed by the dispatcher before another canonical activity.
