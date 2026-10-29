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

Ordinary source CI has **27 expanded jobs**: eight normal and twelve race Go
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

Packing now uses retained source-f603 hosted run **40872151401**, which invalidated
older run 40871817726 estimates. Job names/counts stay unchanged. The following
mapping supersedes the first compact candidate (names omit `manager-contribution-`
and `cli-contribution-` within their respective rows):

| Mode / lane | Old groups | New groups | New execution sum (s) |
| --- | --- | --- | ---: |
| normal manager-a | manager, repair, resume, joint | manager, restart, final, readback | 469.098 |
| normal manager-b | disconnect, restart, cancel, final, readback | repair, disconnect, resume, joint, cancel | 509.365 |
| race manager-a | manager, repair, disconnect | manager, final | 505.162 |
| race manager-b | resume, restart | restart, cancel | 473.598 |
| race manager-c | joint, cancel | resume, repair | 491.935* |
| race manager-d | final, readback | disconnect, joint, readback | 491.791 |
| race cli-command | command, root, final, disconnect, approval | command, final | 403.915 |
| race cli | cli | cli, root, disconnect, approval | 390.665 |
| race support-a | remainder-a, remainder-b | persistence, remainder-a | 475.562 |
| race support-b | persistence, update | update, remainder-b | 453.215 |

All other lanes retain their mapping. *Repair contributes a **failed** 242.858s
observation, NOT a passing cost. The metrics packet lacks complete implementation
and race api-a measurements; no missing timing is imputed. Old race manager-a,
manager-b and cli-command sums exceeded 600s; old support-b was 580.054s. These
are package execution sums, not measured compact runtimes: setup, cold compilation
and listing still count against the unchanged deadlines. Link/list reuse applies
only within the same package/lane; the support lanes retain stable package packs.
Compiler, inventory and partition elapsed times are emitted separately as
`go_lane_phase` observations. Job alarms remain ten minutes except sixteen for
race API. No hosted runner-minute reduction, deadline fit or no-latency-regression
claim follows from this topology or local tests alone; compare complete successful
exact-head hosted runs, including queue/setup and all jobs.

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
