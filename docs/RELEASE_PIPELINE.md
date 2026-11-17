# Release pipeline: bounded work, exact evidence

## Acceptance target

The operator target is less than 600 seconds from a release request to a published,
installable release. A ten-minute job timeout only bounds failure; it does not prove
that target. Record successful workflow elapsed time including queue/setup, and
record fresh source-CI critical-path time separately. Do not call an untested source
revision releasable or count an unpublished archive as a completed release.

## Source verification

CI checks the exact source revision, not an independently created synthetic commit.
Normal and race test modes use disjoint exhaustive package groups. The group
inventory is derived from `go list ./...`; coverage must include every package once
per mode. Source checks, installed archive/browser/fleet proof, normal/race tests,
authority denial, vet and vulnerability scanning execute independently. Each worker
keeps the 6-GiB, zero-swap, 256-task, two-CPU budget. The required exact-head aggregate
fails when any constituent fails, is cancelled, or is skipped. The full sequential
local `make verify` remains available; no general environment switch disables gates.

Push CI runs on main and PR CI covers proposed changes. Feature pushes do not
launch a second identical workflow, and a release tag does not launch another full
source suite. Package compilation caches are an optimization, not green-test evidence.
Normal and race candidate tests use fresh execution rather than relying on cached
results.

Ordinary source CI has **29 expanded jobs**: seven normal and fourteen race Go
lanes plus eight repository, packaging, security and aggregate jobs. The extra
non-browser package job separates eligibility from optional browser coverage
(see the v0.3.0 profile below). There are no
reusable/child workflows on those events. `scripts/verify-go-shard.py --bundle`
packs the existing exact-once partitions into mode-specific sequential lanes;
it does not remove tests or combine their alarms. Dynamic actual-mode inventories
retain future tests, runnable examples, default fuzz seeds and children. A split
package is compiled and listed once per lane, then its native Go test executable
runs each partition from the package directory with its own TestMain, uncached
execution and original eight-minute alarm (twelve for race API). Separate `go vet`
continues to cover the complete module. The existing setup-go compiler/module
cache remains enabled; no cached test result substitutes for execution. This is
within-job executable/compiler reuse, not a promise of a complete cross-job cache.

Packing now addresses actual hosted run **40888280401** at source **505c4ea**:
22 jobs succeeded, three hit annotated **10m0s job deadlines**, and two aggregate
cancellations were consequences, not independent test failures. The three deadline
lanes were normal implementation, race orchestration and race manager-b. Completed
Go assertions passed; unfinished groups are not passing evidence. Retained logs
and `metrics-40888280401.json` are in the parent's `issue404-evidence` directory.
This supersedes the earlier 40884797403 packing estimates; the fixture-only
baseline **40882148345** remains 57 PASS, 209.13 runner-minutes and 999s peak.

The historical mapping below records the **40888280401** packing repair; later
adjustments are described separately. `cli` means `cmd/aegis` + `internal/console`, not the
separate command/default or contribution partitions.

| Mode / lane | 40888280401 groups | Revised groups |
| --- | --- | --- |
| normal implementation | implementation, implementation-contribution, remainder-a | implementation, implementation-contribution |
| normal orchestration | orchestration, orchestration-contribution, persistence | orchestration, orchestration-contribution, remainder-a |
| normal api-b | api-b, cli | api-b, cli, persistence |
| race orchestration | orchestration, orchestration-contribution, cli | orchestration, orchestration-contribution |
| race api-b | api-b | api-b, cli |
| race manager-b | manager-contribution-restart, manager-contribution-final | manager-contribution-restart |
| race manager-e | manager-contribution-disconnect, cli-contribution-approval | manager-contribution-disconnect, cli-contribution-approval, manager-contribution-final |

Observed normal implementation phases were compile 16.3s + default 349.98s +
contribution 101.68s before remainder-a was unfinished. Normal orchestration
completed compile 12.5s + default 141.19s + contribution 148.32s + persistence
41.2s. Moving remainder-a (~191s) there projects ~492s; moving persistence to
normal api-b (~343s including cli) projects ~384s.

Race orchestration consumed compile ~74s + default 189.86s + contribution 257.41s
before cli was unfinished. Race api-b consumed ~505s including compile/list;
adding cli (~93s) projects ~598s within its **existing sixteen-minute job cap**.
Its API process retains twelve minutes; cli retains eight minutes. Race manager-b
consumed compile ~85s + restart 392.73s + final 111.75s, leaving no setup margin.
Moving final to manager-e projects ~550s (existing ~438.9s + final), while manager-b
projects ~478s. These are planning sums, **not fresh hosted success**.

