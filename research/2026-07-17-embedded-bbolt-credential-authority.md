# Embedded bbolt credential authority and host-native broker

- Status: Architecture decision and implementation specification
- Date: 2026-07-17
- Prepared for: Aegis
- Supersedes: The SOPS/age canonical-store portions of `specs/DEPLOYMENT_PROJECTION.md` and `research/2026-07-17-secure-prompt-ingress-and-secret-intake-mvp.md`
- Does not supersede: Prompt ingress/egress scanning, exact trust-stanza binding, selective deployment projections, or brokered-use requirements

## Executive decision

Aegis will replace the proposed file-per-secret SOPS canonical store with one host-native Aegis daemon and one embedded bbolt authority database per physical computer. Secret values will be encrypted independently before entering bbolt. bbolt will organize records, versions, bindings, revocations, projection state, and authorization metadata; it will not provide encryption or authorization itself.

The initial Linux implementation is:

```text
one physical computer
    -> one enrolled Aegis deployment identity
    -> one native aegisd process supervised by systemd
    -> one aegis-service-owned bbolt authority database
    -> one pathname Unix-domain broker socket
    -> zero persistent plaintext credentials
```

The fleet controller will compile complete target-specific projections. Every node pulls only its approved trust stanzas and credential records over the existing private Tailscale network. Projection payloads remain independently encrypted to each deployment recipient and signed by the controller. Fleet nodes do not replicate a global bbolt database and do not become consensus peers.

The current Infisical deployment is private, reachable only inside the tailnet, and is not served publicly. This decision is motivated by footprint, host-native operation, and exact Aegis trust-stanza semantics—not by a claim that the current Infisical service is Internet-exposed or immediately unsafe. Infisical remains available during migration and rollback until the replacement passes the acceptance gates in this report.

Mongo remains outside this decision. Existing per-profile Mongo databases continue to hold agent data while credential authority moves behind Aegis. Hermes profiles and Mongo database names remain metadata, never authentication or authorization evidence.

## Goals

1. Treat one computer as one independently enrolled Aegis deployment rather than a collection of persistent credential-service containers.
2. Run as one small Go binary with an embedded NoSQL store and no local secret-manager server.
3. Preserve one-record-at-a-time encryption, rotation, revocation, backup, and projection.
4. Enforce exact `agent + stanza + deployment + credential scope + operation + destination` authorization.
5. Keep reusable credentials out of Hermes, model context, profile files, Mongo, logs, argv, audit values, and plaintext temporary files.
6. Support many concurrent broker reads with deliberately serialized, atomic administrative writes.
7. Keep the public-facing node's maximum secret blast radius equal to its target-specific active projection.
8. Preserve Tailscale as a private network boundary while requiring application-layer deployment identity and signed artifacts.

## Non-goals

The first implementation will not provide:

- An Infisical-compatible API or UI.
- General Vault/OpenBao dynamic-secret engines.
- Fleet-wide bbolt replication, Raft, or peer-to-peer synchronization.
- General hierarchical wildcard authorization.
- Direct database access from Hermes or arbitrary agent tools.
- Guaranteed plaintext zeroization in Go, guaranteed deletion from all media, or protection from a fully compromised root account on an active node.
- HSM-backed online operations, threshold key custody, or TPM monotonic anti-rollback counters.
- Replacement of Mongo agent data.
- Public exposure of the controller or local broker.

## Why bbolt

The selected package is `go.etcd.io/bbolt`. Internet verification on 2026-07-17 found `v1.5.0` as the current stable Go module release. It is MIT-licensed and declares Go 1.25, compatible with Aegis's Go 1.26 baseline.

bbolt fits this workload because it is an embedded, pure-Go, ordered key/value store represented by one database file. It has nested buckets, ACID transactions, one read-write transaction at a time, and concurrent read-only transactions with consistent snapshots. Credential use is read-heavy; intake, binding, rotation, revocation, and projection activation are intentionally low-rate writes.

The following bbolt properties are architectural requirements, not incidental details:

