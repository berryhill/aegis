# Issue 250: implementation-loop attempt 2 repair evidence

## Domain/control contract

Obligation: ordinary browser identity lasts one fixed hour by default, independently of sensitive-action freshness. Authentication, stanza selection, approval, mandates, runtime admission, and audit remain outside the model. Explicit shorter configuration, exact expiry denial, logout, revocation, password rotation, origin/CSRF checks, and review invalidation must remain effective.

Technical scope: preserve the existing Go implementation and task branch; repair stale launch documentation and the installed-browser fixture. No live operator configuration, runtime version, model, profile, gateway deployment, external issue, release, or PR mutation is part of this step. Acceptance for this implementation step is source readback, focused regression verification, and an explicit handoff of unresolved installed/release/environment gates, not publication clearance.

## Evidence custody

- Task `t_34a9ecf`, flow session `sess_3076378491ea`, implementation attempt 2.
- Input packet `tflp_db3bc3110d3849f9`, hash `49a74abd508b51f693454d6a7876f8cf562bbf420cbb977eb292c468e231185d`.
- Starting HEAD `ffec2562ac6e5a0351cea20a63657cdc002d6a9d`, branch `task-t_34a9ecf-250`; tracked source was clean before these repairs. Historical untracked logs were retained.
- Authenticated profile-scoped FlowStore read succeeded with in-memory fallback disabled. Active step index 4, attempt 2, previous packet ID/hash, worktree, and branch matched the envelope. Complete diagnose/security/launch/final/report/publication/CI outputs were recovered; earlier authentication/read limitations are historical, not current.
- GitHub readback: actor `xxander-x`, exact repository `berryhill/aegis`, issue 250 inspected, no PR returned for the exact task head across all states. No remote mutation occurred.

## Finding-by-finding disposition

| Finding | Current source/evidence | Gap, risk, and verification boundary |
| --- | --- | --- |
| LA250-01 quickstart five-minute/default and principal clamp | `docs/QUICKSTART.md` now separates one-hour default/maximum from the retained example's explicit five-minute override, independent freshness, explicit reauthentication, invalidation, and non-sliding expiry. | Source repair made; exact-candidate executable workflow verification remains a later gate. |
| LA250-02 security fifteen-minute cap/clamp | `SECURITY.md` now separates fixed browser identity/cookie duration from fresh sensitive authority and preserves all other authority deadlines. | Source repair made; subsequent security review must re-evaluate the repaired candidate. |
| LA250-03 architecture read-freshness/clamp | `docs/ARCHITECTURE.md` now describes ordinary metadata identity admission and independent sensitive freshness; no union or new browser authority is introduced. | Source repair made. Diagram inspection found the affected browser-lifetime label in the threat model. |
| LA250-04 threat-model exposure | `docs/THREAT_MODEL.md` labels fixed SessionTTL and distinguishes metadata exposure from the earlier sensitive-action cutoff. | Source repair made; no claim of host confinement or same-account protection. |
| LA250-05 default/upgrade/example guidance | README, changelog, quickstart, and example comments explain one-hour default/maximum, shorter overrides and unchanged explicit values. Initialization's omitted key inherits the default; ambiguous historical explicit values are not rewritten. | No operator configuration was changed. |
| LA250-06 installed fixture forced ten minutes | `scripts/verify-installed-fleet-vertical.py` now omits session_ttl. `scripts/console_browser_test.py` requires a one-hour protected cookie after actual password login and Graph/Loop/Agent navigation without deadline renewal. | Harness implementation and regression assertions pass. Actual installed-browser execution has not passed; this short journey is explicitly not an elapsed-hour proof. |
| LA250-07 Chrome readiness | Fresh full harness run reproduced both native-touch failures before DevTools readiness. A separate isolated about:blank probe retained `.scratch/issue-250-chrome-probe/startup.log`; no port appeared within 20 seconds, process had not exited, stderr was empty, and parent terminated/waited for the probe. | UNRESOLVED environment/readiness cause. No timeout increase, sandbox disablement, skipped assertion, or fabricated browser success. |
| LA250-08 real no-key Hermes discovery | Direct read of `.scratch/issue-250-launch/recheck-15.log` confirms unsupported Hermes identity output before design. Supported real Hermes remains a prerequisite. | UNRESOLVED; no runtime replacement or discovery-policy weakening. Historical recording replay does not satisfy current real-runtime acceptance. |
| Prior PTY lifecycle timeouts | Canonical final/report outputs retain initial failures and later isolated/serialized passes. | Root cause remains unestablished here; broad and race exact-candidate verification must retain both failures and reruns. |
| Publication and CI deferred | Canonical publication and CI outputs report no push/PR write and DEFERRED_LOCAL_FAILURE. | No publication clearance, CI pass, merge, or task completion is claimed. |

