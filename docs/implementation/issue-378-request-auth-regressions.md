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

Launch-asset impact: test-only fixture and evidence documentation repairs do not
change command syntax, production policy, shipped authority, package provenance,
license/conduct, release binaries or remote delivery claims. This focused note
supplies the qualification recipe; it does not assert overall acceptance.