All 26 current groups remain exact-once in each mode, with seven normal and
fourteen race lanes after the dedicated-host correction below. The historical
packing change above did not alter partitions,
resource budgets or native unexpected-zero-exit guards.
Compile/list reuse remains once per split package per lane. Compiler, inventory
and partition elapsed times remain separate `go_lane_phase` observations. Job
alarms remain ten minutes except sixteen for race API and orchestration; package alarms remain
eight minutes except twelve for race API. Fresh complete exact-head hosted runs,
including queue/setup and all jobs, must establish deadline fit and performance.

The manager native fixture's session leader now waits on a private per-runtime-home
FIFO until normal service termination/revocation instead of independently exiting
after 240 wall seconds. The adapter still owns, terminates and Waits the exact
process group; test cleanup revokes still-authorized sessions or terminates and
Waits already-expired fixture processes before closing stores, then verifies
`InspectSession` reports no surviving process.
No authority TTL (300s), native check timeout (60s), cumulative budget, real context
deadline, process-liveness gate or exact drift callback assertion is relaxed.

### Native manager partition balancing

`TestEngineeringProseHugeAssetsLocalTarget` remains in the dynamic `manager`
group and existing `manager-a` lane with offline-dependency preparation. To avoid
competing native fixture compilation inside the same two-core group, five complete
roots move into existing groups in both normal and race modes:

- `manager-contribution-final`: `TestManagerJoinedContributionCompletion`,
  `TestContributionDeliveryControllerNativeLocal`,
  `TestContributionDeliveryLocalNarrowJourney` and
  `TestContributionManagerJoinedPublicResultHistoricalPolicy`.
- `manager-contribution-readback`: `TestManagerTerminalContributionReadback`,
  `TestEngineeringProseRepairLocalTarget` and `TestRetainedDeliveryNativeComposition`.

All children/seeds remain selected. The dynamic remainder retains every other
current/future default test. That partition balancing preserved workflow jobs,
evidence names, group-to-lane assignments, dependency preflight, package/native
deadlines and resource limits. Local supervised group PASS is not hosted
whole-workflow PASS.

### Normal manager whole-group packing

Hosted run **43969794998** cancelled normal `manager-a` at its unchanged 600s job
deadline during readback, after compile 41.289s and passing manager 153.167s,
restart 127.838s and final 162.732s phases. Move only the existing whole restart
group between the two existing normal lanes; race mappings and test partitions
are unchanged:

| Normal lane | Revised groups |
| --- | --- |
| manager-a | manager, manager-contribution-final, manager-contribution-readback |
| manager-b | manager-contribution-repair, manager-contribution-disconnect, manager-contribution-resume, manager-contribution-joint, manager-contribution-cancel, manager-contribution-restart |

The passing normal `manager-b` measured 121.316s + 72.636s + 73.750s + 58.145s +
35.543s for its existing groups. Adding restart projects **489.228s** of execution,
or **523.897s** with its measured 34.669s compile, leaving **76.103s** for inventory,
setup and cleanup within 600s. Normal `manager-a` gains 127.838s of room. These are
planning sums, not fresh hosted success. All groups/roots remain exact-once per
mode; job names/counts, 480s manager process alarms, 600s normal job alarms,
resource limits and offline-dependency preparation remain unchanged.

### Race API whole-group packing

The same hosted run **43969794998** also cancelled race `api-b` at its unchanged
960s job deadline, just after api-b (423.410s), cli (92.241s) and remainder-b
(328.890s) completed and the missing-host group started. This is an aggregate
deadline, not a product-test failure or superseded-run cancellation. The initial
repair moved two existing whole groups as below. Its shared `cli-command` packing
was subsequently superseded by the dedicated-host correction; normal is unchanged:

| Race lane | Revised groups | Measured-phase planning sum, before setup/cleanup |
| --- | --- | --- |
| api-b | api-b, remainder-b | 100.553s compile + 1.080s inventory + 423.410s + 328.890s = 853.933s of 960s |
| cli-command | cli-command, cli-contribution-host | 82.361s compile + 1.064s inventory + 282.494s + historical approximately 164s host = approximately 529.919s of 600s |
| support-a | remainder-a, cli-contribution-disconnect, cli | 84.593s compile + 1.064s inventory + 200.161s + 106.907s + 92.241s = 484.966s of 600s |