## Verification performed during this step

- RED: `python3 -m unittest scripts.console_session_lifetime_test` failed because the new cookie-proof helper did not yet exist.
- GREEN: the same four tests pass after implementation, rejecting five-/fifteen-minute clamps, unsupported larger deadlines, missing/ambiguous/weakened cookies, and insecure HTTPS cookies. Only deadline metadata is returned; cookie material is not rendered.
- `go test ./internal/config ./internal/console ./internal/app ./internal/api -run 'Browser|Freshness|SessionTTL|SessionLifetime|OneHour' -count=1`: passed using `/home/silas/go/bin/go` (Go 1.26.6).
- `go test ./internal/config/... ./internal/console/... ./internal/api/... ./internal/app/... -count=1`: all four packages passed.
- `python3 -m unittest scripts.console_session_lifetime_test scripts.verify_installed_fleet_vertical_test`: nine tests passed.
- `python3 -m unittest scripts.console_browser_test_test scripts.console_session_lifetime_test scripts.verify_installed_fleet_vertical_test`: 33 tests ran; two real-Chrome startup failures, retained in `.issue-250-attempt2-harness.log`. This run did not pass.
- `python3 scripts/console_security_test.py`: passed all reported static security checks.
- `python3 scripts/verify-console-vendor.py`: pinned asset check passed.
- `git diff --check`: passed.
- Independent read-only Hermes subprocess review: `.issue-250-attempt2-independent.log`, session `20260913_144929_673cba`; parent directly verified the cited retained installed/no-key failures and reproduced Chrome startup failure. The reviewer did not implement or authorize anything.

Full/race/vet/build, new clean committed candidate packaging, archive checksums, installed authentication acceptance, and real no-key reproduction remain subsequent flow gates. Prior exact-head results are not transferred to the new dirty candidate.

## Launch-asset impact

Changed: README, SECURITY, CHANGELOG, architecture prose, threat model and its diagram label, quickstart, example comments, CONTRIBUTING harness command, demonstration/recording guidance, and installed-browser proof sources/tests. The historical recording bytes are unchanged and now explicitly identified as using the supported five-minute example override, not the product default.

Reviewed as unaffected by this source repair: Apache LICENSE, conduct behavioral standards, architecture diagrams without lifetime claims, retained recording capture/replay syntax, repository-local `docs/contributing/ISSUE_BACKLOG.md` proposal scope, and `.github/workflows/release.yml` publication/packaging boundaries. No binaries or checksums were regenerated in this step; previously verified archives belong to the prior candidate only and must be rebuilt/reverified for the next exact candidate.

Owner gates remain open: private vulnerability-reporting availability/contact decision (`SECURITY.md`), private conduct-reporting route (`CODE_OF_CONDUCT.md`), and external release/recording publication approval. No contacts or authorizations were invented. These gates do not prevent local repairs or safe proof collection, but this report does not waive launch/publication policy.

## Judgment and residual risk

Confirmed: changing the fixture alone is insufficient evidence; cookie protections and non-renewal need assertions tied to actual login/navigation. Recovered canonical outputs revealed additional environment findings absent from the truncated packet. Working remediation: documentation separates two clocks without widening Go authority. Uncertain: why Chrome never publishes DevTools readiness and whether available real Hermes satisfies supported identity discovery. Missing: successful exact-candidate installed and real-runtime demonstrations. Human review remains necessary for the owner-only reporting/publication decisions.
