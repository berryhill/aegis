# Fast release launch-impact review

## Contract

Deliver the owner's under-ten-minute release target without removing source,
race, resource, signing, changelog-only-child, archive, installed/browser or
publication-identity gates. A timeout is not success and no unmeasured SLA is
claimed. Full local verification remains available; hosted CI performs the same
obligations in disjoint parallel jobs and canonical publication reuses exact
service-owned source results rather than repeating them serially.

## Assets

Updated: CHANGELOG.md, CONTRIBUTING.md, SECURITY.md, docs/ARCHITECTURE.md,
docs/THREAT_MODEL.md and new docs/RELEASE_PIPELINE.md. These describe the explicit
parent-source/child-packaging boundary, committed helper custody and GitHub
publication digest readback rather than claiming the whole source suite runs
again on a changelog-only child. CI, Make stages and release automation changed.

Reviewed as unaffected by automation-only changes: root README installation and
no-key path, LICENSE, CODE_OF_CONDUCT.md, docs/QUICKSTART.md, docs/DEMO_NO_KEY.md,
docs/RECORDING.md, retained docs/assets/aegis-no-key.typescript and timing,
and docs/contributing/ISSUE_BACKLOG.md. No runtime/Console/API/charter/model or
credential boundary changed. No unrelated asset rewrite, new contributor issue,
recording, production update or gateway restart is included.

Release binaries/checksums are affected delivery evidence: the actual final
version/revision's four platform archives, skill archive and SHA256SUMS must pass
the installed verifier and then match remote publication readback. Local fixture
and synthetic-provider success is not native execution on all platforms, public
release proof or successful live Doer execution.

## Focused evidence

Parent ran 40 passing focused tests covering real Go package fixture examples,
fuzz seeds and subtests; disjoint package coverage; mandatory workflow topology;
all new security tests wired into Make; strict all-success aggregation; API
pagination/latest-run/attempt/readback drift; missing or failed evidence; exact
publication asset/digest comparisons; canonical CLI fresh and recovery paths;
noncanonical denial; and dirty-helper exclusion before refs change. The legacy
release-recovery shell suite, shell syntax and git diff whitespace checks also
passed. Parent checked the real inventory: 56 packages, each exactly once across
five groups per mode. These are local scoped results, not hosted timing proof.

Independent initial review found the working-tree helper boundary and unwired
tests. The helper now executes only inside the committed clone and the new
security suites are explicit verify-repository-stage dependencies. Same-run
failed-jobs-only retries use GitHub's latest-job projection with stable green
run/attempt readback before and after; old-run fallback remains denied.

## Remainder headroom follow-up

The original race/remainder job exhausted its ten-minute deadline again. Replace
only that group's topology: sort the exact unassigned package set and alternate
entries into remainder-a and remainder-b (currently 24 and 23 packages). Both
normal and race matrices and canonical evidence require both names. Regression
coverage requires sorted disjoint exact-once coverage, exact matrix equality and
fail-closed evidence when any new mode/shard is missing. Ten-minute job deadlines,
cgroup budgets, complete uncached package-test flags and every required aggregate
gate remain unchanged. No original remainder retry or deadline increase is used.

Reviewed the launch assets listed above for this topology-only follow-up: only
CHANGELOG.md and this existing review require changes. Runtime behavior, public
commands, supported versions, security boundaries, diagrams, quickstart, no-key
demo, recording and contributor materials are unaffected. Existing released
binaries/checksums are not regenerated or represented as candidate delivery proof.
Hosted timing, merge and release remain parent-owned acceptance, not local claims.

## Prepared changelog whitespace repair

Inspected immutable v0.2.32 commit 3ad44ed601a8923a184be05287fb41f92f99a3ff
and its parent: preparation removed one blank line before the existing 0.2.31
heading. Preparation now inserts only the new heading, retaining the complete
source suffix rather than stripping pending-section newlines. The strict final
classifier and immutable-tag recovery validation are unchanged; arbitrary
historical edits remain denied. Regression tests prepare a real changelog-only
fixture commit and run final prepared classification with two, three and four
separator newlines, then reject a historical edit.

Reviewed every asset in the Assets inventory above: only CHANGELOG.md and this
ledger change. No runtime, authority, API, installation, quickstart, no-key demo,
recording, license, conduct, contributor-issue or architecture claim changes.
Existing release binaries/checksums are untouched; final immutable 0.2.33 archive,
checksum and hosted-publication evidence remain parent-owned, not local proof.

## PR355 measured updater isolation

Run `39502940004` at `f1aae59cb1b4a073556f2c84d05db8a80b2cd63c`
exhausted the unchanged ten-minute race/remainder-a job deadline twice. GitHub
job `118909490259` explicitly annotates "The job has exceeded the maximum
execution time of 10m0s"; this was not a test assertion failure. Both attempts
actually printed successful results for all packages before final job completion,
but too late to leave safe cleanup headroom. A successful individual step does
not override the authoritative cancelled job/run result.

Measured package elapsed times from the full job logs:

| Package/cost | Attempt 1 (`118904678540`) | Attempt 2 (`118909490259`) |
| --- | ---: | ---: |
| `internal/update` race | 373.013s | 368.026s |
| `internal/persistence/authority/badger` race | 60.954s | 59.794s |
| All other remainder-a package tests combined (excluding update) | 115.850s | 114.501s |

