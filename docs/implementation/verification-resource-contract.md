# Release verification resource and termination contract

## Contribution CLI cumulative race alarm (#378)

Local acceptance observed the original three-package CLI race command hit its
unchanged eight-minute package alarm late in Gateway intake PTY/multiline tests
(86s/29s), after native root journeys had consumed cumulative time; no data race
was reported. This is not proof of a hosted timing bound.

Both modes now retain `cli` for cmd/aegis and console, use `cli-command` for the
dynamic command remainder, and isolate the exact root, final, disconnect/resume
and declared-operation-approval journeys in `cli-contribution-root`,
`cli-contribution-final`, `cli-contribution-disconnect`, and
`cli-contribution-approval`; the separately measured installed-upgrade parent is
isolated as `cli-installed-upgrade`. Only those exact parents are excluded from
the remainder. Future tests, runnable examples and default fuzz seeds stay covered;
TestMain runs in every process. Missing targets deny before any execution.
Package alarms remain eight minutes (API race twelve), and job/cgroup limits
are unchanged. Fresh complete local/hosted shard timing remains acceptance work.


## Manager contribution inventory partitions

Both normal and race require `manager`, `manager-contribution-repair`,
`manager-contribution-disconnect`, `manager-contribution-resume`,
`manager-contribution-restart`, `manager-contribution-joint`,
`manager-contribution-cancel`, `manager-contribution-final`, and
`manager-contribution-readback`. These top-level integrations are isolated by actual-mode complete inventory;
all other current/future tests, examples and fuzz seeds remain in `manager`.
Missing targets deny before execution. `TestMain`, uncached coverage, 8-minute
package, 10-minute manager job and existing cgroup limits are unchanged.
Workflow names and exact-head evidence gates change together; omitted, cancelled
or skipped groups deny readiness. Local measurement sources are
`377-evidence/managed-integration-first.log` (132.55s PASS),
`managed-resume-first.log` (disconnect 139.00s PASS), and
`managed-resume-third.log` (expired-resume 136.14s FAIL). These explain partitioning,
not hosted/race qualification or successful expiry acceptance.

The expanded manager remainder reached its unchanged race alarm at 480.259s
while `TestContributionDeliveryJointNativeNonlocalCancellation` had run for only
18s. This demonstrates cumulative fixture cost, not an eight-minute hang in that
case or a reported data race. Isolate the exact joint-native, nonlocal-cancel,
joined-finalization and terminal-readback parents through the existing partition
map. Their children stay included; similarly named future tests stay in the
remainder. Workflow and required exact-head evidence names move together in both
modes; missing/cancelled/skipped successors deny even with green aggregates.
Package/job/cgroup/authority deadlines are unchanged. Local partition-contract
tests are not hosted timing or whole-suite acceptance.


Objective: retain every release gate while bounding verification CPU, memory, process count, output and elapsed time; always clean up owned children and close authority storage after failure.

Acceptance: inherited conservative Go defaults with valid explicit overrides preserved; Linux cgroup-supervised verification with fail-closed unavailable supervision; bounded CDP framing/events/deadlines; process-tree timeout cleanup; bounded test helpers and PTY buffers; Sync-failure close regression with no CLEAN marker; exact-head CI and verified main merge.

Non-goals: no service/laptop configuration changes, release publication, weaker signing/authority/admission gates, or claim that these source risks caused the reported desktop freeze.

Proof policy: low concurrency, bounded cgroup execution, durable proof roots. Production storage tuning is not changed merely to make tests cheaper. Launch review and actual verification evidence follow in the delivery review.

## Independent-review corrections

- PTY fixtures now have one exclusive `exec.Cmd.Wait` goroutine. A closed completion
  channel publishes its result to all callers; timeout cleanup kills the owned
  group and joins that same waiter without polling `ProcessState`.
- Direct Chrome launches own a process group. Cleanup signals the entire group
  even after a successful leader exit; the leader-before-child regression uses
  real processes. Integrated verification still requires the aggregate cgroup:
  process groups alone do not confine deliberately escaping native processes.
- `testprocess.CombinedOutput` accepts an unstarted plain `exec.Command` specification,
  preserves argv[0], inherited descriptors, directory, stdin and environment, and
  explicitly rejects context/custom process settings and preassigned output.
  Architecture tests classify it as test-only and deny application imports,
  including from the command binary.
- Supervisor command-construction tests mock platform/cgroup/systemd discovery.
- The first clean candidate failed architecture classification and later hit the
  unchanged 6 GiB cgroup OOM limit. A focused `internal/command` run with the new
  `GOMEMLIMIT=512MiB` soft target passed in 50.480s. This supports reducing GC heap
  accumulation, not blaming or tuning storage: Badger cache, memtable, sync and
  conflict options remain unchanged. Full-suite and race readback are separate
  acceptance requirements, not implied by that focused result.

