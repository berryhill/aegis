# Missing-config transport recovery review

Contract: safe exact-development orphan recovery, followed by initialization progress; no broad missing-config wipe, no production authority weakening.

Changed: reset service/command, Linux regressions, CHANGELOG, PATH_LAYOUT, SECURITY, README and QUICKSTART. LICENSE, CONTRIBUTING, CODE_OF_CONDUCT, threat model, architecture diagram, no-key demo, recording documentation and retained typescript/timing, contributor backlog, release workflow and archive/checksum verifier reviewed as unaffected. Ordinary bootstrap still never removes transport; this is separate explicit reset. No release assets were rebuilt/published or remotely checksum-verified. No-key/provider recording was not replayed; no live systemd/laptop proof is claimed.

Verification: `timeout --kill-after=5s 110s env GOMAXPROCS=2 go test -p 1 -timeout 95s ./internal/reset ./internal/initialize ./internal/command -run 'TestOrphan|Test.*Reset|TestFreshBootstrapTransport|TestBootstrapTransport|TestPlanRejectsPresentTransport' -count=1` passed (reset 0.547s, initialize 0.007s, command 77.234s). `git diff --check` passed. No full suite or long proofs were run in this bounded continuation. Parent owns independent review, exact-head CI, and merge.
