# Aegis Research Report

**Scope:** Identity, trust, authorization, session control, approvals, audit integrity, and information-flow separation for a control layer placed in front of explicit agent runtimes.

**Research date:** 2026-07-17  
**Method:** Primary and authoritative sources were fetched directly with `curl`. No repository files were modified.

---

## 1. Executive conclusion

Aegis should treat an agent runtime as an untrusted workload that can propose actions, not as an authority that can authorize those actions.

The minimum defensible architecture is:

1. A stable **logical-agent identity** distinct from every runtime process.
2. **One or more explicit trust stanzas per logical agent**, each describing one accepted runtime-to-agent trust route.
3. Cryptographic workload authentication at session establishment.
4. A short-lived, proof-of-possession-bound session locked to exactly one trust stanza.
5. A non-bypassable policy enforcement point in front of every side effect.
6. Default-deny authorization over structured action envelopes.
7. Exact-payload, one-shot approvals for high-risk actions.
8. Tamper-evident audit records containing the identity, policy, session, action, and approval chain.
9. A hard separation between model-controlled content and trusted identity, policy, approval, and audit inputs.

The most important design rule for the proposed **1-N trust stanza** model is:

> A session selects exactly one trust stanza. Matching multiple stanzas must be rejected, and permissions from multiple stanzas must never be unioned.

Without that rule, overlapping runtime selectors can silently amplify authority.

Aegis does **not** need SPIRE, OPA, generalized capability delegation, federation, hardware attestation, a formal information-flow lattice, or a transparency-log network to ship an absolute MVP. It needs clean abstractions that permit those additions later.

---

## 2. Proposed security model

### 2.1 Identity hierarchy

Aegis should distinguish four identities:

| Identity | Lifetime | Security meaning |
|---|---:|---|
| Human/operator | Long-lived | Person or service accountable for creating policy or granting approval |
| Logical agent | Long-lived | Stable policy principal, such as “release agent” or “support agent” |
| Runtime workload | Process/deployment lifetime | Cryptographically authenticated software instance permitted to act as a logical agent |
| Session | Short-lived | Runtime’s current, constrained authority under one selected trust stanza |

These identities must not collapse into a single opaque “agent ID.”

In particular:

- A logical-agent name is not evidence that a runtime is entitled to assume it.
- A valid runtime credential authenticates a workload but does not, by itself, authorize an action.
- A session is not a durable identity.
- An approval is not a session and should not expand the underlying trust stanza.
- Model output is never identity evidence, policy, or approval.

### 2.2 Control planes

Aegis should separate at least these conceptual planes:

1. **Identity plane:** validates workload credentials and maps a runtime to a logical agent.
2. **Policy/control plane:** stores trust stanzas, authorization rules, revocations, and policy versions.
3. **Execution plane:** receives structured action requests and performs allowed effects.
4. **Approval plane:** presents significant transaction data to an approver and creates one-shot approval evidence.
5. **Audit plane:** receives append-only security events; the runtime must not be able to rewrite them.
6. **Model/data plane:** contains prompts, retrieved documents, tool results, and other attacker-influenceable content.

The model/data plane may request operations through the execution plane, but it must not write directly into the identity, policy, approval, or audit planes.

---

## 3. Threat model

### 3.1 Protected assets

- Credentials and signing keys
- Runtime-to-logical-agent mappings
- Trust policies and policy revisions
- Session secrets and proof-of-possession keys
- Tool and API authority
- High-risk action approvals
- Tenant and environment boundaries
- Secrets and sensitive tool results
- Audit records and policy-decision evidence

### 3.2 Principal threats

| Threat | Aegis implication |
|---|---|
| Direct prompt injection | User text cannot change trust, policy, session scope, or approval requirements. |
| Indirect prompt injection | Web pages, files, emails, tool output, and retrieved documents must be treated as untrusted data. |
| Compromised runtime | Limit damage to one stanza, one logical agent, one tenant/environment, short session lifetime, and explicit action grants. |
| Stolen bearer token | Bind sessions to a runtime-held key; do not rely on replayable bearer sessions for valuable authority. |
| Confused deputy | Bind every action to logical agent, runtime identity, tenant, resource, audience, policy version, and session. |
| Cross-agent privilege leakage | Never union trust stanzas; never inherit another agent’s session, credentials, memory, or approvals. |
| Cross-tenant or cross-environment leakage | Include tenant/environment in both runtime selection and every authorization decision. |
| Approval substitution | Approval must cover the exact canonical action payload, not merely an action type or session. |
| Approval replay | One-shot consumption, nonce, expiry, and atomic “consume then execute” semantics. |
| TOCTOU after approval | Recompute the action digest at the final execution gate; any mutation invalidates approval. |
| Policy ambiguity | Reject requests and session creation when identity or stanza matching is ambiguous. |
| Audit rewriting | Use an append-only restricted sink, record chaining, and externally retained signed checkpoints. |
| Malicious or compromised policy administrator | Separate policy administration from runtime execution and record every policy change; stronger protection requires independent witnesses or governance and is later scope. |
| Enforcement bypass | All side effects must cross an Aegis-controlled PEP. A policy engine is useless if runtimes retain direct credentials or alternate paths. |

