# Aegis Selective Deployment Projection Architecture

**Status:** Architecture decision and implementation report
**Decision:** Adopt Aegis-owned trust stanzas, selectively materialized deployment projections, an embedded bbolt credential authority with independently encrypted records, and disposable Hermes homes per session
**Scope:** Multi-location deployment, trust-stanza selection, secret distribution, runtime materialization, synchronization, revocation, and failure handling

## 1. Executive decision

Aegis will not replicate a complete global configuration or secret store to every server. It will not use OpenBao Raft, another consensus database, or peer-to-peer replication as its fleet distribution mechanism.

Aegis will instead maintain a canonical logical-agent definition and produce a different, approved projection for each deployment. A projection contains only the trust stanzas, runtime configuration, memory bindings, credential references, and encrypted secret material authorized for that deployment.

Hermes profiles are not the canonical representation of an Aegis logical agent or trust stanza. Aegis owns the security model. The Hermes adapter materializes one disposable Hermes home for each session from one effective Aegis trust stanza.

The intended flow is:

```text
canonical Aegis charter
        +
approved deployment binding
        +
target environment and location
        |
        v
deterministic projection compiler
        |
        +-- signed non-secret manifest
        +-- target-encrypted secret payload
        +-- runtime artifact digests
        |
        v
Aegis edge reconciler at one deployment
        |
        v
verified local deployment projection
        |
        v
one mandate + one selected stanza
        |
        v
one disposable Hermes home and process
```

This architecture provides selective physical distribution. A lower-trust location does not merely lack permission to read a higher-trust stanza: that stanza and its secrets are absent from the location's projection.

## 2. Problem statement

One logical agent may run in multiple locations. Each location may have a different trust relationship, caller population, tenant, environment, runtime configuration, tool set, memory scope, and credential set.

For example, one agent may define:

- `principal`
- `internal`
- `customer-support`
- `public`

Its deployments may be:

```text
Principal workstation:    principal + internal
Company server:           internal + customer-support
Public edge:              public
Customer-managed server:  customer-support for one tenant
```

Aegis must keep the selected stanza definitions and their associated secrets current across these deployments without sending the complete global store to every server.

The system must preserve the existing Aegis invariants:

- A session binds to exactly one stanza.
- Stanza authority is never unioned.
- Caller or model text cannot select authority.
- Runtime configuration is derived from an approved charter.
- Credentials are scoped independently per stanza.
- Provisioning is deterministic, reviewable, and verifiable.
- A runtime cannot approve or expand its own authority.

## 3. Why Raft is the wrong distribution mechanism

Raft provides consensus and complete state-machine replication among members of one cluster. Every voting member must be able to reconstruct the same authoritative state so that a follower can become leader.

That conflicts with Aegis's fleet requirement:

```text
Required:
    server A stores stanzas A and B
    server B stores stanza B
    server C stores stanza C

Raft model:
    server A stores the complete database
    server B stores the complete database
    server C stores the complete database
```

API authorization cannot repair that mismatch. A policy may prevent a client from reading a record through an API while the underlying Raft member still physically stores the record.

Raft may become relevant later for high availability inside a central Aegis control plane. If that occurs, fleet deployments still must not become Raft peers. Central control-plane replication and selective fleet distribution are separate concerns.

The initial design does not require control-plane high availability, so Raft is not an implementation dependency.

## 4. Relationship to the current codebase

The current specification already establishes most of the security model needed by this design:

- `Charter` is the canonical logical-agent specification.
- `TrustStanza` independently defines authentication, grants, scopes, session policy, approval policy, information-flow policy, and runtime configuration.
- `ScopeSet.Credentials` identifies credential scopes without embedding secret values.
- `Environment` binds provisioning and authorization to a name, host, and tenant.
- `ProvisioningPlan` binds effects to an exact charter digest, runtime, and environment.
- `RuntimeLaunchSpec` binds one launch to one agent, one stanza, one charter digest, concrete capabilities, and isolated state.
- `Mandate` binds an authenticated subject to one stanza and one charter revision.