The soft Go target is inherited by Make, the supervisor and nested testprocess
builds; explicit overrides remain supported. It is not a hard per-process RSS
ceiling or a replacement for cgroup enforcement.

## Command contribution inventory partitions

The expanded command package reached its unchanged race alarm at 480.222s
while `TestInstalledUpgradeFullCommand` had run for only 11s (its `healthy`
subtest for 9s). The adjacent command binary and console packages passed. This is
cumulative package cost, not an eight-minute installed-upgrade hang or a reported
data race. The existing dynamic inventory now gives the exact production-root
legacy and final contribution journeys their own `cli-contribution-root` and
`cli-contribution-final` groups. `cli-command` retains every other command test, example
and default fuzz seed, including similarly named future tests; `cli`
retains the complete command-binary and console packages. Both modes require all
corresponding contexts. Workflow, required evidence and missing-target denial change
together; package/job/cgroup/authority limits and race coverage do not widen.
Local contract tests and planned inventories are not actual shard or hosted
acceptance; the failed aggregate invocation remains retained.

The first partitioned remainder also reached 480.188s, this time with the full
installed-upgrade parent active for 2m42s and its current subcase for only 7s.
Isolate that measured parent as `cli-installed-upgrade`, still with its complete
children and unchanged alarm. The remainder retains every other default case;
missing/skipped/cancelled upgrade evidence denies readiness in both modes.

## Launch-asset impact review

Current CI groups in each of `normal` and `race` are `api-a`, `api-b`,
`implementation`, `implementation-contribution`, `orchestration`, `orchestration-contribution`, `manager`,
`manager-contribution-repair`, `manager-contribution-disconnect`,
`manager-contribution-resume`, `manager-contribution-restart`,
`manager-contribution-joint`, `manager-contribution-cancel`,
`manager-contribution-final`, `manager-contribution-readback`, `cli`, `cli-command`,
`cli-contribution-root`, `cli-contribution-final`, `cli-contribution-disconnect`,
`cli-contribution-approval`, `cli-installed-upgrade`,
`persistence`, `update`, `remainder-a` and `remainder-b`. The measured cumulative
`execution` shard was replaced by separate implementation/orchestration packages.
Within orchestration, the contribution integration alone measured 373.40s and the
implementation controller about 140s: together they exceed the eight-minute alarm.
Both orchestration groups inventory the actual mode's complete default tests via
`go test [-race] -count=1 -timeout=8m -list .`. An anchored escaped parent-name
selection isolates `TestContributionQueueNativeRepairIntegration` in the
contribution group; every other current/future test, runnable example and fuzz
target belongs to orchestration. All children/seeds execute and TestMain runs in
each process. Missing/invalid inventories or a missing contribution target deny.
Each retains a ten-minute job and `-count=1 -timeout=8m`. Existing API partitions,
alarms and other job exceptions remain unchanged. Other packages, including future
packages, remain exact-once. All three successor groups in both modes are mandatory
for exact-head evidence; cancellation/missing evidence denies with green aggregates.
The synthetic contribution fixture installs immutable authoritative time before
worker/service clock capture, preventing wall-time native checks from expiring its
300-second authority contract/claim. Explicit expired-subject and revoked-host
negatives remain; production freshness, TTLs and real native timeouts are unchanged.
Toolchain preparation, cgroup/CPU/memory limits and success-only aggregates are
unchanged. Focused local script tests are not hosted timing proof.

- Updated CONTRIBUTING.md: the complete bounded gate and Linux/systemd/cgroup-v2 prerequisites, explicit macOS verification denial, focused supervised command, inherited defaults and limits.
- Updated CHANGELOG.md: verification resource/termination and authority-close corrections.
- Updated CI and release workflows: retain all tests, race, vet, vulnerability, archive/checksum and installed gates behind supervision. No release is published by this change.
- Updated README.md with the full-verification prerequisite; reviewed LICENSE, SECURITY.md, CODE_OF_CONDUCT.md, docs/THREAT_MODEL.md, docs/ARCHITECTURE.md and docs/QUICKSTART.md: identity, runtime, installation, trust and product CLI contracts unchanged. Verification limits are not a security sandbox or supported-platform expansion.
- Reviewed docs/DEMO_NO_KEY.md, scripts/demo-no-key.sh, docs/RECORDING.md and retained docs/assets/aegis-no-key.{typescript,timing}: no demonstration command/output change; retained capture remains explicitly historical, not fresh provider or release evidence.
- Reviewed .github/workflows/release.yml, archive/checksum verifier and docs/contributing/ISSUE_BACKLOG.md: packaging formats and owner publication boundaries unchanged; backlog is explicitly repository-local. No GitHub release, recording publication or contributor issue was created.

The installed, full-suite, race and vulnerability gates must pass on the committed
candidate before merge. A dirty-tree local gate correctly stops at provenance;
that is not passing installed verification. Exact-head CI and independent parent
review remain mandatory; this document does not attest they have completed.