The host estimate was the earlier run 42089450269 observation, not a newly measured
passing host group. That repair kept its exact existing partition **after defaults
in a separate process** using cli-command's already compiled binary. It must not
rejoin the defaults process and delay that process's early upgrade child behind
the missing-host cleanup barrier. Neither destination nor the source had locked
offline-dependency preparation; its existing implementation/manager-a routing is
unchanged. All group definitions, mode inventories, selectors, test bodies,
TestMain invocations, process alarms, lane counts and resource bounds were
unchanged by that repair. These sums were planning evidence, **not hosted PASS**; complete
exact-head hosted timing remains the acceptance gate.

### Post-merge implementation root balancing

Main run **43976201285** exhausted the implementation race process's 480s alarm,
not a native operation deadline. Move only the measured 66.26s complete
`TestManagedContributionEngineeringRegressionRepair` root into the existing
`implementation-contribution` partition. Normal keeps both partitions in its
implementation lane; race contribution already belongs to CLI. Its measured
compile/list and existing execution phases plus the moved root project 509.813s
of the unchanged 600s CLI job, before setup/cleanup. Preserve every other mapping,
all process/native limits, then-existing seven normal and thirteen race lanes, and exact-once
actual-mode inventories. The synthetic stdlib-only root needs no locked module
bundle; real offline/templ roots retain implementation/manager-a preparation.
See the [budget analysis](implementation/issue-379-verification-budget.md) for
the dependency trace and evidence limits. Fresh hosted success remains required.

### Dedicated race host-review lane

Run **43979771143**, job **133692963167**, disproved the shared-job estimate:
command compilation took **129.037s**, inventory **1.078s**, and the complete
command defaults **345.418s (PASS)**. The separate missing-host process started
but the unchanged **600s job deadline** cancelled it before completion. This is
aggregate packing exhaustion, not an observed host assertion failure.

Move only the existing complete `cli-contribution-host` group from race
`cli-command` into new race **`cli-host`**. Command defaults remain alone in
`cli-command`; all seven normal lanes and every other race assignment are unchanged.
The new mandatory job is **`Go race / cli-host`**, joined by both workflow aggregates
and exact-head release evidence. Missing, duplicate, cancelled, skipped, failed or
wrong-head host evidence denies readiness, including the bounded v0.3.0 profile.

This is one additional execution lane, not another test partition: **26 groups**
still cover every actual-mode root/child/example/default seed exactly once. The
host job compiles its own command test binary through the existing pinned toolchain,
inventories that binary, and runs the same exact parent in its own uncached process.
It never rejoins the defaults process or delays that process's early upgrade child.
It inherits the same matrix runner, read-only permissions, environment/toolchain,
supervisor, source checks and credential handling, without new services or secrets.
Job/process/native deadlines stay **600s / 480s / unchanged**, with the same
6-GiB/zero-swap/256-task/two-CPU limits and no shared test-result cache.

The observed 129.037s compile plus 1.078s inventory plus historical approximately
164s host execution projects **294.115s**, leaving approximately **305.885s** for
setup/cleanup/variation. These are mixed-run planning sums, not a new hosted PASS;
fresh complete exact-head CI remains required. The prior implementation partition
repair and deployed service are outside this lane-only adjustment.

## Release transaction

`make release VERSION=MAJOR.MINOR.PATCH` retains its explicit publication authority,
configured signing identity, signing preflight, immutable signed tag, deterministic
changelog transformation, atomic push and bounded recovery rules. On the canonical
signing path, both the disposable preflight tag and the final newly created tag
must pass signature and configured signer-policy verification before publication.
A cryptographically valid signature from an unenrolled signer is not sufficient.
Preflight failure leaves real refs unchanged; later signer drift preserves the
unpublished commit/tag for inspection without moving an immutable remote tag.
On the canonical
GitHub origin it reads GitHub-owned CI evidence for the exact source revision.
By default it accepts only the latest canonical main push CI run, completed successfully,
with successful complete job readback and all required aggregates. The exact
v0.3.0 exception below distinguishes release eligibility from complete coverage.
Missing, failed,
partial, ambiguous, source-mismatched or rerunning evidence denies; no caller-authored
JSON, old green attempt or local verdict can substitute. Repair or complete the
named source CI before retrying release. Other GitHub origins cannot borrow these
results. Non-GitHub repositories retain complete local verification.

