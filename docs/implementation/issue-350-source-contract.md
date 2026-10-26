# #350 scoped source operation contract

This is a source-adapter contract, not final #348 distribution, release-package acceptance, or managed-target evidence. The structural operation catalog does not prove per-operation behavior. The actual online router is `internal/command/fleet_online.go`; lifecycle handlers call existing application services in `internal/api/server.go`. No runtime lifecycle policy is added here.

| Operation | Online adapter | Evidence / remaining gap |
|---|---|---|
| Charter import / validate / exact show | Supported | Existing `TestUnixAPICompleteOperationalWorkflow` and owning-transport regressions; preserved, not new authority |
| Agent registration / exact replay / conflict | Supported | `TestInstalledOnlineLifecycle`: original exact record, changed ownership conflict HTTP 409, full canonical registration/revision history unchanged |
| Agent exact show / history | Supported | Lifecycle journey exact revision and append-only history |
| Agent approve-charter | Supported | Existing strict charter and online publication regressions; unchanged adapter |
| Agent disable / enable / retire | Supported | Exact revision/digest CAS; stale and digest HTTP 409, foreign path/reference fixed CLI diagnostic, retired HTTP 409; full canonical history unchanged on denial |
| Loop validate / publish / exact show / activate / retire | Supported | Existing `TestInstalledOnlineLoopPublication`, owning-transport and exact-Run regressions; immutable publication and lifecycle remain separate |
| Retained task save / show / continue / execute and exact published Run | Supported | Existing retained/exact-Run parity, reusable direct admission and failure-continuity regressions; synthetic runtime qualification, not the required managed-provider proof |
| Graph list / exact show / publish / submit | Supported | Existing owning-transport and lifecycle submission/binding journey; no new Graph lifecycle support |
| Queue list / show / result / preparations | Supported | Existing transport/readback tests; lifecycle journey uses full QueueExecutionView; not every read is independently package-qualified |
| Queue runtime binding / processing | Supported | Existing preparation and transport tests; runtime fixture is synthetic, not managed-provider acceptance |
| Queue cancel / revoke | Supported | Queued durable dispositions; canonical seeded claims through the sole owning fixture store exercise claimed cancel and revoke, retaining Queue/run/Loop/attempt identities; no runtime-stop acknowledgment claimed |
| Queue retry, expired lease only | Supported | Genuine controlled-clock expiry, same immutable Queue/run/Loop/attempt history and cumulative budget; live-lease, ordinary retry, substituted authority, conflicting reason pair, stale replay, exhausted and terminal guards assert specific HTTP status and complete before/after canonical Queue views |
| Queue ordinary retry | Unavailable | No authoritative durable runtime-stop acknowledgment; denial is not success |
| Queue expire / exhaust | Missing online CLI adapter | Existing separate offline/API primitives do not authorize bypass of the owner |
| Graph lifecycle application | Unavailable online | No generic client or second writer workaround |
| Unknown-after-commit reconciliation | Test-only fault fixture | Transparent Unix proxy forwards opaque request/auth bytes, drops a completely received committed mutation response; exact Agent history and claimed Queue disposition read back before repeat. CLI must report `execution_outcome_unknown` |
| Managed reusable Doer on hard 8448 target | NOT EXERCISED | Parent-owned authorized acceptance gate remains mandatory; synthetic fixtures cannot satisfy it |

`AEGIS_TEST_PACKAGED_EXECUTABLE=/absolute/path/to/aegis` supplies exact caller-owned executable bytes to the real single-owner Serve lifecycle fixture. It must be a regular executable; the test logs SHA-256 and fails on byte drift. Absence preserves provenance-script source rebuild. Supplying a development candidate proves those exact bytes, not release/archive provenance. Independent tester must bind the supplied artifact to the intended package and verify the frozen inventory before/after.
