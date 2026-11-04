# Queue evaluation ledger, v1

This is a mapping to executable repository tests, not a second evaluator or a
runtime protocol. `aegis skills` installation grants no authority. Without a
checkout, use installed help and the bundled request contracts; source test
names below are qualification references, not commands on an installed system.

`skills/evaluations.json` remains structural fixtures. The evaluator reports
`structural_fixture_validation` and behavioral/service/runtime `not_run`.
Instruction strings passing that check do **not** prove model adherence. The
following distinct tests execute services or persistence; no managed provider
is needed for these advisory-skill contracts, and none is claimed qualified.

## Exact coverage mapping

| Class / obligation | Executable source fixture | Actual boundary |
| --- | --- | --- |
| happy_path: binding and reclaim examples, exact direct source | `internal/api/online_lifecycle_installed_test.go` / `TestInstalledOnlineLifecycle` | Compiled CLI through real Serve Unix peer/bearer, shared application and isolated Badger; shipped JSON examples have fixture-only exact references substituted. Checks direct `source_kind`, `source`, normalized `inputs`, workspace cancellation, binding, expired reclaim, backoff, stable IDs and consumed attempts. |
| authority_denial and stale_replay | `internal/api/online_lifecycle_installed_test.go` / `TestInstalledOnlineLifecycle` | Live reclaim, ordinary retry before/after expiry, foreign authority, wrong reason, exhausted budget, terminal replay, unsupported online expire/exhaust; full Queue readback unchanged on relevant denials. |
| malformed_input: distributed wire examples | `internal/command/queue_skill_contract_test.go` / `TestQueueSkillBundledContracts` | Real strict decoder and unknown-field rejection; constructor sentinel only, **not** service execution. |
| happy_path: verified runtime output and reloaded evidence | `internal/orchestration/fleet_service_test.go` / `TestQueueWorkerRunsInjectedAdapterToDurableEvidenceDisposition` | Injected runtime adapter, real verifier and durable evidence; not a real provider. |
| authority_denial: repeated fresh admission | `internal/orchestration/fleet_service_test.go` / `TestQueueWorkerRepeatsAdmissionAndDurablyDeniesRevokedRuntimeEffect` | Revoked runtime effect is denied after claim, not converted into success. |
| stale_replay: authority drift before claim | `internal/orchestration/fleet_service_test.go` / `TestQueueWorkerRejectsAuthorityDriftBeforeClaim` | Exact authority mismatch denies before effects. |
| happy_path and interruption_recovery: canonical retry/reopen | `internal/persistence/fleet/badger/lifecycle_test.go` / `TestRetryReclaimAndCancellationAreAtomicDurableLifecycleFacts` | Real Badger claim/reclaim/retry, same run/child and bounded numbered attempts, ordinary retry denial, cancellation and reopen. No stop acknowledgment is invented. |
| stale_replay: deterministic single winner | `internal/persistence/fleet/badger/lifecycle_test.go` / `TestConcurrentInitialClaimsHaveExactlyOneWinner` | Competing claims cannot both create an attempt. |
| authority_denial: dependencies | `internal/persistence/fleet/badger/lifecycle_test.go` / `TestClaimRejectsProjectedDependencySuccessWithoutCanonicalDispositionAtomically` | A projection cannot manufacture successful dependency evidence. |
| stale_replay: exact Loop pin | `internal/persistence/fleet/badger/completion_foreign_loop_test.go` / `TestCompletionRejectsForeignPinnedLoopEvidenceAtomically` | Valid receipt for a different published revision fails `exactNodeEvidence` and completion; all store bytes including audit unchanged, also after reopen. |
| stale_replay: evidence substitution | `internal/persistence/fleet/badger/lifecycle_test.go` / `TestCompletionRejectsUnsatisfiedOrSubstitutedEvidencePolicy` | Missing, failed, substituted or wrong-policy evidence cannot mint success. |
| happy_path and authority_denial: lease expiry | `internal/persistence/fleet/badger/completion_expiry_test.go` / `TestCompletionExpiryExactClaimOnly` | Only exact evidence-free expiry may terminalize an expired active claim, never late success. |
| happy_path: bounded raw two-node chain | `internal/orchestration/queue_worker_raw_chain_test.go` / `TestQueueWorkerProcessRawProducerConsumerBadger` | Exact raw-v2 producer/consumer evidence and causality in real Badger, not general scheduling. |
| happy_path: bounded managed two-node chain | `internal/orchestration/queue_worker_raw_chain_test.go` / `TestQueueWorkerManagedProducerConsumerSynthetic` | Synthetic managed-v7 two-node handoff, one outer attempt; not managed-provider acceptance. |
| interruption_recovery: lost committed response | `internal/api/online_lifecycle_installed_test.go` / `TestInstalledOnlineLifecycle` | Transparent test-only Unix response loss, then canonical readback before repeating; no new writer. |
| secret_canary: bundle non-retention | `internal/skillbundle/skillbundle_test.go` / `TestExecutionQueueSkill` | Safety and required fixture classes; structural only, not runtime-output redaction or model behavior. |
| secret_canary: validator denial | `internal/skillbundle/skillbundle_test.go` / `TestValidateFailsClosed/secret_shaped_content` | Real validator rejects constructed dummy secret-shaped content with `secret_literal`, after fixture digests are refreshed. No real secret, model turn or provider-output redaction proof. |
| portable package | `internal/skillbundle/queue_packaged_install_test.go` / `TestPackagedQueueSkillInstall` | Opt-in supplied executable/archive, shipped `skills install` and `inventory` outside checkout, exact hashes/revision and portable reference bytes. Missing artifact is SKIPPED. |

