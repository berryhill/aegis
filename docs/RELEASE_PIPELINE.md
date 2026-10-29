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

Packing uses actual hosted run **40884797403** (first compact candidate),
which cancelled race cli-command and manager-a/c/d at the unchanged 600-second
job deadline. Completed phases showed command compile ~109s plus base ~364s
before final was unfinished; manager compile ~85s plus base ~405s before final
was unfinished; resume ~285s before repair was unfinished; and disconnect ~252s
plus joint ~148s before readback was unfinished. These cancellations supersede
older execution-only packing estimates, not the successful baseline run
**40882148345** (57 PASS, 209.13 runner-minutes, 999s peak).

The mapping below is from that first compact candidate to this revision. Names
omit `manager-contribution-` and `cli-contribution-` in their respective rows;
`cli-group` means cmd/aegis + internal/console, not command partitions.

| Mode / lane | Old groups | New groups |
| --- | --- | --- |
| normal api-a | api-a | api-a, remainder-b |
| normal api-b | api-b | api-b, cli-group |
| normal implementation | implementation, implementation-contribution | same, remainder-a |
| normal orchestration | orchestration, orchestration-contribution, remainder-b | orchestration, orchestration-contribution, persistence |
| normal manager-a/b | existing manager packs | unchanged |
| normal cli | cli-group, command, root, final, disconnect, approval | command, root, final, disconnect, approval, update |
| normal support | persistence, update, remainder-a | removed; redistributed above |
| race orchestration | orchestration, orchestration-contribution | same, cli-group |
| race manager-a | manager, final | manager, cancel |
| race manager-b | restart, cancel | restart, final |
| race manager-c | resume, repair | resume, readback |
| race manager-d | disconnect, joint, readback | repair, joint |
| race manager-e | absent | disconnect, approval |
| race cli-command | command, final | command |
| race cli | cli-group, root, disconnect, approval | root, final, disconnect |
| race support-a/b | persistence + remainder-a / update + remainder-b | unchanged |

Race api-a/b and implementation are unchanged. Rounded planning sums including
package compile are manager-a ~562s, b ~533s, c ~500s, d ~508s,
e ~523s (both manager and command compile), cli-command ~473s and cli ~480s.
These are estimates, NOT successful hosted deadline evidence: setup and listing
must also fit the unchanged 600s. The whole CLI group moves without an extra
split-package compile. Successful implementation and support packs are retained.
Link/list reuse remains once per split package per lane, including mixed
manager-e; no new partitions or reduced coverage are introduced.
Compiler, inventory and partition elapsed times remain separate `go_lane_phase`
observations. Job alarms remain ten minutes except sixteen for race API.
Fresh complete successful exact-head hosted runs, including queue/setup and all
jobs, must establish deadline fit, runner-minute reduction and comparable speed.

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
