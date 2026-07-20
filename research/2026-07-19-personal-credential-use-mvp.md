# Personal credential custody and safe model use: finalized Aegis MVP plan

- Status: Finalized product decision and retained architecture; the first GitHub credential-use vertical slice is implemented
- Date: 2026-07-19
- Prepared for: Aegis
- Product owner decision: The immediate MVP centers on a principal supplying personal credentials to Aegis, managing them through the Aegis TUI, and permitting models to perform narrowly authorized operations without receiving reusable credential plaintext.
- Normative relationship: This report records the finalized product direction as supporting research. `specs/MVP.md` now defines the personal credential-use objective and the typed GitHub operation as the release-defining slice; normative specifications and implemented behavior take precedence over this report if they differ.
- Scope boundary: Agent-owned credentials, autonomous agent credential administration, fleet distribution, and general secret retrieval are deferred.
- Implementation evidence: encrypted custody and manager administration predated this report; this change adds the hidden Aegis stdio MCP bridge, exact grant/toolset launch gating, live Hermes one-tool registration verification, and model-to-broker request/result mediation for `github.get_repository.v1`. Hermetic unit/race tests and a real Hermes 0.18.2 gateway verification cover the bridge registration; fake downstream tests remain the reproducible no-secret broker proof.

## 1. Executive decision

Aegis's immediate product promise is:

> One trustworthy place where an authenticated person stores personal credentials and controls exactly how models may use them, while reusable credential values remain outside model context and the general-purpose runtime.

The release-defining proof is:

> The principal enters one real credential through protected intake, reviews one exact grant, starts one clean Hermes session with one Aegis-owned typed operation, receives one real sanitized downstream result, and then proves that unauthorized use, replay, expiry, rotation drift, and revocation fail closed without exposing the credential.

This is the MVP. Charters, trust stanzas, mandates, clean runtime homes, audit, and deterministic provisioning remain necessary enabling controls. They are not the user-visible outcome by themselves.

The MVP has two deliberately separate experiences:

1. **Control experience:** the authenticated principal manages credential custody, metadata, grants, rotation, revocation, audit, and recovery through the Aegis TUI and deterministic Aegis services.
2. **Use experience:** an operational model requests an approved business operation through one Aegis-owned tool; Aegis independently authorizes and performs it using the stored credential.

The model may propose. Aegis authenticates, authorizes, selects the credential, applies it, sanitizes the result, and audits the decision. Prompt text never authenticates the principal, selects a credential, expands a grant, or approves a consequential action.

## 2. Why this is the right MVP

The current broad MVP proves a secure session-construction workflow. That foundation is valuable, but it does not yet answer the personal product question:

> Can I remove a credential from ad hoc files and environment setup, put it in Aegis, understand who can use it, and let a model accomplish something useful without handing over the token?

The personal credential-use slice is stronger because it is:

- immediately useful to the product owner;
- demonstrable with a real downstream service;
- narrow enough to threat-model completely;
- built on code that already exists;
- an honest test of identity, stanza, mandate, runtime, storage, broker, TUI, and audit together;
- extensible service by service without introducing generic secret retrieval;
- measurable through positive and negative executable tests.

Aegis is already close to the control experience. It has protected intake, encrypted authority storage, metadata operations, confirmation-bound manager proposals, rotation, revocation, bindings, and audit. The principal missing product edge is verified model-visible broker use.

## 3. Product semantics

### 3.1 Three intents that must remain separate

| Principal intent | Meaning | MVP behavior |
|---|---|---|
| Store this credential | Put reusable plaintext under Aegis custody | Protected intake; encrypted immutable version; plaintext excluded from model and ordinary output |
| Let a model use this credential | Permit an exact downstream operation | Aegis-owned typed operation; Aegis selects and applies credential; model receives sanitized result |
| Show the credential | Reveal reusable plaintext | Not supported in the MVP |

The safe default is brokered use. Storage never implies use, and use never implies reveal.

### 3.2 “The model uses the credential”

This phrase is convenient but technically imprecise. The intended boundary is:

```text
model requests business operation
    -> Aegis authorizes request
    -> Aegis performs authenticated downstream call
    -> model receives bounded result
```

It is not:

```text
model retrieves secret
    -> model/runtime constructs arbitrary authenticated request
```

### 3.3 Credential classes in this MVP

The MVP supports principal-supplied reusable credentials such as:

- a fine-grained personal access token;
- an API key;
- an OAuth refresh credential where a reviewed adapter can safely exchange it;
- a GitHub App private key or other minting credential in a later typed adapter.

The first vertical slice may use a low-risk, repository-scoped GitHub credential supplied by the principal. A GitHub App installation flow is the preferred follow-on because installation tokens are short-lived and can be restricted to repositories and permissions.

### 3.4 Disclosure modes

The credential record and grant model must distinguish:

- `brokered`: Aegis applies the credential and does not disclose it to Hermes or the model;
- `process`: compatibility-only injection into a selected process, visibly weaker and not accepted for the credential-under-test in the MVP demonstration;
- `reveal`: unsupported in the MVP.

No mode may be inferred from credential kind or prompt wording.

## 4. Final MVP user journey

### 4.1 First run and unlock

1. The operator starts bare `aegis` in a real terminal.
2. Aegis authenticates the configured principal outside the model.
3. Aegis initializes or verifies the local encrypted authority after exact plan review.
4. The principal unlocks passphrase custody through pinentry-first protected input, with the existing pre-interaction terminal fallback rules.
5. The TUI displays authoritative readiness without exposing key paths, values, or passphrases.

### 4.2 Add a credential

1. The principal tells the TUI to add a GitHub credential.
2. The manager model may propose a typed create operation containing metadata only.
3. Aegis renders an authoritative preview naming the reference, service, account/purpose metadata, and disclosure mode.
4. The principal confirms the exact mutation.
5. Aegis temporarily owns terminal input and collects the value through protected no-echo intake outside Hermes and Ollama.
6. Aegis creates an independently encrypted immutable version.
7. The TUI displays only record ID, reference, service, metadata, status, version, and timestamps.
8. Audit records the mutation without value, passphrase, ciphertext, credential reference text when policy excludes it, or protected input.

### 4.3 Review and grant use