Implementation status: the repository contains the local encrypted-authority foundation plus one Linux session-bound broker action. The broker applies an exact `github/read` binding to the fixed `github-api` destination for typed repository metadata, authenticates `SO_PEERCRED` plus a short-lived runtime/session capability, and returns a sanitized result. Its Aegis-owned MCP bridge is model-visible only for this action and launch verifies the live Hermes gateway's exact one-tool registration. It is not a generic proxy. Deployment enrollment/bindings/projection objects, age transport, signed generations, edge reconciliation, production `aegisd`/systemd user provisioning, TPM/recovery operations, network confinement, and Infisical migration remain later acceptance gates. Existing Hermes provider authentication remains environment-backed.

The missing architectural objects are:

1. A stable deployment identity.
2. A location/deployment binding that selects the stanzas permitted at that location.
3. A deterministic deployment projection derived from a charter and binding.
4. A secret binding that resolves logical credential scopes to location-specific records.
5. A signed generation protocol for distributing and activating projections.
6. A local edge reconciler and credential broker.

These should be introduced explicitly rather than overloading `Environment`. Environment is decision context; it is not deployment identity or desired state.

## 5. Authoritative object model

### 5.1 Logical agent

A logical agent remains the stable entity defined by a charter. It is independent of any server, Hermes process, profile, or session.

### 5.2 Trust stanza

A trust stanza is an Aegis security context. It defines:

- Accepted authenticated identities
- Concrete capabilities and tools
- Memory scopes
- Credential scopes
- Session lifetime and reauthentication policy
- Approval requirements
- Information-flow constraints
- Runtime-specific settings permitted by the adapter

A trust stanza is not a Hermes profile. A Hermes profile or home may be generated from a stanza, but it does not define or authorize the stanza.

### 5.3 Deployment

A deployment is one independently authenticated installation of the Aegis edge service on a server or equivalent isolation boundary.

A deployment requires:

- Stable deployment ID
- Location ID
- Environment and tenant
- Workload identity
- Encryption recipient/public key
- Runtime target
- Enabled/disabled state
- Current desired generation
- Current acknowledged generation

A physical host may contain multiple deployments if they have meaningful isolation. If they share root authority, their effective compromise boundary is still the host.

### 5.4 Deployment binding

A deployment binding specifies which parts of an approved logical agent may exist at one deployment.

Conceptual contract:

```go
type DeploymentID string
type LocationID string
type DeploymentGeneration uint64

type DeploymentBinding struct {
    ID              DeploymentID
    LocationID      LocationID
    AgentID         AgentID
    CharterRevision CharterRevision
    CharterDigest   Digest

    Environment Environment

    AllowedStanzas []StanzaID
    RuntimeTarget  string
    Recipient      string

    Generation DeploymentGeneration
    Enabled    bool
}
```

`AllowedStanzas` is approved desired state. It must never be accepted as an authoritative value from the target server or agent runtime.

### 5.5 Credential binding

Charters refer to logical credential scopes. A deployment resolves those scopes to physical secret records.

Example charter scope:

```yaml
credential_scopes:
  - github/read
  - analytics/read
```

Example US deployment binding:

```yaml
credentials:
  github/read:
    secret_ref: github/company-us/reader
  analytics/read:
    secret_ref: postgres/analytics-us/readonly
```

Example EU deployment binding:

```yaml
credentials:
  github/read:
    secret_ref: github/company-eu/reader
  analytics/read:
    secret_ref: postgres/analytics-eu/readonly
```

The logical stanza can therefore remain stable while each location receives different credentials.

### 5.6 Deployment projection

A projection is an immutable, target-specific materialization of one approved charter revision and deployment binding.

Conceptual header:

```go
type DeploymentProjection struct {
    SchemaVersion string

    DeploymentID DeploymentID
    LocationID   LocationID
    Environment  Environment

    AgentID         AgentID
    CharterRevision CharterRevision
    CharterDigest   Digest
    BindingDigest   Digest

    Generation         DeploymentGeneration
    PreviousGeneration DeploymentGeneration

    StanzaIDs     []StanzaID
    StanzasDigest Digest

    CredentialsDigest Digest
    ArtifactsDigest   Digest

    IssuedAt  time.Time
    ExpiresAt time.Time

    ContentDigest Digest
    Signature     []byte
}
```

The projection contains only selected stanza definitions. It does not contain the complete canonical charter.

