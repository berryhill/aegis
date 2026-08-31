# Storage Contract

## Status and scope

This specification freezes the post-Track-A minimum viable implementation (MVI) storage boundary. It is normative for the current release line. “Qualified” means only that the exact combination below is covered by repository tests; it is not a claim of host sandboxing, distributed consensus, arbitrary-filesystem safety, or crash safety beyond the named protocol.

Unknown engines, versions, operating systems, architectures, filesystems, authority models, or durability settings are unqualified and MUST fail closed at a qualification gate. A compatible upstream version is not qualified until this matrix and its executable checks change together.

## State classes

Aegis keeps these classes distinct:

1. **Canonical authority facts** — mandates, authority contexts, and immutable transition facts. These are controller-authored security facts; derived transition roots are not independent authority.
2. **Credential custody** — credential metadata, encrypted versions, exact bindings, and revocation metadata. Reusable plaintext and key-encryption keys are not ordinary persistence facts.
3. **Canonical operational documents** — charters, exact approvals, provisioning plans/receipts, sessions, and canonical audit events. The current create-only/digest-chain filesystem implementation remains authoritative for these records, but it is not thereby qualified as a general transactional database.
4. **Rebuildable projections** — authority roots, audit outbox/projection state, indexes, and status summaries. A projection MUST be reconstructable from verified canonical facts and MUST NOT authorize an effect by itself.
5. **Content-addressed blobs** — runtime artifacts whose digest is verified on read. A filename or caller-provided digest is not authority.
6. **Operational metadata** — lifecycle markers, lock state, process identity, delivery retries, and maintenance state. It may deny readiness but cannot grant authority.
7. **Runtime state** — disposable Hermes homes and process-local state. It is not a canonical Aegis record and cannot survive as session authority after expiry, revocation, or PID reuse.
8. **Fleet-control definitions** — stable Agent, Loop, and Graph identities; immutable revisions/digests; exact validation records; and immutable Graph run snapshots. These are canonical definitions and composition facts, not queue lifecycle or authority.
9. **Fleet-control lifecycle facts** — durable submission or rejection, queue transition, claim/lease, Graph-run, Loop-execution, attempt, cancellation, expiry, retry, and terminal disposition facts. Current status and counts are rebuildable projections.

No transaction may silently span these classes or engines. Cross-store operations use explicit ordering, durable intent/recovery where implemented, and deny readiness while an authoritative write and required projection are inconsistent.

The fleet planes are qualified as one shared Badger store because Graph submission must atomically publish its immutable run snapshot and durable rejection or queue item. The internal adapter implements this accepted-or-rejected outcome transaction; dependency-gated, attempt-bounded claims that transactionally validate projected attempt count, retry availability, and dependency success against canonical facts; expired-lease retry/reclaim with bounded backoff; queued-or-claimed cancellation; rebuildable queue projections; and terminal evidence/disposition transactions. A projection divergence denies claim admission atomically. This does not complete authenticated public application admission, dirty-store recovery, automated lifecycle scheduling, multi-node execution, or installed end-to-end acceptance. Existing Badger session-authority qualification does not implicitly qualify fleet-control records, and fleet qualification does not permit records in the session-authority store.

## Qualification matrix