- Only `aegisd` opens the database.
- The file is on a local filesystem, not NFS, SMB, or another network filesystem.
- `bolt.Open` uses mode `0600` and a non-zero lock timeout.
- The parent state directory is `0700` and owned by the static `aegis` service account.
- `NoSync` and `NoGrowSync` remain false.
- Read transactions are short-lived.
- No bbolt key, value, bucket, cursor, or transaction-derived byte slice escapes its transaction closure; values are copied or decoded into owned memory before return.
- `DB.Batch` is not used for security-sensitive writes because its function may be called more than once.
- Backups use a consistent read transaction and `Tx.WriteTo`/`Tx.CopyFile`, not an arbitrary copy of a live file.
- Startup detects an already-open database promptly instead of hanging.
- Startup validates schema and store integrity before serving broker requests.

bbolt does not provide encryption, RBAC, network identity, secure deletion, or safe plaintext memory. Aegis must supply all five.

## Process and filesystem topology

The Linux reference deployment is:

```text
/usr/local/bin/aegisd
/etc/aegis/aegis.yaml
/var/lib/aegis/authority.db
/run/aegis/broker.sock
```

`systemd` creates and owns the state and runtime directories. The daemon runs as a static, unprivileged `aegis` system user. A static user is preferred over `DynamicUser` because the database and deployment identity are persistent and must not be exposed to UID recycling ambiguity.

Hermes runs under a distinct OS identity. It cannot traverse `/var/lib/aegis`, open `authority.db`, read the key-encryption key, or modify active projection metadata. It may connect to the broker socket only when its Unix identity and Aegis session token are authorized.

Recommended unit hardening, subject to validation on every supported distribution:

```text
User=aegis
Group=aegis
StateDirectory=aegis
RuntimeDirectory=aegis
UMask=0077
NoNewPrivileges=yes
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=yes
PrivateDevices=yes
ProtectKernelTunables=yes
ProtectKernelModules=yes
ProtectControlGroups=yes
ProtectProc=invisible
RestrictNamespaces=yes
RestrictRealtime=yes
RestrictSUIDSGID=yes
LockPersonality=yes
MemoryDenyWriteExecute=yes
LimitCORE=0
```

Aegis must not claim these directives are a host sandbox. The exact unit must be tested with `systemd-analyze security` and real broker/network workflows. Required address families must include `AF_UNIX` and the families needed for outbound Tailscale-routed HTTPS. The daemon does not embed `tsnet` in the initial design: it uses the computer's existing Tailscale interface and performs outbound pull, avoiding another Tailscale state store inside Aegis.

## Embedded authority model

### Top-level buckets

The bbolt schema begins with fixed top-level buckets:

```text
meta
agents
deployments
secret_records
secret_versions
credential_bindings
roles
role_bindings
projection_generations
revocations
receipts
```

The `meta` bucket contains at least:

```text
schema_version
store_id
deployment_id
created_at
active_projection_generation
last_clean_shutdown
```

Secret values are keyed by opaque record IDs. Human-readable tree paths are indexes and policy resources; they are not the authoritative secret key and do not contain secret values.

### Resource hierarchy

The logical authorization hierarchy is:

```text
tenant
    / agent
        / exact stanza
            / deployment
                / credential scope
                    / binding
```

Example resource:

```text
tenant/acme/agent/brand-agent/stanza/public/deployment/web-prod-01/scope/mongodb-profile
```

The tree improves organization and inspection, but it does not imply generic inheritance. Authority from sibling stanzas is never unioned. A grant at `agent/brand-agent` does not automatically grant every descendant stanza. Wildcards, prefix-only allows, and "first match wins" behavior are forbidden in the initial implementation.

### Record and version separation

A logical record is separate from its immutable encrypted versions:

```go
type SecretRecord struct {
    ID             string
    Kind           string
    Status         string
    CurrentVersion uint64
    CreatedAt      time.Time
    CreatedBy      string
}

type EncryptedSecretVersion struct {
    RecordID       string
    Version        uint64
    FormatVersion  uint16
    Algorithm      string
    KEKID          string
    KEKVersion     uint64
    RecordNonce    []byte
    Ciphertext     []byte
    WrapNonce      []byte
    WrappedDEK     []byte
    CiphertextHash Digest
    CreatedAt      time.Time
}
```