1. The principal selects the stored credential and chooses the first reviewed action: `github.get_repository.v1`.
2. The principal selects one exact repository from configured or newly reviewed destination policy.
3. Aegis compiles the friendly choice into the full internal binding tuple:
   - logical agent;
   - exact trust stanza;
   - deployment;
   - `github/read` scope;
   - `brokered` mode;
   - `github-api` destination;
   - current-version policy;
   - exact repository constraint.
4. The TUI renders both the friendly summary and authoritative technical scope.
5. The principal confirms the exact grant.
6. Aegis persists and audits the binding.

### 4.4 Model performs the operation

1. Aegis starts a clean Hermes session under one mandate and one stanza.
2. The disposable Hermes home contains exactly one Aegis-generated bridge configuration and no inherited profile, project plugin, MCP server, skill, memory, or credential value.
3. Hermes receives exactly one model-visible operation schema containing only `owner` and `repository`.
4. The model requests `github.get_repository.v1`.
5. The bridge sends a bounded request over the local Unix socket with session capability, fresh request ID, and deadline.
6. The broker reauthorizes the complete live session, mandate, charter, stanza, deployment, action, scope, destination, binding, record, and version.
7. Aegis decrypts the credential only inside the bounded use callback.
8. Aegis constructs the fixed GitHub request, disables redirects and proxy-environment behavior, applies authentication internally, and bounds the response.
9. Aegis returns only the existing sanitized repository metadata allowlist.
10. The model treats returned data as untrusted content and answers the principal.
11. Aegis records metadata-only broker audit.

### 4.5 Rotate and revoke

1. The principal supplies a replacement value through protected intake.
2. Aegis stores a new immutable version and current-version bindings begin using it.
3. A second model request succeeds without reconfiguring Hermes or exposing the new value.
4. The principal revokes the credential or applicable version.
5. A subsequent broker request fails immediately under current Aegis authority.
6. The TUI explains that local revocation prevents future Aegis use but does not revoke a token at GitHub unless a provider-side typed revocation action was separately executed.

### 4.6 Recover

1. The principal creates a consistent ciphertext backup.
2. The MVP recovery documentation identifies the separately protected custody material required to decrypt it.
3. A hermetic recovery test restores into a fresh disposable Aegis root.
4. Aegis verifies schema, deployment/store linkage, ciphertext integrity, key sentinel, metadata, and use behavior.
5. No release claim is made until this recovery test is automated and exercised.

## 5. Internet research findings and adopted patterns

Research was performed against primary vendor documentation and standards on 2026-07-19. Sources are listed in section 23.

### 5.1 Separate references from values

1Password documents secret references as identifiers for stored fields and resolves them only when launching or serving an integration. This validates the usability of stable references while also showing the limitation of common tooling: `op run` makes resolved values available as environment variables to a subprocess.

Aegis adopts:

- stable, non-secret record references for principal organization;
- independent opaque record IDs as authority keys;
- references in metadata and policy, never values;
- explicit provenance for every resolved value.

Aegis does not adopt environment injection as its preferred model boundary. Environment injection remains a compatibility mode because the entire child process can read the credential.

### 5.2 Distinguish brokering from injection

HashiCorp Boundary explicitly separates credential brokering, where credentials are returned for use, from credential injection, where a worker authenticates to the target on the user's behalf. The important lesson for Aegis is that “brokered” is not automatically secretless; some products call a flow brokered even when the client receives or interacts with the credential.

Aegis therefore defines its own stricter term:

> `brokered` in Aegis means the reusable credential value is not returned to Hermes, the model, tool arguments, or tool results. Aegis applies it at the protected downstream edge.

This is closer to Boundary worker-side credential injection than to credential return, but Aegis applies it to typed service actions rather than a general remote session.

### 5.3 Prefer short-lived derived credentials where providers support them

AWS recommends temporary credentials for both humans and workloads and long-lived access keys only where temporary mechanisms cannot be used. Azure managed identity guidance emphasizes least privilege and warns that any code executing on a resource gains the identity's assigned authority. Google Workload Identity Federation similarly exists to exchange external identity for short-lived Google credentials rather than distribute service-account keys.

GitHub documents that installation access tokens:

- expire after one hour;
- may be restricted to selected repositories;
- may request a subset of the GitHub App's granted permissions;
- cannot exceed installation repository or app permission grants.

Aegis adopts a credential strategy hierarchy:

1. provider-issued short-lived, audience/resource-constrained token minted at use time;
2. Aegis-custodied refresh or minting credential used to obtain such a token;
3. narrowly scoped reusable credential;
4. broad reusable credential only as an explicitly warned compatibility case.

The first implementation remains compatible with a fine-grained read-only PAT, but the GitHub App installation-token adapter is the preferred next credential mechanism.

### 5.4 Model typed grants after structured authorization, not free-form scopes

RFC 9396 Rich Authorization Requests exists because coarse OAuth scope strings cannot represent fine-grained actions and resources. It defines structured authorization details with fields such as type, locations, actions, datatypes, identifiers, and privileges.

Aegis adopts the pattern, not the OAuth wire format. An Aegis action grant must be a strict, versioned typed object that binds:

- action type;
- exact destination;
- exact or bounded resource identifiers;
- allowed argument constraints;
- read/write consequence class;
- result schema;
- approval rule.

The current `scope + destination` binding is necessary but insufficient as the long-term user-facing action policy. Repository constraints currently live in broker configuration and must become part of one reviewable compiled grant or an exact referenced destination-policy digest.

### 5.5 Bind tokens and grants to intended resources

RFC 8707 explains that resource indicators allow an authorization server to audience-restrict a token and warns that multi-audience bearer tokens require greater trust because one recipient may use the token at another. RFC 9449 DPoP demonstrates sender-constraining and replay detection for OAuth tokens.

Aegis adopts:

- one fixed destination per initial action;
- no caller-selected URL;
- exact resource constraints;
- short-lived session capability;
- peer identity plus capability, not bearer capability alone;
- request ID, deadline, replay cache, and request budget;
- one action invocation reauthorization at a time.

Aegis does not claim that its local capability is DPoP. The relevant pattern is defense in depth against bearer replay. Future provider adapters should prefer sender-constrained provider tokens where the provider supports them.

### 5.6 Do not pass third-party tokens through an MCP server

