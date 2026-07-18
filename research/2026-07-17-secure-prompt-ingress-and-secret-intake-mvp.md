# Secure prompt ingress and intentional secret intake: minimum viable feature set

- Status: Complete
- Date: 2026-07-17
- Prepared for: Aegis
- Decision: Add a small deterministic ingress guard to every prompt/event source, a mandatory exact-request pre-egress guard, and a separate authenticated secret-intake control path. Ordinary prompts never store secrets. Secret values are independently envelope-encrypted in the embedded bbolt credential authority and exposed to sessions by credential reference through the existing trust-stanza, deployment-projection, and broker architecture.
- Storage revision: The original SOPS/age canonical-store decision in this report is superseded by `research/2026-07-17-embedded-bbolt-credential-authority.md`. age remains available for per-deployment projection encryption only.

## Executive summary

Aegis needs two related but different paths:

1. **Untrusted content path:** user prompts, webhooks, schedules, files, retrieval, tool output, plugins, subagents, and external messages are scanned before they reach a model. The final serialized model request is scanned again immediately before egress.
2. **Intentional secret path:** an authenticated principal deliberately creates or rotates a secret through a control-plane operation that never sends the value to a model.

A user should not paste a credential into ordinary chat and rely on the model to interpret “store this here.” In the minimum viable feature, a likely secret in ordinary content is blocked before model execution and the user is directed to an explicit operation such as:

```text
/secret put github/company-us/reader \
  --agent office \
  --stanza principal \
  --scope github/read \
  --deployment company-us-01
```

The command is parsed and authorized by Aegis, not by Hermes or an LLM. Aegis then requests the value through a no-echo local terminal, protected form, or dedicated secret-upload API. Chat transports that cannot provide a confidential input channel must not accept the value.

The repository already establishes the storage architecture:

- bbolt-backed canonical metadata and independently envelope-encrypted secret versions;
- one host-native Aegis authority database per physical computer, exclusively opened by `aegisd`;
- logical credential scopes in charters;
- location-specific credential bindings from scope to secret reference;
- selective, recipient-encrypted deployment projections;
- exactly one trust stanza per session;
- disposable Hermes homes rather than persistent profiles;
- a local credential broker that applies credentials without normally exposing them to the runtime or model.

Therefore, the new feature should not invent a second vault or put secrets in Hermes profiles, prompt history, memory, Aegis audit records, or charter files.

The default meaning of “give the agent this secret” is **permit brokered use**. “Reveal this value to the model” is a distinct disclosure mode and should be deferred from the first release unless a concrete use case requires it.

## Existing Aegis decisions

### Verified repository decisions

The following are already decided in `specs/DEPLOYMENT_PROJECTION.md`:

1. Canonical secret versions are independently envelope-encrypted before bbolt persistence.
2. bbolt provides embedded transactional NoSQL persistence; Aegis provides exact RBAC and encryption.
3. Charters contain logical credential scopes, not plaintext values.
4. Deployment bindings resolve those scopes to physical secret references.
5. A projection contains only the stanzas and credential material authorized for one deployment.
6. Projection secret payloads are encrypted independently to each deployment recipient.
7. Raw credentials should not normally be written into Hermes profiles or model context.
8. A local credential broker validates the mandate, deployment, stanza, and credential scope before applying or injecting a credential.
9. A runtime session receives exactly one stanza, even when a deployment contains several.
10. Every session gets a disposable Hermes home; profiles are not the canonical security object.
11. Credential values are omitted from audit; references and versions are recorded instead.
12. Missing or ambiguous credential bindings fail closed.

The broader Aegis model adds these invariants:

- A prompt cannot authenticate a principal or select a privileged stanza.
- A session cannot union credential scopes from multiple stanzas.
- Changing stanza requires a new mandate and clean session.
- Secrets, memory, transcripts, and tool handles do not cross stanzas unless an explicit disclosure contract permits it.
- Model/runtime output is untrusted and is not copied into Aegis logs.

### Current implementation status

The current Go implementation remains narrower than the deployment architecture:

- `internal/core/model.go` represents credential scopes as strings.
- MVP charter validation permits the selected provider credential scope plus the fixed `github/read` scope only when paired with `github.get_repository.v1`.
- `internal/config/config.go` supports environment-backed credential references and provider authentication mappings.
- `internal/runtime/hermes/hermes.go` constructs a minimal child environment and injects the selected provider environment.
- A deployment-bound encrypted bbolt authority, principal secret intake, exact local bindings, and one Linux session-bound GitHub metadata broker action are implemented. Signed selective deployment projections and a verified model-visible Hermes bridge are not.

