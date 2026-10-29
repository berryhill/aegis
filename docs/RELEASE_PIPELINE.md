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

Ordinary source CI has **27 expanded jobs**: seven normal and thirteen race Go
lanes plus seven repository, packaging, security and aggregate jobs. There are no
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

The mapping below is specifically **40888280401 -> this revision**. All other
lanes/groups are unchanged. `cli` means `cmd/aegis` + `internal/console`, not the
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

All 25 groups remain exact-once in each mode, with the same seven normal and
thirteen race lane names plus seven fixed jobs: 27 jobs total. No matrix, job,
partition, timeout, resource budget or native unexpected-zero-exit guard changes.
Compile/list reuse remains once per split package per lane. Compiler, inventory
and partition elapsed times remain separate `go_lane_phase` observations. Job
alarms remain ten minutes except sixteen for race API; package alarms remain
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
It accepts only the latest canonical main push CI run, completed successfully,
with successful complete job readback and all required aggregates. Missing, failed,
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
the extracted native archive and runs the complete browser/fleet/first-run proof.
Those exact bytes—not synthetic-source binaries—are then published. Independent
GitHub API readback must show the exact stable non-draft release, asset set, uploaded
state, byte sizes and SHA-256 digests matching local artifacts. API or upload
uncertainty is not reported as success and never authorizes deleting or replacing
a published release. The separately available advisory model review is not a
publication authority or a critical-path requirement.

## Timing and boundaries

Source CI and the tag job have ten-minute job deadlines. Timed successful CI and
installed packaging runs are necessary performance evidence, not proof of a public
release. Cold runner/cache, external queueing and upload/API delay count toward
observed end-to-end latency; report them rather than silently excluding them.
Publication requires the owner's release instruction and configured signer; this
pipeline repair does not itself publish a new version, alter signing policy,
update an installed executable, restart a gateway or prove live Doer execution.