A release child can reuse parent source verification only after the tag workflow
proves the child changes CHANGELOG.md alone, has one available parent, preserves
parent content outside additive Unreleased notes, and exactly reproduces the dated
release transformation. Changed source, modes, version or history denies reuse.
A tag name is passed as shell data, not interpolated into shell source.

The tag workflow builds its actual final version/revision-bound four platform
archives and skill bundle once, verifies checksums and archive structure, installs
the extracted native archive and runs the selected version's fleet/first-run proof
(plus browser acceptance for every version other than exactly 0.3.0).
Those exact bytes—not synthetic-source binaries—are then published. Independent
GitHub API readback must show the exact stable non-draft release, asset set, uploaded
state, byte sizes and SHA-256 digests matching local artifacts. API or upload
uncertainty is not reported as success and never authorizes deleting or replacing
a published release. The separately available advisory model review is not a
publication authority or a critical-path requirement.

## Timing and boundaries

Source CI has the job deadlines above; the tag job has a ten-minute deadline. Timed successful CI and
installed packaging runs are necessary performance evidence, not proof of a public
release. Cold runner/cache, external queueing and upload/API delay count toward
observed end-to-end latency; report them rather than silently excluding them.
Publication requires the owner's release instruction and configured signer; this
pipeline repair does not itself publish a new version, alter signing policy,
update an installed executable, restart a gateway or prove live Doer execution.

## Exact v0.3.0 bounded release profile

The owner-approved exception applies only to stable **0.3.0**, never adjacent
versions, prereleases, or the default verifier invocation. `release.sh` and the tag
workflow pass the actual release version to `verify-ci-evidence.py SOURCE VERSION`.
There is no environment waiver, caller-authored green receipt, log-to-green
conversion or fallback to an older run. Canonical repository/workflow/main-push,
exact SHA, latest attempt, pagination and stable service readback remain required.

`Release package proof` runs the version-bound 0.3.0 profile independently of
`Release package and installed proof`, which is explicitly skipped when the
committed Makefile declares exactly 0.3.0. Other versions retain unchanged full
browser acceptance. The skipped job retains its real GitHub conclusion, rather
than repeating waived Chrome acceptance or reporting a successful no-op. This adds one
job, not a new qualification campaign or hidden `continue-on-error`. The
`Exact-head release readiness` aggregate now means **eligibility**, not all-coverage
success: it requires the package job, repository checks, every normal/race shard,
vet, authority denial, vulnerability scan and the unchanged `Race tests` aggregate.
The standalone evidence verifier still independently requires all those exact jobs.
For every other version it additionally requires full installed/browser proof
and an overall successful workflow, so other releases stay strict by default.

For 0.3.0 only, completed optional installed/browser outcomes may be success,
failure, skipped, cancelled or timed_out; missing, ambiguous, running or
source-mismatched optional evidence still denies. A terminal workflow conclusion
of failure or cancelled is not relabelled success: the generated service-derived
report returns `status=release_eligible`, `profile=v0.3.0-bounded`, the original
`workflow_conclusion`, `coverage_complete` and each optional conclusion. Required
job failure, cancellation or skip always denies, even with green aggregates.
Historical passing evidence remains historical; previous failures are not erased.

The final 0.3.0 archive verifier skips only the integrated console/browser stage,
explicitly reporting `console=skipped_optional_v0.3.0 real_chrome=not_exercised`.
It still builds all four archives with exact clean VCS version/revision, validates
archive structure and checksums, validates/evaluates/builds/verifies the skill
bundle, exercises packaged skills installation/inventory, unchanged v0.2.29
updater compatibility, native provenance, bare first-run fail-closed/no-mutation,
and the existing non-browser fleet proof. That proof uses a bounded synthetic
Hermes gateway; it is not real-provider qualification. Signing, immutable tags,
source-parent integrity and exact published-asset readback remain unchanged.
The known Chrome `Fetch.continueRequest` invalid-interception failure is neither
repaired nor claimed passed by this exception. No workstation update is implied.

Run 42080286852 at 2c0bf9e had a cancelled race orchestration job despite retained
logs reporting both partitions PASS, supervisor success and exit 0. That cancelled
job remains **cancelled and ineligible**; logs cannot replace its GitHub verdict.
Only that lane's outer hosted job cap is raised from ten to sixteen minutes to
allow setup/cleanup margin, matching race API's existing cap. Test partitions,
package alarms, uncached execution and security/resource bounds are unchanged.
A fresh exact-head green race orchestration job is still mandatory; this change
does not claim fresh hosted success or the sub-600-second publication target.