The current MCP security best-practices document calls token passthrough an anti-pattern. It also identifies confused-deputy risks, SSRF, and session hijacking, and requires appropriate SSRF mitigations for server-deployed clients.

Aegis adopts:

- no third-party token in MCP requests;
- no token returned in MCP results;
- no generic OAuth proxy controlled by the model;
- no dynamic client registration in the first bridge;
- no arbitrary authorization-server discovery from model input;
- no arbitrary URLs, redirects, methods, headers, or resource metadata fetching;
- separate session capability and downstream credential;
- local Unix-socket bridge rather than a remotely exposed generic MCP credential server.

The MCP protocol is transport and schema plumbing only. It is not the authorization authority. Aegis reauthorizes every action independently of the MCP client or model.

### 5.7 Least privilege must be enforced outside the model

OWASP prompt-injection guidance recommends validating tool calls against user permissions and session context, tool-specific parameter validation, human oversight for high-risk operations, and least privilege. Azure guidance adds an important practical warning: code running where an identity is attached can exercise all of that identity's permissions.

Aegis adopts:

- capability removal over prompt-only instruction;
- typed read-only first action;
- exact model-visible schema;
- authorization from Aegis state, never model claims;
- no general shell/file/web tools in the broker demonstration session;
- principal confirmation for grant mutations;
- future per-invocation confirmation for high-consequence write actions;
- explicit statement that an authorized model may still misuse an allowed operation or mishandle returned data.

### 5.8 Local sidecars and agents solve lifecycle but can still disclose secrets

Vault Agent demonstrates useful patterns: local auto-auth, caching, renewal, templating, and a proxy. It also supports templates and process-supervisor environment injection. Its documentation warns that some local APIs should only be enabled on trusted interfaces.

Aegis adopts:

- one local supervised authority boundary;
- renewal/minting inside the protected edge;
- bounded local IPC;
- fail-closed startup validation;
- no public broker listener.

Aegis does not adopt generic secret templates or a general Vault API proxy for model sessions. Those mechanisms would broaden the plaintext and request surfaces beyond the MVP.

### 5.9 Key custody must include recovery semantics

Current systemd credential tooling can bind encrypted credentials to TPM2, a host key, or both; it embeds credential names to detect repurposing and delivers service credentials through a protected runtime directory. It also makes clear that TPM-only material is machine-bound and host-key material is recoverable by sufficiently privileged filesystem access.

Aegis adopts:

- explicit custody mode and recovery consequences;
- purpose/name binding;
- separate service identity for a production authority;
- no claim that filesystem-encrypted custody protects against root;
- a required recovery drill rather than a “backup succeeded” claim.

Passphrase-encrypted local custody remains the working bare-TUI default. Systemd/TPM custody is a hardened deployment option, not a prerequisite for proving the personal local MVP.

### 5.10 Current Hermes has a more promising narrow MCP surface than the pinned adapter

Current Hermes documentation now describes:

- stdio MCP servers;
- per-server `tools.include` allowlists;
- separate disabling of MCP resources and prompts;
- no registration when filters remove all tools;
- dynamic `mcp-<server>` toolsets usable in per-session toolset selection;
- individual tool enable/disable controls;
- startup discovery and reload behavior.

This is materially more promising than the older conclusion that safe-mode bridge registration was unresolved.

However, the current Aegis adapter accepts only Hermes `>=0.18.0,<0.19.0`, while the latest published Hermes release observed during research is `v2026.7.7.2`. Current web documentation describes the current release line, not necessarily 0.18.x. It also does not by itself prove all Aegis requirements:

- safe mode plus exactly one generated stdio server;
- no inherited project/user/pip plugins;
- no inherited MCP configuration;
- exact post-start tool schema verification without trusting model narration;
- no reload or interactive tool mutation during the session;
- stable gateway protocol compatibility;
- capability-file and process identity behavior.

Decision:

> The first implementation gate is a pinned Hermes adapter spike against the current supported release line. Aegis must generate a disposable home with exactly one stdio bridge, an explicit one-tool include list, resources/prompts disabled, and an exact toolset selection; then verify the effective schemas and negative isolation behavior. If any condition cannot be verified, the model-use gate remains closed.

Aegis must not simply copy a normal user's MCP configuration or enable broad plugin discovery.

### 5.11 Hermes secret-source plugins are not the target boundary

Current Hermes secret-source plugins resolve external secret-manager values into environment variables before Hermes reads credentials. This is useful for ordinary runtime integration and includes good subprocess-safety patterns, but it still places resolved credentials in Hermes process state.

Decision:

- do not implement the MVP as a Hermes secret-source plugin;
- do not inject the stored GitHub credential through Hermes environment variables;
- use an Aegis-owned typed tool bridge and broker;
- consider a secret-source integration only for explicitly labeled compatibility cases such as model-provider startup credentials until an Aegis inference proxy replaces them.

## 6. Current implementation inventory

### 6.1 Implemented and reusable

| Capability | Current state | MVP disposition |
|---|---|---|
| OS-account principal authentication | Implemented | Reuse |
| Built-in `secrets-manager` context | Implemented | Reuse |
| Typed manager proposals | Implemented | Reuse |
| Protected no-echo create/rotate intake | Implemented | Reuse and exercise in PTY E2E |
| Pinentry-first passphrase intake | Implemented | Reuse |
| Passphrase-encrypted KEK custody | Implemented | Reuse as local default |
| systemd and host-file custody options | Implemented with documented limits | Preserve; do not make hardened provisioning an MVP blocker |
| bbolt authority and strict startup validation | Implemented | Reuse |
| Independent envelope encryption per version | Implemented | Reuse |
| Immutable version history | Implemented | Reuse |
| Metadata list/search/history | Implemented | Reuse and improve metadata |
| Exact credential bindings | Implemented | Reuse as compiled enforcement form |
| Local rotation/revocation | Implemented | Reuse with clearer provider-side semantics |
| Consistent ciphertext backup | Implemented | Reuse; add restore/recovery workflow |
| Administrative metadata-only audit | Implemented | Reuse |
| Session capability and replay controls | Implemented | Reuse |
| `SO_PEERCRED` Unix broker | Implemented on Linux | Reuse |
| `github.get_repository.v1` | Implemented | First action |
| Broker result sanitization | Implemented | Reuse |
| Broker-use audit | Implemented | Reuse |
| Disposable Hermes homes | Implemented | Reuse |
| Ambient provider key exclusion | Implemented | Reuse |
| Source-aware likely-secret guard | Implemented for manager path | Reuse; do not overclaim complete detection |