Stored encodings must be strictly versioned, length-bounded, and fuzz-tested. Decoding unknown algorithms, unknown format versions, malformed lengths, duplicate fields, or trailing data fails closed. No error includes ciphertext, wrapped keys, or plaintext.

### Binding

A binding maps one exact authority context to one record/version policy:

```go
type CredentialBinding struct {
    AgentID       string
    StanzaID      string
    DeploymentID  string
    Scope         string
    SecretRecord  string
    VersionPolicy string
    Mode          string
    Destinations  []string
    Enabled       bool
}
```

The lookup key contains the complete tuple. Matching only `Scope`, a profile name, or a secret path is insufficient. Missing, duplicate, disabled, stale, or ambiguous bindings deny.

## Cryptographic design

### Record encryption

Every secret version receives a fresh random 256-bit data-encryption key. Aegis encrypts the secret with XChaCha20-Poly1305 from `golang.org/x/crypto/chacha20poly1305`, using a fresh 24-byte nonce from `crypto/rand`. Aegis then wraps the data-encryption key under the active versioned key-encryption key using a separate fresh nonce and independently bound associated data.

XChaCha20-Poly1305 is selected because the Go package specifically recommends it when nonces are generated randomly or uniqueness is not trivially maintained. Random nonces avoid rollback-sensitive persistent counters. The implementation must document that XChaCha20-Poly1305 is not a FIPS-approved choice. A future FIPS profile may use AES-256-GCM with its invocation limits and nonce requirements; mixed algorithms are possible because every stored version identifies its algorithm.

The current researched dependency baselines are:

```text
go.etcd.io/bbolt v1.5.0
golang.org/x/crypto v0.54.0
filippo.io/age v1.3.1    # deployment-payload encryption only
```

These are research baselines, not dependencies added by this report. Implementation must pin exact reviewed versions, run `govulncheck`, inspect release notes, and update `go.mod` only with the code that uses them.

### Associated data

Both record encryption and DEK wrapping authenticate a deterministic, length-prefixed context. At minimum:

```text
format version
algorithm identifier
store ID
record ID
record version
secret kind
KEK ID and version
purpose: record-encryption or dek-wrapping
```

Agent, stanza, deployment, scope, destination, and binding revision remain authenticated binding/projection metadata rather than properties of a reusable physical record. If policy requires a record to be physically non-reusable, those fields are also included in its AAD and the record cannot be rebound without re-encryption.

Canonical encoding must not depend on map iteration, locale, wall-clock formatting, or mutable display names. Ciphertext swapping, record renaming, purpose swapping, version rollback within a generation, or KEK-version substitution must cause authentication failure or projection rejection.

### Key hierarchy

```text
host/controller KEK version
    -> wraps one fresh DEK per secret version
        -> encrypts one secret value
```

KEK rotation rewraps DEKs in one atomic administrative operation without decrypting and rewriting every secret value. Old KEK versions remain available only for the bounded migration/recovery window. Every rewrap is audited by record/version identifiers and key versions, never values.

### Key custody

The preferred Linux path uses an encrypted systemd service credential to deliver the KEK at daemon activation:

```text
LoadCredentialEncrypted=aegis-kek:/etc/aegis/aegis-kek.cred
```

Where supported and operationally approved, `systemd-creds` uses `host+tpm2`: decryption requires both the local TPM-derived key and the host key. The decrypted service credential is scoped to the service and is not passed as an environment variable. The implementation reads it once, validates its identifier/version, and minimizes retention.

Required recovery semantics:

- TPM-bound custody is not enabled without a tested offline recovery wrap or an explicit operator decision to accept machine-loss data loss.
- Firmware/PCR policy changes must be tested before rollout if PCR binding is used.
- The KEK and its recovery material are never stored in the same backup set as `authority.db`.
- A host-only root-owned key is an explicitly weaker fallback; it protects process separation but not a stolen disk available with that key.
- Direct TPM APIs, HSM/PKCS#11, cloud KMS, threshold recovery, and TPM NV anti-rollback counters are deferred behind a `KeyCustodian` interface.

### Plaintext memory limits

bbolt memory-maps ciphertext, so enabling bbolt `Mlock` would lock the encrypted database pages rather than solve plaintext handling. It is not required initially.

