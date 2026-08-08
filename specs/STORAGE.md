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

The MVI cannot ship its queue path until one exact engine, version, host profile, writer model, and atomic claim/lease/recovery protocol is added to the qualification matrix and executable contract. Existing Badger session-authority qualification does not implicitly qualify fleet-control definitions or queue lifecycle. A process lock, filesystem rename, status projection, or passing happy-path test is not evidence of atomic queue claims.

## Qualification matrix

| Plane | Canonical records | Engine and module | Qualified host | Authority | Required durability | Owner |
|---|---|---|---|---|---|---|
| Session authority | mandate, authority context, transition facts; transition root and current authority projection are derived | Badger `github.com/dgraph-io/badger/v4` `v4.9.5` | Linux/amd64, ext4 | one Aegis process | directory `0700`, files `0600`, `SyncWrites=true`, `DetectConflicts=true`, no-follow descriptor-relative lifecycle mutation, generation publication with synced markers and no-replace activation, and a fixed 256 MiB reserve beyond bounded candidate output | `internal/persistence/authority/badger` |
| Credential custody | metadata, encrypted versions, bindings, revocations | bbolt `go.etcd.io/bbolt` `v1.5.0` | Linux/amd64, ext4 | one Aegis process | directory `0700`, file `0600`, two-second lock timeout, `NoSync=false`, `NoGrowSync=false`, encrypted values only | `internal/credentials/bbolt` |
| Fleet-control definitions | Agent, Loop, and Graph identities/revisions; validation records; Graph run snapshots | **UNQUALIFIED — adapter not selected** | none | none | create-only immutable publication, strict canonical decode, digest verification on read, conflict denial, crash/reopen proof required | domain owners plus future narrow adapter |
| Fleet-control lifecycle | submissions/rejections, queue transitions, claims/leases, Graph runs, Loop executions, attempts, retries, cancellations, expiry, disposition | **UNQUALIFIED — adapter not selected** | none | none | atomic single-winner claim and terminal transition, durable attempt history, lease recovery, replay equivalence, conflict detection, and crash/reopen proof required | `internal/queue` and `internal/execution` plus future narrow adapter |

The executable form is `internal/persistence/qualification/contract.go`. `internal/architecture/boundaries_test.go` pins module versions, engine ownership, canonical persistent type ownership, and classification of every top-level production package family.

Darwin release archives are packaging targets, not evidence that either persistence combination is qualified on Darwin. xfs, btrfs, NFS, network filesystems, arm64 persistence, multi-process writers, relaxed sync settings, and alternate engine versions are unqualified.

## Record and authority ownership

`internal/core` owns the canonical identity/session-authority schemas: `Mandate`, `AuthorityContext`, `AuthorityTransitionFact`, `AuthorityTransitionRoot`, and `AuditEvent`. `internal/registry` owns Agent registrations and revisions; `internal/loop` owns Loop revisions and validation records; `internal/graph` owns Graph revisions and run snapshots; `internal/queue` owns submission, queue, claim/lease, and transition facts; `internal/execution` owns Graph-run, Loop-execution, attempt, and runtime-dispatch facts; `internal/evidence` owns artifacts and verification receipts; and `internal/credentials` owns `SecretRecord`, `EncryptedSecretVersion`, and `CredentialBinding`. Engine packages encode and persist domain records; they do not redefine them.

Badger may be imported directly only by its session-authority adapter. bbolt may be imported directly by its credential-custody adapter and by `internal/reset` for bounded recognized-state inspection/removal; reset does not become a credential schema or mutation owner. Application, command, HTTP, runtime, model-facing, and manager packages depend on narrow domain or repository contracts rather than engine APIs.

## Admission and crash behavior

A session effect requires one verified snapshot relationship: mandate, authority context ID/digest, transition state, revocation/expiry state, parent dispatch, and fresh admission decision. Missing, malformed, dirty, substituted, cross-family, partially activated, or ambiguous authority state denies. A derived root alone cannot admit work.

Credential use independently reauthorizes the session/mandate/context, exact binding, version policy, destination, deadline, request identity, and revocation state. Plaintext exists only inside the bounded custody callback and MUST NOT be serialized into authority facts, audit, projections, runtime configuration, argv, environment, or model-visible results.

A fleet-control effect additionally requires the exact immutable Agent, Graph, Loop, submission/run-snapshot, queue-item, claim, and attempt references required for that boundary. Claim and runtime launch each perform fresh authority admission. Missing optional credential state cannot deny a credential-independent operation, but an operation that declares an exact credential grant fails closed when that grant cannot be authorized.

## Migration and recovery

Legacy authority JSON is collision input only; it is never merged into Badger. A generation is activated only through the implemented staged, synced, no-replace publication protocol. Dirty lifecycle evidence requires verified maintenance/recovery before operational open. Logical authority recovery imports verified canonical records into a fresh inactive generation and rebuilds, rather than trusts, derived authority projections. Native recovery requires exact identity for an already retained generation. Selection returns an explicit durable-commit outcome so a failure after marker publication cannot be interpreted as pre-activation failure. Credential backup/recovery remains a separate bbolt and key-custody procedure; copying one engine does not constitute a complete Aegis backup.

A future migration MUST identify source and destination schemas, preserve immutable IDs/digests or record an explicit mapping, verify the complete source before activation, publish the destination crash-safely, retain rollback evidence, and deny mixed-source operation. No model or runtime may authorize or execute migration.

## Qualification evidence

A qualification change requires, in one reviewable change:

- exact module and platform matrix edits in this specification and `contract.go`;
- focused fail-closed tests for every changed dimension;
- engine lifecycle, corruption, interruption, reopen, and ownership tests;
- broad `go test ./...` and repository build;
- installed-MVI proof through `scripts/verify-installed-mvi.sh` where packaging behavior is affected;
- launch-asset review with unsupported claims removed.

Passing unit tests does not promote an unlisted combination to qualified status.