## 6. Two-stage stanza selection

Aegis must distinguish deployment selection from session selection.

### 6.1 Deployment selection

Deployment selection determines the maximum set of stanzas permitted to exist at a location:

```text
deployment-visible stanzas =
    globally enabled charter stanzas
    intersected with
    approved deployment binding stanzas
```

The projection compiler performs this operation centrally.

### 6.2 Session selection

Session selection chooses exactly one stanza from the deployment-visible set based on authenticated identity and request context:

```text
effective session stanza =
    exactly_one(
        authenticated-subject matches
        intersect deployment-visible stanzas
        intersect currently enabled stanzas
    )
```

Zero matches deny. Multiple matches deny as ambiguous. A session cannot combine grants from two deployed stanzas.

A deployment containing `internal` and `customer-support` does not cause one runtime to receive both. It permits that server to launch separate sessions under either stanza when identity policy resolves exactly one.

## 7. Runtime decision: disposable Hermes homes

Aegis will use the disposable-home strategy rather than treating persistent Hermes profiles as the trust model.

For every operational session, Aegis will:

1. Validate the mandate.
2. Resolve exactly one deployment-visible stanza.
3. Compute concrete capabilities and tools.
4. Resolve the permitted memory and credential scopes.
5. Create a new isolated Hermes home/state directory.
6. Materialize only the selected stanza's runtime configuration.
7. Disable ambient profiles, MCP servers, plugins, memory, and project instructions unless explicitly included.
8. Launch a new Hermes process.
9. Verify the effective runtime configuration.
10. Destroy or quarantine the disposable state after termination according to retention policy.

Illustrative path:

```text
/var/lib/aegis/sessions/<session-id>/hermes-home/
```

A generated home must not inherit authority from a user's ordinary Hermes configuration. The adapter must explicitly supply or disable every security-relevant runtime input.

Persistent memory, when permitted, should be mounted or connected as a separately scoped resource. It should not require reusing an entire prior Hermes home.

### 7.1 Why not persistent profiles

Persistent profile-per-stanza deployment would create several problems:

- Profiles could drift from the approved charter.
- Profiles could accidentally retain transcripts or credentials.
- Profile names could be mistaken for authentication or authorization evidence.
- Multiple sessions could collide in persistent state.
- Fleet scale would produce many mutable profile copies.
- A stanza change would require reasoning about in-place profile mutation.

Disposable homes make the charter, projection, mandate, and adapter output the authority chain.

### 7.2 Optional future optimization

A later optimization may cache immutable, non-secret adapter artifacts keyed by projection and stanza digest:

```text
/var/lib/aegis/runtime-bases/<projection-digest>/<stanza-id>/
```

A session could copy or overlay that base into a disposable home. This is an optimization only. Raw credentials, session state, and mutable memory must not be stored in the shared base.

## 8. Secret architecture

### 8.1 Canonical storage

Canonical secret records will be encrypted independently before persistence in an Aegis-owned embedded bbolt authority database. bbolt supplies local transactional key/value persistence; it does not supply encryption or RBAC. Aegis supplies both.

The controller and each enrolled physical computer run one host-native Aegis daemon that exclusively owns its local authority database:

```text
/var/lib/aegis/authority.db

secret_records/<opaque-record-id>
secret_versions/<opaque-record-id>/<version>
credential_bindings/<agent>/<stanza>/<deployment>/<scope>
```

Each secret version uses a fresh data-encryption key and authenticated encryption before its ciphertext enters bbolt. The data-encryption key is wrapped by a versioned key-encryption key held outside the database. The exact storage, cryptographic, host-service, broker, recovery, and migration requirements are normative in `research/2026-07-17-embedded-bbolt-credential-authority.md`.

The global controller store is never replicated to fleet nodes. Each node's local database contains only records selected for its deployment projection. The resource tree organizes exact bindings but does not create wildcard or inherited authority across stanzas.

### 8.2 Projection-time resolution

For one deployment, the controller will:

1. Select approved stanzas.
2. Collect their credential scopes.
3. Resolve each scope through the deployment's credential bindings.
4. Reject missing, duplicate, ambiguous, disabled, or unauthorized bindings.
5. Decrypt only the selected canonical records inside an isolated projection worker.
6. Create a target-specific credential payload.
7. Encrypt the payload to the deployment recipient.
8. Erase plaintext working material as far as the operating environment permits.
9. Bind the encrypted payload digest into the signed projection.