### 6.2 Gaps that block the full MVP

1. No verified model-visible Hermes bridge in the supported adapter.
2. Aegis supports only the older Hermes 0.18.x version contract while current Hermes uses a new release line and documents richer MCP controls.
3. No exact runtime verification that only the Aegis broker tool is model-visible.
4. No real model-to-broker-to-GitHub acceptance workflow.
5. Credential metadata is too sparse for a trustworthy personal inventory.
6. Manager create proposals accept tags/collection but the current authority record does not persist them.
7. Binding list/inspect/disable/remove and friendly effective-access views are missing.
8. Repository resource policy is split between credential binding and broker configuration rather than one digest-bound reviewed grant.
9. Backup exists but first-class restore and recovery verification do not.
10. Local rotate/revoke do not rotate or revoke the provider-side token.
11. External model-provider credentials remain environment-backed compatibility inputs.
12. Production distinct-user/systemd broker deployment is not packaged.

### 6.3 Close to TUI management, not yet close to model use

The principal can already manage much of the encrypted authority through the model-backed manager after local model onboarding and certification. The shortest TUI completion work is:

- persist useful metadata consistently;
- add friendly binding inspection and removal;
- render provider-side lifecycle limitations;
- add backup/recovery guidance and restore verification;
- run a real PTY flow with generated canaries and a certified local model.

That would make the TUI a useful credential manager. It would not by itself let an operational model use the credential. The bridge spike and action E2E are still required for the full product promise.

## 7. MVP architecture

```text
authenticated principal
    -> Aegis TUI
        -> conversational proposal model (untrusted; metadata only)
        -> deterministic confirmation + protected intake
        -> credential authority service
            -> encrypted bbolt records and versions
            -> exact compiled grants
            -> metadata-only audit

operational principal request
    -> Aegis session service
        -> exactly one stanza + mandate
        -> disposable Hermes home
            -> exactly one generated Aegis stdio MCP bridge
            -> exactly one enabled Aegis action toolset
            -> no credential value
        -> bridge capability file (session authentication only)
        -> Unix socket
            -> peer + capability + replay validation
            -> live mandate/grant/record reauthorization
            -> bounded decrypt callback
            -> fixed GitHub request
            -> sanitized typed result
            -> metadata-only broker audit
```

### 7.1 Authority separation

| Component | May propose | May authorize | May see reusable credential | May contact GitHub | May mutate authority |
|---|---:|---:|---:|---:|---:|
| Manager model | Yes | No | No | No | No |
| Operational model | Yes, typed action | No | No | No, except through tool result | No |
| Hermes runtime | Transports model/tool calls | No | No for brokered credential | No direct authenticated path | No |
| Aegis TUI/controller | Collects intent | Enforces confirmed principal action | Protected intake only | No | Through deterministic service |
| Credential authority | No | Resolves exact binding | Yes, bounded decrypt | No | Yes |
| Broker executor | No | Reauthorizes use | Yes, bounded callback | Yes, fixed action | No administrative mutation |
| Audit authority | No | No | No | No | Append metadata events only |

### 7.2 No shared process authority by accident

The production broker requires distinct Aegis service and runtime/bridge OS identities. The local development path may use fixtures and explicit test-only same-UID integration, but release documentation must not present same-user mode as production isolation.

The bridge receives only session authentication material. That capability is not the downstream credential and grants no action beyond the exact live mandate and binding. A compromised authorized runtime can still invoke its allowed action up to its request budget; this is a documented residual risk.

## 8. Credential data model required for the MVP

The existing record must be extended through a versioned migration, not ad hoc unvalidated maps.

Minimum non-secret metadata:

```text
record ID
reference
display label
kind
service/provider
account or tenant label
purpose
collection
tags
status
created at / created by
current version
credential expiration, if known
rotation due interval or date, if configured
last provider validation status/time, if implemented
revocation metadata
```

Security rules:

- metadata is untrusted display data and passes the terminal sanitizer;
- metadata never contains secret values, authorization headers, private keys, connection strings, or recovery material;
- field lengths and character sets are bounded;
- unknown metadata schema versions fail closed;
- search is metadata-only and bounded;
- a model-supplied tag or purpose cannot alter authorization;
- external account labels are not authentication evidence.

### 8.1 Grant model

The human-facing grant should be compiled into a strict form resembling:

```text
grant ID and revision
credential record ID
credential version policy
agent ID
stanza ID
deployment ID
action ID and schema version
destination policy ID and digest
resource constraints
argument constraints
consequence class
approval rule
enabled state
created/updated actor and time
```

For the first action:

```text
action: github.get_repository.v1
destination: github-api
method: GET
resource: exact owner/repository
scope: github/read
consequence: read
result: repository metadata allowlist
approval per use: no
```

The model never sees grant IDs, secret record IDs, scopes, destination IDs, or policy digests as tool arguments.

## 9. TUI product plan

### 9.1 What the principal should be able to ask naturally

- “Add my read-only GitHub token.”
- “Show my credentials.”
- “Find GitHub credentials.”
- “What is this credential for?”
- “Show its version history.”
- “Who can use it?”
- “Allow my research session to read javi/aegis.”
- “Remove that grant.”
- “Rotate this credential.”
- “Revoke it.”
- “Show recent uses and denials.”
- “Back up my credential store.”
- “What would I need to recover it?”

The model may turn complete intents into typed proposals. If required non-secret fields are missing, it asks for them. It never asks for the credential value in chat.

### 9.2 Aegis-owned presentation

The TUI—not model prose—must own:

- authenticated principal identity;
- selected security context;
- authority lock/readiness state;
- credential status and version;
- exact operation preview;
- exact resource and destination;
- whether plaintext reaches the model/runtime;
- confirmation phrase and safe default;
- authoritative success/denial result;
- audit event reference;
- provider-side lifecycle warning;
- recovery readiness.

### 9.3 Required credential views

