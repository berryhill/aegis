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

Root-dispatch, initialization, migration, reset, and updater tests must create and use an isolated temporary `HOME` and isolate `XDG_CONFIG_HOME`, `XDG_STATE_HOME`, canonical `.argis`, legacy XDG defaults, state, audit, checkpoint, authority, and KEK paths. They must never use the developer's real `~/.argis`, legacy installation, installed executable, Hermes profile, Ollama daemon/store, or external credentials as a target. Migration/reset tests must exercise preview/apply identity drift, exact confirmation, non-TTY/cancellation no-write behavior, unknown-file preservation, mode-`0775` legacy-parent behavior, and post-reset onboarding. Explicit configuration fixtures must be mode `0600`; insecure-permission tests must verify fail-closed behavior without rewriting the fixture.

Manager lifecycle tests must use disposable configuration/state, fake Hermes/Ollama processes or loopback fixtures, and PTYs rather than a developer's real model store or runtime profile. Cover cancellation at each intake stage, terminal restoration, EOF, expiry, first/second signals, rollback order, idempotent bounded cleanup, exact readiness reason codes, no-download discovery, declined/interrupted configuration, and certification/configuration drift.

## Change rules

Preserve the identity, trust-stanza, mandate, exact-approval, deterministic-provisioning, credential-binding, and fail-closed invariants in `AGENTS.md`, `specs/MVP.md`, and `research/2026-07-17-embedded-bbolt-credential-authority.md`. In particular, identity must remain external to the model; prompts, profile names, model conclusions, and stanza requests must never authenticate; every session must bind exactly one stanza; zero or multiple matches must deny; stanza authority must never be unioned; and any stanza or material-authority change must require a new mandate and clean session. Cobra and Echo handlers must call shared application services. Keep stdout machine-readable and diagnostics on stderr. Do not add model-generated provisioning shell, ambient credentials, wildcard authority, generic runtime secret retrieval, or claims of sandboxing/guaranteed zeroization.

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

The target validates exact stable SemVer and classifies fresh, resumable-local, completed-remote, and invalid states. A fresh release requires `HEAD` to equal `origin/main`, moves pending changelog entries into the release, and verifies the committed source plus proposed changelog in a disposable clone. Before changing the real repository, it creates and removes a signed preflight tag inside that clone; pinentry, key, or signing failures therefore leave no release commit behind. Other unstaged work is left untouched and excluded; pre-staged changes and pre-existing changelog edits are rejected. Hermes performs an advisory review with only the in-session `todo` toolset and cannot approve or publish.

If an atomic push fails after the local release commit and signed annotated tag are created, rerun the same command. Recovery accepts only the exact fail-closed state: the immutable tag signature and annotation verify; local `main` is its target; the single-parent release commit contains only the reproducible changelog preparation; and `origin/main` is either that commit or its verified parent. The script then re-verifies the exact tagged source and tag signature without regenerating the changelog, committing, moving, deleting, recreating, or re-signing the tag. It atomically pushes `main` and the tag when the remote is at the parent, or pushes only the existing tag when remote `main` already has the commit. A matching remote tag is reported without republication; any object/peeled-commit conflict, lightweight or bad tag, unexpected release file, staged state, or divergent remote fails with manual remediation. Force pushes are never used.

Invoking non-dry-run `make release` is the operator's publication authorization. Use `RELEASE_DRY_RUN=1 make release VERSION=...` to run locally safe classification and verification and print the exact action without changing worktree files, refs, or remotes. Dry-run does not create a signing preflight signature; recovery still verifies the existing signature.

The tag-triggered release workflow reruns formatting, tests, race tests, vet, and vulnerability checks; cross-builds Linux/macOS archives for amd64/arm64; verifies the embedded version and checksums; and creates the GitHub release. Tag creation and pushing are never delegated to Hermes. Until that workflow publishes a non-draft stable release, `aegis update` correctly continues to report the previous published version; it never treats local or remote Git tags as release assets.

Do not move, delete, replace, or reuse a suspicious or remotely published release tag. The resumable path is only for the exact already-created signed local release artifact described above; all other failed-tag states require inspection and explicit manual remediation.
