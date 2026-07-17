# Contributing to Aegis

## Prerequisites

- Go 1.26 or newer
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

Run focused tests while developing, for example `go test -run TestUnixAPICompleteOperationalWorkflow ./internal/api`. Tests must use temporary Aegis state and disposable `HERMES_HOME`; never modify a developer's normal Hermes profile.

## Change rules

Preserve the identity, trust-stanza, mandate, exact-approval, deterministic-provisioning, and fail-closed invariants in `AGENTS.md` and `specs/MVP.md`. Cobra and Echo handlers must call shared application services. Keep stdout machine-readable and diagnostics on stderr. Do not add model-generated provisioning shell, ambient credentials, wildcard authority, or claims of sandboxing.

Every behavior change requires tests and a review of README, security guidance, threat model, architecture, quickstart, demonstration, recording, release, and contributor-issue assets. Update only affected assets.

## Issues and pull requests

Use a focused issue from `docs/contributing/ISSUE_BACKLOG.md` or describe scope, security impact, acceptance criteria, and verification. Pull requests should be small, explain trust-boundary changes, list exact commands run, and identify any external verification blocker. Do not include credentials, generated state, binaries, or normal Hermes profile content.
