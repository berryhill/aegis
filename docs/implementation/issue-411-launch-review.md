# #411 candidate-bound contribution setup (source candidate)

## Contract

`/contribute readiness [scope]` and exact `/contribute select LOOP` read back the
published v6 profile, controller/builder/tester Agent/charter/runtime bindings,
provisioning, deterministic stanza selection, native policy, repository/base and
bounded effect. Missing profile is explicit, not an invitation to invent one.
The root client passes `expected_service` (executable SHA-256, VCS revision and
dirty flag) through the actual owning Unix API. Mismatched Run denies before
intake; the server never replaces another service or copies its authority.

`configured` means an existing provider/controller configuration is present but
ordinary fresh provider/actor/permission admission remains required. Historical
provider scopes retain their ordinary credential contract; omission of explicit
controller Codex authentication is not reclassified as missing Codex setup.
`preparation_eligible` is observational and never execution or qualification.
An executable without VCS metadata is source-unqualified, but still may use the
existing authenticated execution contract; it cannot establish fresh-source PASS.
Dirty source is identified by exact executable bytes plus VCS dirty provenance,
not by pretending the base revision alone identifies its complete source.

Protected role provisioning is delivered through the existing retained decision
owner before final host/provider/remote preflight denial. Its canonical consumed
approval and verified receipt, not transport text, decide success. No new native
host-grant dialog or remote-approval API is invented: missing configuration or an
unavailable operation decision remains an explicit independent owner gate.
After setup, Resume with the original key and zero attempts routes to existing
execution; no replacement publication, renewed task consent, new worker selection
or budget reset. Existing attempts retain the #377 continuity path.

First admitted effectful preparation pins `contribution-image` to contract/run/
Loop and actual service image. Failed prerequisite preflight does not pin an image.
Status and restart preserve that record, not a fresh claim for the observer.
Historical records without an image return absence, never synthesized provenance.
A different image cannot silently continue effectful historical preparation.

## Evidence tiers and commands

All fixtures use isolated state and the actual owning service/root transport.
The controlled Hermes protocol is synthetic; Git worktrees, native RED/GREEN,
source custody, local Commit, final report/artifact/receipt/disposition and cold
readback are real owning implementations. A local-only terminal result is
`narrower-target-completed`, with whole-contribution success false.

Focused app tests: `TestContributionReadinessObservesWithoutAllocating`,
`TestContributionReadinessPreservesHistoricalProviderContract`, and
`TestContributionRunProvenanceRetainsExactImageAndDeniesDrift`.
Public root: `TestPublicRootAuthenticatedContributionFinalJourney` separately
publishes then invokes the exact profile and checks canonical terminal joins,
replay and restart. `TestPublicRootContributionMissingHostPreflight` additionally
checks absent profile, mismatched image/no intake, exact role/native readiness,
repeated denial/no role authorities or worktree, absent execution provenance,
fresh-session retained identity, then supplies isolated independent host setup
and resumes the SAME intent through finalization/restart.

On this workstation run these command tests under the shared lifetime helper:

```sh
python3 scripts/acceptance_port.py \
  --pool /home/silas/.hermes/profiles/xander/workspace/aegis-acceptance-ports/locks \
  -- env AEGIS_CONTRIBUTION_TEST_ROOT="$PWD/.aegis-e2e" \
  TMPDIR="$PWD/.aegis-e2e/t" GOMAXPROCS=2 GOFLAGS=-p=1 \
  go test ./internal/command -run '^TestPublicRootContributionMissingHostPreflight$' -count=1 -v
```

The short private repo-local temp root preserves pinned executable ancestry and
Unix socket length, not weaker launcher permissions. Reserved 8448/8098 remain
untouched. Existing #375–#379 cancellation/rejection/expiry/revocation, duplicate,
response-loss and restart machinery is reused; this change does not claim every
negative or the full race suite was newly run. Exact executed results accompany
the candidate handoff. Independent frozen testing and CI remain parent-owned.

## Launch-asset impact review

Updated README, changelog, slash reference, Security Policy, threat model and
architecture text for readiness/provenance and explicit authority boundaries.
Reviewed LICENSE, CONTRIBUTING and CODE_OF_CONDUCT: licensing, contributor
workflow and conduct/reporting policy unchanged. Reviewed QUICKSTART, DEMO_NO_KEY
and RECORDING: their no-key/historical scopes do not exercise contribution setup;
no new successful recording or demo output is claimed or generated. Reviewed
RELEASE_PIPELINE and contributor ISSUE_BACKLOG: no CI topology (27 jobs), release
binary/checksum/publication, installation or new remote issue is changed. Existing
unresolved launch gates remain; no release artifact is manufactured by this work.

## Builder verification result

PASS: actual public-root final journey (46.53s); missing-host denial followed by
SAME-key Resume, local finalization, replay and restart (latest 68.02s); missing
remote-scope denial before image/role/worktree effects (4.35s). Focused readiness
and image tests, including historical provider contract (3.74s), architecture and
slash tests passed. Broad contribution app selection passed (87.57s). The combined
manager selection hit its deliberately short 150s package alarm while joined
completion had run only 37s; isolated `TestManagerJoinedContributionCompletion`
then passed (53.67s). This is not aggregate manager or race acceptance.

Plain `go build -buildvcs=true` succeeds and the repo-root `aegis version` reports
`dev`. Verified `build-source.sh` is BLOCKED: Go 1.26.10 omits all VCS settings in
this linked-worktree environment despite the required per-command core.worktree
binding, so the helper correctly refuses publication (`vcs expected git, got
missing`). No build stamp was fabricated or guard weakened. Parent must resolve
this provenance/build-environment gate before treating the candidate as an
exact-source frozen build. The plain executable remains source-unqualified.

## Remaining qualification

Live-provider whole-contribution execution, actual independent human decisions,
remote protected-base merge and installed/main-service qualification are NOT PASS.
No live settings, grants, credentials, providers, model, release or deployment were
changed. Source merge may proceed under owner direction with those explicit gates;
readiness, historical hello/native receipts and local fixtures cannot close them.