The broader projection and bridge sections therefore remain implementation targets; the narrow local authority-and-broker slice is current behavior.

## Product semantics

### Three different user intentions

Aegis must not collapse these requests:

| User intention | Meaning | Default result |
|---|---|---|
| “Remember this fact” | Store non-secret memory | Normal memory policy; secret ingress guard still runs. |
| “Let the agent use this credential” | Authorize a brokered downstream operation | Store secret securely and bind a credential reference; model does not see value. |
| “Show/send this secret to the model” | Disclose plaintext into model context | High-risk disclosure; not part of the initial MVP. |

The safe default is brokered use.

### Recommended interaction

#### Local CLI

```text
$ aegis secret put github/company-us/reader \
    --agent office \
    --stanza principal \
    --scope github/read \
    --deployment company-us-01

Authenticated principal: principal-1
Target agent: office
Authorized stanza: principal
Credential scope: github/read
Deployment: company-us-01
Storage: Aegis embedded encrypted record
Runtime disclosure: no; brokered use only
Secret value: [no echo]
Confirm binding and publish a new projection? [y/N]
```

The value must not appear in command arguments because process listings, shell history, telemetry, and error reports may retain arguments.

Supported value sources should initially be:

- no-echo TTY input;
- an already-open file descriptor or stdin with an explicit warning;
- a dedicated authenticated API request whose body is excluded from normal request logging.

Environment variables, command-line literals, ordinary chat messages, clipboard automation, and temporary plaintext files should not be the recommended intake path.

#### Prompt or chat surface

A control directive can provide convenience without involving the model:

```text
/secret put github/company-us/reader --agent office --stanza principal --scope github/read
```

Aegis consumes the directive before Hermes. It returns a secure intake link or local no-echo prompt. The next ordinary chat message must not be interpreted as the secret unless the transport provides a dedicated confidential field with explicit protocol separation.

If the user writes:

```text
Store this here: <credential-like value>
```

then the ingress guard should:

1. prevent the message from reaching any model;
2. avoid writing the value to logs, audit, memory, or prompt history;
3. report only a typed finding and safe fingerprint;
4. discard the captured value after the request unless the user was already inside an authenticated secret-intake transaction;
5. offer the explicit secure command/workflow.

The MVP should not silently auto-store an accidentally pasted secret. Auto-storage creates ambiguity about target, stanza, scope, deployment, retention, and authority.

## Minimum viable feature set

## P0 — Required vertical slice

### 1. Source-aware ingress guard

Every model-bound input enters through one shared interface carrying:

- source type: user, API, webhook, scheduler, retrieval, file, tool, plugin, subagent, or memory;
- authenticated sender when available;
- agent and active stanza/session when available;
- content type and byte length;
- provenance identifier that does not contain sensitive content.

The first guard performs bounded, local, deterministic checks:

- maximum payload size;
- private-key delimiters;
- known provider/token prefixes and structured credential formats;
- authorization/header and obvious password-assignment fields;
- bounded base64, hex, and percent decoding for plausible candidates;
- contextual keywords plus candidate entropy;
- invalid scanner state or timeout.

This is not a prompt-injection solution. It is an early secret and malformed-input gate.

### 2. Mandatory exact-request egress guard

Immediately before any model request, scan the exact bytes produced after:

- system and user message assembly;
- memory/retrieval insertion;
- tool-result wrapping;
- prompt templates;
- provider transforms;
- compression or encoding;
- attachment extraction.

The egress decision is authoritative. A clean ingress result does not bypass it. Scanner failure blocks external/cloud egress.

For the initial implementation, decisions are:

- `allow`;
- `block_secret`;
- `block_policy`;
- `block_scanner_error`.

Automatic redaction and local-model rerouting can follow later. Blocking is easier to reason about safely.

### 3. Explicit secret-intake transaction

Add a non-model application operation, shared by CLI and API:

```go
type BeginSecretIntakeRequest struct {
    ReferenceID  string
    AgentID      string
    StanzaID     string
    Scope        string
    DeploymentID string
    Purpose      string
}
```

The actual value is supplied in a separate protected step and represented as a sensitive byte buffer, never as an ordinary command DTO, prompt, audit metadata value, or charter field.

The operation must:

1. authenticate the principal outside the model;
2. validate the agent, stanza, scope, and deployment;
3. require that the stanza may reference the requested credential scope;
4. display the complete non-secret binding change;
5. collect the value through a confidential channel;
6. encrypt it immediately into one canonical record;
7. zero/release plaintext buffers as far as Go and the OS permit;
8. write no plaintext temporary file;
9. produce a new secret version;
10. approve and publish any required deployment-binding/projection change atomically;
11. audit only identifiers, digests, version, actor, target, outcome, and reason.

