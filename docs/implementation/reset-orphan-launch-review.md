# Missing-config transport recovery review

Contract: safe exact-development orphan recovery, followed by initialization progress; no broad missing-config wipe, no production authority weakening.

Changed: reset service/command, Linux regressions, CHANGELOG, PATH_LAYOUT, SECURITY, README and QUICKSTART. LICENSE, CONTRIBUTING, CODE_OF_CONDUCT, threat model, architecture diagram, no-key demo, recording documentation and retained typescript/timing, contributor backlog, release workflow and archive/checksum verifier reviewed as unaffected. Ordinary bootstrap still never removes transport; this is separate explicit reset. No release assets were rebuilt/published or remotely checksum-verified. No-key/provider recording was not replayed; no live systemd/laptop proof is claimed.

Historical pre-security-follow-up verification (not evidence for the later `3114520` command test): `timeout --kill-after=5s 110s env GOMAXPROCS=2 go test -p 1 -timeout 95s ./internal/reset ./internal/initialize ./internal/command -run 'TestOrphan|Test.*Reset|TestFreshBootstrapTransport|TestBootstrapTransport|TestPlanRejectsPresentTransport' -count=1` passed (reset 0.547s, initialize 0.007s, command 77.234s). `git diff --check` passed. No full suite or long proofs were run in this bounded continuation. Parent owns independent review, exact-head CI, and merge.

## PR269 independent security follow-up

Reproduced RED: relative and alias non-listening binds were accepted, and reset bypassed a startup-held flock. Replaced path-name inventory with Linux SOCK_DIAG_BY_FAMILY / UNIX_DIAG_VFS identity matching and complete-dump validation. ABI checked against installed linux/unix_diag.h, pinned x/sys v0.47.0, and Linux v6.17 net/unix/diag.c (raw kernel s_dev, not stat encoding). Recovery takes and validates the persistent API lifecycle lock and retains it through revalidation, unlink, fsync and absence readback. No changes to dev/config-absent admission or production authentication.

Focused GREEN: `GOMAXPROCS=2 go test -race -p 1 ./internal/reset -run TestOrphan -count=1 -timeout 60s` passed (2.289s), including relative/alias bound sockets, startup-held contention, recovery-held exclusion, unsafe symlink/hardlink/writable/directory lock rejection, existing identity replacement, denials and reset-to-initialize proof. No full suite, live service, laptop or merge performed. Additional targeted reset/initialize checks passed (0.307s/0.005s). The separately selected command `TestResetOrphanThenBare` exceeded a 60s timeout waiting in host pinentry after entering initialization; no command-level end-to-end pass was established at `3114520`. The hermetic follow-up below supersedes that limitation.

Launch assets re-inspected: PATH_LAYOUT, SECURITY, CHANGELOG, architecture and threat model updated; README, LICENSE, CONTRIBUTING, CODE_OF_CONDUCT, quickstart, no-key demo/script, recording docs/typescript/timing, contributor backlog, release workflow and archive verifier unaffected. No release binaries/checksums rebuilt or remotely verified; no demo/recording replay or release qualification claimed.

## PR269 hermetic command follow-up

`TestResetOrphanThenBare` now injects the existing `sequencePassphrases` fixture with one ephemeral generated test value and uses `bootstrapApprovalReader` to deliver initialization approval followed by explicit credential-custody decline. It never falls through to host pinentry. The test requires: declined reset preserves the exact socket inode; confirmed development reset removes that pathname; subsequent bare startup returns without error, persists valid configuration, consumes exactly one protected-input request, and reaches credential-custody selection without transport denial. Declined custody must leave the socket and credential authority/key artifacts absent. Authentication and gateway operations are injected test boundaries; this is not installed-binary, real-pinentry, live-service, or complete manager-onboarding proof. No production code or authentication behavior changed.

Focused verification on the repaired test:

- `GOMAXPROCS=2 go test -race -p 1 ./internal/command -run '^TestResetOrphanThenBare$' -count=5 -timeout 60s -v` — five passes, package 5.998s.
- `GOMAXPROCS=2 go test -p 1 ./internal/command -run 'TestResetOrphanThenBare|TestFreshBootstrapTransport|TestBootstrapTransport|TestExplicitInitDecline' -count=1 -timeout 60s` — passed, 1.787s.
- `GOMAXPROCS=2 go test -race -p 1 ./internal/reset ./internal/initialize -run 'TestOrphan|TestPlanRejectsPresentTransport' -count=1 -timeout 60s` — passed, 2.123s / 1.019s.

Launch impact review: README, LICENSE, SECURITY, CONTRIBUTING, CODE_OF_CONDUCT, CHANGELOG, threat model, architecture, quickstart, no-key demo/script, recording docs/typescript/timing, contributor backlog, release workflow and archive verifier re-inspected and unaffected by this test-only repair. This review document corrects the historical command-coverage claim. No release binaries/checksums were rebuilt or remotely verified; no demo/recording replay, full suite, or merge performed. Parent retains full-suite/CI/merge responsibility.