No unreferenced secret should be included for convenience.

### 8.3 Per-deployment encryption

Each deployment should have an independent encryption identity. A projection may be logically identical for several replicas but must be encrypted independently to each replica's recipient.

Benefits include:

- Individual deployment revocation
- Smaller compromise radius
- Better attribution
- No location-wide shared private key
- Re-enrollment without changing unrelated deployments

A shared per-location key may be supported only as an explicit weaker mode with documented rotation consequences.

### 8.4 Local credential broker

Raw credentials should not normally be written into Hermes profile files or model context.

The Aegis edge service should expose a local credential broker that:

- Accepts requests only from an active Aegis session.
- Validates the mandate, deployment, stanza, and credential scope.
- Rechecks expiry and revocation.
- Audits credential application without logging the value.
- Returns a narrowly scoped injection or applies the credential to an authorized downstream action.

Preferred mode:

```text
Hermes proposes structured action
        |
        v
Aegis authorizes action and selects credential
        |
        v
Aegis applies credential to downstream request
        |
        v
external service
```

Weaker compatibility mode:

```text
Aegis writes short-lived credential material to a session-private file
        |
        v
Hermes tool reads it
```

Compatibility injection should be treated as disclosure to the runtime. A runtime that can read a secret can copy or exfiltrate it.

## 9. Synchronization protocol

### 9.1 Pull-based reconciliation

Deployments should pull desired state from the controller over authenticated HTTPS. Pull is preferable because deployments may be behind NAT, intermittently connected, or operated in separate locations.

The authenticated deployment identity determines which projection may be returned. Caller-provided deployment IDs, stanza names, labels, or secret selectors are not authoritative.

Conceptual request:

```json
{
  "current_generation": 41,
  "current_digest": "sha256:..."
}
```

Unchanged response:

```json
{
  "status": "current",
  "generation": 41
}
```

Update response:

```json
{
  "status": "update",
  "generation": 42,
  "projection": "<signed manifest>",
  "credential_payload": "<target-encrypted bytes>"
}
```

The transport may later use an object store or OCI registry, but the authorization and projection semantics remain Aegis-owned.

### 9.2 Complete target snapshots

Synchronization will distribute a complete snapshot of the target's authorized projection, not a global snapshot and not an initial delta stream.

"Complete" means all current material for one deployment only:

```text
deployment company-us-01 generation 42
  internal stanza
  customer-support stanza
  their runtime configuration
  their referenced credentials
```

Complete target snapshots provide:

- Deterministic recovery
- Unambiguous removal
- Atomic activation
- Digest verification
- No missed tombstones
- No replay log or compaction requirement
- Simple rollback policy

Record-level deltas may be considered only after measurement demonstrates that snapshots are a real bottleneck.

### 9.3 Edge activation

The edge reconciler must:

1. Download into a staging location.
2. Authenticate the controller and verify the projection signature.
3. Verify that deployment ID, location ID, environment, and recipient match local enrollment.
4. Reject generation rollback unless an explicit approved rollback artifact authorizes it.
5. Verify charter, binding, stanza, artifact, and credential digests.
6. Decrypt the credential payload with the local deployment key.
7. Confirm every included stanza is listed in the signed projection.
8. Confirm every secret is referenced by an included stanza and binding.
9. Prepare runtime artifacts without modifying the active generation.
10. Run adapter validation.
11. Atomically switch the active-generation pointer.
12. Stop or expire sessions that cannot remain valid under the new generation.
13. Remove stale material after no active authorized session references it.
14. Verify effective local state.
15. Send an acknowledgment and provisioning receipt.

Partial activation is forbidden. A projection is active as one generation or not active.

## 10. Consistency and lifecycle semantics

### 10.1 Desired consistency

Aegis does not require consensus among fleet locations. Each deployment converges independently toward controller-approved desired state.

The controller is authoritative for:

- Approved charter revision
- Deployment binding
- Projection generation
- Enrollment and revocation
- Credential mapping

