# Upgrade orphan transport launch review

Source-only candidate; no installed production state was read or changed.

Changed: CHANGELOG, SECURITY, threat model, architecture diagram and in-place
upgrade procedure. Exact service/principal admission and original password expiry
remain mandatory. Startup lock now spans no-replace quarantine and authority
validation, including absent-socket resumes. Existing reset authorization is not
reused: only its diagnostic/lock mechanics are shared.
The independent review found a missing-parent compatibility regression. The
revised candidate prepares configured missing directories privately beneath an
owned non-writable anchor after fresh admission. Dedicated regressions cover
missing-parent recovery, denied/cancelled/unsafe-anchor no-write behavior, and
exact stopped-service admission including active/PID/loaded-unit drift denials.

Reviewed unaffected: root README (links to upgrade detail), LICENSE,
CONTRIBUTING, CODE_OF_CONDUCT, five-minute QUICKSTART, DEMO_NO_KEY,
RECORDING and retained no-key recording files, RELEASE_PIPELINE, and local
contributor ISSUE_BACKLOG. Their workflows do not claim orphan-upgrade proof.
No CLI syntax, runtime contract, licensing, contributor policy, or no-key
workflow changes. No live Hermes/demo/model run was performed. Existing release
binaries/checksums were not regenerated or represented as this candidate.
Publication, live installed acceptance, exact-candidate release checksums and
independent full-suite review remain parent/operator gates. Existing documented
community contact/recording qualification blockers remain unchanged.

Verification: focused production Recover regressions (healthy, interrupted,
stopped-orphan with retained evidence and repeated resume, password cancellation,
configuration/verifier drift, expiry/deadline); real kernel bound/relative/alias
non-listening denials; unsafe lock, contention, denied admission, changed socket
and startup exclusion during validation. Focused tests pass normally and under
race instrumentation. Complete reset and userservice package suites pass.
Frozen source-built candidate and race/build logs are under
`.artifacts/upgrade-orphan/`; these are local evidence, not release assets.