1. **Inventory:** reference, service, account label, purpose, status, expiry/rotation health.
2. **Credential detail:** immutable versions, current version, bindings/grants, last provider validation, local revocation state.
3. **Effective use:** “which sessions may use this credential for which actions/resources?”
4. **Recent activity:** successful and denied broker actions, metadata only.
5. **Health:** expiring, stale, never used, locally revoked but not known provider-revoked, missing recovery test.

### 9.4 Protected intake invariants

- terminal echo is disabled before the prompt is rendered;
- bracketed paste does not enter normal composer history;
- protected bytes never become a model event;
- cancellation restores terminal state;
- no fallback to a second input surface after pinentry interaction begins;
- values are bounded and non-empty;
- create/rotate confirmation compares exact bytes without displaying them;
- best-effort buffer overwrite is used without claiming guaranteed Go memory zeroization;
- no credential literal in argv;
- stdin/pipe intake is explicit and warned as weaker operator hygiene.

## 10. Hermes bridge decision and validation spike

### 10.1 Preferred bridge

The preferred bridge is one Aegis-owned, pinned stdio MCP executable generated into or referenced from the disposable Hermes home.

It exposes exactly one first-release schema:

```text
github_get_repository(owner, repository)
```

The schema contains conservative bounded strings only. The bridge does not accept:

- secret reference;
- credential scope;
- destination;
- URL;
- method;
- header;
- token;
- agent/stanza/deployment;
- mandate/session identifiers;
- timeout beyond the broker-defined bound.

### 10.2 Generated Hermes configuration

Subject to exact current-version validation, Aegis should generate:

- one stdio MCP server entry;
- absolute pinned bridge executable path;
- fixed arguments only;
- minimal allowlisted bridge environment;
- `tools.include` containing exactly the reviewed action;
- MCP resources disabled;
- MCP prompts disabled;
- no HTTP MCP server;
- no OAuth discovery;
- one dynamic MCP toolset selected explicitly;
- no wildcard toolsets;
- no user/project/pip plugins;
- no inherited MCP config;
- no reload command authority.

### 10.3 Required adapter evidence

The spike passes only if tests prove:

1. The pinned Hermes version launches through the supported gateway protocol.
2. Safe mode and generated MCP coexist, or an equally restrictive explicit launch mode is verified.
3. Exactly one tool schema reaches the model request.
4. MCP resources and prompts are absent.
5. Built-in broad tools are absent.
6. User, project, pip, and bundled ambient plugin surfaces are absent unless explicitly required and pinned.
7. Normal Hermes home state is untouched.
8. The generated home contains no reusable credential.
9. Runtime cannot reload or add MCP servers during the session.
10. Immediate process failure, bridge failure, malformed schemas, name collision, duplicate registration, or extra tool registration fails session startup.
11. Aegis verifies tool schemas from runtime protocol/state, not by asking the model what tools it sees.
12. Session termination removes capability material and broker state.

If current Hermes cannot satisfy these tests, Aegis must either contribute/require an explicit safe tool-injection contract upstream or retain the broker as non-model-visible. It must not weaken the boundary through inherited MCP configuration.

## 11. First downstream adapter: GitHub repository metadata

### 11.1 Why it remains the first action

- already implemented;
- real utility for private repositories;
- read-only and idempotent;
- fixed host, method, and path shape;
- exact repository allowlist;
- small request schema;
- small sanitizable result;
- no generic proxy requirement;
- easy mock and real-service verification.

### 11.2 Credential choice

Pilot options in order:

1. GitHub App installation token minted by Aegis from a custodied App credential;
2. fine-grained read-only PAT limited to the test repository;
3. classic PAT only if no safer option is available, with explicit blast-radius warning.

The shortest existing-code path is option 2. The report does not require building GitHub App minting before the first proof, but the chosen real credential must be low-risk, scoped, and disposable.

### 11.3 Response policy

Return only fields already implemented and required by the user experience:

- owner;
- name;
- private;
- default branch;
- archived;
- visibility;
- updated time.

Do not return:

- response headers;
- URLs;
- permissions;
- owner profile data;
- error bodies;
- rate-limit tokens or diagnostic headers;
- credential or authorization material;
- arbitrary nested GitHub JSON.

## 12. External model-provider credentials

The MVP demonstration should use the existing authenticated local Ollama manager/route where practical so the credential-under-test can be proven absent from Hermes and model-provider transport.

Operational Hermes provider credentials currently remain selected environment bindings. This is narrower than ambient inheritance but still process disclosure. Therefore:

- do not claim that every Aegis credential is brokered;
- label provider environment injection as compatibility behavior;
- do not use the stored GitHub credential through that path;
- plan an Aegis inference proxy for external providers immediately after the first GitHub vertical slice;
- preserve a local-model no-provider-key demonstration.

A future external-provider proxy must fix the provider destination, apply authentication inside Aegis, bound streaming requests/responses, disable unsafe redirects/proxy inheritance, sanitize errors, and bind route/model identity to the mandate.

## 13. Rotation, revocation, and health semantics

### 13.1 Local rotation

Current Aegis rotation stores a new supplied value. It does not ask the provider to mint one.

TUI language must say:

> A new Aegis version is active. Confirm separately that the provider issued this value and retire the old provider credential when safe.

### 13.2 Local revocation

Current Aegis revocation denies future authorized Aegis use. It does not invalidate a token already copied or exposed elsewhere.

TUI language must say:

> Aegis use is revoked. Provider-side validity is unknown unless Aegis completed and recorded a provider revocation action.

### 13.3 Provider lifecycle follow-on

Provider-side rotate/revoke actions may be added only as typed, reviewed operations with transaction semantics. A token-creation response containing a new secret must be captured directly into the authority before any model-visible result is constructed.

### 13.4 Health states

The TUI should distinguish:

- active;
- expiring;
- rotation due;
- locally revoked;
- provider validity unknown;
- provider validated;
- provider rejected;
- recovery unverified.

Unknown is not healthy.

## 14. Audit requirements

Every administrative mutation records:

- authenticated principal subject;
- manager security context;
- operation type;
- record ID where applicable;
- outcome and stable reason;
- timestamp;
- resulting non-secret revision/version.

Every broker use records:

- subject, agent, stanza, session, and mandate;
- charter/grant digest or revision;
- action;
- destination identifier;
- resource fingerprint or policy-safe exact resource according to retention policy;
- credential record ID and version;
- binding/grant revision;
- request ID;
- outcome and stable reason;
- timing and bounded downstream status class where safe.