| Plane | Canonical records | Engine and module | Qualified host | Authority | Required durability | Owner |
|---|---|---|---|---|---|---|
| Session authority | mandate, authority context, transition facts; transition root and current authority projection are derived | Badger `github.com/dgraph-io/badger/v4` `v4.9.5` | Linux/amd64, ext4 | one Aegis process | directory `0700`, files `0600`, `SyncWrites=true`, `DetectConflicts=true`, no-follow descriptor-relative lifecycle mutation, generation publication with synced markers and no-replace activation, and a fixed 256 MiB reserve beyond bounded candidate output | `internal/persistence/authority/badger` |
| Credential custody | metadata, encrypted versions, bindings, revocations | bbolt `go.etcd.io/bbolt` `v1.5.0` | Linux/amd64, ext4 | one Aegis process | directory `0700`, file `0600`, two-second lock timeout, `NoSync=false`, `NoGrowSync=false`, encrypted values only | `internal/credentials/bbolt` |
| Fleet-control definitions | Agent, Loop, and Graph identities/revisions; validation records; Graph run snapshots | Badger `github.com/dgraph-io/badger/v4` `v4.9.5`, schema `fleet-v1` | Linux/amd64, ext4 | one Aegis process holding the exclusive fleet writer lock | shared `state/persistence/fleet-v1`; directory `0700`, files `0600`, `SyncWrites=true`, `DetectConflicts=true`; strict canonical decode, immutable create/conflict denial, digest verification on read; clean-or-verified-recovered open; fixed 256 MiB free reserve | domain owners through `internal/persistence/fleet/badger` |
| Fleet-control lifecycle | submissions/rejections, queue transitions, claims/leases, Graph runs, Loop executions, attempts, retries, cancellations, expiry, disposition | Badger `github.com/dgraph-io/badger/v4` `v4.9.5`, schema `fleet-v1` | Linux/amd64, ext4 | one Aegis process holding the exclusive fleet writer lock | same shared `state/persistence/fleet-v1`; directory `0700`, files `0600`, `SyncWrites=true`, `DetectConflicts=true`; atomic submission, dependency-gated attempt-bounded claim, expired-lease retry/reclaim, queued-or-claimed cancellation, validated projection, and terminal transitions; durable attempt history; clean-or-verified-recovered open; fixed 256 MiB free reserve | `internal/queue`, `internal/execution`, `internal/evidence`, and `internal/disposition` through `internal/persistence/fleet/badger` |

The executable form is `internal/persistence/qualification/contract.go`. `internal/architecture/boundaries_test.go` pins module versions, engine ownership, canonical persistent type ownership, and classification of every top-level production package family.

Darwin release archives are packaging targets, not evidence that any persistence combination is qualified on Darwin. xfs, btrfs, NFS, network filesystems, arm64 persistence, multi-process writers, relaxed sync settings, split definition/queue databases, alternate fleet paths or schemas, and alternate engine versions are unqualified.

## Record and authority ownership

`internal/core` owns the canonical identity/session-authority schemas: `Mandate`, `AuthorityContext`, `AuthorityTransitionFact`, `AuthorityTransitionRoot`, and `AuditEvent`. `internal/registry` owns Agent registrations and revisions; `internal/loop` owns Loop revisions and validation records; `internal/graph` owns Graph revisions and run snapshots; `internal/queue` owns submission, queue, dependency, claim/lease, retry, cancellation, transition, and projection records; `internal/execution` owns Graph-run, Loop-execution, attempt, and runtime-dispatch facts; `internal/evidence` owns artifacts and verification receipts; and `internal/credentials` owns `SecretRecord`, `EncryptedSecretVersion`, and `CredentialBinding`. Engine packages encode and persist domain records; they do not redefine them.

Badger may be imported directly only by its session-authority and fleet adapters. bbolt may be imported directly by its credential-custody adapter and by `internal/reset` for bounded recognized-state inspection/removal; reset does not become a credential schema or mutation owner. Application, command, HTTP, runtime, model-facing, and manager packages depend on narrow domain or repository contracts rather than engine APIs.

## Admission and crash behavior

A session effect requires one verified snapshot relationship: mandate, authority context ID/digest, transition state, revocation/expiry state, parent dispatch, and fresh admission decision. Missing, malformed, dirty, substituted, cross-family, partially activated, or ambiguous authority state denies. A derived root alone cannot admit work.

Credential use independently reauthorizes the session/mandate/context, exact binding, version policy, destination, deadline, request identity, and revocation state. Plaintext exists only inside the bounded custody callback and MUST NOT be serialized into authority facts, audit, projections, runtime configuration, argv, environment, or model-visible results.

A fleet-control effect additionally requires the exact immutable Agent, Graph, Loop, submission/run-snapshot, queue-item, claim, and attempt references required for that boundary. Claim and runtime launch each perform fresh authority admission. Missing optional credential state cannot deny a credential-independent operation, but an operation that declares an exact credential grant fails closed when that grant cannot be authorized.

### Fleet owned path and lifecycle