The Queue-specific structural cases additionally explain `failed` plus
`retry_exhausted` (no exhausted enum), direct-source readback, both unsupported
online expire/exhaust adapters, ordinary `reclaimed: false` denial even after
expiry, and raw-v2/managed-v7 `max_attempts: 1` with no generic retry/reclaim.
Cancellation, expiry, revocation, failure, denial, success and retry exhaustion
must be read with their exact reason, not merged into a generic terminal label.

## Reproduce only the applicable tier

For source tests, use the named package and anchored test name with `-count=1
-timeout=8m`; preserve `GOMAXPROCS=2 GOFLAGS=-p=1 GOMEMLIMIT=512MiB`. Do not
increase limits or launch a provider to make advisory documentation green.

For the installed lifecycle fixture, build with `scripts/build-source.sh`, supply
its absolute path as `AEGIS_TEST_PACKAGED_EXECUTABLE`, and execute only
`TestInstalledOnlineLifecycle` under the existing `scripts/acceptance_port.py`
lifetime helper with the operator's approved shared pool. The fixture applies
`AEGIS_ACCEPTANCE_PORT` before Serve; unset retains normal CI behavior. All
service calls are sequential on that single fixture listener. The helper does
not isolate stores or grant authority; the test creates its own isolated stores.

For portable acceptance, the independent parent verifies the exact archive
SHA-256, full source revision and frozen binary first. Supply absolute binary
and archive as `AEGIS_TEST_PACKAGED_EXECUTABLE`, `AEGIS_TEST_SKILL_ARCHIVE`, plus
`AEGIS_TEST_SKILL_ARCHIVE_SHA256` (`sha256:` prefixed) and
`AEGIS_TEST_SKILL_SOURCE_REVISION` to `TestPackagedQueueSkillInstall`. It invokes:

- `aegis skills install ARCHIVE SHA256_DIGEST SOURCE_REVISION ABSOLUTE_HOME`
- `aegis skills inventory ABSOLUTE_HOME`

The parent then independently installs into an explicitly approved fresh
absolute Hermes home using that same shipped installer, reads inventory, and
starts a **fresh** supported Hermes process outside this checkout with that
home. Call actual `skills_list` and `skill_view` for `aegis-execution-queue`, then
load its bundled Queue contract and evaluation ledger. Record executable,
archive, source revision, Hermes version, home and returned paths. Do not copy
normal profiles/configuration/credentials or use an older PATH `aegis` binary.
Byte extraction, mocked tools and `/reload-skills` alone are not fresh discovery.

Installed discovery, model adherence, managed-provider qualification, release
publication and live target acceptance are separate evidence tiers. This ledger
prescribes no real-provider run and reports no such PASS. Parent review,
exact-head source delivery and final independent installed loading remain
separate from builder tests.
