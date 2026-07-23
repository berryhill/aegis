# Cross-deployment credential leasing and provider-issued authority

- Status: Complete supporting research; non-normative
- Date: 2026-07-23
- Prepared for: Aegis
- Scope: Credential delegation between Aegis deployments, provider-issued child credentials, lease lifecycle, open protocols, AP2/Visa analogies, security boundaries, and implementation direction
- Evidence cutoff: 2026-07-23 UTC
- Current authority: `AGENTS.md`, `specs/`, and implemented/tested behavior remain authoritative

## Executive summary

Aegis should support one authenticated deployment granting another deployment temporary, narrowly bounded credential-backed authority. It should not implement that feature by replicating an entire credential database or routinely copying the same long-lived bearer credential to both hosts.

The preferred operation is provider-issued delegation:

```text
authenticated principal mandate
        +
approved recipient deployment identity
        +
exact provider scope and lifetime
        |
        v
Aegis controller uses a separate provider-management credential
        |
        v
provider creates a recipient-specific child credential
        |
        v
credential is encrypted only for the recipient deployment
        |
        v
recipient stores and brokers it under its own local authority
        |
        v
controller renews, rotates, or revokes it independently
```

For Doppler, a controller can use a separately stored service-account API token with appropriate administrative permissions to create a project/config-scoped service token for a workstation. The Doppler control credential must remain on the controller and must never be leased to the recipient, supplied to Hermes, or used as the target credential being rotated. The newly issued service token should be unique to the recipient deployment, short-lived where practical, and independently revocable.

No single open protocol completely specifies this system. The strongest design composes established mechanisms:

- SPIFFE/SPIRE for deployment workload identity and mutually authenticated transport;
- OAuth 2.0 Token Exchange subject/actor semantics for delegation;
- OAuth 2.0 Rich Authorization Requests or GNAP for exact structured grants;
- mTLS or DPoP for sender-constrained control-plane capabilities;
- OpenBao/Vault lease, renewal, revocation, and response-wrapping semantics;
- HPKE or age-style recipient encryption when raw secret delivery is unavoidable;
- an AP2-inspired signed mandate binding authenticated human intent to derived authority;
- Aegis-owned charter, trust-stanza, deployment-binding, projection, approval, and audit enforcement.

The analogy to agent-payment protocols is substantive. Visa Intelligent Commerce describes agent-specific payment tokens whose lifecycle and use must match authenticated user payment instructions. The open Agent Payments Protocol (AP2) uses mandates and verifiable digital credentials to bind user intent to agent payment authority. Aegis should apply the same control-plane pattern to operational authority:

```text
authenticated principal intent
→ signed credential-lease mandate
→ deployment-specific child credential
→ constrained brokered operation
→ authoritative lifecycle and use evidence
```

AP2 and Visa payment mechanisms are not themselves general credential-leasing protocols. Their mandate pattern is reusable; their payment-specific token and transaction schemas are not a substitute for Aegis deployment identity, provider adapters, credential custody, or trust-stanza enforcement.

The current repository already selects the compatible fleet architecture in `specs/DEPLOYMENT_PROJECTION.md`: do not replicate a global store; establish stable deployment identities; compile selective target-specific projections; encrypt secret material to the recipient; and reconcile signed monotonic generations at each edge. That fleet transport and reconciliation path is not yet implemented. The present implementation contains the local encrypted authority and one narrow local broker action.

## 1. Problem definition

An operator may run Aegis on multiple independent hosts, for example:

```text
Laptop Aegis       deployment=laptop
Workstation Aegis  deployment=workstation
```

Each installation should have:

- its own deployment identity;
- its own authority database and store identifier;
- its own key-encryption key custody;
- its own approved trust-stanza projection;
- its own local credential bindings;
- its own lifecycle and audit state.

The operator may want the laptop to authorize the workstation to use one credential-backed capability without giving the workstation every credential or all provider-management authority available on the laptop.

The term **lease** is potentially ambiguous. It may refer to three materially different operations:

1. **Provider-issued child credential:** create a distinct credential for the recipient. This is preferred.
2. **Encrypted static replication:** deliver the same provider secret to the recipient under a different encryption domain. This is weaker and must be labeled honestly.
3. **Remote brokering:** retain the secret on the controller and perform a narrow operation for the recipient. This avoids secret duplication but requires controller availability.

Aegis must represent these as distinct strategies rather than presenting all three as equivalent leasing.

## 2. Core security decision

### 2.1 Prefer authority delegation over secret copying

The recommended model is:

```text
Laptop:
  doppler-rotation-control
  laptop-specific Doppler service token

Workstation:
  workstation-specific Doppler service token

Doppler:
  records separate token slugs, expirations, and activity
```

The laptop delegates a bounded scope. It does not copy its own service token to the workstation.

Benefits include:

- per-deployment attribution at the provider;
- independent revocation and expiration;
- scope differences by deployment;
- smaller compromise blast radius;
- no coordinated cutover of one shared bearer token;
- simpler machine replacement;
- no need to synchronize the controller's local secret value.

### 2.2 Treat static replication as an explicit weaker mode

When a provider cannot issue child credentials, Aegis may target-encrypt an existing static credential for another deployment. Both deployments then possess the same plaintext under different local encryption domains:

```text
same provider plaintext
├── independently encrypted under laptop authority
└── independently encrypted under workstation authority
```

The ciphertext, record identifier, KEK, and local binding may differ, but provider authority is shared. Aegis must disclose:

```text
strategy: encrypted-static-replica
provider-attribution: shared
revocation-granularity: shared
offline-immediate-revocation: impossible
compromise-blast-radius: all replicas
```

This requires explicit principal approval of the expanded blast radius.

### 2.3 Prefer remote brokering when operations are narrow

If the recipient needs only a small typed operation, the controller can retain the secret and expose a narrow authenticated action:

```text
workstation → controller Aegis:
  doppler.fetch_config(logical_scope)

controller Aegis → Doppler:
  exact authenticated provider request

controller Aegis → workstation:
  bounded sanitized result
```

This avoids credential delivery. It must not become a generic URL, method, header, or arbitrary-request proxy. The controller becomes availability-critical, and network confinement is still required to prevent alternate paths.

## 3. Doppler feasibility

### 3.1 Service-token lifecycle

Doppler's current API provides explicit service-token lifecycle operations:

- `POST /v3/configs/config/tokens` creates a token for a required project, config, and name;
- the create request can specify `read` or `read/write` access and an expiration;
- the response returns a token slug and the one-time token key;
- `GET /v3/configs/config/tokens` lists metadata for a project/config;
- `DELETE /v3/configs/config/tokens/token` revokes by slug or token value.

Doppler documents that a service token is scoped to one config within one project, that its key is shown only at creation, and that revocation is immediate and irreversible [1][2][3][4].

### 3.2 Control identity is separate from target identity

A Doppler service token with `read/write` access can read or write secrets in its selected config. That does not imply authority to administer service tokens. Aegis should use a separately provisioned Doppler service-account API token or another appropriately authorized administrative identity for token lifecycle operations [5][6].

Required invariant:

```text
target_record_id != control_record_id
```

Aegis must also reject direct or indirect control cycles.

### 3.3 Recommended Doppler topology

```text
Controller authority record:
  reference: doppler-rotation-control
  type: Doppler service-account API token
  destination: doppler-control-api
  scope: exact allowed workplace/projects/configs and token lifecycle actions

Recipient authority record:
  reference: bd-site-doppler-prod
  type: Doppler service token
  provider external ID: token slug
  scope: exact project/config
  access: preferably read
  expiry: bounded
```

The controller credential remains encrypted in the controller's Aegis authority. It is lent only to a deterministic Doppler adapter for a bounded exact request and is never entered into process-wide environment, Hermes context, command-line arguments, logs, audit, plans, or receipts.

## 4. Relationship to AP2 and Visa Intelligent Commerce

### 4.1 Shared control-plane pattern