### 3.3 Explicit trust assumptions

The MVP necessarily assumes:

- The Aegis enforcement point cannot be bypassed.
- The configured workload credential issuer and trust roots are controlled.
- Aegis’s session and audit signing keys are protected.
- The approval UI/channel faithfully displays the action fields it signs.
- Policy administrators are authorized to define trust relationships.
- Downstream services do not also accept unmediated credentials from agent runtimes.

Aegis cannot fully contain a compromised runtime when its granted action set itself permits arbitrary code execution, arbitrary network access, unrestricted file reads, or arbitrary secret retrieval. Those grants are equivalent to broad ambient authority and should be classified accordingly.

---

## 4. The 1-N trust stanza model

### 4.1 Meaning

Each logical agent has one or more trust stanzas. Each stanza is a directional statement:

> “A runtime satisfying this exact identity and context selector may establish a constrained session as this logical agent, with these action/resource limits and these session, approval, flow, and audit requirements.”

Why N stanzas may be necessary:

- Production and staging runtimes
- Interactive and unattended runtime types
- Read-only and mutation-capable workers
- Different workload identity issuers
- Emergency or break-glass execution
- Regional or tenant-specific deployments
- Migration between old and new runtime identity systems

A stanza is not a role to be accumulated. It is one complete trust route.

### 4.2 Stanza contents

A full design should support these fields conceptually:

| Category | Required meaning |
|---|---|
| Stanza identity | Stable ID, version, status, creation metadata |
| Logical principal | Exact logical-agent ID |
| Runtime selector | Credential issuer/trust domain plus exact subject or tightly constrained subject namespace |
| Runtime context | Runtime type, environment, tenant, deployment, optional attestation claims |
| Accepted credential | mTLS/SPIFFE SVID/JWT type and verification profile |
| Allowed audience | Exact Aegis gateway or downstream audience |
| Grant | Explicit action types and resource bounds |
| Session controls | Maximum lifetime, inactivity policy, proof-of-possession requirement |
| Approval controls | Which action classes require approval and what approver class is acceptable |
| Information-flow controls | Allowed input/output labels or basic zone restrictions |
| Audit controls | Required event class and payload-retention treatment |
| Revocation state | Disabled time, reason, replacement stanza if applicable |

The **absolute MVP stanza** can be much smaller:

1. Stanza ID and version
2. Logical-agent ID
3. Exact credential issuer/trust domain
4. Exact runtime subject
5. Tenant/environment
6. Allowed actions and resource bounds
7. Maximum session lifetime
8. Approval-required action classes
9. Enabled/disabled state

### 4.3 Matching rules

Stanza evaluation should be conjunctive:

- Every selector field must match.
- Missing evidence must not be interpreted as a wildcard.
- Display names and caller-supplied role names must not participate in authentication.
- A wildcard runtime subject should be prohibited by default.
- Subject-prefix matching, if eventually supported, must operate on parsed identity components rather than raw string prefixes.
- Issuer and subject must be checked together.
- Trust-domain identity must be preserved in authorization decisions, not discarded after certificate-chain validation.
- Environment and tenant must be mandatory decision inputs where applicable.

At session creation:

1. Validate the runtime credential.
2. Find all enabled stanzas whose complete selectors match.
3. Zero matches: deny.
4. More than one match: deny as policy ambiguity.
5. Exactly one match: lock the session to that stanza ID and policy version.

At action time:

- Reevaluate the selected stanza and revocation state.
- Do not rerun a broad search that might move the session to a different stanza.
- Do not combine grants from other matching or newly added stanzas.
- Require a new session when trust selection or material policy scope changes.

### 4.4 No implicit or transitive trust

The following must not imply authority:

- Running on an internal network
- Sharing a host with an approved runtime
- Using the same model
- Using the same logical-agent display name
- Possessing another runtime’s log output
- Receiving a message from a trusted runtime
- Federation with a workload trust domain
- A previous successful action
- Approval of a different payload
- Authentication to a different resource

NIST explicitly states that authorization to one resource should not automatically grant access to another, and that resource access is per-session and least privilege.

---

## 5. Sessions and session binding

### 5.1 Session claims

An Aegis session should be bound to at least:

- Session ID
- Logical-agent ID
- Authenticated runtime subject
- Runtime credential issuer/trust domain
- Proof-of-possession key identifier or certificate thumbprint
- Selected stanza ID and stanza version
- Policy snapshot/hash or policy generation
- Tenant and environment
- Intended Aegis/downstream audience
- Issued-at and expiry times
- Revocation generation
- Authentication/attestation strength where relevant

The session must not be considered proof that a human is currently present. NIST SP 800-63B-4 specifically warns that an access token may outlive the authenticated human session and must not alone be treated as evidence of subscriber presence.

### 5.2 Proof of possession

For valuable authority, a stolen serialized session token should be insufficient.

Suitable patterns include:

- mTLS certificate-bound sessions
- DPoP-like application-level proofs
- A runtime-generated ephemeral session key authenticated during session establishment

RFC 9449’s DPoP design binds a token to a public key and requires a fresh signed proof for each HTTP request. Its proofs include request method, target URI, unique identifier, issuance time, and, when applicable, an access-token hash and server nonce.

Important limitations:

- DPoP does not itself authenticate a client.
- A DPoP proof does not automatically sign the action body.
- Proof-of-possession session binding and exact-payload approval solve different problems.
- TLS alone does not bind a human approval to the operation later executed.

### 5.3 Session lifecycle

Absolute MVP requirements:

- Short, explicit maximum lifetime
- No unbounded refresh
- Server-side revocation
- Revalidation of stanza enabled state on every action
- Session termination when its stanza is revoked
- New session required when changing logical agent or stanza
- New session required for material privilege expansion
- Proof freshness and replay detection
- No session fallback to an insecure transport
- No automatic session sharing between runtimes

Risk-adaptive continuous authentication can come later. Per-action authorization cannot.

---

## 6. Policy and enforcement

### 6.1 Policy decision point versus enforcement point

NIST SP 800-207 distinguishes the policy decision point from the policy enforcement point. OPA makes the same distinction: software submits structured input to OPA, OPA returns a decision, and the integrating software enforces it.

For Aegis:

- **PDP:** evaluates structured identity, session, action, resource, approval, and context.
- **PEP:** prevents execution unless the PDP permits it.
- **Policy administrator/control plane:** changes stanzas, rules, and revocations.
- **Runtime:** proposes actions but cannot enforce its own decisions.

The PEP is the critical MVP boundary. Deploying OPA without removing direct runtime access to credentials or tools creates the appearance of control without actual enforcement.

### 6.2 Structured action envelope

Every action should be transformed into a deterministic, server-validated envelope containing:

- Operation type and schema version
- Logical agent and runtime/session identifiers
- Tenant and environment
- Destination service and resource
- Exact normalized arguments
- Body/content digest and length where applicable
- Side-effect/risk classification
- Policy and stanza versions
- Approval requirement and approval reference
- Request nonce and freshness data

The model may propose envelope fields, but Aegis must validate or derive security-sensitive fields server-side. In particular, the runtime must not be authoritative for:

- Its own logical-agent identity
- Tenant
- Risk class
- Whether approval is needed
- Credential selection
- Policy version
- Stanza selection
- Audit suppression

### 6.3 Default deny

A safe decision contract should treat all of the following as deny:

- Undefined policy result
- Missing required input
- Unknown action schema version
- Unknown runtime issuer
- Ambiguous stanza match
- Policy engine unavailable
- Expired session
- Failed proof-of-possession
- Unknown resource
- Missing approval
- Approval digest mismatch
- Audit sink unavailable for actions configured to require durable audit

OPA supports declarative structured decisions and decision logging, but Aegis should explicitly define deny behavior rather than relying on language-level “undefined” semantics.

### 6.4 OPA placement

OPA is useful but not necessary for the first working security boundary.

**MVP:** a small, deterministic policy evaluator over versioned trust stanzas and action classes.

**Later:** an OPA adapter or OPA-backed PDP for richer organizational policy, provided that:

- Inputs use a versioned schema.
- Decisions include reason codes and policy revision.
- Undefined/error means deny.
- Bundle rollback is prevented.
- Decision logs are exported and sensitive fields are masked.
- The PEP remains independent and non-bypassable.

OPA decision logs include the query input, queried policy, bundle metadata, and a `decision_id`. OPA also supports masking or removing sensitive input/result fields before exporting logs. These features are useful for Aegis, but OPA decision logs alone are not tamper-evident audit storage.

---

## 7. Exact-payload approvals

### 7.1 Required property

An approval must mean:

> “This identified approver authorizes this exact normalized operation, for this logical agent and runtime session, against this exact target and payload, under this policy version, before this expiry, once.”

It must not mean:

- “Allow shell commands for five minutes”
- “The agent may deploy”
- “Approve the remainder of this session”
- “Approve any request having the same action type”
- “Approve whatever body the runtime sends after confirmation”

### 7.2 Approval envelope

The approval digest should cover all security-significant fields:

- Operation and schema version
- Target service/resource
- Exact arguments
- Content digest and content length
- Tenant/environment
- Logical-agent identity
- Runtime/session identity
- Stanza and policy versions
- Risk classification
- Nonce/challenge
- Creation and expiry times
- Approval purpose
- Optional expected precondition/version of the destination resource

The approver should see meaningful transaction fields, not merely a hash. The hash is for cryptographic binding; human authorization requires comprehensible presentation.

OWASP’s transaction authorization guidance calls this **What You See Is What You Sign** and requires users to identify and acknowledge significant transaction data.

### 7.3 One-shot and final-gate enforcement

The MVP must provide:

- Unique approval credential for every operation
- Short expiry
- One-time atomic consumption
- Final digest comparison immediately before execution
- Invalidation after any payload mutation
- Reset after changed destination or arguments
- Separation between authentication and transaction approval
- Server-generated challenge and server-maintained transaction state
- Audit records for request, presentation, grant/deny, consumption, and execution result

