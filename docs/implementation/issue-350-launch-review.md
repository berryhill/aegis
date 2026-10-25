# #350 scoped launch-asset review

Candidate scope: six owning-service online adapters in `internal/command/fleet_online.go`; no application/domain/persistence/stop changes. Source-only; no install/publication/live acceptance.

| Asset | Disposition | Reason / evidence |
|---|---|---|
| README.md | Reviewed unaffected | Existing controller/manual lifecycle claims remain valid; detailed online support is in linked publication guidance. |
| LICENSE | Reviewed unaffected | No licensing or dependency change. |
| SECURITY.md | Reviewed unaffected | Existing authentication, authority and no-second-writer boundaries retained. |
| CONTRIBUTING.md | Reviewed unaffected | No contribution/build/CI contract change. |
| CODE_OF_CONDUCT.md | Reviewed unaffected | No community-policy change. |
| CHANGELOG.md | Changed | Source-only adapter support and unavailable operations stated explicitly. |
| docs/THREAT_MODEL.md | Reviewed unaffected | Unix peer/bearer owning-service admission retained; no new authority. |
| docs/ARCHITECTURE.md | Reviewed unaffected | Existing CLI -> application/API owner topology unchanged. |
| docs/QUICKSTART.md | Reviewed unaffected | Manual API/offline lifecycle examples retained; no new quickstart requirement. |
| docs/DEMO_NO_KEY.md | Reviewed unaffected | Explicitly excludes Queue lifecycle acceptance; not rerun as adapter proof. |
| docs/RECORDING.md and retained recording assets | Reviewed unaffected/historical | Existing recording explicitly not lifecycle proof; no fabricated replacement. |
| Release binaries/checksums | Not produced / not authorized | Local source binary only; no release qualification/publication claimed. |
| docs/contributing/ISSUE_BACKLOG.md | Reviewed unaffected | Existing local contributor backlog, no issue publication. |
| docs/ONLINE_LOOP_PUBLICATION.md | Changed | Exact online lifecycle support, strict input/readback and unavailable ordinary retry/expire/exhaust boundaries. |
| Source-owned Registry/Queue/router skills and operation matrix | Changed | Add six adapter routes and qualification test references, preserve other existing fields. Manifest refreshed with canonical opt-in test; no fleet install. |

## Verification and remaining gates

- `TestInstalledOnlineLifecycle` passes using the source-built CLI, real Serve, isolated Badger, actual peer/bearer transport and controlled fixture clock. Canonical abandoned claims are fixture setup in the sole already-open store (as in existing TestDQHandoff), not failed-operation fallbacks or runtime crash evidence.
- Agent registration replay/conflict, immutable history, enable/disable/retire, stale/digest/foreign/retired denials, strict JSON, auth, target/config ownership and no-second-store checks pass.
- Queue cancel/revoke, expired-lease reclaim, unchanged Queue/run/Loop/attempt identity and consumed budget, live-lease/ordinary-retry/wrong-authority/stale/terminal/exhausted denial pass.
- Transport tests cover exact verb/body mapping, unknown/trailing/malformed/type-invalid JSON, 403/409 denial, 500 uncertainty with one request/no automatic retry, unsupported online expire/exhaust and absent fallback store.
- Full-package verification is separate from focused passes. Parent must inspect handed-off process outcomes; a timeout is not a pass.
- Fresh independent frozen-candidate acceptance, main integration (main advanced independently), current-main manifest reconciliation, protected-base PR/CI/merge, release-shaped package acceptance and reusable live 8448 acceptance remain parent-owned/unexercised. PR #390/window and #385 untouched.
- Known pre-existing online `agents show AGENT` omission sends revision=0 and returns HTTP 400; new tests use exact `agents show AGENT REVISION`. No unrelated read adapter repair included.
