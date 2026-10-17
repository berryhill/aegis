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

## Release transaction

`make release VERSION=MAJOR.MINOR.PATCH` retains its explicit publication authority,
configured signing identity, signing preflight, immutable signed tag, deterministic
changelog transformation, atomic push and bounded recovery rules. On the canonical
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