Plaintext credentials and DEKs exist transiently in Go memory during intake, projection, and brokered use. Aegis must:

- use byte buffers rather than immutable strings where interfaces permit;
- minimize scope and lifetime;
- avoid caches, globals, panic payloads, goroutines, logging, metrics labels, and formatted errors containing sensitive buffers;
- best-effort overwrite owned buffers after use;
- disable core dumps;
- document that the Go runtime, OS, privileged debugger, or compromised root may retain or inspect copies.

Go 1.26's `runtime/secret` is experimental, requires `GOEXPERIMENT=runtimesecret`, is platform-limited, and is outside the Go 1 compatibility promise. The MVP must not depend on it. It may be evaluated later behind build and runtime capability checks.

## Local broker protocol

The broker listens on a pathname `AF_UNIX` stream socket:

```text
/run/aegis/broker.sock
```

Abstract Unix sockets are forbidden because they do not have pathname permissions. The runtime directory is not writable by Hermes. The socket is owner/group restricted; mode `0660` is the maximum intended mode.

For every accepted connection, Aegis obtains Linux `SO_PEERCRED` and validates UID/GID before reading a request body. PID and `/proc/<pid>` executable inspection are not primary authorization because PID reuse creates races. If a future implementation uses `SO_PEERPIDFD`, binary identity may become defense in depth. UID/GID still does not identify the principal or stanza: the request also carries a short-lived unforgeable session capability bound to an active Aegis mandate.

The authorization decision uses:

```text
peer UID/GID
+ session capability
+ active mandate
+ logical agent
+ exact selected stanza
+ local deployment ID
+ credential scope
+ requested operation
+ approved destination
+ secret record/version
+ expiry and revocation state
```

Zero matches deny. Multiple matches deny. No request may supply an authoritative secret reference, deployment ID, stanza, or destination override.

The initial API is HTTP/1.1 with bounded JSON over the Unix socket so it can reuse Aegis's strict decoding, request limits, timeouts, request IDs, and centralized error handling. It exposes typed operations such as:

```text
POST /v1/broker/actions/<supported-action>
POST /v1/secrets/intake/begin
POST /v1/secrets/intake/<transaction>/value
POST /v1/secrets/<reference>/rotate
POST /v1/secrets/<reference>/revoke
GET  /v1/secrets/<reference>/metadata
```

There is no generic runtime `GetSecret` endpoint. Preferred broker actions apply the credential to the downstream request and return a sanitized result. Any compatibility endpoint that injects plaintext into a Hermes process is separately authorized, short-lived, audited as runtime disclosure, and unavailable by default.

## Fleet synchronization over Tailscale

The controller and nodes use the existing tailnet. The controller is not exposed publicly. Tailscale ACLs/grants restrict network reachability, but tailnet membership alone does not authorize a projection.

Each node performs outbound authenticated HTTPS pull and presents its enrolled deployment identity. The authenticated identity, not a caller-provided deployment string, selects the projection. The controller returns either `current` or a complete immutable target snapshot.

Every projection contains:

```text
deployment ID
location/environment
agent and allowed stanza IDs
charter and binding digests
monotonic generation and previous generation
issue/expiry/offline policy
credential references and versions
ciphertext payload digest
content digest
controller signature
```

The manifest uses deterministic encoding and Ed25519 signatures from Go's standard library. Credential payloads are encrypted independently to each deployment recipient using the reviewed age library. age remains a deployment transport format; it is no longer the canonical secret-record store.

The edge performs this order:

1. Authenticate the controller and download to bounded staging memory/storage containing ciphertext only.
2. Verify manifest encoding, signature, target deployment, expiry, generation monotonicity, previous generation, and all digests.
3. Decrypt the target payload with the enrolled deployment identity.
4. Validate that every record is referenced by an included exact binding and stanza.
5. Re-encrypt each value immediately under the local active KEK with fresh local DEKs/nonces.
6. Commit the complete candidate generation and active-generation pointer in one bbolt write transaction.
7. Re-evaluate active mandates and terminate sessions invalid under the new generation.
8. Remove superseded local wrapped DEKs according to rollback and backup policy.
9. Send a signed/authenticated receipt containing identifiers and digests only.

