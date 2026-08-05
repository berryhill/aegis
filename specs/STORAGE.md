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

No transaction may silently span these classes or engines. Cross-store operations use explicit ordering, durable intent/recovery where implemented, and deny readiness while an authoritative write and required projection are inconsistent.

## Qualification matrix

| Plane | Canonical records | Engine and module | Qualified host | Authority | Required durability | Owner |
|---|---|---|---|---|---|---|
| Session authority | mandate, authority context, transition facts; transition root is derived | Badger `github.com/dgraph-io/badger/v4` `v4.9.5` | Linux/amd64, ext4 | one Aegis process | directory `0700`, files `0600`, `SyncWrites=true`, `DetectConflicts=true`, generation publication with synced markers and no-replace activation | `internal/persistence/authority/badger` |
| Credential custody | metadata, encrypted versions, bindings, revocations | bbolt `go.etcd.io/bbolt` `v1.5.0` | Linux/amd64, ext4 | one Aegis process | directory `0700`, file `0600`, two-second lock timeout, `NoSync=false`, `NoGrowSync=false`, encrypted values only | `internal/credentials/bbolt` |

The executable form is `internal/persistence/qualification/contract.go`. `internal/architecture/boundaries_test.go` pins module versions, engine ownership, canonical persistent type ownership, and classification of every top-level production package family.

Darwin release archives are packaging targets, not evidence that either persistence combination is qualified on Darwin. xfs, btrfs, NFS, network filesystems, arm64 persistence, multi-process writers, relaxed sync settings, and alternate engine versions are unqualified.

## Record and authority ownership

`internal/core` owns the canonical identity/session-authority schemas: `Mandate`, `AuthorityContext`, `AuthorityTransitionFact`, `AuthorityTransitionRoot`, and `AuditEvent`. `internal/credentials` owns `SecretRecord`, `EncryptedSecretVersion`, and `CredentialBinding`. Engine packages encode and persist those domain records; they do not redefine them.

Badger may be imported directly only by its session-authority adapter. bbolt may be imported directly by its credential-custody adapter and by `internal/reset` for bounded recognized-state inspection/removal; reset does not become a credential schema or mutation owner. Application, command, HTTP, runtime, model-facing, and manager packages depend on narrow domain or repository contracts rather than engine APIs.

## Admission and crash behavior

A session effect requires one verified snapshot relationship: mandate, authority context ID/digest, transition state, revocation/expiry state, parent dispatch, and fresh admission decision. Missing, malformed, dirty, substituted, cross-family, partially activated, or ambiguous authority state denies. A derived root alone cannot admit work.

Credential use independently reauthorizes the session/mandate/context, exact binding, version policy, destination, deadline, request identity, and revocation state. Plaintext exists only inside the bounded custody callback and MUST NOT be serialized into authority facts, audit, projections, runtime configuration, argv, environment, or model-visible results.

## Migration and recovery

Legacy authority JSON is collision input only; it is never merged into Badger. A generation is activated only through the implemented staged, synced, no-replace publication protocol. Dirty lifecycle evidence requires verified maintenance/recovery before operational open. Credential backup/recovery remains a separate bbolt and key-custody procedure; copying one engine does not constitute a complete Aegis backup.

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