Approval should be additive:

> A valid approval can satisfy an approval requirement inside an existing grant; it cannot authorize an action forbidden by the stanza.

### 7.4 Relevant protocol patterns

- RFC 9421 defines signatures over selected HTTP components and strict canonicalization, including method, target URI, content digest, creation time, expiry, and nonce.
- RFC 9396 defines structured OAuth authorization details and is useful as a model for typed, resource-specific authorization requests.
- Neither specification by itself supplies Aegis’s human approval semantics.
- Aegis needs an application-level canonical action envelope because HTTP signatures only protect the components the application chooses to cover.

---

## 8. Prompt injection and information-flow separation

### 8.1 Security conclusion from OWASP

OWASP states that indirect prompt injection arises when an LLM processes external sources such as websites or files, and that successful injection can cause:

- Sensitive information disclosure
- Unauthorized access to functions
- Arbitrary commands in connected systems
- Manipulation of critical decisions

OWASP also states that foolproof prompt-injection prevention is unclear and recommends:

- Least privilege
- Handling functions in deterministic code
- Human approval for high-risk operations
- Segregating and identifying external content
- Treating the model as an untrusted user during testing

Therefore, Aegis must not depend on a system prompt telling the model to ignore malicious instructions. Prompt hardening can reduce accidents but is not an authorization boundary.

### 8.2 Absolute MVP separation

The MVP does not need a formal label lattice, but it does need a hard binary distinction:

1. **Trusted control inputs:** authenticated identity, selected stanza, policy, server-derived context, approval evidence, revocation state.
2. **Untrusted model/data inputs:** all prompts, model output, retrieved content, web pages, files, emails, and tool output unless independently validated.

Rules:

- Untrusted content cannot directly populate trusted-control fields.
- Text saying “approved,” “administrator,” or “use production credentials” carries no authority.
- Policy and approval checks occur outside the model.
- Credentials should be selected and applied by Aegis after authorization, not exposed for model-directed selection.
- Retrieved content must not alter the active trust stanza or session.
- Audit events must be generated by Aegis, not accepted as authoritative runtime narratives.
- Tool output from one agent must not automatically become trusted control input for another.
- Secrets should not be inserted into the model context unless the action explicitly requires disclosure and policy allows it.

### 8.3 Later information-flow controls

Later versions can add:

- Source labels such as control, user, external-untrusted, tool-result, secret, and tenant-confidential
- Egress policies based on labels
- Taint propagation through action proposals
- Per-agent memory partitions
- Cross-agent declassification gates
- One-way guards for sensitive environments
- Content-based policy checks
- Dedicated secret and signing services
- Separate runtime sandboxes and network egress controls

NIST SP 800-53 AC-4 emphasizes that information-flow control regulates **where information may travel**, which differs from deciding who may access it. SC-4 separately requires preventing unintended transfer through shared resources. Both principles apply directly to shared caches, conversation stores, runtime workspaces, tool outputs, and agent memory.

---

## 9. Capability-security implications

Capability systems provide an attractive model: authority should be explicit, narrow, and transferable only through controlled delegation rather than ambient identity.

For Aegis, the useful principles are:

- Possession of a grant should convey only its explicit authority.
- Authority should be attenuable, not expandable.
- Grants should be resource- and action-specific.
- Delegation should be explicit and auditable.
- Ambient credentials should be avoided.
- Capabilities should be revocable or short-lived where practical.
- Bearer capability leakage must be assumed possible.

However, generalized delegation is not MVP material. It introduces difficult questions around:

- Delegation-chain validation
- Attenuation correctness
- Revocation
- Cycles and depth limits
- Cross-tenant transfer
- Confused deputies
- Audit attribution
- Combining multiple capabilities

For the MVP, an Aegis session can be **capability-like but non-delegable**: proof-bound, short-lived, resource-scoped, action-scoped, and locked to one stanza.

The W3C Capability URLs document is only a 2014 Working Draft and warns that anyone possessing a capability URL receives access and that URLs are difficult to keep secret. The ZCAP document is a Community Group draft rather than a W3C Recommendation. These are useful design references, not normative dependencies.

---

## 10. Workload identity: SPIFFE and SPIRE

### 10.1 Applicable SPIFFE concepts

SPIFFE defines:

- A SPIFFE ID as a URI containing a trust domain and workload path.
- A trust domain as an identity namespace backed by an issuing authority and cryptographic keys.
- SVIDs as cryptographically verifiable identity documents.
- A Workload API for delivering identities and trust bundles to local workloads.
- Federation as exchange of trust bundles so identities from another trust domain can be authenticated.

Useful Aegis mapping:

| SPIFFE | Aegis |
|---|---|
| SPIFFE trust domain | Runtime credential issuer/security domain |
| SPIFFE ID path | Runtime workload subject |
| SVID | Runtime authentication credential |
| Workload API | Runtime credential acquisition/rotation mechanism |
| Federated bundle | Accepted external workload issuer |
| Aegis trust stanza | Authorization mapping from authenticated SPIFFE identity to logical agent and grant |

