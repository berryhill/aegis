# Contributing to Aegis

## Prerequisites

- Go 1.26.5 or newer
- Hermes Agent `>=0.18.0,<0.19.0` for real adapter checks
- Linux for Unix `SO_PEERCRED` API tests
- `govulncheck` for vulnerability scanning

## Setup and checks

```sh
go mod download
go build ./cmd/aegis
go test ./...
go test -race ./...
go vet ./...
go install golang.org/x/vuln/cmd/govulncheck@v1.6.0
govulncheck ./...
test -z "$(gofmt -l ./cmd ./internal)"
```

Run focused tests while developing, for example `go test -run TestUnixAPICompleteOperationalWorkflow ./internal/api` or `go test -race ./internal/credentials/...`. Tests must use temporary Aegis state and disposable `HERMES_HOME`; never modify a developer's normal Hermes profile. Credential tests must generate random fixture values in memory, verify that values do not enter databases/logs/errors/output, and never use real credentials. Keep authority databases on local filesystems and KEK fixtures separate from backup fixtures.

## Change rules

Preserve the identity, trust-stanza, mandate, exact-approval, deterministic-provisioning, credential-binding, and fail-closed invariants in `AGENTS.md`, `specs/MVP.md`, and `research/2026-07-17-embedded-bbolt-credential-authority.md`. Cobra and Echo handlers must call shared application services. Keep stdout machine-readable and diagnostics on stderr. Do not add model-generated provisioning shell, ambient credentials, wildcard authority, generic runtime secret retrieval, or claims of sandboxing/guaranteed zeroization.

Every behavior change requires tests and a review of README, security guidance, threat model, architecture, quickstart, demonstration, recording, release, and contributor-issue assets. Update only affected assets.

## Issues and pull requests

Use a focused issue from `docs/contributing/ISSUE_BACKLOG.md` or describe scope, security impact, acceptance criteria, and verification. Pull requests should be small, explain trust-boundary changes, list exact commands run, and identify any external verification blocker. Do not include credentials, generated state, binaries, or normal Hermes profile content.

## Releases

Releases use stable Semantic Versioning. Commit the release automation first, ensure `main` is clean and already matches `origin/main`, and run:

```sh
make release
# Later versions:
make release VERSION=0.1.1
# Exercise preparation/review without committing or publishing:
RELEASE_DRY_RUN=1 make release VERSION=0.1.1
```

The target validates exact stable SemVer, rejects dirty/non-`main`/unsynchronized/already-tagged state, moves pending changelog entries into the release, runs the complete local verification target, and asks Hermes one-shot for an advisory review with only the in-session `todo` toolset. Hermes cannot approve or publish the release. The script shows the changelog diff and requires the operator to type the exact tag on `/dev/tty` before it commits, creates a signed tag, and atomically pushes `main` with that tag.

The tag-triggered release workflow reruns formatting, tests, race tests, vet, and vulnerability checks; cross-builds Linux/macOS archives for amd64/arm64; verifies the embedded version and checksums; and creates the GitHub release. The explicit terminal confirmation is the release authorization; tag creation and pushing are never delegated to Hermes.
