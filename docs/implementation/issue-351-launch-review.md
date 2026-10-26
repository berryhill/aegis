# Issue 351 source qualification and launch impact

## Scope and publication boundary

This change adds a focused two-recorded-owner application-service regression and
a reusable pilot qualification recipe. No production Go behavior, public schema,
CLI/API adapter, authority, credential custody, runtime or installation code is
changed. It does not close #351 or establish a completed fleet pilot.

The regression uses synthetic registered participants with real isolated service
persistence. It verifies different recorded owners under the configured principal,
owned publication/activation/successor mutation, foreign-owner denial, shared
same-revision direct admission, same-intent replay, budget-change conflict,
participant substitution denial and historical revision integrity. No provider
runs and no artifact/receipt/disposition are manufactured. A Queue item in
preparation remains explicitly non-executed.

The recipe preserves separate publication and execution, the same-v5/two-task
requirement, actual evidence and cumulative budgets, and an additional separately
approved v4 native case. It explicitly identifies native coding through closed
v5 inputs as unavailable. Profile-private managed installation can coexist with
existing skills only when actual fresh discovery is unambiguous; a collision
requires scoped rollback and an independently authorized recoverable archive,
not overwritten or silently hidden local content.

## Current-source verification

Reconciled from delivered #348 and #350 through current main
`f3b8fa81918f51ec1a3fe12c3a0964aa6848629d`. Focused verification on that base:

- `go test ./internal/api -run 'Test(Issue351|DoerWorkspacePublication|WorkspaceSkillSubmissionPublicReadback|UnixPeerAuthenticationAndBearerSeparation)' -count=1 -timeout=3m`: PASS.
- The same API selection with `CGO_ENABLED=1` and `-race`: PASS.
- `go test ./internal/architecture ./internal/skillbundle/... -count=1 -timeout=5m`: PASS.
- `python3 scripts/console_security_test.py`: PASS.
- `git diff --check`: PASS.

Go checks use `GOMAXPROCS=2 GOFLAGS=-p=1 GOMEMLIMIT=512MiB`. Fresh exact-head CI
must still qualify the published commit; these local results are not full-suite
or merge evidence. Earlier independent source/package/installed-fixture audits
retain their original source and evidence tier; they are not silently promoted
to proof for a later base or actual managed-provider execution.

## Launch asset impact

Reviewed the source-scope implications against the existing root and launch
materials. No source runtime or release behavior changes, so no unrelated asset
rewrite or release generation is warranted.

| Asset | Disposition |
| --- | --- |
| Root README | Unaffected: current reusable/native links and weaker-result caveats remain; new focused recipe is an additional qualification document, not a default quickstart or completed-pilot claim. |
| LICENSE and Code of Conduct | Unaffected; no license/conduct change. Existing owner-designated private conduct reporting prerequisite is not resolved by this change. |
| SECURITY and threat model | Unaffected implementation; new recipe retains external authentication, no authority union, no credential/profile copying and no sandbox claim. |
| CONTRIBUTING and architecture | Unaffected runtime/process topology; existing canonical contribution/port policies apply. New regression exercises existing application boundaries, not new orchestration. |
| CHANGELOG | No production capability or completed pilot is advertised. This change is qualification coverage and guidance only. |
| Quickstart and no-key demo | Unaffected; no live model/demo run performed for this test/documentation change. Their existing limited evidence claims remain. |
| Recording | Unaffected and historical; no new recording or successful runtime output synthesized. |
| Release binaries/checksums | Not generated or published by this source contribution. Development package proof remains separately identified, not a release. |
| Contributor backlog/issues | Unaffected; #351 is the controlling issue and remains open. No new issue or rollout is inferred. |
| Source-owned skills and manifest | Unchanged by this patch; preserve delivered owners and canonical archive mechanisms. |

## Mandatory acceptance still pending

Do not merge merely because source tests or CI are green. Preserve the
candidate-bound reusable Doer pre-merge requirement and #351's full applicable
pilot acceptance unless the owner explicitly changes that gate. The source
regression is not a substitute for either real participant's admitted Run,
causal artifact/receipt/disposition, installation/fresh adoption, actual native
repository acceptance or denial/recovery accounting.

Current operational partial evidence is retained outside committed source. Exact
participant setup was owner-approved and canonical readback verified, but Archer's
managed discovery encountered pre-existing official-suite name collisions. The
new discovery setting was rolled back to exact prior bytes; all prior skill
content and the verified managed generation were preserved. Archival choice
remains unanswered, not denied or approved. No provisioning/runtime/host grants
or pilot Run were included in that setup approval. #385's original native case
and remaining budget remain owner-deferred and untouched.

PR publication may carry this scoped source work for review and exact-head CI;
it must not close #351, claim live acceptance, or waive the merge gate. Release,
workstation update, candidate service activation and universal rollout remain
outside this source change.
