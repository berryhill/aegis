# #379 implementation verification budget

## Observed failure, not a deadlock diagnosis

The existing whole `implementation` race shard reached its fixed eight-minute
Go package alarm at 480.110s. At the alarm, the active
`TestRealGoVerification/first-pass` subtest had run for only about 11s. The
supervisor reported aggregate memory 1.0G, CPU 10m55s and service duration 8m1s.
These observations support cumulative package cost, not an eight-minute deadlock
in that active subtest. The normal app check was unaffected.

Measured native contribution cost was 176.25s under race in builder-v2 and about
87s normal in fresh tester2. Those are prior measured test observations, not
proof that a new complete shard passed. The earlier builder-v2 race log also
reported package PASS while its external 180s wrapper exited 124; retain both
facts rather than promote the wrapper to success.

## Narrow topology repair

`scripts/verify-go-shard.py` uses the existing orchestration partition mechanism:

- `implementation-contribution` selects exactly the anchored, escaped top-level
  `TestManagedContributionRepairAndCustody`,
  `TestManagedContributionProductiveDeliveryRepair`,
  `TestManagedContributionSameAgentSeparateSessions` and
  `TestManagedContributionEngineeringRegressionRepair` parents.
- `implementation` dynamically selects every other default entry returned by the
  actual mode's Go test inventory, including future tests, runnable examples and
  fuzz targets. No static exclusion list or skipped test is introduced.
- Selecting a parent preserves its child subtests and default fuzz seeds.
  `TestMain` still runs separately in each Go test process.
- Missing target, malformed/incomplete inventory and empty partitions deny
  execution. Both modes retain `-count=1` and `-timeout=8m`.
- CI runs both groups in the normal implementation lane; race defaults remain
  in implementation and race contribution runs in the existing CLI lane, each
  under the unchanged ten-minute job limit. Exact-head evidence requires every named lane;
  missing, cancelled, skipped or failed shards deny release even if aggregate
  gates appear green. Supervisor/cgroup/KDF/crypto limits remain unchanged.

Run each group through the standard supervisor, for example:

    GOMAXPROCS=2 GOFLAGS=-p=1 python3 scripts/verify-budget.py python3 scripts/verify-go-shard.py --group implementation-contribution --race
    GOMAXPROCS=2 GOFLAGS=-p=1 python3 scripts/verify-budget.py python3 scripts/verify-go-shard.py --group implementation --race

Use `--list-only` with either group and with/without `--race` to inspect the
actual selected command without running its tests.

## Evidence tiers and remaining gates

### Post-merge aggregate margin correction

Main run **43976201285**, job **133680692904**, exhausted the implementation
process alarm at **480.165s**. `TestNativeContinuation` had been active for 17s
and its `failed_acceptance` child for 6s: this is cumulative execution cost, not
an eight-minute hang in that child. The prior passing local default group took
435.219s; its JSON stream measured the complete engineering regression repair
root at **66.26s**. Moving that root projects **368.959s** locally, not a hosted
success claim or a subtraction from a completed hosted total (the failed group
had unfinished tests).

The receiving race CLI lane's retained hosted phases were compile 110.501s,
inventory 2.107s, CLI root 87.048s, CLI final 85.193s and contribution 158.704s.
Adding the measured root projects **509.813s**, leaving **90.187s** for setup,
cleanup and variation within 600s. The contribution process projects 224.964s
within its own 480s alarm. No additional process, compilation, lane or job is
introduced; normal mode keeps both partitions in the same lane. These are
mixed-host planning estimates; fresh exact-head hosted CI must verify the fit.

The dependency trace is `TestManagedContributionEngineeringRegressionRepair`
→ `testManagedContributionRepairAndCustody` → `fixture` / `stdlibSeededNative`.
It creates a synthetic module without third-party requirements, no dependency
bundle and no templ files. Its native commands use fresh private module caches
and `GOPROXY=off`. It therefore needs no new race CLI offline-preparation step.
The actual locked-dependency and templ roots remain in implementation; manager's
offline fixture remains in manager-a. The unchanged workflow condition and that
selection parity are regression-tested. All native TTLs, test bodies and
resource limits remain unchanged, as do the earlier normal-manager and race-API
packing corrections.

### Historical evidence

Deterministic Python tests qualify exact-once default inventory coverage,
future-test inclusion, missing-target denial, escaped/anchored parents, real
normal/race fixture children/seeds and per-process `TestMain`. Workflow and
exact-head evidence tests qualify the mandatory job-name join. They are not
whole-package runtime timing proof or hosted CI delivery evidence.

Current local evidence and bounded race process handles are retained under
`.candidate/issue379/builder-v3/`; consult `report.md` and `race-status.json` for
actual commands/exits and pending groups. A launched supervisor is not a passing
shard. Parent owns final review, freeze, independent topology acceptance and
PR/CI/permitted merge. No full `make verify` success is asserted.

Parent readback subsequently verified both actual supervised race groups exited
zero: `implementation-contribution` completed in 173.359s and the remaining
`implementation` defaults in 384.626s. Both retained the eight-minute package
alarm, uncached execution and standard supervisor. Normal/race inventories each
contained 57 defaults, split exactly once as 1 + 56. Parent also reran all 42
shard/evidence/workflow/resource Python tests and architecture regression tests
with zero exits. These local results are not hosted CI or whole-issue acceptance.

The separate whole-API ten-minute cumulative timeout is unchanged and remains a
separate verification gap; this patch does not change API policy, Makefile,
timeouts or runtime behavior. Actual #377/#378 producer integration and the final
whole-contribution join remain pending their active owners. Fresh tester2's
F1/F2 probes qualify those evidence repairs, not missing production integration
or live full-Loop/protected-merge acceptance.