### 4. Canonical encrypted record

Suggested logical metadata:

```go
type SecretRecord struct {
    ReferenceID string
    Version     uint64
    Kind        string
    Status      string
    CreatedAt   time.Time
    CreatedBy   string
    CiphertextDigest Digest
}
```

The value is encrypted under a fresh per-version data-encryption key before persistence in the bbolt authority database. The data-encryption key is wrapped under a versioned host/controller key-encryption key held outside the database. Secret metadata and ciphertext are versioned. Plaintext must not enter Git, Mongo, the charter, the deployment binding, audit records, or plaintext temporary files.

The reference must not reveal a secret value. It may reveal operational metadata, so deployments that consider metadata sensitive should minimize it. Exact record format, AAD, key custody, bbolt options, backup, and recovery requirements are defined in `research/2026-07-17-embedded-bbolt-credential-authority.md`.

### 5. Explicit credential binding

A secret record does not grant access by existing. A binding maps one logical scope at one deployment to one reference/version policy:

```yaml
agent: office
stanza: principal
scope: github/read
deployment: company-us-01
secret_ref: github/company-us/reader
version: latest-approved
mode: brokered
```

The binding is approved desired state. Caller text, profile name, model output, and runtime request cannot create or broaden it.

A binding change creates a new deterministic projection generation. Rotation under the same approved binding creates a new secret version and projection according to rotation policy.

### 6. Stanza-bound broker authorization

At runtime, broker authorization checks the full tuple:

```text
mandate
+ logical agent
+ exact selected stanza
+ deployment
+ credential scope
+ requested downstream action/destination
+ secret reference/version
+ expiry/revocation
```

Matching only `github/read` is insufficient. A same-named scope in another agent, stanza, tenant, or deployment does not confer access.

Preferred use:

```text
model proposes “read repository X”
  -> Aegis validates action under mandate
  -> broker selects github/read binding
  -> broker adds credential to outbound GitHub request
  -> model receives sanitized result, never token
```

### 7. Disposable runtime materialization

No secret is stored in a persistent Hermes profile. Every session uses a fresh Hermes home containing only one stanza’s non-secret runtime configuration and credential references.

If an integration cannot support brokered use, the only MVP compatibility option is a session-private, short-lived injection with all of these properties:

- exact stanza and mandate binding;
- owner-only permissions;
- not included in shared runtime bases;
- not copied to model context;
- removed on termination;
- explicitly labeled as disclosure to the runtime;
- unavailable to any other session.

Compatibility injection is weaker and should not be the default.

### 8. Safe audit and user feedback

Audit fields may include:

- event ID;
- authenticated principal;
- agent, stanza, scope, and deployment IDs;
- secret reference ID and version;
- binding and projection digests;
- action (`created`, `rotated`, `bound`, `unbound`, `used`, `revoked`);
- outcome and reason code;
- safe keyed fingerprint if needed for correlation.

Never audit:

- plaintext;
- partial plaintext;
- authorization headers;
- reversible low-entropy hashes;
- command arguments containing values;
- model prompts containing detected values.

## Multi-agent and multi-stanza behavior

### One logical agent with multiple stanzas

Example:

```yaml
agent: office
stanzas:
  principal:
    credentials: [github/admin]
  teamwide:
    credentials: [github/read]
  public:
    credentials: []
```

The deployment may contain all three stanza definitions, but each session receives exactly one. The principal session cannot lend its credential handle to the teamwide session. A public session cannot request `github/read` merely by naming it.

### Multiple agents

Credential authorization is explicit per agent and stanza:

```text
secret record: github/company-us/reader

binding A:
  agent: office
  stanza: teamwide
  scope: github/read

binding B:
  agent: release-agent
  stanza: principal
  scope: github/read
```

Reusing one physical record across agents is possible but increases shared blast radius. The safer default is separate downstream credentials and separate records per agent/purpose, even when permissions look similar. If one record is shared, each binding still requires independent approval and produces attributable broker-use events.

### Profiles

Hermes profiles do not decide secret access. Profile names are neither identities nor ACLs. Aegis materializes disposable runtime state from:

```text
authenticated identity
  -> exactly one trust stanza
  -> short-lived mandate
  -> deployment-visible credential binding
  -> broker authorization
```

A profile cannot acquire a secret by copying another profile, changing its name, or asking the model.

### Cross-stanza transfer

