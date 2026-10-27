# Request-auth regression qualification

`TestPublicRootHTTPAuthenticationDenials` and
`TestPublicRootAuthenticatedContributionJourney` use actual `newRoot` factory
composition and `api.Serve`, without an injected authentication extractor.
The isolated denial test checks missing/incorrect bearer, TCP bearer without Unix
peer proof (with a claimed listener), session-token mismatch, original manager
expiry despite a fresh peer request, old-token rejection after exact-store restart,
and zero durable Queue items. The native journey uses actual Serve Console login
and logout, verifies password/native charter separation, closes/reopens the same
stores and compares retained execution/history. Test clocks stage expiry without
changing production authentication TTLs. Native progress is timed; after 90 seconds
all goroutines are logged. Existing HTTP/package budgets are unchanged.

Run controlled cold qualification with its required test-only overlay:

```sh
GOMAXPROCS=2 GOFLAGS=-p=1 python3 scripts/verify-budget.py \
  python3 scripts/acceptance_port.py \
  --pool /home/silas/.hermes/profiles/xander/workspace/aegis-acceptance-ports/locks -- \
  python3 scripts/test-cold-publication-owner.py test \
  -run '^TestCold(IsolationComplete|JoinedPublicationOwner)$' -v
```

This uses encrypted isolated custody and a controlled loopback remote, not native
provider or real GitHub publication proof. Naked tagged compilation without the
overlay is not the supported qualification recipe. Cold request fixtures now
mint genuine configured-UID request proofs and reauthenticate after clock advance;
the mode-isolation probe preserves request proof while discarding adapter ancestry.

The generated development executable is `./aegis-378-request-candidate` in the
real module/Git root, built with `scripts/build-source.sh` and task-root
`core.worktree` environment. It does not overwrite `./aegis` or relabel a release.
Evidence stays private under `.acceptance/request-auth-repair/`; earlier failures
remain immutable. The earlier independent freeze is invalidated by these repairs.

## Explicit issue-disposition composition (source candidate)

Productive issue disposition additionally requires both exact capabilities in one
already approved charter/mandate/session authority:

```text
github.issue_disposition.v1
github.issue_disposition.v1:repository:<owner>/<repo>:<decimal-repository-ID>
```

This is an additive operation-qualified resource-capability convention carried by
the existing capability field, not an issued grant or an inferred repository
permission. Repository spelling and immutable numeric ID must match the trusted
projected plan exactly. No prefix, case folding, wildcard, cross-stanza union,
token pushability, delivery target or credential scope substitutes for either
capability. Existing authorities without both remain denied and retain the
operation list that excludes issue disposition. No live charter, mandate,
credential or configuration is changed by this implementation.

Existing request/control/role, deployment, exact credential binding/version and
operation-specific approval checks still precede the real #379 adapter's
`VerifyContributionIssueIntent`. The exact adapter constructed in the productive
private frame must validate its opaque preclosure and the canonical issue intent;
a capability string or a policy receipt alone cannot authorize closure. Only
explicitly qualified authorities project the issue operation into the immutable
credential pin and custody allowlist. Historical policies are never rewritten.

Journey readback consults the canonical final-result getter before new effects
and after delivery. Only exact not-found falls through; corrupt or denied evidence
is not treated as absent. A narrower completed target is separately represented
by `boundary_completed`, while `completed` denotes whole-contribution success.
These source additions and their new scope regression require compiled and real
production-route qualification after the unchanged verification reserve recovers.

## Terminal-cleanup evidence

The actual Serve journey exposed concurrent role cleanup and supervisor terminal
transitions. `TestSessionTerminalConcurrentCleanup` reproduced the canonical
authority-command precondition failure. The owning service now serializes terminal
transitions and checks the stored session before issuing another revoke. A later
transition returns the existing state conflict without replacing the first outcome.

Contribution cleanup reconciles that conflict only when independently reloaded
continuity proves the exact role's completed native effects, closed boundary and
acknowledged stop, and the original session/process/mandate and exact terminal
authority projection agree. It never changes terminal history or grants authority.
`TestContributionRoleCloseJoined` covers controlled positive readback and missing,
mismatched, active, cancelled and unknown-evidence denials. Controlled continuity
readback in that test is not actual native Wait proof.

Focused terminal-concurrency race and reconciliation tests pass. The full native
HTTP journey must be rerun against a rebuilt, refrozen candidate when the retained
verification reserve is available; this repair is not yet production-route
acceptance, protected remote delivery or whole-contribution completion.

Launch-asset impact: the cleanup repair changes internal owning-service terminal
coordination, not command syntax, authority policy, expiry limits, release binaries,
license/conduct or remote delivery claims. This focused note describes that bounded
behavior and the qualification recipe; it does not assert overall acceptance.

## #377 follow-up: bounded per-session cleanup admission

Read-only review of the imported terminal mutex found that unrelated session
termination could hold the service-wide lock past a waiting cleanup's context.
The #377-owned follow-up replaces that global operation lock with keyed session
tokens. Only the short reference-map operations use a mutex; waiting for the same
session observes context cancellation, and unrelated sessions do not serialize.
The final holder/waiter removes the entry, so historical sessions do not accumulate
locks. Terminal-before-revoke checks and read-only exact quiescence joins remain.

New lock/admission regressions are source-authored; compilation, race and actual
Serve/native qualification remain held below the explicit 8-GiB verification floor.
Do not promote the earlier scoped tests or source review to this successor's
executable acceptance.