Partial activation is forbidden. Reapplying the same immutable generation is idempotent. Ordinary lower generation numbers are rejected. A rollback uses a newly signed higher generation that deliberately references older content and includes an approved reason.

## Concurrency and performance model

The intended access pattern is many short reads and rare bounded writes:

```text
broker use          -> short DB.View, copy/decode, close transaction, decrypt/use
metadata inspection -> short DB.View
intake/rotation     -> one validated DB.Update
projection activate -> one bounded DB.Update
backup              -> read transaction + Tx.WriteTo
```

No network call, model call, user prompt, downstream API operation, cryptographic projection compilation, or no-echo intake wait occurs while a bbolt transaction is open.

The store layer serializes administrative commands before entering bbolt and exposes no transaction object to application services. Performance tests must measure p50/p95/p99 broker lookup latency under concurrent readers and simultaneous rotation/activation. The design does not set a speculative throughput claim before benchmarks.

An initial mmap size may be configured after measurement to reduce remapping, but it must be bounded for the smallest supported machines. Long-running read transactions are defects because they pin old pages and can grow the file.

## Durability, integrity, backup, and recovery

### Durability

The production store keeps bbolt synchronization defaults. `NoSync` and `NoGrowSync` are forbidden. Secret writes return success only after the bbolt commit succeeds. Projection receipts are sent only after local activation and post-activation verification succeed.

The first database initialization uses a staging path, validates the initialized schema, synchronizes it and its parent directory as supported, and atomically renames it into place. This addresses bbolt's documented first-initialization power-loss caveat.

### Integrity

At startup Aegis verifies:

- owner, group, mode, file type, and local-filesystem policy;
- exclusive bbolt lock within a finite timeout;
- database format and schema version;
- required buckets and metadata;
- active projection generation consistency;
- decryptability of a dedicated non-secret/key-check sentinel without exposing a real credential;
- bbolt structural integrity using the supported check path.

On failure, the daemon does not broker credentials. It emits a safe degraded-health event and requires restore or authenticated repair. bbolt surgery is not the normal recovery path for an authority store.

### Backup

A backup is a consistent bbolt snapshot created through `Tx.WriteTo` or `Tx.CopyFile`. Backups are ciphertext, but metadata remains sensitive. Every backup is additionally encrypted to offline recovery recipients before leaving the host, hashed, versioned, and tested for restore.

The KEK or recovery unwrap material is stored separately. A TPM-bound deployment requires a tested recovery export before the operator treats backup as complete.

### Deletion

Deleting a bbolt key does not shrink the file and stale encrypted bytes or wrapped DEKs may remain in free pages, filesystem snapshots, and backups. Therefore:

- deletion is logical revocation immediately;
- broker authorization denies revoked versions immediately;
- destruction of the relevant wrapped DEK is cryptographic erasure only within the stated key and backup-retention assumptions;
- no claim of guaranteed physical erasure is made;
- offline compaction may reduce retained free pages but is not proof of sanitization;
- downstream reusable credentials must also be revoked or rotated at the issuer.

## Infisical migration

Infisical remains tailnet-only throughout migration. No migration step exposes it or the new controller publicly.

### Phase A: Inventory and model

- Inventory Infisical projects/environments, machine identities or tokens, secret versions, rotation procedures, audit requirements, and Tailscale ACLs without exporting values into reports.
- Map each credential to an exact Aegis agent, stanza, deployment, scope, destination, and owner.
- Reject ambiguous/shared credentials or explicitly approve their blast radius.
- Define recovery and rollback owners.

### Phase B: Embedded authority implementation

- Implement storage-neutral repository and key-custodian interfaces.
- Implement bbolt schema, migrations, strict codec, encryption, no-echo intake, and metadata-only inspection.
- Implement the Unix-socket broker and one downstream integration.
- Keep Infisical as the active source.

### Phase C: Controlled import

- Authenticate an explicit principal migration operation.
- Read one Infisical value through a protected process path.
- Encrypt immediately into a new bbolt secret version.
- Compare only safe keyed fingerprints/digests needed for migration verification.
- Never place values in argv, shell history, logs, temporary files, Mongo, prompts, or generated reports.
- Record source provenance as `infisical` plus safe source identifiers/version, not value.