Secrets do not cross stanzas as memory. If a lower-trust stanza needs an operation requiring a higher-trust credential, use one of these explicit patterns later:

- a brokered action approved by the higher-trust principal without revealing the value;
- a narrowly sanitized result disclosure;
- a new approved credential binding with reduced downstream privileges.

Never copy plaintext from the principal transcript into teamwide memory.

## Deliberate model disclosure

The MVP should support brokered use only. If Aegis later supports “the model must see this value,” it must be a separate operation, not an exception hidden inside the scanner.

A disclosure grant should bind:

- secret reference and exact version;
- authenticated principal;
- logical agent and exact stanza;
- session/mandate;
- model provider, endpoint, and immutable model route;
- purpose;
- maximum uses or one-shot use;
- expiry;
- whether transcript/memory retention is prohibited;
- exact destination request digest where practical.

Changing model, provider, endpoint, stanza, or session invalidates the disclosure. Cloud disclosure should show the processor explicitly. The audit records the grant and reference but not the value.

Even with authorization, deletion cannot be guaranteed after disclosure to a runtime or provider. The user must be told that disclosure may require downstream credential rotation.

## Proposed architecture

```text
ordinary content sources
  -> source envelope
  -> fast deterministic ingress guard
  -> prompt/session assembly
  -> exact serialized pre-egress guard
  -> approved model route

explicit /secret put or API operation
  -> principal authentication
  -> target and binding validation
  -> no-echo/dedicated secret value channel
  -> independently encrypted bbolt record version
  -> approved credential binding
  -> target-specific encrypted deployment projection
  -> local credential broker
  -> authorized downstream action
```

These paths meet only at credential references and broker decisions. The secret value does not travel through the ordinary prompt path.

## Implementation sequence

### Phase 1 — Prompt and event guard

- Define `ContentEnvelope`, source types, scanner interface, finding, and decision reason codes.
- Integrate all current Aegis-to-Hermes prompt paths through one ingress function.
- Add exact serialized request guard at the runtime/provider boundary.
- Start with deterministic high-confidence formats and blocking.
- Ensure no scanner or request logging contains raw content.

### Phase 2 — Secret records and intake

- Add secret reference/version metadata and storage-neutral interfaces.
- Implement `/secret put`, rotate, inspect-metadata, revoke, and delete-reference commands.
- Add no-echo CLI input and logging exclusions.
- Implement the embedded bbolt authority, strict codec, per-version envelope encryption, and versioned key custody.
- Add safe crash/cancellation handling, race tests, corruption tests, and wrong-key/AAD tests.

### Phase 3 — Binding and brokered use

- Add deployment credential bindings and deterministic projection updates.
- Add broker authorization over mandate, agent, stanza, deployment, scope, and destination.
- Implement one downstream integration end to end.
- Remove that provider’s reusable upstream credential from the Hermes process.
- Add revocation and lease cleanup.

## Acceptance criteria

The feature is not complete until tests demonstrate:

1. A user prompt containing every supported structured credential format is blocked before the mock model receives bytes.
2. A webhook, retrieved document, tool result, plugin result, subagent message, and scheduled payload receive the same ingress treatment.
3. A secret introduced after ingress by a prompt template or tool result is blocked by the exact-request egress guard.
4. Scanner error or timeout cannot result in cloud allow.
5. `/secret put` is handled without invoking Hermes or another model.
6. The value is absent from argv, logs, audit, charter, binding, prompt history, and plaintext temporary files.
7. A canonical record is authenticated and encrypted and cannot be read with the wrong KEK, nonce, AAD, record identity, or version.
8. A binding for agent A/stanza X cannot be used by agent A/stanza Y or agent B.
9. Two sessions in the same deployment do not share secret-bearing files or handles.
10. A profile name or prompt instruction cannot select a secret.
11. Missing or ambiguous binding fails closed.
12. The broker applies the credential to the approved destination while the model sees no value.
13. Rotation publishes a new version/projection and old sessions follow explicit revocation policy.
14. Revocation blocks new use and terminates/revokes affected leases and mandates as configured.
15. Audit reconstructs actor -> reference/version -> binding -> projection -> mandate -> brokered action without containing the secret.

## Explicitly deferred

- Automatic storage of secrets pasted into ordinary prose.
- LLM-based intent recognition for secret storage.
- Cloud-based secret classification.
- Generic password classification with a transformer model.
- Active validation of candidate credentials against external services.
- Reversible prompt redaction and placeholder reinsertion.
- Plaintext disclosure to model context.
- General cross-stanza secret delegation.
- Full OpenBao/Vault integration.
- HSM/enclave guarantees.
- Multi-party approval.
- Guaranteed erasure after host/runtime/provider compromise.

