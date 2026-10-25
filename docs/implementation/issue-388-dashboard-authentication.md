# Issue 388: absolute dashboard authentication windows

## Policy and operator impact

`api.console.session_ttl` and `api.console.action_auth_ttl` independently default to six hours and accept positive duration strings up to six hours. The effective mutation deadline is the earlier issued deadline. Both begin only after successful principal password verification. Reads, navigation, reconnects, mutation attempts and failed/cancelled/unavailable reauthentication never renew either deadline. Exact expiry denies. Reauthentication explicitly replaces the old session and pending reviews; logout, password rotation, revocation and restart invalidate sessions early.

Existing explicit shorter session values (including the example's `5m`) remain unchanged. Existing omitted action policy now inherits six hours: this intentionally expands dashboard mutation exposure beyond the old fifteen-minute shared freshness window. Operators requiring that shorter dashboard boundary should explicitly set `api.console.action_auth_ttl: 15m`. Increasing policy must not reinterpret an already-issued session's captured action deadline. No live configuration migration is performed.

A stolen authenticated cookie can now permit both metadata access and admitted dashboard mutations for up to six hours. Exact origin/CSRF, one-use review receipts, current target/version checks, authoritative audit and independent operation-specific approval remain mandatory. This does not protect against same-account/root/browser compromise. No sliding renewal or persistent remember-me is added.

## Authentication and launch impact ledger

| Boundary / consumer | Effect |
| --- | --- |
| config defaults, strict typed validation, YAML/JSON presence and environment parsing | Separate bounded dashboard duration values; explicit malformed/null values deny. YAML and environment require duration units; legacy typed JSON integer nanoseconds remain supported within the same positive six-hour bound. Environment overrides retain Viper precedence. |
| Console Login / Authenticate / AuthorizeMutation / ValidateActionSubject | Issue immutable session and action deadlines after successful verification; service admission requires a matching live issued subject and revocation generation. No asserted/unissued password subject fallback when the hook exists. |
| Console review receipts, command reviews, logout, rotation | Receipt expiry stays separately bounded; no authentication deadline renewal. Existing invalidation and CSRF controls remain. |
| API Serve / CLI construction | Owning Console installs the live dashboard hook before serving; command and API Fleet adapters retain action-specific dispatch. No headless CLI/browser exchange is added. |
| Agent registration/lifecycle, Loop validation/publication/lifecycle, Graph publication/submission, Queue admission/lifecycle | Explicit dashboard-capable allowlist uses dashboard freshness; exact controller-derived authority, immutable targets and operation-specific rules remain required. |
| Doer draft/readiness/successor proposals | Dashboard editing and proposals can use the bounded dashboard window; host approval and runtime preparation remain independent. |
| Credential metadata mutation review/execute | Dashboard authentication changes only admission freshness. Custody, exact binding/version, receipt, protected confirmation and broker capability expiry remain independent. |
| `principal.auth_ttl`: app service local OS and Linux peercred subjects, shared principal check, session preview clamp | Unchanged fifteen-minute default and maximum; no browser deadline substitution. |
| `principal.auth_ttl`: Doer host approval, run preparation/grant cap, continuation requester/executor/intent limits | Unchanged; stale dashboard subjects cannot mint host consent or runtime authority. |
| `principal.auth_ttl`: local Hermes import, controller Codex connection, initialization and authority recovery | Unchanged; separate protected/native/host authentication boundary. |
| Fleet claim/reclaim/direct runtime/effect/evidence/disposition and unknown actions | Shared principal path, not dashboard allowlist. Runtime mandates, expiry and revocation are not extended. |
| Contribution admission and role/effect decisions (#377), #376/#378 delivery | Unchanged generic principal and role controls; dashboard TTL is not contribution execution authority. |
| #384 configuration history/native transition integration | Read-only integration boundary at revision `8b916814`; this change neither edits that worktree nor migrates binding history/budgets. Future native transition still requires its own fresh authentication/approval and exact artifact checks. |
| Browser harness / operator docs | Harness expects a protected six-hour cookie from real login and checks nonrenewing navigation. Short browser runs do not prove elapsed six-hour behavior; deterministic clock tests cover boundaries. |

## Serialization and activation boundary

New `json.Marshal(config.Config)` output serializes both Console durations as explicit Go duration strings; typed JSON decoding and the normal configuration loader round-trip those values. Existing JSON integer `time.Duration` values remain nanoseconds (for example, `900000000000` is fifteen minutes), not seconds or hours. Even `1` in legacy JSON means one nanosecond, never one second. Numeric strings, fractions, null, nonpositive and values beyond six hours deny. This JSON-only compatibility does not admit bare numeric YAML or environment values. Explicit shorter session/action policy remains unchanged. Configuration writers outside Console retain their existing duration representation.

Changing a file does not change a running manager or renew issued sessions. A subsequent supported service start loads and validates policy and creates new process-local session state; this patch performs no activation, connection change, provider/login mutation or #384 history migration.

The one-request and installed requeue synthetic journeys now log in on their actual Serve listener. Requeue tests observe the owning portal through a package-private composition callback before listeners start; public Serve uses the unchanged default composition. Their former second isolated Manager issued a real password identity unknown to the production service registry; rejection was correct. The existing short test-only session/action policy and shared stage clock remain in place, as do exact native-review binding, altered-intent/session denials and durable no-mutation checks. All remaining `consoleQueueRouteFixture` callers are standalone handler/manager tests, not clients of an already active Serve. No registry fallback, issuer relaxation or fresh native/provision/host-consent weakening was introduced.

Custom Console JSON decoding enforces its own closed object schema even inside an enclosing strict decoder: unknown and duplicate keys, explicit null values and non-object Console values deny. The normal loader independently preserves object presence before Viper can discard null. This does not change the bounded legacy integer-nanosecond duration contract.

## Evidence scope

Focused clock tests exercise 15 minutes, one hour, immediately before six hours and exact six hours; shorter issued policies, failed/unavailable reauthentication, explicit successful replacement, logout/revocation, clock rollback, concurrent validation, origin/CSRF and cookie behavior are covered by focused Console/API regressions. Actual host-write approval, provisioning plan/approval/apply and mandate preview service calls reject a stale dashboard subject. Broker expiry/isolation regressions remain separate from dashboard metadata mutation.

Generated `components_templ.go` is regenerated with pinned templ v0.3.1020. Its large diff is generator-wide numeric temporary/string-index and source-line renumbering following the added authentication paragraph, not a generator upgrade; do not hand-edit generated output.

Independent built-candidate browser acceptance, exhaustive suites, final review and publication are parent-owned gates. This document is not evidence of live activation, elapsed-time browser testing or passing suites not recorded in the candidate evidence directory.
