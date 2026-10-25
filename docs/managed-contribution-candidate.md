# Managed contribution candidate qualification (source candidate)

The managed controller is an intermediate candidate path, not GitHub publication
or final contribution success. Its native execution profile is restricted to
explicitly approved dependency-free Go modules; native checks are not a sandbox.
Production Git origin checks require exact canonical public GitHub HTTPS. The
optional trusted fetch seam replaces transport only: origin and fetched base
checks still execute. It is not model-selected or exposed in a Loop document.

## Sealed identity

`head_commit` and `head_tree` identify the Git HEAD anchor, **not** a commit of
candidate changes. `source.digest` is the tested complete worktree identity,
including staged, unstaged, new and ignored regular files and permission modes;
`.git` is excluded. The candidate also binds index, executable bytes, Go binary,
contract, Queue attempt and builder identity. `candidate_commit` and
`candidate_tree` are real local Git objects of the complete tested inventory,
created with a disposable index and fixed controller identity, without changing
the branch/index or publishing. HEAD remains only an anchor.
Readback reloads immutable baseline, candidate and acceptance records and checks
content-addressed source, executable and check output before returning evidence.

## Executed regression

```
GOMAXPROCS=2 go test -p=1 ./internal/implementation -run '^TestManagedContributionRepairAndCustody$' -count=1 -v
```

This deterministic role/transport fixture uses real Git linked-worktree creation,
real native builds and tests, and invokes the built candidate executable through
an immutable required test. A two-file defect starts RED; the wrong first patch
fails a frozen check; a second proposal from the same builder receives prior
failure output and passes with a new tester identity. Drift checks cover source,
new/ignored files, mode, index, toolchain and executable substitution. Same-run
reservation and restart replay are denied, and corrupt prior failure evidence
makes readback fail closed.

## Unqualified acceptance / remaining work

This fixture does not prove real-provider behavior or installed/live acceptance.
The separate `TestContributionQueueNativeRepairIntegration` exercises actual Queue,
Git worktrees, native build/checks and Hermes protocol with a synthetic role
resolver and synthetic runtime/authority fixtures. It does **not** exercise real
application role provisioning and is not live-provider qualification.

Authenticated manager `/contribute run` now routes qualified managed profiles
through `ExecuteContributionAs`, reusing the exact Queue preparation/binding path.
Historical profiles without `managed` remain explicitly intake-only. Missing
independently approved role provisioning is returned inline with exact role and
charter; Run does not provision or renew authority. Same-intent attempts read
retained evidence rather than starting another worker. Candidate acceptance is
not delivery, merge or final contribution disposition (#378/#379).

## Retained custody and restart

Existing foreign worktrees remain denied and untouched. Only the same durable
run, contract, Queue attempt, implementation, base, workspace, repository and
controller-derived branch may continue. Continuation acquires the existing
advisory custody lock, reloads durable state and validates the complete boundary
source and Git identity. Supported boundaries are after trusted failing baseline
and after a completed failed candidate/check with the tester closed. Every new
candidate consumes its durable pass before runtime/file effects. Exhaustion,
stale Queue attempts, unknown running/interrupted effects and drift deny without
releasing reservations. A retained builder must still match its original role;
this does not provide #377 authority renewal. Noncooperating same-UID writers
remain outside advisory-lock guarantees.

Focused retained-boundary and privacy regressions:
```
GOMAXPROCS=2 go test -p=1 ./internal/implementation -run '^TestContribution(RetainedBoundary|TesterEvidencePrivacy)$' -count=1 -v
```

## Source privacy

Complete inventories and bytes remain private controller custody for native
checks and frozen identity. Tester model input contains source schema/digest only,
not inventory bytes or filenames. Builder context remains restricted to the
explicit admitted UTF-8 repository context and its per-file/total/serialized
bounds; role validation grants no credential access. Sensitive-name and
recognizable private-key rejection are narrow checks, not a general secret
scanner or proof that arbitrary approved source contains no secrets.
The tester runtime has no generic tools. A one-shot controller capability runs
immutable checks after clean runtime-session creation, with fresh tester admission
before and after execution. Acceptance uses controller observations, not model
text. Each tester is closed before repair; close failure aborts rather than
resuming the builder. Typed sanitized criterion packets feed the same builder.
Readback rederives observations and pins exact policy; internal accepted-candidate
export denies missing/corrupt evidence and stale policy. Runtime callback protocol
regressions are deterministic fixtures, not real-provider acceptance.

Skill/launch impact: existing shipped contribution/intake instructions must not
advertise whole-contribution execution or delivery from this intermediate path.
No release, installation, production activation, model or provider change is
included. Full launch-asset synchronization remains required before completion.