## Risks and mitigations

| Risk | Mitigation |
|---|---|
| User pastes a secret before entering secure mode | Block, avoid retention, and direct to explicit intake. Do not auto-store. |
| “Store this here” names an ambiguous target | Require explicit agent, stanza, scope, and deployment; fail closed. |
| Shell history/process list captures value | Never accept value as an argument; use no-echo input or dedicated body. |
| Same reference is bound too broadly | Independent binding approval and full tuple authorization; prefer separate downstream credentials. |
| Runtime reads a compatibility-injected secret | Label as runtime disclosure, narrow session, restrict egress, and prefer brokered execution. |
| bbolt database or backup is copied | Secret values remain independently encrypted, but metadata may leak; keep the KEK/recovery material separate, minimize metadata, and encrypt off-host backups. |
| Deployment host is compromised | Treat projection as maximum local blast radius; use separate deployments/keys and online broker for high-value credentials. |
| Secret appears in detector error | Findings contain type/span/reason only; error formatting never includes candidate bytes. |
| Secret value remains in Go memory | Minimize copies/lifetime, avoid strings where practical, disable dumps for secret worker; do not claim perfect zeroization. |
| User actually wants model disclosure | Require a separate future disclosure grant; never weaken the normal scanner implicitly. |

## Open questions

1. Should `/secret put` require a charter revision when the credential scope already exists, or only a deployment-binding/projection revision?
2. Which supported Linux distributions and systemd versions qualify for `host+tpm2` key custody, and what is the mandatory recovery ceremony?
3. Which age recipient type should be the default for target-specific projection transport?
4. What is the first brokered downstream integration: model provider, GitHub, or another service?
5. How are secure intake links delivered on messaging platforms without teaching users to paste secrets into chat?
6. Should a blocked accidental secret be irreversibly discarded immediately or held briefly in locked memory for an explicit authenticated conversion to intake? Immediate discard is safer for MVP.
7. What downstream credential types can be replaced with short-lived scoped tokens rather than stored reusable values?
8. Which stanza changes require termination of active sessions versus denial only at next broker use?

## Repository files reviewed

- `specs/DEPLOYMENT_PROJECTION.md` — authoritative decision for the embedded credential authority, selective projections, disposable Hermes homes, credential broker, deployment binding, revocation, and audit.
- `research/2026-07-17-embedded-bbolt-credential-authority.md` — normative bbolt, encryption, key-custody, host broker, synchronization, recovery, and Infisical migration specification.
- `docs/product/BIG_IDEA.md` — logical-agent, trust-stanza, mandate, session, isolation, and disclosure model.
- `specs/MVP.md` — current MVP authority and security invariants.
- `AGENTS.md` — trust-stanza invariants and contribution constraints.
- `research/GO_RESEARCH.md` — separation of operational configuration, charters, and secret references.
- `research/SECURITY_CONTROL_PLANE_RESEARCH.md` — non-bypassable enforcement and separation of trusted control inputs from untrusted model/data inputs.
- `research/2026-07-17-frontier-models-local-inference-hermes-secret-protection-research.md` — ingress/egress scanning and credential-broker recommendations.
- `internal/core/model.go` — current credential-scope representation and MVP provider-only constraint.
- `internal/config/config.go` — current environment-backed credential mappings.
- `internal/runtime/hermes/hermes.go` — minimal environment and output-discard behavior.
- `internal/store/store.go` — current audit metadata redaction.
- `specs/IDENTITY_AND_AUTHORIZATION.md` and `specs/RUNTIME_AND_SESSIONS.md` — current session isolation and scope contracts. Historical Go contracts are archived under `docs/archive/go-contracts/`.
- The original version of this report selected SOPS/age canonical files. That storage decision is superseded by the embedded bbolt authority report; the non-model intake and brokered-use decisions remain in force.

## Conclusion

The correct Aegis interaction is not “tell the model to store a secret.” It is “ask Aegis’s authenticated control plane to create a secret reference and authorize its use.”

For the minimum viable feature, ordinary secret-bearing prompts are blocked, explicit `/secret put` begins a confidential non-model transaction, Aegis stores one independently envelope-encrypted record version in its embedded bbolt authority, the deployment binding grants a logical scope to an exact agent/stanza/deployment, and the broker uses the value without revealing it to Hermes or the model.

This design composes directly with multi-agent deployments because access is never attached to a mutable profile. It is attached to the approved authority chain: authenticated identity, logical agent, one trust stanza, deployment projection, mandate, credential scope, and brokered action.