Never record:

- plaintext;
- wrapped key or ciphertext;
- authorization header;
- session capability;
- provider error body;
- full downstream result;
- protected intake bytes;
- passphrase.

The TUI must provide a useful recent-use view without requiring the principal to manually interpret raw audit JSON.

## 15. Backup and recovery MVP

### 15.1 Required artifacts

A recovery plan must identify separately:

- ciphertext database backup;
- authority configuration and deployment/store identifiers;
- KEK recovery material or passphrase knowledge;
- Aegis version/schema compatibility;
- integrity verification procedure.

### 15.2 Required behavior

- consistent database snapshot;
- restrictive output permissions;
- refusal to overwrite ambiguous existing files;
- explicit warning that metadata is sensitive;
- explicit warning not to colocate weaker host-file KEK with the database backup;
- a restore command or deterministic documented restore service;
- restore only into an explicitly reviewed destination;
- validation before activation;
- no automatic replacement of active authority state;
- hermetic restore test in CI;
- one locally exercised recovery walkthrough before release.

### 15.3 Deferred hardening

- TPM anti-rollback counters;
- HSM custody;
- threshold recovery;
- fleet backup orchestration;
- guaranteed secure deletion from backup media.

## 16. Threat model summary

| Threat | MVP control | Residual risk |
|---|---|---|
| Secret pasted into chat | deterministic ingress guard blocks likely patterns before model; protected intake separate | heuristic scanner has false negatives/positives; user can encode novel secrets |
| Model requests plaintext | no reveal/GetSecret schema; bridge has business arguments only | model may ask user to disclose outside Aegis; UI education remains necessary |
| Prompt selects another credential | broker derives binding from trusted session/grant | misconfigured approved grant can still be broad |
| Prompt causes SSRF | fixed HTTPS destination, method, path construction; exact resource allowlist; redirects/proxy env disabled | configured DNS/network and downstream endpoint remain trusted |
| Compromised Hermes reads token | credential absent from environment/home/tool result; Aegis applies internally | root/kernel or Aegis process compromise can expose memory |
| Same-host process calls broker | distinct UID/GID, `SO_PEERCRED`, session capability, PID/start binding, replay controls | root can inspect or impersonate local state |
| Capability replay | short TTL, fresh request IDs, deadlines, request budget, live reauthorization | authorized compromised runtime can consume allowed budget |
| Cross-stanza use | exact session/mandate/stanza/deployment binding; no union | host compromise outside Aegis boundary |
| Malicious GitHub response | bounded body, strict JSON, field allowlist, identity checks; treat result as untrusted | allowed repository metadata may itself contain hostile text in future broader actions |
| Token remains valid after local revoke | Aegis denies future broker use and labels provider status unknown | token copied before custody or retained elsewhere still works at provider |
| Backup loss or mismatch | explicit separate recovery artifacts and restore verification | forgotten passphrase or destroyed machine-bound key can make data unrecoverable |
| Extra Hermes tools appear | generated home/config, exact allowlist, runtime schema verification, fail startup | depends on pinned Hermes contract and complete adapter tests |
| Authorized model abuses read access | smallest resource/action grant, audit, expiry, request budget | least authority is not correct intent; allowed data may be exfiltrated through another granted channel |

## 17. Explicit MVP scope

### 17.1 Required

- one authenticated principal;
- one local encrypted authority;
- TUI create/list/search/detail/history/rotate/revoke;
- useful persisted metadata;
- friendly grant preview and exact compiled binding;
- binding list/inspect/disable/remove;
- one clean operational Hermes session;
- one verified model-visible Aegis tool;
- one real GitHub repository metadata action;
- no reusable GitHub credential in model/runtime surfaces;
- immediate Aegis revocation;
- current-version rotation behavior;
- metadata-only audit and TUI activity view;
- consistent backup and verified restore;
- complete negative test matrix;
- honest documented limits.

### 17.2 Deferred

- agent-owned credentials;
- agent-managed credential lifecycle;
- credential plaintext reveal;
- generic GetSecret;
- arbitrary authenticated HTTP;
- write actions;
- generic database proxying;
- browser credential filling;
- multi-principal approval;
- fleets and cross-device projection;
- Infisical cutover;
- automatic secret discovery across the host;
- desktop/web secret intake;
- multiple agent runtimes;
- generalized third-party MCP installation;
- provider-side rotation for arbitrary services;
- complete host sandboxing;
- protection from root/kernel compromise.

## 18. Implementation phases

### Phase 0 — Adopt the product contract

1. Amend `specs/MVP.md` so personal credential custody and model use are the primary vertical slice.
2. Reconcile `specs/AEGIS_MANAGER.md` and broker documents with this report.
3. Define stable acceptance-test names and claims before implementation.

Exit: normative scope has one objective and no contradictory “administrative only” release claim.

### Phase 1 — Finish personal TUI credential management

1. Version and migrate credential metadata.
2. Persist tags/collection rather than accepting and dropping them.
3. Add service, account label, purpose, expiry, and rotation metadata.
4. Add binding list, detail, disable, and remove domain/repository operations.
5. Add friendly effective-use and recent-activity TUI views.
6. Add explicit local-versus-provider rotation/revocation language.
7. Add PTY E2E with generated secret/passphrase canaries.

Exit: the principal can manage the store coherently through the TUI without using raw secret subcommands for the normal path.

### Phase 2 — Validate and upgrade the Hermes adapter

1. Pin a reviewed current Hermes release.
2. update version parsing/support contract deliberately;
3. generate isolated one-server MCP config;
4. enable one exact dynamic MCP toolset;
5. disable resources/prompts and ambient plugins/config;
6. verify effective tool schemas and startup behavior;
7. retain disposable homes and lifecycle cleanup;
8. update adapter conformance fixtures and real smoke tests.

Exit: Aegis can prove exactly one Aegis action tool is visible and no ambient tools/extensions are present.

### Phase 3 — Connect the existing broker

1. Implement the pinned Aegis stdio MCP bridge.
2. Translate only the business arguments to the existing broker request.
3. Keep capability/request metadata outside model arguments/results.
4. Wire lifecycle cleanup and bridge identity.
5. Add mock downstream positive and full negative tests.
6. Add a real GitHub opt-in acceptance command using a disposable low-risk credential.