### 10.2 Important limits

SPIFFE authenticates workloads; it does not determine what they may do in Aegis.

Aegis must:

- Check the complete trust domain and workload path.
- Avoid validating only that a certificate chains to some accepted root.
- Keep staging and production trust domains or roots isolated.
- Treat federation as authentication enablement, not authorization.
- Require a matching Aegis stanza even for a valid federated SVID.
- Preserve issuer/trust-domain identity in audit records.

SPIFFE warns that reusing root keys across trust domains degrades isolation and can cause catastrophic authentication/authorization confusion if trust-domain names are not checked.

### 10.3 MVP decision

Do not make operating SPIRE mandatory for the Aegis MVP.

Instead:

- Define a generic verified-runtime identity abstraction.
- Initially support a tightly controlled mTLS or signed-token issuer.
- Accept SPIFFE IDs directly when an existing deployment already uses SPIFFE/SPIRE.
- Add first-class SPIRE registration, workload attestation, rotation, and federation later.

Building a workload CA and attestation platform inside Aegis would substantially expand scope and duplicate SPIRE.

---

## 11. Audit integrity

### 11.1 Required event content

Every security-relevant decision should record:

- Event ID and event type
- Wall-clock and monotonic/order information
- Logical-agent identity
- Runtime issuer and subject
- Session ID and proof-key identifier
- Stanza ID/version
- Policy version/hash
- Action type, target, and canonical action digest
- Decision and machine-readable reason codes
- Approval ID, approver identity, approval digest, and consumption state
- Execution result or failure class
- Previous-record hash or batch-root reference
- Aegis signer/key identifier

Sensitive payloads should generally be stored separately under tighter controls. The main audit stream can retain a digest, metadata, and a controlled reference.

### 11.2 Minimum integrity design

Absolute MVP:

1. Aegis, not the runtime, emits authoritative decision records.
2. Logs go to an append-only sink outside the runtime’s write control.
3. Records or batches are hash-linked.
4. Aegis periodically signs a checkpoint containing the latest chain value or Merkle root.
5. Checkpoints are copied to a separate administrative or storage boundary.
6. Deletion, export failure, and checkpoint failure are themselves security events.
7. Policy and trust-stanza changes use the same integrity mechanism.

A local hash chain alone is insufficient against an administrator who can rewrite the entire chain and signing state. Retaining signed checkpoints outside the primary log boundary makes later rewriting detectable.

### 11.3 Standards implications

- NIST SP 800-53 AU-9 requires protecting audit information and logging tools from unauthorized access, modification, and deletion.
- AU-10 identifies digital signatures and receipts as mechanisms for evidence that an individual or process performed an action.
- RFC 9162’s Certificate Transparency design demonstrates append-only Merkle trees, inclusion proofs, and consistency proofs.
- RFC 9162 also warns that a malicious log can present inconsistent views to different clients; independent witnessing or gossip is needed to address split views.

RFC 9162 is not a generic agent-audit protocol. Its Merkle and signed-checkpoint model is an architectural pattern Aegis can adopt later.

OPA decision logs are valuable evidence but should feed the Aegis audit pipeline rather than be treated as the complete integrity solution.

---

## 12. Sharply prioritized scope

## P0 — Absolute MVP

These items are required before Aegis can credibly claim to be an identity, trust, and session control layer.

### 1. Stable logical-agent registry

- Immutable internal logical-agent IDs
- Human-readable names treated only as metadata
- Enabled/disabled state
- Ownership/administrative metadata
- No dynamic agent creation by model output

### 2. Versioned 1-N trust stanzas

- Exact issuer and runtime-subject matching
- Tenant/environment binding
- Explicit action/resource grants
- Session lifetime
- Approval-required action classes
- Default deny
- Policy changes audited
- Zero matches deny; multiple matches deny
- One selected stanza per session; no permission union

### 3. Verified runtime authentication

- Validate issuer, subject, audience, signature, time validity, and revocation state
- Support one credential profile well
- Preserve complete authenticated identity in policy input and audit
- Do not build generalized federation or attestation yet

### 4. Proof-bound, short-lived sessions

- Session bound to logical agent, runtime identity, stanza, audience, tenant, and policy version
- Runtime proves possession of a session key or mTLS key
- Replay detection
- Server-side termination and revocation
- No session transfer or delegation

### 5. Non-bypassable execution gateway

- Every protected tool/API side effect crosses Aegis
- Runtime has no equivalent direct credential
- Per-action authorization, not login-time authorization only
- Fail closed on policy, identity, session, approval, or audit failure

### 6. Deterministic default-deny policy

- Structured action envelope
- Explicit action and resource allowlists
- No security decision made by the LLM
- Machine-readable decision reasons
- Versioned decision-input schema
- Unknown or missing input denies

### 7. Exact-payload approval

- Required only for sharply defined high-risk classes
- Human-readable display of significant fields
- Digest of the full canonical action envelope
- Short expiry
- One-shot, atomic consumption
- Final execution-gate verification
- Mutation invalidates approval
- Approval cannot exceed stanza authority