The edge is authoritative only for reporting observed local state and application results.

### 10.2 Disconnected operation

A deployment may continue using its last valid projection while disconnected only according to explicit policy.

The projection should include:

- Issue time
- Maximum offline lifetime
- Expiry time
- Required revalidation interval
- Behavior for active sessions after expiry

High-trust stanzas may require online validation and fail closed immediately when the controller is unavailable. Lower-risk stanzas may permit bounded offline operation.

Offline permission must be declared per deployment or stanza; it must not be an implicit availability fallback.

### 10.3 Revocation limits

Removing a secret from a future projection prevents future authorized use through Aegis but cannot prove that a previously exposed credential was erased from a compromised server or runtime.

Effective revocation may require:

1. Disable the stanza or deployment binding.
2. Revoke active mandates and terminate sessions.
3. Publish a new projection without the credential.
4. Rotate or revoke the downstream credential itself.
5. Distribute the replacement only to remaining authorized deployments.
6. Record completion or failure in audit.

Short-lived downstream credentials and brokered application provide stronger revocation than synchronized reusable credentials.

## 11. Security boundaries

### 11.1 Server compromise

A server that stores an encrypted payload and possesses its decryption key must be assumed capable of exposing all secret material in its deployment projection after full host compromise.

Therefore:

```text
deployment projection = maximum local secret blast radius
```

Aegis limits that radius by excluding other stanzas and locations. It cannot make locally usable plaintext inaccessible to a fully compromised root account without an external broker, HSM, enclave, or comparable boundary.

Where stronger separation is required:

- Use separate VMs or hosts for incompatible stanzas.
- Use independent deployment keys.
- Keep high-value credentials at an online broker.
- Use short-lived provider credentials.
- Prevent direct runtime network paths that bypass the broker.
- Never deploy `principal` credentials to unattended lower-trust servers.

### 11.2 Controller compromise

The projection controller can potentially access canonical secret decryption authority and authorize deployments. It is a high-value trusted component.

Controls should include:

- Dedicated service identity
- No model or runtime access
- Minimal operator access
- Read-only canonical source during compilation where practical
- Isolated secret-decryption worker
- No plaintext logs
- Core dumps disabled
- Strict temporary-file handling
- Signed audit events
- Separation between charter approval and projection execution
- Explicit key rotation and recovery procedures

### 11.3 Runtime compromise

A compromised Hermes session remains bounded only if direct credentials, filesystem access, network access, plugins, MCP servers, and tools are actually constrained outside the model.

Disposable homes prevent accidental state inheritance; they do not by themselves provide host confinement. Container, VM, operating-system, network, and broker enforcement remain separate controls.

## 12. Failure handling

### 12.1 Invalid signature or digest

Reject the candidate projection, retain the last valid unexpired generation, emit an audit event, and report degraded synchronization health.

### 12.2 Missing credential binding

Projection generation fails closed. Do not publish a partially functional bundle unless the charter explicitly marks the credential optional and the resulting capability removal is deterministic and reviewable.

### 12.3 Ambiguous scope binding

Fail projection generation. Do not choose the first matching secret or merge records.

### 12.4 Edge cannot decrypt

Do not activate. Likely causes include incorrect recipient, lost key, stale enrollment, or payload corruption. Re-enrollment must require authenticated administrative action.

### 12.5 Runtime adapter validation fails

Do not activate the projection. The controller's desired state does not override local proof that the declared runtime cannot enforce or materialize it.

### 12.6 Acknowledgment is lost

The controller may resend the same immutable generation. Reapplication must be idempotent. The edge can respond with its already-active digest and receipt.

### 12.7 Rollback request

Ordinary lower generation numbers are rejected. A rollback requires a newly signed projection generation that deliberately references older charter or artifact content and includes an approved rollback reason. Generation remains monotonic even when content is reverted.

## 13. Audit and provenance

Aegis should record the complete derivation and application chain without recording secret values:

```text
principal approval
    -> charter revision and digest
    -> deployment binding and digest
    -> selected stanza IDs
    -> credential reference IDs and versions
    -> projection generation and digest
    -> target deployment identity
    -> edge receipt
    -> session mandate
    -> disposable runtime session
```

Required event classes include:

- Deployment enrollment and key registration
- Deployment binding approval/change/disablement
- Projection compilation success or failure
- Selected stanza set
- Secret reference/version selection, without value
- Projection publication
- Edge download, rejection, staging, activation, and rollback
- Stale deployment detection
- Session launch against projection generation
- Credential broker authorization and application
- Revocation and downstream rotation status

An edge acknowledgment is not proof that secret material was erased. Audit language must distinguish desired-state convergence from guaranteed deletion.

## 14. Proposed service boundaries

The following transport-neutral contracts should eventually be added after this report is approved.

### 14.1 Deployment repository

```go
type DeploymentRepository interface {
    SaveBinding(context.Context, DeploymentBinding) error
    GetBinding(context.Context, DeploymentID) (DeploymentBinding, error)
    ListBindings(context.Context, AgentID) ([]DeploymentBinding, error)
    SetEnabled(context.Context, DeploymentID, bool) error
}
```

### 14.2 Projection compiler

```go
type ProjectionCompiler interface {
    Compile(
        context.Context,
        CanonicalCharter,
        DeploymentBinding,
    ) (CompiledProjection, error)
}
```

The compiler must be deterministic for identical approved inputs, excluding clearly separated issuance metadata.

### 14.3 Credential resolver

```go
type CredentialResolver interface {
    Resolve(
        context.Context,
        DeploymentBinding,
        []CredentialScope,
    ) ([]ResolvedCredential, error)
}
```

### 14.4 Projection publisher

```go
type ProjectionPublisher interface {
    Publish(context.Context, CompiledProjection) (PublishedProjection, error)
    Current(context.Context, DeploymentID) (PublishedProjection, error)
}
```

### 14.5 Edge reconciler

```go
type ProjectionReconciler interface {
    Check(context.Context) (ReconcileDecision, error)
    Stage(context.Context, PublishedProjection) (StagedProjection, error)
    Activate(context.Context, StagedProjection) (ProjectionReceipt, error)
    Inspect(context.Context) (ObservedDeploymentState, error)
}
```

### 14.6 Credential broker

```go
type CredentialBroker interface {
    Authorize(context.Context, Mandate, CredentialScope) error
    Inject(context.Context, Mandate, CredentialScope) (CredentialLease, error)
    Execute(context.Context, Mandate, BrokeredAction) (BrokeredResult, error)
}
```

Exact interfaces should be designed alongside failure semantics and conformance tests rather than copied mechanically from these sketches.

## 15. Required invariants and tests

The implementation is not complete unless tests demonstrate at least:

1. A deployment receives only stanzas listed in its approved binding.
2. A deployment cannot request an additional stanza by ID, label, or caller text.
3. A projection never embeds the complete canonical charter by default.
4. Only credential scopes referenced by selected stanzas are resolved.
5. A missing or ambiguous credential binding fails closed.
6. A credential for one tenant or environment cannot satisfy another binding accidentally.
7. Every projection is bound to one deployment recipient.
8. A projection encrypted for deployment A cannot be decrypted by deployment B.
9. Policy and credential payload digests are activated atomically.
10. Generation rollback is rejected.
11. Reapplying the same generation is idempotent.
12. Removing a stanza removes its runtime artifacts and local broker authorization from the next active snapshot.
13. One runtime session receives exactly one stanza even when the deployment hosts several.
14. Every session receives a new disposable Hermes home.
15. Ambient Hermes profiles, MCP servers, plugins, memory, and project instructions are absent unless explicitly authorized.
16. Credentials from one session or stanza do not appear in another session's home.
17. Runtime adapter verification checks the active projection and mandate digests.
18. Expired offline projections fail according to explicit policy.
19. Deployment disablement prevents new sessions and credential-broker operations.
20. Audit records reconstruct charter -> binding -> projection -> mandate -> runtime provenance.

## 16. Implementation phases

### Phase 1: Domain model

- Add deployment and location identifiers.
- Add deployment binding and projection types.
- Add credential reference/binding types.
- Define canonical encoding and digest rules.
- Extend validation and invariant tests.
- Keep these contracts transport- and storage-neutral.

### Phase 2: Local deterministic projection