### Phase D: Shadow and cutover

- Resolve both backends in a non-runtime verification path and compare safe fingerprints.
- Exercise rotation, revocation, backup/restore, wrong-stanza denial, and node reprojection.
- Cut over one low-risk credential and deployment first.
- Keep a bounded authenticated rollback window.
- Revoke old Infisical client authority only after the replacement receipt and downstream operation succeed.

### Phase E: Retirement

- Rotate downstream credentials so old exported values cease to work.
- Remove obsolete Infisical machine identities/tokens and Tailscale grants.
- Retain only the audit/provenance required by policy.
- Decommission Infisical only after every credential, recovery path, and dependent node has an owner-confirmed receipt.

## Interfaces

The implementation should introduce storage-neutral contracts before bbolt-specific code:

```go
type SecretRepository interface {
    Create(context.Context, SecretRecord, EncryptedSecretVersion) error
    AddVersion(context.Context, EncryptedSecretVersion) error
    Metadata(context.Context, string) (SecretRecord, error)
    Resolve(context.Context, CredentialBindingKey) (ResolvedSecret, error)
    Revoke(context.Context, string, uint64, string) error
}

type KeyCustodian interface {
    ActiveKEK(context.Context) (KEKHandle, error)
    KEK(context.Context, string, uint64) (KEKHandle, error)
    Rotate(context.Context, RotateKEKRequest) error
}

type CredentialAuthorizer interface {
    AuthorizeUse(context.Context, BrokerRequest) (BrokerGrant, error)
}

type CredentialBroker interface {
    Execute(context.Context, BrokerGrant, StructuredAction) (SanitizedResult, error)
}
```

Key handles must not expose printable values or implement `fmt.Stringer`. Repository methods return encrypted records or metadata; plaintext flows only through narrowly scoped cryptographic/broker callbacks.

Suggested package boundaries:

```text
internal/credentials/model
internal/credentials/codec
internal/credentials/crypto
internal/credentials/store/bbolt
internal/credentials/custody/systemd
internal/credentials/policy
internal/credentials/broker
internal/credentials/projection
internal/credentials/intake
```

## Required tests and acceptance gates

### Store and concurrency

1. A second writer open fails within the configured timeout instead of hanging.
2. Concurrent `View` operations and serialized `Update` operations pass `go test -race`.
3. No transaction-derived byte slice escapes; copied/decoded values remain valid after remap-heavy writes.
4. Long-running transactions are detectable in tests/metrics without logging keys or values.
5. Wrong ownership, mode, file type, or network filesystem fails startup.
6. Unknown schema, malformed encoding, oversized value, duplicate binding, and trailing data fail closed.
7. Fuzzing stored records never panics or allocates without configured bounds.

### Cryptography

8. Wrong KEK, nonce, AAD field, record ID, purpose, or version fails authentication.
9. Every encryption and wrap operation uses a fresh nonce from `crypto/rand`.
10. Record and wrapped-DEK swaps fail.
11. KEK rewrap preserves the secret and changes wrapping metadata without changing authorization.
12. No secret appears in logs, errors, metrics, audit, argv, environment, Mongo, or plaintext temporary files.
13. Core dumps are disabled; process-crash tests do not emit sensitive panic values.
14. Restore on a different host fails for TPM-bound custody unless the explicit recovery path is used.

### Authorization and broker

15. Socket pathname, directory ownership, and mode are enforced; an unauthorized UID cannot use the API.
16. `SO_PEERCRED` UID/GID and a valid session capability are both required.
17. A profile name, prompt, caller-supplied stanza, or caller-supplied secret reference cannot authorize use.
18. Agent A/stanza X cannot use agent A/stanza Y or agent B bindings.
19. Zero and multiple binding matches deny.
20. Expired/revoked mandate, binding, projection, or secret version denies at each broker use.
21. The first broker integration applies a credential without returning it to Hermes.

### Projection and Tailscale