Exit: a model completes `github.get_repository.v1` through Aegis without credential exposure.

### Phase 4 — Unify grants and destination policy

1. Compile repository constraints into one exact reviewable grant or bind an exact destination-policy digest.
2. Prevent configuration drift from silently changing approved resources.
3. Render the friendly and technical grant in TUI.
4. Add grant mutation/replay/ambiguity tests.

Exit: the principal can answer exactly what was approved and configuration mutation invalidates or denies use.

### Phase 5 — Recovery and release proof

1. Add first-class restore/verify workflow.
2. Exercise backup/restore with passphrase custody.
3. Add credential canary scans over generated homes, logs, audit, captures, and backups where plaintext must be absent.
4. Run the documented five-minute local path and opt-in real GitHub path.
5. Update all affected launch assets.

Exit: the complete positive and negative acceptance matrix passes and all claims match observed behavior.

### Phase 6 — Immediate post-MVP

1. Aegis external model-provider inference proxy.
2. GitHub App installation-token minting.
3. First reviewed write action with exact confirmation.
4. Credential health findings and provider validation.
5. Secure import assistants for explicitly selected sources.

## 19. Release acceptance gates

### 19.1 Positive gates

- [ ] Principal authenticates outside the model.
- [ ] Bare TUI unlocks the authority through protected input.
- [ ] Principal creates a real low-risk GitHub credential through TUI protected intake.
- [ ] Credential metadata and version appear without value.
- [ ] Principal approves one exact repository read grant.
- [ ] Aegis starts a fresh Hermes session with one exact model-visible tool.
- [ ] Model invokes the tool with owner/repository only.
- [ ] Aegis returns real sanitized private repository metadata.
- [ ] Audit reconstructs identity, session, stanza, grant, record/version, action, destination, resource, and outcome.
- [ ] Rotation changes the current version used without Hermes reconfiguration.
- [ ] Revocation blocks the next request.
- [ ] Backup restores and verifies in a fresh disposable Aegis root.

### 19.2 Secret absence gates

A generated random canary must be absent from:

- [ ] argv and process listings captured by the test;
- [ ] Hermes environment;
- [ ] bridge environment;
- [ ] disposable Hermes home except no file should contain it at all;
- [ ] model request and transcript;
- [ ] MCP tool schema, arguments, and result;
- [ ] Aegis stdout/stderr;
- [ ] broker errors;
- [ ] Aegis logs;
- [ ] audit records and checkpoints;
- [ ] TUI history and terminal capture;
- [ ] plaintext temporary files;
- [ ] ciphertext backup as a plaintext byte sequence.

Tests must not claim absence from swap, kernel memory, crash dumps, storage remnants, or root inspection unless those surfaces are separately controlled and tested.

### 19.3 Denial gates

- [ ] Wrong peer UID/GID denied before body processing.
- [ ] Missing, malformed, expired, or unknown capability denied.
- [ ] Replayed request ID denied.
- [ ] Stale or overlong deadline denied.
- [ ] Request budget exhaustion denied.
- [ ] Stopped, failed, expired, or revoked session denied.
- [ ] Runtime PID/start-token mismatch denied.
- [ ] Charter/grant mutation denied.
- [ ] Wrong agent, stanza, deployment, scope, destination, or operation denied.
- [ ] Zero or multiple bindings denied.
- [ ] Disabled binding denied.
- [ ] Revoked record/version denied.
- [ ] Pinned/current version mismatch denied.
- [ ] Unapproved owner/repository denied.
- [ ] Invalid path segment denied.
- [ ] Redirect denied.
- [ ] Oversized, malformed, wrong-content-type, or identity-mismatched downstream result denied.
- [ ] Additional Hermes tool, MCP resource, prompt, plugin, or inherited configuration fails startup verification.

### 19.4 Recovery gates

- [ ] Wrong passphrase/KEK fails closed without corrupting state.
- [ ] Wrong deployment/store binding fails.
- [ ] Mutated ciphertext or envelope context fails.
- [ ] Incomplete backup is not published as successful.
- [ ] Restore never overwrites an active authority implicitly.
- [ ] Restored authority can execute the same mock broker operation.
- [ ] Recovery documentation states exactly which artifacts and knowledge are required.

## 20. Claim language after completion

Allowed claim:

> Aegis can store a principal-supplied credential in its encrypted local authority and let an authenticated, mandate-bound Hermes session invoke one explicitly granted GitHub repository metadata action. Aegis selects and applies the credential internally and returns a bounded sanitized result without placing the reusable credential in model context, Hermes environment, tool arguments/results, ordinary logs, or audit records.

Required qualifiers:

- one implemented GitHub action, not arbitrary services;
- brokered credential under test, not all provider credentials;
- no protection from root/kernel compromise;
- no guarantee of Go memory zeroization or deletion from all media;
- authorized model may misuse the allowed action or returned data;
- local revocation is not provider-side token invalidation;
- Hermes process/home isolation is not a host sandbox.

Forbidden claims:

- “models can never access secrets”;
- “all credentials are secretless”;
- “zero trust” without qualification;
- “complete least privilege”;
- “sandboxed Hermes”;
- “rotation/revocation at the provider” unless a typed provider action completed;
- “secure backup” without an exercised recovery path;
- “generic credential broker”;
- “safe arbitrary MCP.”

## 21. Implementation map

Likely primary code areas:

- `internal/credentials/model.go` — versioned metadata and grant/repository contracts;
- `internal/credentials/authority.go` — metadata/grant lifecycle and restore verification services;
- `internal/credentials/bbolt/` — schema migration, indexes, binding enumeration/state;
- `internal/credentials/broker/` — preserve narrow executor and add bridge-facing conformance as needed;
- `internal/app/broker.go` — grant digest/resource reauthorization and audit;
- `internal/manager/contracts.go` — reconcile proposal metadata with persisted fields;
- `internal/manager/orchestrator.go` — TUI proposal flows, no secret values;
- `internal/command/manager_runtime.go` — deterministic operations and audit;
- `internal/tui/` — inventory, detail, grant, activity, and recovery presentation;
- `internal/runtime/hermes/` — current-version adapter, generated MCP config, exact tool verification;
- new Aegis-owned bridge package/binary — one stdio MCP action and no credential logic;
- `docs/CREDENTIAL_BROKER.md` — current bridge and version contract;
- `specs/MVP.md` — normative product objective.