- Implement projection compilation from in-memory or file-backed fixtures.
- Select explicit stanza IDs only.
- Resolve logical credential references without secret values initially.
- Render a complete human-readable projection preview.
- Bind approval to charter and deployment binding digests.

### Phase 3: Embedded bbolt credential authority

- Pin the reviewed bbolt and cryptographic dependencies.
- Implement the fixed bucket schema, strict versioned codec, finite lock timeout, safe durability options, integrity checks, and consistent backup.
- Implement per-version envelope encryption, versioned key custody, no-echo intake, and isolated decrypt/select/re-encrypt flow.
- Register per-deployment age recipients for projection transport only; age is not the canonical store.
- Ensure logs and errors cannot contain plaintext.
- Add race, crash, corruption, cross-recipient, wrong-key, and wrong-binding tests.

### Phase 4: Edge reconciliation

- Implement authenticated pull.
- Verify signatures, digests, recipient, and monotonic generation.
- Stage and atomically activate complete snapshots.
- Implement observed-state receipts and idempotent retries.
- Implement expiry and bounded offline behavior.

### Phase 5: Disposable Hermes runtime

- Generate a new Hermes home for each mandate.
- Materialize one effective stanza.
- Disable ambient state.
- Launch, inspect, terminate, and clean up Hermes deterministically.
- Prove no cross-stanza runtime-state reuse.

### Phase 6: Local credential broker

- Enforce mandate and stanza checks.
- Start with narrowly scoped session-private injection where unavoidable.
- Add brokered downstream execution for the first supported provider.
- Revoke leases and remove session-private material on termination.

### Phase 7: Operational hardening

- External append-only audit sink.
- Controller signing-key rotation.
- Deployment re-enrollment and key rotation.
- Stale deployment alerts.
- Downstream credential rotation workflows.
- Backup and disaster-recovery tests.

## 17. Explicit non-goals for the first implementation

The initial implementation does not require:

- Raft or peer-to-peer consensus
- Full control-plane high availability
- Full-store replication to edge servers
- Arbitrary selector languages
- OPA as a dependency
- SPIRE as a dependency
- Record-level delta synchronization
- Cross-organization federation
- Multi-party approval
- Hardware attestation
- Guaranteed erasure from compromised hosts
- Hermes profiles as canonical security objects
- Reusing runtime homes across trust stanzas

## 18. Open decisions

The architecture fixes the major boundaries but leaves several implementation choices for focused follow-up decisions:

1. Whether controller canonical metadata begins in files/Git or SQLite.
2. Whether projection transport begins as direct HTTPS or immutable object storage.
3. Which signing algorithm and key custody mechanism protect projection manifests.
4. Whether age recipients are raw age identities, SSH recipients, or backed by another key provider.
5. The exact maximum offline lifetime model.
6. Which first downstream provider receives brokered credential application.
7. Whether a deployment maps one-to-one to a host, container, VM, or configured isolation domain.
8. How permitted persistent memory is mounted into disposable runtime homes.
9. Which projection metadata is considered sensitive and encrypted with the credential payload.
10. The cleanup and forensic-retention policy for failed or terminated session homes.

These choices must not weaken the fixed invariants: selective physical distribution, one stanza per session, target-specific encryption, deterministic projection, atomic activation, and Aegis ownership of authority.

## 19. Final architecture statement

Aegis will deploy logical agents by compiling approved, location-specific projections from canonical charters. Each projection will contain only the trust stanzas and credential material authorized for one authenticated deployment. The projection will be signed, its secret payload will be encrypted to that deployment, and a local Aegis reconciler will activate it atomically.

At runtime, Aegis will authenticate the caller, select exactly one stanza from the deployment-visible set, issue a mandate, create a fresh disposable Hermes home, materialize only that stanza's effective runtime configuration, and launch Hermes without ambient authority.

The resulting authority chain is:

```text
principal intent
    -> canonical charter
    -> approved deployment binding
    -> selective deployment projection
    -> authenticated edge activation
    -> one-stanza mandate
    -> disposable Hermes runtime
    -> brokered or narrowly injected credentials
    -> attributable action and audit record
```

This design directly supports deploying the same logical agent across multiple locations with different trust stanzas while preventing complete-store replication and avoiding Hermes profile state as the security source of truth.