Attempt 1 race/remainder-b (`118904677872`) finished successfully in 141s
including setup/cleanup. Sorted alternating package counts were therefore not a
runtime balance: update dominated one remainder while sharing compilation,
setup and cumulative test costs. The minimum bounded repair promotes the whole
update package to one named shard in **both** modes, rather than splitting
individual tests, changing cryptography or widening any deadline. Recomputing
sorted exact-set subtraction leaves 23 packages in each remainder; the inventory
is 56 packages, exactly once across seven groups per mode (1/2/3/3/1/23/23).
No package or test is skipped, cached, duplicated between groups or filtered.

The workflow and canonical evidence require both new updater job names. Regression
coverage pins updater isolation under inventory growth/reordering, missing-job
denial in both modes, exact matrix/evidence equality, complete uncached commands,
real Go examples/subtests/fuzz seeds, all-success aggregation and resource limits.
Ten-minute job deadlines, eight-minute Go package alarms and the complete cgroup
budget remain unchanged. Hosted timing still requires fresh exact-head CI; local
runs are not a hosted SLA or independent acceptance.

Local scoped validation with Go 1.26.9, `GOFLAGS=-p=1`, `GOMAXPROCS=2`
and the unchanged verification supervisor passed 45 focused script regressions.
All three affected complete race groups passed: update package 356.279s / cgroup
357.585s; remainder-a cgroup 252.923s; remainder-b cgroup 68.241s. These were
bounded local runs, not unchanged hosted retries. Broader Python discovery was
also attempted but cannot serve as full acceptance: eight unrelated browser
modules lack Playwright in the system interpreter (and one prerequisite skipped).
The focused topology/resource/evidence suites themselves passed without skips.
Detailed job logs, annotations, real inventory and local results are retained
outside source control under `.candidate/issue334-followup/`.

Launch impact: reviewed the Assets inventory above and its runtime/security,
architecture, installation, quickstart, no-key demo, recording, licenses and
contributor-material claims as unaffected by CI-only topology. Only CHANGELOG.md
and this ledger need documentation edits. No release binaries/checksums are
regenerated, no tag or release is published, and production behavior is unchanged.

## Remaining acceptance

### Measured execution deadline follow-up

The execution job at `232dbfe` completed `internal/implementation` in 382.390s
before `internal/orchestration` ran into the ten-minute job deadline. This is
cumulative whole-package cost, not evidence of a failing assertion. Replace
`execution` with singleton `implementation` and `orchestration` groups in both
normal and race modes. Exact required names are `Go normal / implementation`,
`Go race / implementation`, `Go normal / orchestration` and
`Go race / orchestration`; legacy execution checks cannot substitute for them.
The complete package commands remain uncached and unfiltered with eight-minute
alarms, ten-minute jobs and unchanged supervisor limits. No product or delivery
test changes belong to this topology repair. The existing API split is unchanged.
Fresh exact-head hosted success for both successors in both modes and all other
mandatory checks remains required; local focused regressions prove topology and
fail-closed evidence only. Launch impact is CI configuration/evidence and these
resource/launch notes plus CHANGELOG; runtime, security, architecture, quickstart,
no-key demo, recordings, licensing and release artifact formats are unaffected.

### API cumulative-deadline follow-up

The seven-group, whole-package topology above is historical. Hosted run
40055537827 exhausted the API race package's 12-minute alarm while its active
authenticated HTTP test had just started. The repair replaces API with `api-a`
and `api-b` in both modes. Only API compiles twice; every other package remains
exact-once by validated `go list ./...` set subtraction. Each API group derives
the same sorted inventory from `go test [-race] -count=1 -timeout=... -list .`
and alternates top-level names. An anchored, escaped parent-name regex preserves
children and fuzz seeds without prefix matches. Benchmarks are recognized but
are not default executions. Real TestMain runs for discovery and each partition.
Empty, duplicate, malformed or incomplete discovery fails closed.

Alarms remain eight minutes normal and twelve minutes API race; outer API jobs
remain ten minutes normal and sixteen minutes race. Toolchain, CPU, memory and
supervisor are unchanged. Exact CI evidence requires every partition and rejects
unknown, missing, skipped or failed jobs; all-success aggregate guards remain.
No product/authentication code, KDF, test body or test skip changes. A real Go
fixture proves every default leaf once across partitions in normal and race
modes, including TestMain, subtests, examples, fuzz seeds and future tests.

Focused local script validation passed; production partition timing and fresh
hosted exact-head success remain separate acceptance. CI-only launch impact
affects this ledger and CHANGELOG.md, not runtime/security, architecture,
installation, quickstart, demos, recording, licenses, contributor material or
release bytes. No release or publication is performed by this source repair.

Exact-head hosted CI must exercise every group, repository stage, authority and
vulnerability/vet gate plus final archive/browser proof. Measure created-to-finish
elapsed time, including setup/queue; job deadlines alone are insufficient. Final
publication timing additionally requires an owner-authorized real release and its
configured signer. This source repair itself does not publish a new tag/version.
