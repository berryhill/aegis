# Issue 352 — scoped Console port regression qualification

## Candidate and scope

Base: `e524371343bae72ee11f3c3fe01dc4c4d85cfe45`, workspace
`/home/silas/.hermes/profiles/xander/workspace/aegis-352`.
This is a dirty, local source candidate, not installed/live acceptance or closure
of the broader issue. Only the installed fleet verifier, its regression tests,
and this note are changed. The two unrelated `.issue327-state-*` directories
are preserved. No build, package, commit, push, installation, deployment,
provider execution or product-authority operation was performed.

## Obligation, defect and repair

The Console proof must consume the lifetime helper's exported
`AEGIS_ACCEPTANCE_PORT`, not silently select an unrelated ephemeral port.
The regression exercised real `main()` setup with mocked sockets/processes;
before repair, helper values 8450 and 8469 both produced mocked port 47965.

The verifier now accepts only four ASCII decimal digits in inclusive range
8450–8469. Empty, noncanonical, non-ASCII, reserved 8448/8098 and out-of-range
values deny before socket creation or subprocess invocation. An exclusive
loopback bind probe fails closed when occupied; there is no alternate-port
retry, socket reuse, listener takeover or new claim/pool logic. The existing
helper remains the lifetime-lock owner. A probe cannot eliminate an
uncooperative bind race; the actual server bind remains final, and existing
startup-exit failure handling remains in place.

When the variable is absent, ephemeral loopback allocation remains available
for existing hermetic CI compatibility. This fallback is NOT a workstation
engineering port claim and does not waive helper custody for candidate services.
The same generated configuration, origin and Unix socket port identity are
retained across clean stop/restart phases. `stop_server` is unchanged.

## Actual checks and retained RED evidence

Inspected `scripts/acceptance_port.py` and ran:

```text
python3 scripts/acceptance_port.py --help
exit 0
usage: acceptance_port.py [-h] [--pool POOL] [--ports PORTS [PORTS ...]] ...
Linux foreground candidate-port custody. Coordination, not a sandbox.
```

Added causal regression tests BEFORE repairing the verifier and ran:

```text
python3 -m unittest scripts.verify_installed_fleet_vertical_test -v
Ran 17 tests in 0.587s
FAILED (failures=20, errors=1)
exit 1
```

Selected actual RED diagnostics (full output retained in worker tool transcript):

```text
(port=8450) ... FAIL
AssertionError: 47965 != 8450
(port=8469) ... FAIL
AssertionError: 47965 != 8469
(value='') ... FAIL
AssertionError: SystemExit not raised
(value='8448') ... FAIL
AssertionError: SystemExit not raised
(value='8098') ... FAIL
AssertionError: SystemExit not raised
ERROR: test_occupied_helper_port_fails_without_fallback_or_takeover
  probe.bind(("127.0.0.1", 0))
OSError: address already in use
```

The other invalid-value subcases also failed to raise `SystemExit`. After the
source repair, the same command returned:

```text
Ran 17 tests in 0.447s
OK
exit 0
```

After adding a test-only type-narrowing assertion, final focused rerun:

```text
python3 -m unittest scripts.verify_installed_fleet_vertical_test -v
Ran 17 tests in 0.599s
OK
exit 0
```

Coverage: supplied lower/upper boundaries; absent fallback; empty, signed,
leading-zero, whitespace/newline, decimal, Unicode, underscore, reserved and
out-of-range inputs; occupied-port failure with no service launch/reuse/fallback;
real main configuration wiring; two mocked browser server phases using the same
origin/config and clean shutdown; existing sibling `runpy` loading compatibility
and pre-existing proof contracts. New socket/process operations are mocked: no
candidate listeners or actual Console/product operations were started.

## Launch impact and qualification boundary

Engineering harness only; no runtime, CLI, authority, release packaging or
user-facing product behavior changes. The qualification note is the scoped
launch-impact artifact. No launch assets were edited. Broad launch-asset audit,
release binaries/checksums and external contributor issues were not inspected or
qualified in this narrowly bounded worker task; no whole-launch readiness claim
is made. Existing tests and policy were not weakened. The parent-owned trusted
regression was not edited or run by this worker.

PASS: focused mocked regression suite and hermetic sibling-load regression.
NOT EXERCISED: build/full suites, independent frozen-candidate testing,
Loop/Graph/Trigger real-provider journeys, installed/live target acceptance,
release and publication. Parent owns freeze/build/full checks and independent
testing. Source writes stop after this note.

## Continuation recheck — fresh observed evidence

Re-read the scoped source, tests, this artifact, and required repository
contracts. `git rev-parse HEAD` returned
`e524371343bae72ee11f3c3fe01dc4c4d85cfe45`. The existing diff contains only the
port-selection repair and its causal tests; `stop_server` has no diff. No new
source defect was demonstrated, so no additional source/test patch was made.

Fresh command and actual output:

```text
python3 -m unittest scripts.verify_installed_fleet_vertical_test -v
Ran 17 tests in 0.549s
OK
exit 0
```

All 17 named tests reported `ok`, including main/config/restart wiring,
boundaries, invalid-input denial, absent fallback, occupied-port denial and
isolated sibling loading. `python3 scripts/acceptance_port.py --help` returned
the documented pool/subset usage with exit 0. `git diff --check` emitted no
diagnostics. Both unrelated `.issue327-state-*` directories remain listed as
untracked; neither was modified by this continuation.

The RED output above is retained historical evidence from the existing note,
not a newly reproduced or independently authenticated RED run. This recheck
does not reconstruct a pre-repair candidate or claim independent tester,
installed/live, build, release or whole-issue acceptance. Only this qualification
artifact was updated during the continuation; no external writes were made.