### 8. Basic information-flow boundary

- Trusted control fields separated from model/data fields
- All retrieved and model-produced content considered untrusted
- Model cannot select stanza, credentials, policy, tenant, approval status, or audit outcome
- Separate per-agent and per-tenant session state
- No secrets in model context by default

### 9. Tamper-evident audit trail

- Authoritative events emitted by Aegis
- External append-only sink
- Hash-linked records or batches
- Periodically signed checkpoints retained separately
- Identity → session → stanza/policy → action → approval → execution linkage
- Sensitive-field minimization and redaction policy

### P0 release gates

Aegis should not ship until tests demonstrate:

- Unknown runtimes cannot establish sessions.
- A runtime matching two stanzas is rejected.
- Permissions from two stanzas are never combined.
- Authentication to one logical agent or resource grants no authority to another.
- A stolen serialized session cannot be used without the proof key.
- Direct tool/API access from the runtime is unavailable.
- Prompt text cannot alter identity, stanza, policy, or approval state.
- An exact-payload approval fails after any protected field changes.
- A consumed approval cannot be replayed.
- Stanza revocation blocks subsequent action decisions.
- Cross-agent and cross-tenant state does not leak.
- Deleting or rewriting committed audit records is detectable.

---

## P1 — First hardening release

1. **OPA integration**
   - Versioned decision schema
   - Signed/versioned policy bundles
   - Rollback protection
   - Decision log export and masking

2. **First-class SPIFFE/SPIRE support**
   - SPIFFE ID selectors
   - Workload API integration
   - Credential rotation
   - Trust-bundle update handling
   - Environment-specific trust domains

3. **Stronger approval authentication**
   - Phishing-resistant approver authentication
   - Independent approval device/channel
   - Richer What-You-See-Is-What-You-Sign presentation

4. **Credential broker**
   - Downstream credentials applied only after authorization
   - Runtime never receives reusable ambient credentials
   - Destination- and action-specific credentials where available

5. **Richer information-flow labels**
   - External-untrusted, tool-result, secret, tenant-confidential, control
   - Egress checks
   - Memory and retrieval-store partitions

6. **Operational revocation**
   - Rapid session invalidation
   - Key rotation
   - Runtime quarantine
   - Policy rollback with audited emergency procedures

7. **Audit verification tooling**
   - Automated chain/checkpoint verification
   - Detection alerts
   - Independent checkpoint storage

---

## P2 — Later architecture

- SPIFFE federation across organizations
- Hardware-backed runtime attestation
- Continuous runtime posture evaluation
- Dynamic risk-based policy
- Generalized capability delegation and attenuation
- Multi-agent delegation chains
- Multi-party or threshold approvals
- Formal information-flow-control lattice
- Cross-domain declassification workflows
- Merkle inclusion/consistency APIs
- Independent transparency witnesses or gossip
- Privacy-preserving audit disclosure
- Offline/disconnected approval protocols
- Policy simulation, provenance analysis, and change impact analysis
- Automated privilege minimization from observed use
- Broad ecosystem-specific runtime adapters

---

## Explicitly not in the absolute MVP

- Running a full SPIRE deployment
- Making OPA an operational dependency
- Cross-organization trust federation
- General capability-token delegation
- Agent-to-agent transitive trust
- Dynamic policy learned from model behavior
- Arbitrary wildcard runtime selectors
- Session-wide “approve all” modes
- A formal secrecy-label lattice
- Hardware enclave requirements
- Public transparency logs
- Complex behavioral biometrics
- Storing full prompts and secrets in audit logs

---

## 13. Source findings and direct URLs

### NIST zero trust

**NIST SP 800-207, Zero Trust Architecture**  
https://csrc.nist.gov/pubs/sp/800/207/final  
https://doi.org/10.6028/NIST.SP.800-207

Relevant findings:

- Trust is never implicitly granted and must be continually evaluated.
- Access decisions should be accurate, least privilege, and per request.
- Access to individual resources is per session.
- Authentication/authorization to one resource does not automatically grant access to another.
- Access is determined by dynamic policy using subject, service, asset, and environmental context.
- Authentication and authorization must be strictly enforced before access.
- The PDP and PEP are separate logical components.

**Threat-model implication:** Aegis must authorize every protected action at an enforcement point and must not treat a successful runtime login as broad durable trust.

---

### NIST digital identity and session binding

**NIST SP 800-63-4, Digital Identity Guidelines**  
https://pages.nist.gov/800-63-4/sp800-63.html  
https://doi.org/10.6028/NIST.SP.800-63-4

**NIST SP 800-63B-4, Authentication and Authenticator Management**  
https://pages.nist.gov/800-63-4/sp800-63b.html  
https://pages.nist.gov/800-63-4/sp800-63b/session/

Relevant findings:

- A session secret binds the session subject and host.
- Continuity is based on direct presentation of the secret or cryptographic proof of possession.
- Proof-of-possession mechanisms reduce session-secret theft risk.
- Session assurance cannot exceed the authentication event that created it.
- Session secrets require protected transport, expiry, logout invalidation, and protection from intermediaries.
- Access tokens alone do not demonstrate the subscriber’s current presence.
- Reauthentication and session monitoring can be used in response to risk.

**Threat-model implication:** Adapt the session-binding principles to workloads: bind each Aegis session to the authenticated runtime’s key, constrain lifetime, and do not confuse an access token with current human approval.

---

### OWASP prompt injection

**OWASP GenAI Security Project, LLM01 Prompt Injection**  
https://genai.owasp.org/llmrisk/llm01-prompt-injection/

Relevant findings:

- Indirect injection is delivered through external files, websites, and other content.
- Impact depends heavily on the agency and privileges given to the model.
- Consequences include unauthorized function access, arbitrary commands, data disclosure, and critical-decision manipulation.
- Foolproof prevention is not known.
- OWASP recommends deterministic output validation, least privilege, human approval for high-risk actions, segregation of untrusted content, and treating the model as an untrusted user.

**Threat-model implication:** Prompt controls cannot be Aegis’s authorization boundary. Model-controlled content must be unable to alter identity, policy, session, approval, or audit state.

---

### SPIFFE and SPIRE workload identity

**SPIFFE overview**  
https://spiffe.io/docs/latest/spiffe-about/overview/

**SPIFFE ID and SVID specification**  
https://spiffe.io/docs/latest/spiffe-specs/spiffe-id/

**Trust Domain and Bundle specification**  
https://spiffe.io/docs/latest/spiffe-specs/spiffe_trust_domain_and_bundle/

**SPIFFE Workload API**  
https://spiffe.io/docs/latest/spiffe-specs/spiffe_workload_api/

**SPIFFE Federation**  
https://spiffe.io/docs/latest/spiffe-specs/spiffe_federation/

**SPIRE concepts**  
https://spiffe.io/docs/latest/spire-about/spire-concepts/

Relevant findings:

- A SPIFFE ID is a URI containing an issuing trust domain and workload path.
- Trust domains delineate administrative/security boundaries.
- SVIDs provide cryptographically verifiable workload identity.
- Workload identity is bootstrapped through local caller identification and workload attestation.
- Federation exchanges trust bundles to permit cross-domain authentication.
- Federation authenticates foreign identities; it does not define Aegis authorization.
- Root reuse across trust domains degrades isolation.
- A validator must select the bundle corresponding to the presented trust domain.

**Threat-model implication:** Use SPIFFE as an identity source, not as the Aegis policy model. Always authorize the complete trust-domain-plus-workload identity through an explicit stanza.

---

### Open Policy Agent

**OPA documentation**  
https://www.openpolicyagent.org/docs/latest/

**Rego policy language**  
https://www.openpolicyagent.org/docs/latest/policy-language/

**OPA decision logs**  
https://www.openpolicyagent.org/docs/latest/management-decision-logs/

Relevant findings:

- OPA decouples policy decision-making from enforcement.
- Callers submit structured data and receive structured decisions.
- Decision logs can include input, queried policy, bundle metadata, trace identifiers, and decision identifiers.
- Sensitive decision-log fields can be erased or modified through masking policy.

**Threat-model implication:** OPA is a suitable later PDP, but Aegis must still own the non-bypassable PEP, fail-closed semantics, policy-version binding, audit integrity, and sensitive-field handling.

---

### Session proof of possession

**RFC 9449, OAuth 2.0 Demonstrating Proof of Possession (DPoP)**  
https://www.rfc-editor.org/rfc/rfc9449.html  
https://www.rfc-editor.org/rfc/rfc9449.txt

**RFC 8705, OAuth 2.0 Mutual-TLS Client Authentication and Certificate-Bound Access Tokens**  
https://www.rfc-editor.org/rfc/rfc8705.html  
https://www.rfc-editor.org/rfc/rfc8705.txt

Relevant findings:

- DPoP binds tokens to a public key and requires request-specific proofs.
- It protects against use of stolen tokens by parties lacking the private key.
- Each request requires a unique proof.
- Server nonces can reduce precomputed-proof attacks.
- mTLS can bind tokens to a client certificate.

**Threat-model implication:** Aegis sessions should be sender-constrained. This does not replace exact-body approval because a session proof may bind only the method and URI, not application semantics.

---

### Exact-payload and transaction approval

**OWASP Transaction Authorization Cheat Sheet**  
https://cheatsheetseries.owasp.org/cheatsheets/Transaction_Authorization_Cheat_Sheet.html

Relevant findings:

- Use What You See Is What You Sign.
- The approver must identify and acknowledge significant transaction data.
- Authentication and transaction authorization should be distinguishable.
- Each transaction should use unique authorization credentials.
- Authorization must be enforced server-side.
- Significant transaction data must be server-maintained and resistant to client tampering.
- Changed transaction data should invalidate previous approval.
- A final gate must check authorization immediately before execution.
- Credentials should be time-limited and unique per operation.

**Threat-model implication:** An approval must cover the exact canonical action and be consumed at the final execution gate. Session-wide approvals are insufficient.