Visa Intelligent Commerce describes provisioning and lifecycle management of agent-specific payment tokens, authenticated payment instructions, controls ensuring that credential requests match those instructions, and validation that resulting authorizations match the original instruction [7].

AP2 is an open protocol and Apache-2.0 reference project for agent commerce. Its architecture uses mandates and Verifiable Digital Credentials to establish user intent and payment authorization across agents and commerce participants [8][9].

The common security pattern is:

```text
human authentication
→ explicit signed intent
→ derived limited credential
→ constrained action
→ verifiable evidence
```

Aegis maps that pattern as follows:

| Agent-payment concept | Aegis concept |
|---|---|
| authenticated cardholder/user | authenticated principal |
| intent or payment mandate | credential-lease mandate |
| agent-specific payment token | deployment-specific provider credential |
| merchant/payment constraint | provider scope, destination, and operation constraint |
| transaction validation | broker and provider-operation validation |
| payment lifecycle evidence | metadata-only lease, rotation, use, and revocation audit |

### 4.2 Important differences

AP2 and Visa mechanisms operate within payment ecosystems that can enforce token constraints at payment networks and participating merchants. A generic provider bearer token may not support sender constraint or transaction-level mandate validation.

For example, Aegis can bind its lease activation capability to the workstation's mTLS or DPoP key, but that does not make a Doppler service token proof-of-possession-bound unless Doppler validates the same mechanism. Once delivered, a Doppler service token remains a bearer credential. Aegis can reduce risk through provider scope, expiration, local brokering, host isolation, and revocation, but it must not claim that a copied token is cryptographically unusable elsewhere.

AP2's mandate pattern should inform Aegis. AP2's payment schemas should not become an Aegis dependency for non-payment credentials.

## 5. Applicable open protocols

### 5.1 SPIFFE and SPIRE

SPIFFE standardizes workload identities and verifiable identity documents. SPIRE is an open-source implementation that attests workloads and issues short-lived X.509-SVIDs or JWT-SVIDs through a Workload API. SPIFFE federation can establish trust between independently administered trust domains [10].

Possible deployment identities:

```text
spiffe://operator.example/aegis/deployment/laptop
spiffe://operator.example/aegis/deployment/workstation
```

Aegis can use X.509-SVID mTLS to authenticate both ends of a lease protocol. SPIFFE answers who the workload is. It does not decide which stanza, provider, scope, secret, operation, or lifetime the workload receives. Aegis remains the authorization authority.

### 5.2 OAuth 2.0 Token Exchange

RFC 8693 defines an HTTP/JSON security token service for token exchange, including delegation and impersonation [11]. Its subject/actor model maps well to Aegis:

```text
subject = authenticated principal
authorizing actor = controller Aegis
recipient actor = workstation deployment
```

Aegis can issue a short-lived control-plane capability authorizing the exact recipient to activate one approved lease. The capability should not be the downstream provider credential.

### 5.3 OAuth 2.0 Rich Authorization Requests

RFC 9396 defines structured `authorization_details` for fine-grained grants [12]. An Aegis profile could represent:

```json
{
  "type": "aegis_credential_lease",
  "provider": "doppler",
  "credential_type": "service-token",
  "recipient": "spiffe://operator.example/aegis/deployment/workstation",
  "resource": {
    "project": "bd-site",
    "config": "prod"
  },
  "access": "read",
  "operations": ["secrets.fetch"],
  "maximum_lifetime_seconds": 28800,
  "delegable": false
}
```

This shape is preferable to broad string scopes such as `admin`.

### 5.4 DPoP and mTLS sender constraints

RFC 9449 defines DPoP, an application-layer proof-of-possession mechanism for sender-constraining OAuth tokens [13]. Aegis could bind a lease activation capability to a workstation key. mTLS with an attested deployment identity is likely simpler for the initial host-to-host protocol.

Sender constraint applies only where the verifier participates. It can protect the Aegis control plane without automatically protecting an independently issued provider bearer token.

### 5.5 GNAP

