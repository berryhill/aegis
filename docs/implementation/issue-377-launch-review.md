# #377 source continuity launch-impact ledger

## CI recovery performance increment

The imported #378 terminal-cleanup delta is retained with its provenance. A fresh
source review identified that its service-wide mutex could delay bounded cleanup
behind an unrelated session. The follow-up uses per-session, context-aware
admission with reference-counted ephemeral lock entries; the stored terminal
re-read, canonical revoke/expire and exact native-quiescence reconciliation remain
unchanged. Cancellation during lock admission makes no session/authority change.
Contention, unrelated-session, cancelled-admission and supervisor-first outcome
regressions are authored but not executed below the owner-required 8-GiB reserve.
Earlier v3 acceptance predates this lifecycle delta and does not qualify it.

The replay-prefix cache retains per-transaction contract/event/head byte checks
and cold canonical validation; it caches no current authority decision. Exact-size
input reads retain EOF, inode, path, size and mutation checks. Recovery suffix
comparison still examines every logical byte, with unchanged checksum, transaction,
evidence-preservation and authentication deadline requirements. Cache cardinality
is bounded by one contribution and the existing event ceiling, not a fixed RSS cap.

Original CI failures and subsequent unsuccessful candidates remain retained.
Independent v2 race repair, fresh resume and disconnect passed; its restart
reserved-before-start case failed during authenticated physical recovery. The
follow-up bounded-chunk suffix comparison requires fresh restart qualification;
a later successful diagnostic rerun does not erase that earlier failure.

Current-main documentation was integrated as an additive union, preserving the
separate two-participant and continuity qualification boundaries. CHANGELOG records
the performance repair. README, SECURITY, architecture and threat-model claims do
not expand authority or declare live qualification. License, conduct, contributing,
quickstart, no-key demo, historical recording, release format/checksums and contributor
backlog have no changed commands or public contracts from this increment. No new
release, installed, real-provider or whole-contribution acceptance is claimed.

## Owner discovery/readback and CI partition increment

| Scope | Source / qualification |
| --- | --- |
| Fresh owner retained-run discovery | `internal/app/contribution_discovery.go`; existing Queue inventory, exact canonical issue URL, ambiguity conflict, no runtime side effect |
| Sanitized checkpoint/gates | `internal/app/contribution_readback.go`, `internal/managergateway/contribution.go`; typed allowlist, no raw process continuity |
| Mandatory manager integrations | `scripts/verify-go-shard.py`, `.github/workflows/ci.yml`, `scripts/verify-ci-evidence.py`; three isolated heavy groups plus dynamic remainder, both modes |
| Initial expiry injection | `internal/managergateway/contribution_native_test.go`; only initial subject narrowed to 60s, unchanged 300s charter/5m policy and 60s native command limit |

README/SECURITY/architecture/threat model existing continuity boundaries remain;
focused guide, queue skill and resource contract add exact discovery/readback rows.
License, conduct, contributing, quickstart, no-key demo, recording, release bytes
and contributor issues are unaffected by this local-only increment. No release,
provider, installed or hosted-CI proof is asserted. Parent retains heavy expiry
rerun, independent acceptance, final freeze and publication.


Local source-only increment; no publication, installation, restart or live proof.

| Asset | Disposition |
| --- | --- |
| README, SECURITY, threat model, architecture, CHANGELOG | Updated with retained Resume/protected readback/native custody seam and qualification limits. |
| docs/continuity/* | Consolidated stale incremental status into one current guide; old paths redirect. |
| Canonical contribution contract | Added source/seam mapping only; matrix and #379 acceptance not promoted. |
| Session, Queue, provisioning approval, Loop, evidence source skills | Updated retained Resume, protected gates, immutable claim/attempt, counters, unknown/native quiescence; Doer controls unchanged. |
| Source skill manifest | Refreshed with canonical maintenance test; no other worktree edited. |
| Five-minute quickstart | Inspected opening scope: no-key path does not cover contribution continuity; unchanged, not rerun. |
| No-key demo | Inspected scope: does not exercise native companion/whole execution; unchanged, not rerun. |
| Recording | Historical capture remains explicitly unqualified for this journey; not regenerated. |
| LICENSE, CONTRIBUTING, CODE_OF_CONDUCT | No intended policy impact; full final review remains with parent. |
| Release binaries/checksums | No artifact produced/published; existing release content not requalified. |
| Contributor issues | No external issue created/edited; backlog not requalified. |

Protected HTTP/owning-port focused tests cover real canonical persistence and
negative transport/replay outcomes. Controlled helper output is not human approval.
The full expensive managed Resume/source-custody join, race suite, independent
frozen acceptance and complete launch sweep remain parent verification gates.
Six-hour browser dashboard policy and generic fifteen-minute principal policy are
unchanged; neither grants native-role/provisioning authority.
