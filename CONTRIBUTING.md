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
./scripts/verify_installed_mvi_test.sh
./scripts/verify_release_candidate_test.sh
./scripts/verify-installed-mvi.sh
```

Run focused tests while developing, for example `go test -run TestUnixAPICompleteOperationalWorkflow ./internal/api` or `go test -race ./internal/credentials/...`. Tests must use temporary Aegis state and disposable `HERMES_HOME`; never modify a developer's normal Hermes profile. Credential tests must generate random fixture values in memory, verify that values do not enter databases/logs/errors/output, and never use real credentials. Authority-passphrase tests must inject or build a fake pinentry, isolate display/session environment, and must never discover or invoke the developer's real helper, GPG/GPG-agent state, desktop keyring, or authority. Keep authority databases on local filesystems and KEK fixtures separate from backup fixtures.

Run `python3 -m unittest scripts/operator_acceptance_poc_test.py` for the hermetic operator-acceptance recorder checks. A live human-to-manager run is separately gated and must follow [docs/OPERATOR_ACCEPTANCE_POC.md](docs/OPERATOR_ACCEPTANCE_POC.md): use a disposable initialized authority, never a real credential, retain no protected plaintext, and do not report the fake PTY fixture as model success.

Root-dispatch, initialization, migration, reset, updater, and audit-delivery tests must create and use an isolated temporary `HOME` and isolate `XDG_CONFIG_HOME`, `XDG_STATE_HOME`, canonical `.argis`, legacy XDG defaults, state, audit, checkpoint, audit-outbox/projection, authority, and KEK paths. They must never use the developer's real `~/.argis`, legacy installation, installed executable, Hermes profile, Ollama daemon/store, or external credentials as a target. Audit-delivery tests must cover canonical ordering, bounded batches, restart reconciliation, tampered derived-state denial, readiness transitions, and rebuild without canonical event/checkpoint mutation. Initialization/migration tests must exercise Enter defaults, explicit yes/no, EOF, non-TTY no-write behavior, digest/identity drift, fake-pinentry encrypted authority creation/unlock/cancellation, real-PTY no-echo fallback and restoration, and incomplete-systemd recovery. Reset retains a default-deny `[y/N]` confirmation and must cover explicit acceptance, default/explicit decline, cancellation, unknown-file preservation, mode-`0775` legacy-parent behavior, and post-reset onboarding. Explicit configuration fixtures must be mode `0600`; insecure-permission tests must verify fail-closed behavior without rewriting the fixture.

Badger authority-maintenance tests must use a disposable mode-`0700` authority root and backup directory. Cover the closed-clean prerequisite, context cancellation while an open store holds the shared lease, non-replacing mode-`0600` export, corrupt/truncated/oversized backup denial, fixed disk-reserve denial before candidate mutation, logical recovery into a fresh inactive identity with canonical projection rebuild, exact native recovery/activation/rollback and typed post-selection outcomes, descriptor-relative marker/generation mutation, and identity-revalidated fail-closed garbage collection that preserves active, substituted, or unknown state. Never point export, import, recovery, activation, rollback, projection rebuild, or garbage collection at a developer authority or retained backup.

Production import changes must satisfy `internal/architecture/boundaries_test.go`; update its explicit package-family allowlist only when the reviewed architecture intentionally changes. Security-relevant authority persistence changes must retain real subprocess interruption/restart coverage, malformed and substituted identity denial, concurrent and race execution, bounded fuzz seeds, and generated-canary checks across persisted bytes and diagnostics.

Manager lifecycle and terminal-presentation tests must use disposable configuration/state, fake Hermes/Ollama processes or loopback fixtures, and PTYs rather than a developer's real model store or runtime profile. Terminal changes must cover rich and `AEGIS_ACCESSIBLE=1` profiles, 40-column/no-color output, multiline/history/paste/help keys, adversarial ANSI/OSC/DCS/control/bidi text, bounded event/transcript state, safe streaming across fragmented JSON/UTF-8/control sequences, proposal buffering, random-canary absence from every presentation surface, and raw/echo/canonical restoration. Cover cancellation at each intake stage, terminal restoration, EOF, expiry, first/second signals, rollback order, idempotent bounded cleanup, exact readiness reason codes, no-download discovery, declined/interrupted configuration, and certification/configuration drift.

## Change rules

Preserve the identity, trust-stanza, mandate, exact-approval, deterministic-provisioning, credential-binding, and fail-closed invariants in `AGENTS.md`, `specs/MVP.md`, and `research/2026-07-17-embedded-bbolt-credential-authority.md`. In particular, identity must remain external to the model; prompts, profile names, model conclusions, and stanza requests must never authenticate; every session must bind exactly one stanza; zero or multiple matches must deny; stanza authority must never be unioned; and any stanza or material-authority change must require a new mandate and clean session. Cobra and Echo handlers must call shared application services. Keep stdout machine-readable and diagnostics on stderr. Do not add model-generated provisioning shell, ambient credentials, wildcard authority, generic runtime secret retrieval, or claims of sandboxing/guaranteed zeroization.

Every behavior change requires tests and a review of README, security guidance, threat model, architecture, quickstart, demonstration, recording, release, and contributor-issue assets. Update only affected assets.

## Issues and pull requests

Use a focused issue from `docs/contributing/ISSUE_BACKLOG.md` or describe scope, security impact, acceptance criteria, and verification. Pull requests should be small, explain trust-boundary changes, list exact commands run, and identify any external verification blocker. Do not include credentials, generated state, binaries, or normal Hermes profile content.

## Releases

Releases use stable Semantic Versioning. Commit the release automation first, ensure `main` matches `origin/main`, and run:

```sh
make release
# Later versions:
make release VERSION=0.1.1
# Exercise preparation/review without committing or publishing:
RELEASE_DRY_RUN=1 make release VERSION=0.1.1
```

The target validates exact stable SemVer and classifies fresh, resumable-local, completed-remote, and invalid states. A fresh release requires `HEAD` to equal `origin/main`, moves pending changelog entries—including an unstaged `CHANGELOG.md` edit—into the release, and verifies the committed source plus proposed changelog in a disposable clone. Before changing the real repository, it creates and removes a signed preflight tag inside that clone; pinentry, key, or signing failures therefore leave no release commit behind. Other unstaged work is left untouched and excluded; pre-staged changes are rejected. A failed preparation or dry run restores the pre-existing changelog exactly. Hermes performs an advisory review with only the in-session `todo` toolset and cannot approve or publish.

If an atomic push fails after the local release commit and signed annotated tag are created, rerun the same command. Recovery accepts only the exact fail-closed state: the immutable tag signature and annotation verify; local `main` is its target; the single-parent release commit changes only the changelog, preserves the parent changelog outside additive Unreleased entries, and reproduces the release transformation; and `origin/main` is either that commit or its verified parent. The script then re-verifies the exact tagged source and tag signature without regenerating the changelog, committing, moving, deleting, recreating, or re-signing the tag. It atomically pushes `main` and the tag when the remote is at the parent, or pushes only the existing tag when remote `main` already has the commit. A matching remote tag is reported without republication; any object/peeled-commit conflict, lightweight or bad tag, unexpected release file, staged state, or divergent remote fails with manual remediation. Force pushes are never used.

Invoking non-dry-run `make release` is the operator's publication authorization. Use `RELEASE_DRY_RUN=1 make release VERSION=...` to run locally safe classification and verification and print the exact action without changing worktree files, refs, or remotes. Dry-run does not create a signing preflight signature; recovery still verifies the existing signature.

For an owner-selected candidate, first run `scripts/verify-release-candidate.sh` exactly as documented in `docs/QUICKSTART.md`. Its five explicit inputs bind a stable version, clean `HEAD`, real non-symlink Hermes executable, empty repository-local evidence workspace, and strict bounded decision record. The verifier builds the four archives once, reuses the extracted native binary by digest, rehearses replacement/rollback and withdrawal only below that workspace, and records `published=false`. A `release` value in this local record does not push a tag, authenticate the author, or replace the non-dry-run `make release` authorization boundary. `hold` and `withdraw` likewise record a decision without moving or deleting any repository or GitHub release ref.

The tag-triggered release workflow reruns formatting, tests, race tests, vet, and vulnerability checks; then uses the same `scripts/verify-installed-mvi.sh` path as ordinary CI to cross-build Linux/macOS archives for amd64/arm64, verify checksums and the extracted native binary's embedded version, and prove isolated bare non-TTY first run fails closed without production-state creation. It then creates the GitHub release. Tag creation and pushing are never delegated to Hermes. Until that workflow publishes a non-draft stable release, `aegis update` correctly continues to report the previous published version; it never treats local or remote Git tags as release assets.

Do not move, delete, replace, or reuse a suspicious or remotely published release tag. The resumable path is only for the exact already-created signed local release artifact described above; all other failed-tag states require inspection and explicit manual remediation.