RFC 9635 defines a protocol for delegating authorization to software and conveying the resulting access artifacts [14]. Its resource-oriented grant negotiation is conceptually close to Aegis credential leasing and avoids some legacy OAuth client assumptions.

GNAP is a useful object-model reference. Its implementation ecosystem is smaller than OAuth's, so it should not be an initial hard dependency without a concrete interoperability requirement.

### 5.6 OpenBao and Vault

OpenBao and Vault provide mature lease semantics:

- lease identifiers;
- TTL and maximum TTL;
- renewal;
- revocation;
- dynamic credentials;
- revocation trees;
- one-time response wrapping.

Response wrapping is relevant to one-time delivery: a recipient gets a short-lived single-use wrapping capability rather than receiving secret material in the initial response [15][16][17][18].

Aegis can adopt these semantics or interoperate with OpenBao. A lease mechanism still cannot create provider-level sender constraints that the provider does not support.

### 5.7 SDS and Secrets Store CSI

Envoy Secret Discovery Service distributes and rotates TLS secrets to authenticated Envoy instances. Kubernetes Secrets Store CSI mounts external secret-store material into pods and supports provider plugins and rotation [19][20].

These are useful delivery-adapter references, not complete cross-deployment authorization protocols. SDS is oriented toward Envoy/xDS secret resources, and CSI is Kubernetes-specific.

### 5.8 Target encryption

When a raw secret must cross deployments, the payload should be encrypted to the recipient deployment's approved public key. HPKE provides a standardized hybrid public-key encryption construction [21]. The existing Aegis deployment projection specification currently names age transport as a later acceptance gate. Either selection must bind ciphertext to exact signed lease metadata and avoid treating transport encryption as authorization.

## 6. Proposed Aegis Credential Lease Profile

Aegis should define a narrow profile composed from the preceding standards rather than inventing a monolithic secret-transfer protocol.

```text
Transport authentication:  SPIFFE X.509-SVID mTLS or exact enrolled mTLS identity
Recipient encryption:      approved deployment key using HPKE or age
Delegation semantics:      RFC 8693 subject/actor model
Grant representation:      RAR-shaped authorization details
Sender constraint:         mTLS initially; DPoP where needed
Lease lifecycle:           OpenBao-style TTL/renew/revoke/wrap semantics
Human authorization:       canonical Aegis plan and approval digest
Agent authorization:       AP2-inspired mandate chain
Local enforcement:         Aegis stanza, binding, broker, authority, and audit
```

### 6.1 Credential lease mandate

Conceptual object:

```go
type CredentialLeaseMandate struct {
    SchemaVersion string
    LeaseID       string

    PrincipalID string

    IssuerDeploymentID    string
    RecipientDeploymentID string
    RecipientIdentity     string
    RecipientKeyDigest    string

    AgentID      string
    StanzaID     string
    CharterDigest string
    BindingDigest string

    Provider       string
    CredentialType string
    Resource        map[string]string
    Operations      []string
    Destinations    []string

    Strategy   string
    Renewable  bool
    Delegable  bool
    Persistence string

    IssuedAt  time.Time
    NotBefore time.Time
    ExpiresAt time.Time

    Generation         uint64
    PreviousGeneration uint64
    Nonce              string

    PolicyDigest     string
    ProjectionDigest string
    ContentDigest    string
    Signature        []byte
}
```

All fields must be canonicalized, bounded, strictly validated, and included in the approved content digest. Unknown fields must fail validation rather than being ignored.

The mandate contains no plaintext credential, provider authorization header, refresh token, or controller secret.

### 6.2 Encrypted lease payload

The secret payload is separate:

```go
type EncryptedLeasePayload struct {
    LeaseID             string
    RecipientDeployment string
    RecipientKeyDigest  string
    MandateDigest       string
    CipherSuite         string
    Encapsulation       []byte
    Ciphertext          []byte
    CiphertextDigest    string
}
```

Associated data must include at least the lease ID, recipient deployment ID, recipient-key digest, mandate digest, generation, provider, credential type, and expiry.

