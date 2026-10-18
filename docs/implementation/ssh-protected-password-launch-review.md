# Interactive SSH protected-password repair

## Scope and diagnosis

Base: `23e0ed9073a92acc8837364128619a491229f1de`, branch `fix/ssh-protected-password`. Local source candidate only; no commit, push, installation, production recovery, service action, or release.

`protectedPinentryOptions` already supplies validated terminal descriptor identity and TERM through Assuan; the environment allowlist already carries GPG_TTY. Adding those again does not repair this failure. On a real controlling/foreground Linux PTY, the installed `/usr/bin/pinentry-curses`, launched with the same separate process group as `configureProtectedProcess`, acknowledges setup and then stops at GETPIN (`/proc` state `T (stopped)`). Its process group differs from the terminal foreground group. Background terminal reads trigger job-control stopping; no secret was entered in this reproduction. The existing timeout cleanup kills the group, producing the reported GETPIN timeout.

The shared password/passphrase service now selects its existing Aegis-owned no-echo terminal route **before any helper launch** on Linux when stdin and diagnostic stderr are terminal-backed. The cancellation-safe Linux reader remains bounded by the existing two-minute per-request limit (or earlier parent deadline). Non-Linux helper selection is unchanged. An explicit absolute `--pinentry-executable` still wins: SSH operators should omit it for the native route. There is still no fallback after attempted GETPIN, no widened authentication TTL, and no new credential delivery channel. Process-group isolation for helper cancellation is retained, not weakened to make curses work.

Root injects this shared provider; gateway recovery's installed journey passes Cobra stdin/stderr into it. The existing principal identity/verifier checks, fresh admission and exact recovery confirmation remain unchanged. In particular, acquiring bytes is not authentication: `installedJourney.admit` still verifies the enrolled password and repeats admission before recovery effects.

## Verification

All test temp/state paths were redirected beneath repository-local `.scratch/test-tmp`; no real `.aegis` or systemd state was used. The new session-leader/controlling-PTY subprocess regression exercises real foreground terminal reads, with random in-memory test values supplied over a private fixture pipe and the PTY. Captures are withheld from error reports and tested for non-disclosure. It verifies authentication intake, independent create/confirm intake, ECHO disabled at each prompt, exact terminal restoration, context cancellation with queued input, actual Ctrl-C, EOF, timeout, missing terminal pair, and explicit-helper cancellation/timeout/protocol failure without a second terminal prompt. Explicit-helper failures must reach GETPIN.

Passed final focused race run:

```sh
TMPDIR="$PWD/.scratch/test-tmp" GOMAXPROCS=2 go test -race -p=1 ./internal/command -run 'TestProtectedPassword|TestPinentry|TestPrincipalPassword|Test.*Recovery|Test.*Recover|TestReadPrincipalPassword' -count=1 -timeout 120s
```

Package duration: 49.018s. An earlier three-iteration focused race run passed in 131.132s before the final Linux-only selection guard and extra Ctrl-C regression. The final run includes those changes. Existing cmd/aegis lifecycle/signal selection also passed (5.738s).

Related userservice recovery tests passed with one explicitly excluded inherited fixture:

```sh
TMPDIR="$PWD/.scratch/test-tmp" GOMAXPROCS=2 go test -race -p=1 ./internal/userservice -run 'TestRecovery' -skip 'TestRecoveryAncestryPolicy/accessible775' -count=1 -timeout=120s
```

Duration: 1.101s. The unexcluded run failed `TestRecoveryAncestryPolicy/accessible775`: it changes only its own temporary root to 0755 but assumes there is no earlier private ancestor. Repository-local TMPDIR under this private home retains an earlier restrictive search barrier, which the production policy intentionally accepts. No production access policy, shared ancestor mode or unrelated test was changed to hide this fixture limitation. This is not a full-suite green claim.

No live SSH-server or real-principal authentication acceptance is claimed. The real controlling-PTY proof covers the relevant SSH foreground-terminal boundary. Operator-only candidate recovery remains necessary; the model must not run it or receive the password/confirmation.

## Launch-asset impact review

| Required asset | Result |
| --- | --- |
| Root README | Updated protected prompt selection; commands/authority unchanged. |
| LICENSE | Reviewed, unaffected licensing. |
| SECURITY.md | Updated terminal-first Linux selection and explicit helper override; residual risks unchanged. |
| CONTRIBUTING.md | Reviewed existing isolated-home/state and generated-canary requirements; unchanged. |
| CODE_OF_CONDUCT.md | Reviewed, unaffected. |
| CHANGELOG.md | Added unreleased narrow repair and regression entry; historical releases unchanged. |
| Threat model (`docs/THREAT_MODEL.md`) | Updated terminal edge and controls; no new authority or secret channel. |
| Architecture diagram (`docs/ARCHITECTURE.md`) | Added Linux terminal-default edge and updated selection prose. |
| Five-minute quickstart (`docs/QUICKSTART.md`) | Updated prerequisites/troubleshooting. No production walkthrough run. |
| No-key demo (`docs/DEMO_NO_KEY.md`, `scripts/demo-no-key.sh`) | Reviewed; does not use protected input, unchanged. Not rerun against the host runtime under this isolated repair. |
| Short recording (`docs/RECORDING.md`, retained typescript/timing) | Read; historical no-key evidence, not SSH/password proof. Unchanged, no new capture fabricated. |
| GitHub release binaries/checksums (`.github/workflows/release.yml`, build/source and installed verification contracts) | Reviewed; no packaging change. No release/publication/checksum acceptance claim. Local candidate identities are provided separately; dirty development builds are not releases. |
| Focused contributor issues (`docs/contributing/ISSUE_BACKLOG.md`) | Reviewed; cross-platform pinentry campaign remains separate. No external issues created. |
| Focused protected-input and service recovery guides | Updated `docs/CREDENTIAL_AUTHORITY_SETUP.md` and `docs/SERVICE_RECOVERY.md`. |

Release readiness is not established by this local repair: full launch workflows, supported host runtime demonstration, refreshed SSH operator evidence, release archives/checksums, and external publication remain separate. Frozen source archive, content manifest and diff are retained under ignored `.scratch/frozen/`; the executable is `aegis-ssh-protected-password-candidate` at the checkout root, built through `scripts/build-source.sh`, with exact identities in `.scratch/frozen/identities.json`. An initial nested output build succeeded but its version invocation correctly denied because development executables must reside beside a real `go.mod`; that unusable nested output was removed rather than bypassing profile validation. No production executable was overwritten.