Both fleet planes own exactly one shared root, `<state_dir>/persistence/fleet-v1`. The state directory and resulting root MUST be clean absolute non-root paths. Alternate roots, symlink substitution, separate definition and queue stores, files outside the root, permissive modes, and partial path evidence deny open. The fleet adapter MUST create directories as `0700` and regular files as `0600`, verify ownership and type without following links, and refuse to repair an unexpected object in place.

One Aegis process MUST acquire and retain the exclusive fleet writer lock before readiness. Contention, stale or unverifiable lock identity, lock loss, and a second writer deny all fleet reads that could be used for authority and all writes. Badger runs with `SyncWrites=true` and `DetectConflicts=true`. Before opening for operational use, the host MUST have at least 256 MiB free beyond any separately bounded migration, backup, compaction, or restore candidate output. Falling below the reserve denies new mutations; it never converts a failed commit into success.

Open marks the store dirty before serving operations. A clean prior close permits normal integrity/schema checks. A missing or dirty close marker requires engine integrity verification, immutable-record digest verification, queue-transition invariant checks, lease recovery, and complete projection replay before readiness. Clean close requires mutation quiescence, durable engine sync/close, post-close normalization and descriptor sync of every Aegis-owned directory to `0700` and regular file to `0600`, durable checked removal of `DIRTY`, and only then synced no-replace `CLEAN` publication. Ambient umask MUST NOT cause a group- or other-writable Badger artifact to be certified by `CLEAN`; normalization failure leaves the store dirty. Process exit, a model statement, or an extant database directory is not clean-close evidence.

Readiness is action-specific and implementation-observed. Both planes require the exact qualified contract, path, owner/modes, held writer lock, disk reserve, `fleet-v1` schema, completed migration state, and clean or verified-recovered lifecycle. Definition reads additionally require canonical decode and digest verification. Submission, claim, retry, cancellation, and terminal mutation additionally require current queue invariants and fresh authority admission at the effect boundary. Unknown, unavailable, stale, ambiguous, partially migrated, or projection-only evidence denies; readiness cannot be inferred from process liveness.

## Migration and recovery

Legacy authority JSON is collision input only; it is never merged into Badger. A generation is activated only through the implemented staged, synced, no-replace publication protocol. Dirty lifecycle evidence requires verified maintenance/recovery before operational open. Logical authority recovery imports verified canonical records into a fresh inactive generation and rebuilds, rather than trusts, derived authority projections. Native recovery requires exact identity for an already retained generation. Selection returns an explicit durable-commit outcome so a failure after marker publication cannot be interpreted as pre-activation failure. Credential backup/recovery remains a separate bbolt and key-custody procedure; copying one engine does not constitute a complete Aegis backup.

Fleet schema migration is offline, controller-authorized, copy-and-verify, and no-replace. There is no qualified in-place migration and no qualified source schema before `fleet-v1`. A future migration MUST identify the exact source and destination schema/codec, hold the exclusive writer lock with the operational store closed, reserve space for bounded candidate output plus 256 MiB, preserve immutable IDs/digests or record an explicit mapping, verify every source record and queue transition, rebuild and compare projections, publish the destination crash-safely, retain the prior generation and rollback evidence, and deny mixed-source operation. Interruption before activation leaves the source authoritative; uncertainty after activation denies readiness until selection is proven. No model or runtime may authorize or execute migration.

Fleet backup MUST use a transactionally consistent engine snapshot plus schema, store identity, generation, and digest manifest. Restore targets a fresh inactive root/generation, verifies the complete snapshot and manifest, replays projections and recovery checks, and uses the same no-replace activation discipline as migration. Copying live files, restoring only definitions or only queue facts, accepting an unverified archive, or overwriting the active root is unqualified. Backup/restore does not include session authority, credential custody, blobs, or runtime state; those remain separate procedures.

## Qualification evidence

A qualification change requires, in one reviewable change:

- exact module and platform matrix edits in this specification and `contract.go`;
- focused fail-closed tests for every changed dimension;
- engine lifecycle, corruption, interruption, reopen, and ownership tests;
- broad `go test ./...` and repository build;
- installed-MVI proof through `scripts/verify-installed-mvi.sh` where packaging behavior is affected;
- launch-asset review with unsupported claims removed.

Passing unit tests does not promote an unlisted combination to qualified status.