### 6.3 Local representation

The recipient should import the value into its independently encrypted authority and retain origin metadata:

```go
type LeasedCredentialMetadata struct {
    LocalRecordID       string
    LeaseID             string
    IssuerDeploymentID  string
    Provider            string
    CredentialType      string
    ExternalID          string
    MandateDigest       string
    Generation          uint64
    ExpiresAt           time.Time
    Renewable           bool
    Delegable           bool
    Strategy            string
    RevocationState     string
}
```

The local credential repository is deployment-bound. Existing encrypted records cannot simply be copied between repositories because KEKs, store IDs, record IDs, and authenticated encryption contexts differ.

## 7. Protocol flow

### 7.1 Enrollment

```text
1. Workstation generates or obtains an attested deployment identity.
2. Workstation presents identity and recipient public key.
3. Laptop/controller authenticates the principal.
4. Principal reviews exact deployment ID, identity, key fingerprint, location,
   allowed stanzas, maximum credential scopes, and lease limits.
5. Principal approves the canonical enrollment digest.
6. Controller stores the deployment binding and monotonic generation.
```

Enrollment is provisioning. Discussion or model output cannot authorize it.

### 7.2 Lease request and planning

```text
1. Workstation authenticates with mTLS.
2. Workstation requests one logical credential scope and lifetime.
3. Controller resolves the exact deployment binding.
4. Controller resolves exactly one eligible stanza.
5. Controller resolves exactly one provider control binding.
6. Controller constructs a deterministic lease plan.
7. Principal or pre-approved policy authorizes the exact digest.
```

Zero or multiple eligible stanza/control matches deny.

### 7.3 Provider issuance

```text
1. Controller opens the local encrypted authority.
2. Controller lends the provider-management credential only to the adapter.
3. Adapter sends the exact bounded provider request.
4. Provider creates a recipient-specific credential.
5. Adapter captures external ID, expiry, scope, and one-time secret value.
6. Secret value is immediately encrypted for the recipient.
7. Temporary plaintext buffers are wiped best-effort.
```

The provider adapter must sanitize errors and never retain full response bodies that may contain a one-time secret.

### 7.4 Delivery and activation

```text
1. Recipient verifies issuer identity and mandate signature.
2. Recipient checks exact recipient ID and key digest.
3. Recipient rejects stale/replayed generations.
4. Recipient verifies charter, binding, scope, destination, and expiry.
5. Recipient decrypts and re-encrypts into its local authority.
6. Recipient creates disabled/staged bindings.
7. Recipient validates the credential with a harmless provider-specific call.
8. Recipient activates current-version bindings atomically.
9. Recipient emits a signed activation acknowledgement.
```

If activation acknowledgement is absent, an ordinary availability-preserving rotation must not revoke the old credential.

### 7.5 Renewal, rotation, and revocation

Renewal is provider- and strategy-specific:

- renew a real lease when supported;
- issue a replacement child credential when credentials are immutable;
- update a static credential in place only when the provider requires it;
- require manual issuance when no API exists.

Normal rotation:

```text
issue replacement
→ stage at recipient
→ validate replacement
→ activate recipient binding
→ acknowledge activation
→ revoke old provider credential
→ verify revocation
→ complete
```

Emergency containment may revoke before full cutover, but only under an exact plan that states outage consequences and receives explicit principal approval.

## 8. Provider and delivery adapters

Provider issuance and consumer delivery are separate responsibilities.

### 8.1 Provider lifecycle adapter

```go
type ProviderAdapter interface {
    Name() string
    Capabilities() ProviderCapabilities
    Observe(context.Context, ProviderControl, ProviderTarget) (ObservedState, error)
    Issue(context.Context, ProviderControl, IssueRequest, SecretSink) (IssuedMetadata, error)
    Validate(context.Context, ValidateRequest, SecretSource) error
    Revoke(context.Context, ProviderControl, RevokeRequest) error
}
```

Adapters must be constructor-registered and strictly configured. They must not accept arbitrary URLs, methods, headers, scopes, or secret references from model input.