22. A non-tailnet path cannot reach the controller under the deployed network policy.
23. Tailnet reachability without valid deployment authentication does not return a projection.
24. Signature mutation, target mismatch, digest mismatch, expiry, stale generation, and unauthorized stanza all reject before activation.
25. A node receives no ciphertext or metadata for unreferenced stanzas/secrets.
26. Activation is atomic and idempotent; injected crash points preserve the previous complete generation or the new complete generation, never a mixture.
27. Revocation terminates or invalidates affected sessions according to explicit policy.

### Durability and recovery

28. Repeated forced process/power-loss simulation around commits always reopens to a structurally valid last committed state.
29. Hot backup under concurrent reads/writes restores to one valid snapshot.
30. Backup without recovery material cannot decrypt; recovery material without the backup is insufficient.
31. Unclean shutdown triggers integrity verification before broker readiness.
32. Migration from every previous schema version is atomic, tested, and forward-only.
33. Infisical import/cutover tests prove no value reaches logs or temporary files and rollback remains possible until explicitly closed.

The feature is incomplete until these gates run against the real supported Linux filesystems, systemd configuration, Tailscale policy, and at least one real downstream integration.

## Implementation sequence

### Phase 1: Contracts and threat tests

- Add credential record/version, binding, projection, custody, and broker contracts.
- Add strict authorization tuple and denial reason codes.
- Add tests before selecting storage details in application services.

### Phase 2: bbolt authority

- Pin the reviewed bbolt release.
- Implement directory/file ownership checks, finite lock timeout, safe options, fixed buckets, strict codec, migrations, integrity checks, and consistent backup.
- Add race, fuzz, crash, corruption, and restore tests.

### Phase 3: encryption and custody

- Pin the reviewed crypto dependency.
- Implement per-version DEKs, XChaCha20-Poly1305 encryption/wrapping, canonical AAD, KEK versioning, and systemd credential custody.
- Add host-only development custody clearly labeled as weaker.
- Complete recovery testing before TPM-bound production rollout.

### Phase 4: intake and broker

- Implement no-echo intake with no model invocation.
- Implement pathname Unix socket, peer credentials, session capabilities, strict request bounds, and one brokered downstream action.
- Remove that reusable credential from the Hermes environment.

### Phase 5: selective projections

- Pin the reviewed age dependency for transport only.
- Implement signed manifests, per-deployment encryption, pull over tailnet-routed authenticated HTTPS, staging, atomic bbolt activation, receipts, expiry, and revocation.

### Phase 6: Infisical migration

- Inventory and map without exporting values.
- Import and compare through protected paths.
- Cut over low-risk credentials first.
- Rotate downstream credentials and retire old authority only after verified receipts.

## Security claims and limits

Aegis may claim, after the acceptance tests pass:

- Secret values are independently authenticated and encrypted before bbolt persistence.
- Each node stores only the credential projection authorized for that deployment.
- Broker authorization binds one active mandate to one exact stanza, deployment, scope, operation, and destination.
- Hermes does not have direct access to the authority database or KEK.
- The controller and nodes communicate over a private tailnet path plus application-layer identity and signed artifacts.

Aegis must not claim:

- bbolt provides encryption or RBAC.
- Tailscale membership alone authorizes secret access.
- systemd hardening or Unix users are a host sandbox.
- Docker removal improves every security property.
- Root compromise cannot expose locally usable secrets.
- Go plaintext memory is perfectly zeroized.
- Deletion removes every historical copy or backup.
- A deployment receipt proves erasure.
- This report describes currently implemented behavior.

## Primary Internet sources

Sources were fetched and checked on 2026-07-17. Implementation must recheck version-sensitive material when dependencies are added.

### bbolt

- bbolt repository and README: https://github.com/etcd-io/bbolt
- bbolt Go documentation: https://pkg.go.dev/go.etcd.io/bbolt
- bbolt `v1.5.0` source and options: https://github.com/etcd-io/bbolt/blob/v1.5.0/db.go
- bbolt releases: https://github.com/etcd-io/bbolt/releases
- Go module proxy release metadata: https://proxy.golang.org/go.etcd.io/bbolt/@v/v1.5.0.info
- bbolt command/check/surgery documentation: https://github.com/etcd-io/bbolt/tree/v1.5.0/cmd/bbolt

### Cryptography and key custody

