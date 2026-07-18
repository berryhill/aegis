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

Releases use stable Semantic Versioning. After the intended release commit passes CI, an authorized maintainer creates and pushes a `vMAJOR.MINOR.PATCH` tag, for example:

```sh
git tag -s v0.1.0 -m "Aegis v0.1.0"
git push origin v0.1.0
```

The tag-triggered release workflow rejects non-SemVer tags, reruns tests and vet, cross-builds Linux/macOS archives for amd64/arm64, verifies the embedded version and checksums, and creates the GitHub release. Tag creation and pushing are maintainer authorization and are never performed by the application or a pull-request workflow.