### 8.2 Delivery adapter

```go
type DeliveryAdapter interface {
    Name() string
    Stage(context.Context, DeliveryPlan, SecretSource) (DeliveryReceipt, error)
    Validate(context.Context, DeliveryReceipt) error
    Activate(context.Context, DeliveryReceipt) error
    Rollback(context.Context, DeliveryReceipt) error
}
```

Potential adapters include:

- Aegis deployment projection;
- local current-version binding;
- systemd credentials;
- Kubernetes Secrets Store CSI integration;
- a narrow manual handoff state;
- remote broker with no secret delivery.

Provider issuance must not be coupled to one destination mechanism.

## 9. Durable lifecycle coordinator

Cross-provider rotation and leasing require a crash-recoverable saga, not one database transaction.

```text
requested
→ planned
→ approved
→ issuing
→ issued
→ encrypted-for-recipient
→ delivered
→ staged
→ verified
→ active
→ revoking-previous
→ revocation-verified
→ completed
```

Failure states include:

```text
issue-failed
issue-outcome-unknown
delivery-failed
verification-failed
activation-unacknowledged
revocation-failed
expired
manual-intervention-required
compensated
```

Persist only metadata:

- operation/lease ID;
- provider and adapter version;
- target and control record identifiers;
- recipient deployment;
- external provider slug/identifier;
- plan, policy, binding, charter, and projection digests;
- generation and previous generation;
- phase and sanitized error code;
- timestamps, expiry, retries, and acknowledgements;
- metadata-only provider request IDs.

Never persist plaintext in the journal.

### 9.1 Ambiguous provider outcomes

A crash after provider issuance but before local persistence can create duplicate credentials on retry. The coordinator must:

- use provider idempotency keys where available;
- assign a deterministic provider-visible name containing the operation ID;
- reconcile through provider listing APIs;
- block a second issue attempt while outcome is unknown;
- persist the external credential identifier as soon as available;
- support explicit compensation by revoking an unused replacement.

## 10. Revocation and offline limits

Remote revocation cannot erase an offline copy. Aegis must select and disclose one policy:

1. **Short lease, fail closed without refresh:** strongest revocation; weaker offline availability.
2. **Long lease:** improved offline availability; longer compromise window.
3. **Static projection:** indefinite offline operation; no immediate remote revocation.
4. **Remote broker:** no local secret; controller required for every operation.

Local enforcement should require both:

- unexpired lease state; and
- acceptable revocation/projection freshness.

A provider revocation makes the credential unusable at the provider even if local encrypted bytes remain. A local revocation prevents Aegis broker use but cannot prevent an already-exfiltrated bearer token from being used elsewhere until the provider revokes or expires it.

## 11. Threat model

### 11.1 Controller compromise

Impact may include provider-management authority and issuance/revocation of child credentials. Controls:

- separate OS identity and service boundary;
- narrow provider control scopes;
- hardware-backed or external key custody where available;
- exact project/config allowlists;
- principal approval for consequential changes;
- no provider control credential in model/runtime environment;
- independent provider and Aegis audit review.

### 11.2 Recipient compromise

Impact should be limited to recipient-specific credentials and projected stanzas. Controls:

- unique per-deployment provider credential;
- short expiry;
- non-delegable lease;
- local typed broker rather than raw delivery where possible;
- host and network confinement;
- independent provider revocation;
- no control credential at recipient.

### 11.3 Replay and rollback

Controls:

- recipient-bound encryption;
- signed lease content;
- nonces;
- monotonic generations;
- previous-generation linkage;
- exact issuer/recipient validation;
- rejection of stale projection and binding digests;
- bounded activation capabilities.

### 11.4 Confused deputy

Controls:

- exact trust-domain plus workload identity;
- exact agent, stanza, deployment, scope, operation, destination, and provider target;
- no caller-selected secret reference, URL, header, or provider method;
- zero or multiple matches deny;
- no authority union across stanzas or deployments.

### 11.5 Model manipulation

The model may discuss or propose a lease plan. It may not:

- enroll a deployment;
- select a stanza;
- choose a control credential;
- expand provider scope;
- approve a lease;
- issue, deliver, activate, renew, rotate, or revoke a credential;
- receive plaintext or control-plane capabilities.

All consequential operations remain deterministic Aegis application actions.

## 12. Policy requirements

A lease policy should bound:

```yaml
provider: doppler
credential_type: service-token
allowed_recipients:
  - workstation
allowed_projects:
  - bd-site
allowed_configs:
  - prod
maximum_access: read
allowed_operations:
  - secrets.fetch
maximum_lifetime: 8h
maximum_overlap: 10m
renewable: true
delegable: false
persistence: encrypted-local
revocation_freshness: 5m
emergency_revoke: principal-approval
```

Configuration contains policy and record/binding references only. It must contain no provider token.

## 13. Current Aegis fit and gaps

### 13.1 Existing compatible foundations

Aegis already has:

- authenticated principal and exact stanza invariants;
- canonical charters, plans, digests, and approvals;
- a deployment-bound encrypted bbolt credential authority;
- immutable secret versions;
- exact agent/stanza/deployment/scope bindings;
- current versus pinned version policy;
- local no-echo create and rotation;
- a narrow local credential broker action;
- authoritative metadata-only audit;
- a selective deployment projection architecture decision.

### 13.2 Missing implementation

The current code does not yet implement:

- deployment enrollment and recipient keys;
- remote mutually authenticated Aegis protocol;
- signed credential-lease mandates;
- target-encrypted projection transport;
- provider lifecycle adapter registry;
- Doppler control adapter;
- lease/rotation journal;
- remote generation reconciliation;
- revocation freshness protocol;
- signed activation acknowledgements;
- cross-host edge reconciliation;
- production host/network confinement.

No current release should claim cross-deployment credential leasing.

## 14. Recommended implementation sequence

### Phase 1: normative protocol and local model

- Specify deployment enrollment, lease mandate, payload, journal, acknowledgement, renewal, revocation, and failure contracts.
- Add provider-managed credential metadata separate from `SecretRecord.Kind`.
- Add a constructor-built provider adapter registry.
- Add a fake provider conformance adapter.
- Implement plan, digest, approval, and journal with no network mutation.

### Phase 2: Doppler local issuance

- Add strict Doppler service-token observe/create/validate/revoke adapter.
- Resolve a separate exact control binding.
- Capture one-time token output directly into authority encryption.
- Persist provider slug and sanitized metadata.
- Prove crash reconciliation and idempotency.

### Phase 3: deployment identity and transport

- Implement explicit deployment enrollment.
- Use mTLS with exact enrolled identities; support SPIFFE identities where already deployed.
- Add recipient-key rotation and overlap rules.
- Implement signed monotonic lease generations.
- Implement target-encrypted payload delivery.

### Phase 4: recipient activation

- Import into independent local authority.
- Stage, validate, activate, acknowledge, and revoke through a two-party state machine.
- Initially support only Aegis-owned `VersionCurrent` consumers.
- Require manual intervention for pinned or unmanaged consumers.

### Phase 5: additional strategies

- Add remote broker mode.
- Add OpenBao/Vault lease integration.
- Add delivery adapters independently.
- Add AWS, GCP, GitHub App, and other provider adapters only after the contract proves reusable.

## 15. Acceptance tests

At minimum, tests must prove:

- target credential cannot authorize its own issuance or rotation;
- direct and indirect control cycles deny;
- unknown provider or unsupported capability denies;
- zero or multiple eligible control bindings deny;
- deployment ID and recipient-key mismatch deny;
- stale or replayed generation denies;
- plan mutation invalidates approval;
- recipient cannot expand project, config, access, operation, destination, or lifetime;
- model text cannot authorize enrollment, lease, issuance, activation, renewal, or revocation;
- controller credential never appears in payload, runtime environment, logs, audit, plans, receipts, or model context;
- child plaintext never appears in journal, logs, audit, plans, receipts, or model context;
- issue outcome uncertainty reconciles without duplicate issuance;
- delivery failure leaves old credential active during normal rotation;
- validation failure leaves old credential active;
- activation without signed acknowledgement does not trigger normal revocation;
- emergency revocation requires exact outage acknowledgement;
- pinned or unmanaged consumers prevent automatic completion;
- provider revocation is verified;
- expired/revoked lease fails local broker use;
- static replication is labeled as shared authority;
- recipient cannot redelegate a non-delegable lease;
- audit reconstructs principal, issuer, recipient, stanza, provider, scope, generation, approval, lifecycle, and external identifier without secret material;
- random credential canaries are absent from all retained state;
- real credentials are never used in tests.

## 16. Decision

Aegis should adopt the following direction:

> A credential lease is an authenticated, approved, recipient-bound grant that causes either provider-issued child authority, target-encrypted static replication, or remote brokered use. These strategies are distinct. Provider-issued child credentials are preferred. The provider-management credential remains only at the controller. Every lease is bound to one principal mandate, issuer deployment, recipient deployment, stanza, provider scope, operation set, destination set, lifetime, generation, and non-delegation policy.

Aegis should not invent workload identity, token-exchange semantics, proof-of-possession, or envelope encryption. It should define an Aegis Credential Lease Profile using established protocols while retaining Aegis-owned authorization, projection, approval, provider adaptation, local custody, broker enforcement, and audit.

## Sources

1. Doppler, Service Tokens: https://docs.doppler.com/docs/service-tokens
2. Doppler API, Create Service Token: https://docs.doppler.com/reference/service_tokens-create
3. Doppler API, List Service Tokens: https://docs.doppler.com/reference/service_tokens-list
4. Doppler API, Delete Service Token: https://docs.doppler.com/reference/service_tokens-delete
5. Doppler API, Create Service Account Token: https://docs.doppler.com/reference/service_account_tokens-create
6. Doppler API, Delete Service Account Token: https://docs.doppler.com/reference/service_account_tokens-delete
7. Visa Developer, Visa Intelligent Commerce: https://developer.visa.com/capabilities/visa-intelligent-commerce
8. Agent Payments Protocol documentation: https://ap2-protocol.org/
9. Google Agentic Commerce, AP2 repository: https://github.com/google-agentic-commerce/AP2
10. SPIFFE overview and specifications: https://spiffe.io/docs/latest/spiffe-about/overview/
11. RFC 8693, OAuth 2.0 Token Exchange: https://www.rfc-editor.org/rfc/rfc8693
12. RFC 9396, OAuth 2.0 Rich Authorization Requests: https://www.rfc-editor.org/rfc/rfc9396
13. RFC 9449, OAuth 2.0 Demonstrating Proof of Possession: https://www.rfc-editor.org/rfc/rfc9449
14. RFC 9635, Grant Negotiation and Authorization Protocol: https://www.rfc-editor.org/rfc/rfc9635
15. OpenBao, Lease, Renew, and Revoke: https://openbao.org/docs/concepts/lease/
16. OpenBao, Response Wrapping: https://openbao.org/docs/concepts/response-wrapping/
17. Vault, Lease, Renew, and Revoke: https://developer.hashicorp.com/vault/docs/concepts/lease
18. Vault, Response Wrapping: https://developer.hashicorp.com/vault/docs/concepts/response-wrapping
19. Envoy Secret Discovery Service: https://www.envoyproxy.io/docs/envoy/latest/configuration/security/secret
20. Kubernetes Secrets Store CSI Driver: https://secrets-store-csi-driver.sigs.k8s.io/
21. RFC 9180, Hybrid Public Key Encryption: https://www.rfc-editor.org/rfc/rfc9180
22. Aegis selective deployment projection architecture: `specs/DEPLOYMENT_PROJECTION.md`
23. Aegis local credential broker research: `research/2026-07-17-local-credential-broker-and-hermes-tool-bridge-mvp.md`
24. Aegis personal credential use research: `research/2026-07-19-personal-credential-use-mvp.md`
