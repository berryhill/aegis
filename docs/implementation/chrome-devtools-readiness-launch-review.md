# Chrome DevTools readiness race: launch impact and evidence

## Contract and scope

Repair the installed-console harness's existence-only `DevToolsActivePort`
read from release source `7433c5e19c46a954d4bb51b9795af2895ee52ef2`.
The same reader in the focused touch fixture uses the shared repair.
No application authority, runtime admission, CSP, credential handling, Chrome
launch flags, retry count, cgroup supervision or cleanup budget changes.

A complete decimal port line terminated by `\n`, in range 1–65535, is required.
Missing/empty/unterminated publications poll within the original 15-second
monotonic startup budget. Completed invalid ports and exited Chrome deny;
liveness and deadline are checked again before returning. The independently
bounded page-target poll remains responsible for actual target readiness.
The browser-path line is not consumed and need not end in a newline.

## Verification

- RED: running the new main-entry empty-file regression against the unmodified
  base module reproduces `IndexError: list index out of range` at line 711.
- GREEN: all eight deterministic readiness tests pass, covering missing → empty
  → partial → valid, timeout, invalid/nonnumeric/out-of-range ports, boundary
  ports, exit at startup or during read, and deadline expiry during read.
- `python3 scripts/verify-budget.py timeout 90s python3 -m unittest
  scripts.console_browser_test_test scripts.verification_resources_test
  scripts.console_session_lifetime_test scripts.doer_browser_journey_test_test -v`:
  59 tests pass in 1.926s under the existing fail-closed cgroup supervisor.
  This includes three real Chrome native-touch runs (normal, recovered first
  startup failure, and panned visual viewport); all complete actual CDP target
  connection and trusted gesture/navigation assertions, not mocked browser proof.
- `python3 scripts/console_security_test.py`: all ten reported source-security
  checks pass. These remain source-contract checks, not installed acceptance.
- `GOFLAGS=-p=1 GOMAXPROCS=2 python3 scripts/verify-budget.py timeout 120s
  ./scripts/build-source.sh ./aegis`: exits 0 (8.944s).
- `git diff --check`: passes.

The first Doer candidate incorrectly required a trailing newline on the
browser-path line. A bounded real-browser suite failed and hit its 90-second
outer timeout. A real Chrome probe established the actual unterminated
browser-path publication. A new deterministic regression failed against that
candidate before the correction; the full bounded suite then passed.
The initial Doer verdict is therefore not evidence for the final correction.

Doer ran one implementation attempt: Laya gate 6.018s and judgment 10.912s
(local/unpriced compute); Sol `gpt-6-sol` implementation 190.306s and Luna
`gpt-6-luna` completion 7.502s (ambient provider, included charge, tokens
unavailable); file verifier 0.0002s. Total 214.738s, reported known billed cost
$0. No diagnosis/retry step ran. Final correction and verification were performed
independently after the controller exited. Scratch evidence and controller JSON
are retained locally, not publication artifacts.

## Launch-asset review

- Updated `CHANGELOG.md` under Unreleased and this review record.
- Reviewed as unaffected: `README.md`, `LICENSE`, `SECURITY.md`,
  `CONTRIBUTING.md`, `CODE_OF_CONDUCT.md`, `docs/THREAT_MODEL.md`, and
  `docs/ARCHITECTURE.md` (including diagrams). Product/CLI/trust boundaries and
  the full-verification prerequisites are unchanged.
- Reviewed `docs/QUICKSTART.md`, `docs/DEMO_NO_KEY.md`, `docs/RECORDING.md`, and
  retained `docs/assets/aegis-no-key.{typescript,timing}`. The no-key script and
  historical recording do not exercise this startup reader; no command or
  demonstrated output changes and no new recording/live-provider claim.
- Reviewed `.github/workflows/release.yml`, `scripts/verify-release-archive.py`
  and release/archive/checksum descriptions: format, checksum and exact-source
  gates are unchanged. No release built/published, tag changed or checksum
  invented. Existing release/update workflow remains parent-owned and untouched.
- Reviewed `docs/contributing/ISSUE_BACKLOG.md`: contributor proposals unchanged;
  no remote issue created. Existing owner-designated reporting-route and other
  launch gaps are not closed by this harness fix.

Full clean exact-head installed-MVI acceptance, independent parent review,
PR/CI/merge and any new immutable release are downstream gates. Local real
Chrome touch checks are not full installed-console or live-provider acceptance.