No implementation should place credential logic in model prompts or generated Hermes plugins. The bridge is a deterministic protocol adapter; the broker and authority remain the enforcement boundary.

## 22. Launch-asset impact review

This report changes product priority but no executable behavior. Therefore no launch asset is edited in this report-only change. When the plan is adopted normatively or implemented, review every required launch asset:

- `README.md`: affected when MVP objective, TUI workflow, commands, or claims change;
- `LICENSE`: reviewed, likely unaffected unless a new bridge dependency changes notices/obligations;
- `SECURITY.md`: affected by new supported boundary, disclosure process, and residual risks;
- `CONTRIBUTING.md`: affected by adapter/bridge test requirements and safe credential fixtures;
- `CODE_OF_CONDUCT.md`: reviewed, expected unaffected;
- `CHANGELOG.md`: affected by every implementation phase;
- threat model: affected by bridge, grant, restore, and provider lifecycle semantics;
- architecture diagram: affected by verified bridge replacing the future edge;
- five-minute quickstart: affected when TUI credential workflow is executable;
- no-key demonstration: must remain genuinely no-key and must not imply live GitHub success;
- terminal recording: affected when TUI flow changes; use generated canaries/fake pinentry only;
- release binaries/checksums: affected by new binary/package and release process;
- focused contributor issues: prepare repository-local issue text unless owner authorizes GitHub issue creation.

A real credential must never be used in a recording, fixture, issue, or retained report.

## 23. Primary internet sources

Accessed 2026-07-19 unless otherwise noted.

### Hermes Agent

1. Hermes Agent documentation: https://hermes-agent.nousresearch.com/docs
2. Use MCP with Hermes: https://hermes-agent.nousresearch.com/docs/guides/use-mcp-with-hermes
3. MCP configuration reference: https://hermes-agent.nousresearch.com/docs/reference/mcp-config-reference
4. Toolsets reference: https://hermes-agent.nousresearch.com/docs/reference/toolsets-reference
5. Tools runtime: https://hermes-agent.nousresearch.com/docs/developer-guide/tools-runtime
6. Secret source plugins: https://hermes-agent.nousresearch.com/docs/developer-guide/secret-source-plugin
7. Plugins: https://hermes-agent.nousresearch.com/docs/developer-guide/plugins
8. Latest GitHub release API: https://api.github.com/repos/NousResearch/hermes-agent/releases/latest

Observed release during research: `v2026.7.7.2`, published 2026-07-08. This is discovery evidence, not an Aegis compatibility claim.

### Model Context Protocol and OAuth

9. MCP Authorization specification, current canonical version observed as 2025-11-25: https://modelcontextprotocol.io/specification/2025-11-25/basic/authorization
10. MCP Security Best Practices: https://modelcontextprotocol.io/specification/2025-11-25/basic/security_best_practices
11. RFC 8693, OAuth 2.0 Token Exchange: https://www.rfc-editor.org/rfc/rfc8693
12. RFC 9396, OAuth 2.0 Rich Authorization Requests: https://www.rfc-editor.org/rfc/rfc9396
13. RFC 8707, Resource Indicators for OAuth 2.0: https://www.rfc-editor.org/rfc/rfc8707
14. RFC 9449, OAuth 2.0 Demonstrating Proof of Possession: https://www.rfc-editor.org/rfc/rfc9449

### Credential custody, brokering, and temporary identity

15. 1Password SDKs: https://developer.1password.com/docs/sdks/
16. 1Password CLI environment-variable integration: https://developer.1password.com/docs/cli/secrets-environment-variables/
17. Vault Agent: https://developer.hashicorp.com/vault/docs/agent-and-proxy/agent
18. Boundary credential management: https://developer.hashicorp.com/boundary/docs/concepts/credential-management
19. AWS IAM security best practices: https://docs.aws.amazon.com/IAM/latest/UserGuide/best-practices.html
20. AWS temporary security credentials: https://docs.aws.amazon.com/IAM/latest/UserGuide/id_credentials_temp.html
21. Google Workload Identity Federation: https://cloud.google.com/iam/docs/workload-identity-federation
22. Azure managed identity overview: https://learn.microsoft.com/en-us/entra/identity/managed-identities-azure-resources/overview
23. Azure managed identity best practices: https://learn.microsoft.com/en-us/entra/identity/managed-identities-azure-resources/managed-identity-best-practice-recommendations
24. systemd credentials: https://www.freedesktop.org/software/systemd/man/latest/systemd-creds.html
25. systemd service execution credentials: https://www.freedesktop.org/software/systemd/man/latest/systemd.exec.html

### First downstream service and model risk

26. GitHub App installation authentication: https://docs.github.com/en/apps/creating-github-apps/authenticating-with-a-github-app/authenticating-as-a-github-app-installation
27. GitHub REST installation endpoints: https://docs.github.com/en/rest/apps/installations
28. OWASP LLM Prompt Injection Prevention Cheat Sheet: https://cheatsheetseries.owasp.org/cheatsheets/LLM_Prompt_Injection_Prevention_Cheat_Sheet.html

## 24. Final recommendation

Adopt this as the new MVP center of gravity:

> Aegis is the principal's local credential authority and model-use broker. The principal manages credentials in the Aegis TUI. Models receive narrowly typed actions, never a general secret-reading interface. Aegis independently authorizes every call, applies the credential at a fixed downstream edge, returns a sanitized result, and records metadata-only audit.

The implementation sequence should not begin with more storage cryptography or broader fleet architecture. The highest-value path is:

1. finish the personal TUI metadata/grant/recovery experience;
2. validate the current Hermes one-tool MCP contract in a disposable home;
3. connect the already implemented GitHub broker;
4. prove the complete real and negative workflow;
5. only then add another service or write operation.

The project is close to the first half: the principal managing encrypted credentials through the TUI. It is not yet complete on the second half: an operational model safely using one stored credential. The current Hermes MCP controls may make that second half substantially closer than the older 0.18.x assessment suggested, but only a pinned adapter spike and exact runtime verification can turn that possibility into a supported security claim.
