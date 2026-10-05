# Provider authentication and Doer adapter source-candidate launch review

## Scope

Existing task-owned `feat/doer-provider-auth-readiness` worktree; no GitHub writes, live configuration changes, credential reads, runtime installation/restart, provisioning or execution. The source-helper Doer result is not installed Aegis product proof. Independent provider-authentication security review remains parent-owned.

## Assets reviewed

- Updated: README, SECURITY, CONTRIBUTING, CHANGELOG, Doer guide, threat model and architecture boundary, canonical Loop-authoring skill, structural operation matrix and generated bundle manifest.
- Reviewed unaffected: LICENSE and CODE_OF_CONDUCT (no legal/conduct change); five-minute quickstart and no-key demonstration (feature is outside that proof); existing terminal recording (historical no-key substrate only, not provider/draft acceptance); release script/binary-checksum workflow (no release or publication authorized).
- Contributor issue publication is external and was not performed. Existing local issue material remains separate from this source patch. No new release binary/checksum, live recording or contributor issue URL is fabricated.

## Local verification

- API positive adapter test: save/read, exact CAS update and stale denial/readback, explicit already-approved successor rebind, substituted path denial, replay denial, preserved contract, no Loop/Queue/approval/receipt effects. `GOMAXPROCS=2 go test -p 1 ./internal/api -run TestDoerDraftAdapter -count=1` passed after correcting the synthetic fixture to preserve stable ownership.
- Owning-service Unix CLI adapter parity: all four operations use configured authenticated Unix transport and do not open an alternate store. `GOMAXPROCS=2 go test -p 1 ./internal/command ./internal/core ./internal/runtime/hermes -run 'TestDoerOnline|TestDoerService|Test.*ProviderAuth|TestDistributed' -count=1` passed.
- Structural matrix initially failed on four new CLI nodes and four HTTP registrations; catalog updated and same test passed. Structural coverage is not live acceptance.
- Focused race command: `GOMAXPROCS=2 go test -race -p 1 ./internal/api ./internal/command ./internal/app ./internal/core ./internal/runtime/hermes -run 'TestDoerDraftAdapter|TestDoerOnline|TestDoerService|TestDoerDraftSuccessor|Test.*ProviderAuth|TestDistributed' -count=1`; passing package results retained in `.doer-resume-race.log`.
- Manifest refreshed using existing maintenance interface: `AEGIS_REFRESH_SKILL_MANIFEST=1 GOMAXPROCS=2 go test -p 1 ./internal/skillbundle -run TestRefreshManifest -count=1`; validate passed for 15 skills; evaluate passed for 101 structural fixtures (behavior/service/runtime execution explicitly not run).
- Local bundle built and verified at `.scratch/provider-auth-bundle/aegis-skills_v0.2.18.tar.gz`, archive SHA-256 `5c91146f3608d30c24f62af16e0e158187fea2ca189c805755331440273735ab`, content digest `sha256:5a09cd8a5cc09cbb266ae1e9b0d2de111659cc45b810112a7cba45b42035ddc7`. The supplied source revision is the task base, NOT a committed/published candidate identity; rebuild with final source commit before publication. No installed inventory/public distribution proof is claimed.
- `git diff --check` passed. Independent direct `GOMAXPROCS=2 go build -p 1 ./...` and `GOMAXPROCS=2 go vet -p 1 ./...` both exited 0, with logs at `.doer-resume-build.log` and `.doer-resume-vet.log`.
- Broader focused packages: `.doer-resume-focused.log`. Full suite/build/vet: `.doer-resume-full.log`, `.doer-resume-build.log`, `.doer-resume-vet.log`; launched bounded full-suite process and final result must be read before publication. Empty/in-progress logs are not passing results.

## Remaining gates

## Parent repair and verification

- Independent runtime review identified caller/authority cutoff, near-expiry selector margin, and transport qualification defects. The repaired source uses fresh pre-start admission, caller/authority deadlines for controller-authenticated sessions, a 120-second access-only token margin, and exact Hermes 0.21.3 transport-source fingerprints. General legacy runtime version admission remains unchanged.
- The actual installed Hermes pool pruned the initially proposed `aegis-controller` source. The offline synthetic probe reproduced that failure; `manual:aegis-controller` loads/selects through the real pool and provider resolver. This qualifies only the pinned schema, not remote entitlement or a live model call.
- Parent ran `AEGIS_TEST_PROVIDER_HERMES_INSTALL=/home/silas/.hermes/hermes-agent GOMAXPROCS=2 go test -race -p 1 ./internal/runtime/hermes -run 'TestProvider|TestInstalledProviderTransport|TestLegacyLaunch' -count=1 -timeout=120s`: exit 0. Coverage includes cancellation/expiry, revocation during probing, the 120-second margin, disclosure denial, cleanup and preservation of ordinary persistent-session lifetime.
- Parent build and vet across `./...` exited 0 after the final runtime repairs; adapter/operation-matrix regressions also passed. A temporary ad-hoc verification script caught and then confirmed repair of a context-cleanup vet failure; that script was removed and is not full-suite evidence.
- Earlier broad focused tests failed because selected-file Doer fixtures retained the legacy `no_mcp` toolset spelling. The fixture now has empty Doer toolsets, and an explicit nonempty-toolset denial case preserves enforcement. Targeted v4/v5 regressions passed. The earlier full suite timed out in API tests and also encountered an in-progress compilation change; neither earlier run is claimed green. Final uncached serial verification and exact-head CI must provide the full-suite evidence.

Parent must collect broad/full/build/vet exits, repair any failures, complete independent security review, bind package to final committed source, publish through parent-owned PR/exact-head CI/merge, and obtain separately authorized installed/live runtime proof. Real Codex provider access, exact successor/provisioning, expiry and host-write consent are not established by synthetic tests. No historical charter/receipt/Loop/run is retargeted and no live scope is removed.