- Go `crypto/rand`: https://pkg.go.dev/crypto/rand
- Go `crypto/cipher`: https://pkg.go.dev/crypto/cipher
- Go XChaCha20-Poly1305: https://pkg.go.dev/golang.org/x/crypto/chacha20poly1305
- RFC 8439, ChaCha20 and Poly1305: https://www.rfc-editor.org/rfc/rfc8439
- XChaCha draft: https://datatracker.ietf.org/doc/html/draft-irtf-cfrg-xchacha
- NIST SP 800-38D, GCM: https://csrc.nist.gov/pubs/sp/800/38/d/final
- NIST SP 800-57 Part 1 Rev. 5, key management: https://csrc.nist.gov/pubs/sp/800/57/pt1/r5/final
- NIST SP 800-88 Rev. 1, media sanitization and cryptographic erase: https://csrc.nist.gov/pubs/sp/800/88/r1/final
- systemd credentials: https://systemd.io/CREDENTIALS/
- `systemd-creds`: https://www.freedesktop.org/software/systemd/man/latest/systemd-creds.html
- Go experimental `runtime/secret`: https://pkg.go.dev/runtime/secret
- age package: https://pkg.go.dev/filippo.io/age
- age format specification: https://age-encryption.org/v1

### Host broker and network boundary

- Linux Unix-domain sockets and `SO_PEERCRED`: https://man7.org/linux/man-pages/man7/unix.7.html
- Go `net.UnixConn`: https://pkg.go.dev/net#UnixConn
- Go `unix.GetsockoptUcred`: https://pkg.go.dev/golang.org/x/sys/unix#GetsockoptUcred
- systemd execution hardening: https://www.freedesktop.org/software/systemd/man/latest/systemd.exec.html
- systemd socket units: https://www.freedesktop.org/software/systemd/man/latest/systemd.socket.html
- Tailscale grants and application/network layers: https://tailscale.com/kb/1324/grants
- Tailscale policy syntax: https://tailscale.com/kb/1337/acl-syntax
- NIST SP 800-207, Zero Trust Architecture: https://csrc.nist.gov/pubs/sp/800/207/final

## Repository status

The current Go implementation now includes the storage-neutral record, encrypted-version, exact-binding, custody, repository, and authority contracts; a fixed-schema deployment-bound bbolt store; strict bounded codecs; startup ownership/mode/schema/structural/key-sentinel validation; XChaCha20-Poly1305 per-version envelope encryption; systemd service-credential file loading; an explicitly weaker host-file development custodian; principal-only no-echo/stdin administration; immutable rotation, logical revocation, metadata-only inspection/audit, and consistent bbolt backup. It also includes one Linux pathname-socket broker action with pre-body `SO_PEERCRED`, short-lived session capability and replay/deadline checks, exact live-state binding resolution, internal downstream credential application, and a sanitized result. Unit, race, fuzz, CLI, and tagged pathname-socket integration workflows cover these implemented boundaries.

This remains a partial implementation of the report. Aegis does not yet provide the native `aegisd` service/unit, production user/unit provisioning, TPM-backed and recovery-tested `LoadCredentialEncrypted` provisioning workflow, a verified model-visible Hermes bridge, KEK rotation/rewrap, encrypted offline backup wrapper, signed selective deployment projections, edge reconciliation, Tailscale pull, Infisical import/cutover, or a filesystem/power-loss acceptance matrix. Operational Hermes model-provider authentication remains environment-backed; authority records are available only through the one typed broker action and are never returned to Hermes.

## Conclusion

The exact lightweight replacement is not "bbolt as a vault." It is an Aegis security service that happens to use bbolt for embedded NoSQL persistence:

```text
bbolt provides atomic local key/value storage
Aegis provides identity, RBAC, stanza isolation, and audit
AEAD provides record confidentiality and integrity
systemd/TPM provides local KEK custody
Tailscale provides private network reachability
signatures and per-node encryption provide projection authenticity and selectivity
Unix peer credentials plus mandates protect the local broker
```

This preserves the useful isolation of the current private Infisical deployment while removing the requirement for a local containerized secret-manager service. One computer remains one enrolled computer, one Aegis daemon owns the authority store, and every Hermes session receives only brokered authority from exactly one trust stanza.