**RFC 9421, HTTP Message Signatures**  
https://www.rfc-editor.org/rfc/rfc9421.html  
https://www.rfc-editor.org/rfc/rfc9421.txt

Relevant findings:

- Defines canonical signatures over selected HTTP message components.
- Can cover method, target URI, content digest, authorization data, creation time, expiry, and nonce.
- Only selected components are protected.

**Threat-model implication:** Aegis must define which semantic action fields are covered; transport-level signing cannot infer application significance.

**RFC 9396, OAuth 2.0 Rich Authorization Requests**  
https://www.rfc-editor.org/rfc/rfc9396.html  
https://www.rfc-editor.org/rfc/rfc9396.txt

**Threat-model implication:** Typed authorization details are a useful model for structured grants, but RFC 9396 does not itself provide exact-payload human approval or one-shot execution binding.

---

### Capability security

**W3C TAG, Capability URLs** — 2014 Working Draft  
https://www.w3.org/TR/capability-urls/

**Authorization Capabilities for Linked Data** — W3C Community Group draft  
https://w3c-ccg.github.io/zcap-spec/

Relevant findings:

- Possessing a capability URL grants access.
- Capability URLs are bearer capabilities and are difficult to keep secret.
- ZCAP describes signed capability invocation, delegation, attenuation through caveats, and revocation-oriented restrictions.

**Threat-model implication:** Prefer narrow, attenuated authority over ambient credentials, but keep MVP sessions non-delegable. These drafts are design references, not mature normative dependencies.

---

### Audit integrity

**NIST SP 800-53 Rev. 5, Security and Privacy Controls**  
https://csrc.nist.gov/pubs/sp/800/53/r5/upd1/final  
https://doi.org/10.6028/NIST.SP.800-53r5

Relevant controls:

- **AU-9:** protect audit information and tools from unauthorized access, modification, and deletion.
- **AU-10:** provide evidence that an individual or process performed an action.
- **AC-6:** least privilege for users and processes.
- **AC-4:** enforce approved information-flow authorizations.
- **SC-4:** prevent unauthorized and unintended transfer through shared resources.
- **SC-7:** monitor and control external and key internal managed interfaces.

Current NIST OSCAL control catalog:  
https://github.com/usnistgov/oscal-content/tree/main/nist.gov/SP800-53/rev5

**RFC 9162, Certificate Transparency Version 2.0**  
https://www.rfc-editor.org/rfc/rfc9162.html  
https://www.rfc-editor.org/rfc/rfc9162.txt

Relevant findings:

- Append-only behavior can be implemented using Merkle trees.
- Inclusion proofs demonstrate that a record is present.
- Consistency proofs demonstrate append-only evolution.
- Split views remain possible without independent observation.

**Threat-model implication:** Aegis needs external append-only storage and signed checkpoints in the MVP; Merkle proofs and independent witnesses are later hardening.

**NIST SP 800-92, Guide to Computer Security Log Management**  
https://csrc.nist.gov/pubs/sp/800/92/final  
https://doi.org/10.6028/NIST.SP.800-92

This remains the final SP 800-92 publication, though it is old and a Rev. 1 draft exists. It is useful for log-management operations but is weaker than SP 800-53 AU-9 and cryptographic transparency patterns for Aegis’s integrity requirements.

---

### Information-flow separation

**NIST SP 800-53 Rev. 5, AC-4 and SC-4**  
https://csrc.nist.gov/pubs/sp/800/53/r5/upd1/final

Relevant findings:

- Information-flow control determines where information can travel, independently of later access decisions.
- Cross-domain transfers can violate the policy of either domain.
- Policy enforcement points and trustworthy guards are appropriate at boundaries.
- Shared resources must not expose information from prior users, roles, or processes to later ones.

**Threat-model implication:** Per-agent authorization alone is insufficient. Aegis must isolate agent sessions, tenant state, memories, caches, workspaces, and tool outputs, and must prevent untrusted content from crossing into the control plane.

---

## 14. Final recommendation

Build the first Aegis release around one narrow security invariant:

> No explicit agent runtime can perform a protected side effect unless it presents a verified workload identity, holds a proof-bound session locked to one unambiguous trust stanza, passes a current default-deny decision for the exact action, supplies an exact one-shot approval when required, and causes an externally committed audit event.

Everything else should be judged against that invariant.

The highest-risk mistakes would be:

1. Treating the model or runtime as a policy authority
2. Unioning privileges from multiple matching trust stanzas
3. Accepting a valid workload credential without resource authorization
4. Using replayable bearer sessions
5. Granting session-wide approvals
6. Allowing post-approval payload mutation
7. Deploying a PDP without a non-bypassable PEP
8. Mixing untrusted content with trusted control inputs
9. Letting runtimes retain direct downstream credentials
10. Calling ordinary mutable logs “tamper-evident”

The sharply constrained P0 above is the absolute MVP. SPIRE, OPA, federation, generalized capabilities, formal information-flow control, and full transparency infrastructure should remain later additions rather than prerequisites for establishing the core security boundary.
