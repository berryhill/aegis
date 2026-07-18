# Base Manager End-to-End Local Session

## 1. Status and purpose

**Status:** Normative implementation profile

**Feature:** Base Manager End-to-End Local Session

**Parent specification:** `specs/AEGIS_MANAGER.md`

This specification defines the next implementation vertical slice for Aegis. It converts the built-in manager contracts and isolated components into one real principal-facing local conversation.

This document is intentionally narrower than `specs/AEGIS_MANAGER.md`. It does not redefine the built-in manager security model. It identifies the exact behavior that the next implementation session MUST connect, exercise, and verify.

The completed feature MUST make this statement true:

> Bare interactive `aegis` starts one Aegis-owned manager session through Hermes and an exact pinned local Ollama model; natural-language turns produce only closed typed proposals; credential values enter only through protected Aegis intake; no cloud fallback exists; and every exit path removes the ephemeral runtime authority and unloads the model.

## 2. Authority and conflict resolution

The implementation MUST follow this authority order:

1. `AGENTS.md`
2. `specs/MVP.md`
3. `specs/AEGIS_MANAGER.md`
4. this implementation profile
5. other focused specifications
6. retained research
7. implementation convenience

If this profile conflicts with a higher-authority document, the higher-authority requirement wins. The implementation session MUST resolve the discrepancy explicitly rather than silently weakening a security boundary.

This profile does not authorize:

- provisioning a user-created logical agent;
- activating a deployment;
- modifying a normal Hermes profile;
- downloading a model;
- changing global Ollama configuration;
- committing or publishing repository changes;
- creating releases, tags, checksums, or remote issues.

Those actions require separate explicit operator authorization.

## 3. Scope

### 3.1 Required outcome

The feature MUST connect the following path end to end:

```text
principal terminal
  -> Aegis-owned terminal UI
  -> deterministic Aegis ingress guard
  -> isolated Hermes structured gateway
  -> session-bound Aegis inference proxy
  -> exact loopback Ollama endpoint
  -> exact pinned and certified local model
  -> complete buffered model response
  -> strict manager envelope decoder
  -> closed typed proposal validator
  -> deterministic Aegis preview and confirmation
  -> shared Aegis application service
  -> metadata-only result, audit event, and receipt
```

No component in this chain may be represented only by a fixture, unused type, or disconnected helper when the feature is declared complete.

### 3.2 Required operations

The initial end-to-end manager MUST support:

- credential metadata list;
- credential metadata search;
- credential metadata inspection;
- proposed credential creation;
- proposed credential rotation;
- proposed credential revocation;
- recent audit inspection;
- deterministic help;
- session exit.

Create and rotate MUST obtain secret bytes only through Aegis-owned protected intake. Revoke MUST require deterministic Aegis confirmation. Read operations MUST not return secret values.

### 3.3 Explicitly excluded

The feature MUST NOT add:

- arbitrary secret reveal;
- generic shell execution;
- generic file access;
- generic HTTP execution;
- generic MCP exposure;
- arbitrary Hermes tools;
- plugin or profile management;
- model-selected providers or models;
- cloud inference;
- remote Ollama endpoints;
- multiple simultaneous manager models;
- wrapped user-created agent credential use;
- a generic broker bridge;
- endpoint monitoring or EDR;
- fleet deployment projection;
- fine-tuned credential classifiers;
- web or graphical UI.

These exclusions are not grounds to weaken the required local manager path.

## 4. Security invariants

The implementation is incomplete if any invariant below is violated.

### 4.1 Authority

- The principal MUST authenticate outside the model.
- The built-in manager security context MUST be deterministic and immutable for the build/configuration revision.
- Prompt content MUST NOT select or broaden the security context.
- The model MAY propose an operation but MUST NOT authorize, confirm, or execute it.
- A model statement that an operation succeeded MUST have no authority.
- Every mutation MUST pass deterministic Aegis authorization and operator confirmation.

### 4.2 Secret non-disclosure

- Protected-intake values MUST NOT enter Hermes input.
- Protected-intake values MUST NOT enter Ollama requests.
- Protected-intake values MUST NOT enter model context or history.
- Secret values MUST NOT appear in stdout, stderr, logs, audit, receipts, errors, argv, environment variables, or plaintext temporary files.
- Credential metadata returned to the model MUST be allowlisted and prompt-injection-safe.
- Ordinary messages containing high-confidence credential formats MUST be blocked and discarded before Hermes receives them.
- The exact serialized inference request MUST receive a second guard before forwarding to Ollama.
- Complete model responses MUST be guarded before release or reuse.
- Scanner, parser, proxy, or policy failure MUST deny rather than allow.

### 4.3 Runtime isolation

- Hermes MUST run with a fresh disposable home.
- Hermes MUST use safe mode and the structured gateway.
- Hermes MUST NOT use one-shot/YOLO mode.
- Hermes MUST NOT attach directly to the principal terminal.
- Hermes MUST NOT inherit normal profiles, memories, skills, plugins, MCP servers, provider credentials, or gateway settings.
- Hermes MUST NOT connect directly to Ollama.
- The Ollama route MUST be loopback-only.
- The session MUST permit exactly one model identity and digest.
- Model switching and fallback MUST be denied.
- Managed Ollama cloud functionality MUST be explicitly disabled.

### 4.4 Lifecycle

- Session capabilities MUST be ephemeral and unguessable.
- Every proxy request MUST be bound to an active, unexpired session and immutable route.
- Cleanup MUST be bounded and idempotent.
- Session exit MUST terminate Hermes, close the proxy, unload the exact model, remove disposable state, and invalidate the session capability.
- Partial startup failure MUST roll back all resources already created.

## 5. Entry behavior and prerequisites

### 5.1 Bare command dispatch

Bare `aegis` MUST start the manager only when stdin and stdout are interactive terminals.

The command MUST preserve the established behavior of:

- help;
- version;
- update;
- initialization/setup;
- explicit subcommands;
- non-TTY structured output.

Bare non-TTY execution MUST NOT start Hermes, Ollama, protected intake, or an interactive prompt.

### 5.2 Missing initialization

When configuration is genuinely absent, bare interactive `aegis` MUST enter or offer the deterministic first-run path defined by the current first-run specification and implementation.

Malformed, insecurely permissioned, ambiguous, or partially initialized configuration MUST NOT be treated as absent. It MUST fail closed or enter an explicit deterministic recovery path.

### 5.3 Manager preflight

Before any model or Hermes process starts, Aegis MUST verify:

- authenticated principal;
- manager configuration decoded once into strict typed values;
- credential authority health and permissions;
- supported Hermes executable and version;
- permitted Ollama mode and exact loopback endpoint;
- exact configured model name;
- exact installed model artifact digest;
- valid certification for the complete route identity;
- resource and context bounds;
- no active conflicting manager session where prohibited.

A missing or invalid prerequisite MUST produce a stable manager reason code and safe remediation.

## 6. Immutable route plan

Aegis MUST build one canonical route plan before starting Hermes.

The plan MUST bind at least:

- authenticated principal ID;
- built-in manager identity;
- built-in manager policy/security-context revision;
- Hermes executable identity and supported version;
- Ollama mode;
- exact loopback Ollama origin;
- exact model name;
- exact model artifact digest;
- quantization and context identity when exposed reliably;
- conformance corpus digest;
- conformance certification digest;
- manager instruction digest;
- manager response schema version;
- proxy origin;
- digest of the ephemeral proxy capability;
- session creation and expiry times;
- request/response size bounds;
- inference timeout bounds;
- no-fallback policy;
- no-model-switching policy.

The plan MUST use deterministic canonical encoding and digesting. Unknown fields, missing fields, duplicate fields, unsupported values, and ambiguous identity MUST fail closed.

The plan MUST be immutable after Hermes starts. A requested model, endpoint, provider, context, policy, or runtime change MUST require a clean session and a new route plan.

## 7. Ollama integration

### 7.1 Modes

Two modes MAY be supported:

1. `managed`, preferred;
2. `external-local`, explicit weaker deployment mode.

No remote or cloud Ollama mode is permitted.

### 7.2 Managed mode

Aegis MUST:

- start `ollama serve` as a supervised child process;
- use an ephemeral loopback endpoint;
- explicitly disable Ollama cloud behavior;
- pass a minimal allowlisted environment;
- avoid placing secrets in argv or environment;
- capture process identity and process-group ownership;
- wait for readiness with bounded retries and cancellation;
- stop the managed process when the manager session closes;
- terminate the process group after a bounded graceful deadline if necessary.

Aegis MUST NOT claim that process/home controls form a host sandbox.

### 7.3 External-local mode

Aegis MUST:

- accept only an exact configured `http` loopback origin;
- reject user information, paths, queries, fragments, redirects, Unix ambiguity, and non-loopback resolution;
- verify API compatibility;
- avoid stopping the external daemon on manager exit;
- still unload the exact manager model on session close when supported.

The documentation MUST state that Aegis cannot independently prove all daemon-start environment settings for an externally managed process.

### 7.4 Model identity and residency

Aegis MUST:

- verify the exact installed model artifact digest;
- reject mutable-tag digest drift;
- reject uninstalled or uncertified models;
- permit one request at a time unless a stricter bounded implementation is used;
- request a five-minute idle residency limit;
- explicitly unload the model during cleanup;
- never pull, update, or replace a model during ordinary startup;
- never select an uncertified default.

## 8. Session-bound inference proxy

The proxy is a mandatory security boundary, not an optional compatibility helper.

### 8.1 Binding

The proxy MUST:

- listen only on an ephemeral loopback endpoint;
- require an unguessable per-session bearer capability;
- retain only a capability digest where practical;
- bind the capability to one route plan and session;
- reject requests after expiry, revocation, or cleanup begins;
- never expose its capability in logs, receipts, or model-visible text.

If supported Hermes cannot send a separate route header, the fixed proxy configuration and session bearer capability MUST carry the immutable route binding. The implementation MUST NOT retain a header requirement that makes the real path unusable, and MUST NOT remove route binding merely for compatibility.

### 8.2 Allowed protocol

The proxy MUST allow only the exact Ollama-compatible inference paths required by the supported Hermes adapter.

It MUST reject:

- unknown methods or paths;
- redirects;
- alternate hosts;
- alternate model names;
- model-switching options;
- auxiliary/fallback models;
- unsupported streaming modes;
- oversized bodies;
- malformed or duplicate-key JSON;
- inactive or expired sessions;
- wrong capabilities;
- concurrent requests beyond configured bounds.

### 8.3 Request and response guard

The proxy MUST fully buffer within explicit bounds before forwarding or releasing content.

For requests it MUST:

1. authenticate the session capability;
2. validate route and model identity;
3. strictly decode the supported request shape;
4. inspect the exact serialized semantic message content;
5. block on a secret finding, scanner failure, timeout, or panic;
6. forward only to the exact loopback Ollama origin.

For responses it MUST:

1. enforce status, content type, size, and timeout bounds;
2. disable redirect following;
3. strictly decode the supported response shape;
4. inspect complete model-controlled text and tool-call fields;
5. block on a secret finding or decoding/scanner failure;
6. release only a complete bounded response.

Neither request nor response bodies may be logged.

## 9. Hermes structured gateway

### 9.1 Process launch

Aegis MUST launch Hermes with:

- an exact supported executable/version;
- a fresh disposable `HERMES_HOME`;
- safe mode;
- `no_mcp` or an equivalently empty hard toolset;
- the structured TUI-gateway stdio protocol;
- the Aegis proxy as the only inference route;
- a minimal environment;
- process-group control;
- bounded stderr capture that excludes prompt/model bodies.

The process MUST NOT inherit:

- normal Hermes home/profile state;
- provider API keys;
- cloud endpoints;
- plugins;
- MCP servers;
- user skills or memories;
- profile/gateway/cron management;
- arbitrary shell or file capabilities;
- Aegis provisioning authority.

### 9.2 Gateway state machine

The manager MUST implement a bounded gateway state machine covering:

1. process start;
2. readiness;
3. session creation;
4. built-in instruction submission;
5. user turn submission;
6. event collection;
7. complete response assembly;
8. repeat turns;
9. cancellation;
10. orderly close.

It MUST fail closed on:

- malformed JSON;
- duplicate keys where applicable;
- unknown required event shapes;
- oversized messages or deltas;
- completion without a valid response;
- timeout;
- EOF;
- child process exit;
- inconsistent session IDs;
- events after cancellation or cleanup.

## 10. Manager response and proposal protocol

### 10.1 Response envelope

Every complete model response MUST decode as exactly one closed manager envelope containing:

- exact schema version;
- response kind;
- bounded user-facing message;
- zero or one typed proposal.

The decoder MUST reject:

- duplicate JSON keys;
- trailing data;
- unknown fields;
- missing required fields;
- unsupported schema versions;
- multiple proposals;
- JSON hidden in prose;
- Markdown fences;
- unsupported operation names;
- malformed operation arguments;
- oversized text;
- model-supplied authorization or confirmation;
- fabricated authoritative success.

### 10.2 Allowed proposals

The initial closed operation set is:

- `secret.list`;
- `secret.search`;
- `secret.inspect_metadata`;
- `secret.propose_create`;
- `secret.propose_rotate`;
- `secret.propose_revoke`;
- `audit.recent`;
- `help.topic`;
- `session.exit`.

No generic command or untyped map operation is permitted.

### 10.3 Proposal execution

For every proposal, Aegis MUST independently verify:

- active authenticated principal;
- correct built-in manager context;
- active immutable route plan;
- unexpired and unrevoked session;
- allowlisted operation;
- strict bounded arguments;
- unique record/reference resolution;
- no authority expansion;
- no secret bytes in proposal fields;
- required deterministic preview;
- required operator confirmation for mutation.

Zero record matches MUST deny. Multiple matches MUST deny as ambiguous.

## 11. Prompt and content ingress

Every natural-language message MUST pass through one Aegis-owned ingress service before Hermes.

The service MUST:

- identify the content source;
- enforce byte, rune, and processing bounds;
- apply deterministic high-confidence credential detection;
- return metadata-only findings;
- block and discard secret-bearing ordinary input;
- direct the principal to protected intake;
- avoid forwarding redacted remnants;
- avoid automatically storing pasted values;
- avoid model-based classification as an authorization dependency.

The same content policy MUST be reusable for future non-terminal sources, but those sources are outside this feature.

## 12. Protected credential operations

### 12.1 Shared services

Interactive manager operations and explicit secret subcommands MUST call shared application services. The manager MUST NOT invoke its own CLI as a subprocess.

### 12.2 Create

The flow MUST be:

1. model proposes non-secret metadata;
2. Aegis validates the proposal;
3. Aegis renders a deterministic preview;
4. the principal confirms;
5. Aegis enters no-echo input;
6. the principal enters and confirms the value;
7. Aegis stores it through the encrypted authority;
8. Aegis returns metadata only.

### 12.3 Rotate

Rotation MUST resolve one exact existing record, preview version effects, obtain confirmation, collect a new value through protected intake, create a new encrypted version, and return metadata only.

### 12.4 Revoke

Revocation MUST preview the exact reference/version and known authority impact, require principal confirmation, execute deterministic revocation, and return metadata only.

### 12.5 Terminal guarantees

Protected intake MUST:

- disable echo before reading;
- use bounded mutable byte buffers where practical;
- avoid values in strings where practical;
- reject oversized and mismatched input;
- restore terminal mode on success, mismatch, error, EOF, cancellation, panic boundary, and signal;
- clear/release buffers best-effort;
- never echo or log the value.

Aegis MUST not claim perfect process-memory zeroization.

## 13. Conversation turn algorithm

For each natural-language turn, Aegis MUST perform this exact logical sequence:

1. reject if the session is inactive, expired, or cleaning up;
2. read bounded terminal input;
3. consume local deterministic slash commands before Hermes;
4. apply the ingress guard;
5. submit the accepted turn through Hermes gateway;
6. authenticate and inspect the serialized proxy request;
7. call the exact local model;
8. inspect the complete model response;
9. assemble the complete gateway response;
10. strictly decode one manager envelope;
11. render bounded non-authoritative message text;
12. validate any typed proposal;
13. render deterministic preview;
14. obtain required operator confirmation outside the model;
15. execute through a shared service;
16. emit authoritative audit metadata;
17. return a metadata-only operation result to the conversation if needed;
18. continue or close.

No error path may skip cleanup or disclose guarded content.

## 14. Degraded mode

If Ollama, the exact model, certification, Hermes, or the proxy is unavailable, Aegis MUST degrade honestly.

It MUST:

- state that conversational local inference is unavailable;
- emit a stable metadata-only reason code;
- provide deterministic manager commands;
- retain protected intake and metadata operations where safe;
- avoid cloud fallback;
- avoid selecting another model;
- avoid claiming that a conversational session started;
- avoid weakening any guard.

The deterministic manager is a supported fallback, not evidence that the end-to-end conversational feature is complete.

## 15. Cleanup state machine

Cleanup MUST run for:

- `/quit`;
- `/exit`;
- typed `session.exit`;
- EOF;
- Ctrl-C or termination;
- session expiry;
- revocation;
- startup failure at every stage;
- Hermes exit or protocol failure;
- proxy failure;
- Ollama failure;
- protected-intake interruption.

The cleanup order MUST be:

1. mark session closing and reject new turns;
2. cancel pending gateway/model work;
3. cancel pending proposal/intake work and restore terminal mode;
4. terminate Hermes gracefully;
5. force-kill the Hermes process group after a bounded deadline;
6. close the inference proxy;
7. request exact-model unload;
8. stop managed Ollama if Aegis started it;
9. remove disposable Hermes/runtime state;
10. invalidate and release capability material;
11. finalize one metadata-only receipt;
12. return a stable exit result.

Every cleanup operation MUST be safe to call more than once and after partial startup.

## 16. Audit and receipts

Authoritative audit and the final receipt MAY contain:

- authenticated principal ID;
- built-in manager identity and revision;
- route-plan digest;
- Hermes version;
- exact model digest;
- certification digest;
- session start/end and outcome;
- operation kind;
- record/reference identifiers;
- confirmation outcome;
- stable reason/decision code;
- cleanup outcomes.

They MUST NOT contain:

- secret values;
- protected-input buffers;
- blocked prompt bodies;
- provider request/response bodies;
- complete model messages unless separately proven safe and required;
- bearer capabilities;
- ambient environment values;
- credential-bearing errors;
- plaintext temporary artifacts.

## 17. Model conformance

The candidate registry MUST remain official, traceable, and without an uncertified default.

Certification MUST bind:

- exact candidate ID;
- exact Ollama artifact name and digest;
- quantization/context identity where available;
- Hermes version;
- manager instruction digest;
- response schema version;
- conformance corpus digest;
- certification timestamp and result.

Every critical corpus case MUST pass. Certification MUST fail on omitted, duplicate, stale, or failed critical cases.

Normal automated tests MUST use fake local services and MUST NOT download models. A real certification MAY run only against an already-installed approved candidate. If no candidate is installed, the implementation MUST report that exact external prerequisite and MUST NOT fabricate certification.

## 18. Implementation work packages

Work MUST proceed in dependency order.

### E0 — Baseline and preservation

- Read all authority documents.
- Inspect current status, diffs, tests, and relevant symbols.
- Preserve unrelated work.
- Identify which existing manager components are wired only in tests.
- Record exact locally available Hermes/Ollama prerequisites without changing them.

Gate: no edits until existing contracts and usages are traced.

### E1 — Shared manager services and orchestrator

- Extract/reuse shared credential operation services.
- Implement orchestrator state and cleanup ownership.
- Connect bare interactive dispatch after preflight.
- Preserve deterministic fallback and non-TTY behavior.

Gate: fake dependencies can drive a complete list/inspect/create/rotate/revoke lifecycle without Hermes/Ollama.

### E2 — Ollama supervision and immutable route

- Complete managed/external-local adapters.
- Canonicalize route identity.
- Verify exact digest/certification.
- Implement bounded readiness, load, idle, unload, and shutdown.

Gate: fake Ollama tests prove no alternate endpoint/model/fallback and complete cleanup.

### E3 — Proxy enforcement

- Complete session capability and route binding.
- Strictly validate allowed request/response protocol.
- Integrate exact-request and complete-response guards.
- Resolve actual Hermes header compatibility without weakening binding.

Gate: unauthorized, drifted, alternate, malformed, secret-bearing, expired, and replayed requests fail closed.

### E4 — Hermes gateway orchestration

- Launch disposable safe-mode Hermes.
- Connect stdio gateway.
- Implement multi-turn state machine.
- Submit immutable manager instruction.
- Assemble and decode complete responses.

Gate: a fake Hermes process completes multiple turns and every protocol/process failure cleans up.

### E5 — Typed proposals and protected mutations

- Wire closed response envelopes.
- Authorize proposals deterministically.
- Integrate previews, confirmation, and no-echo intake.
- Feed metadata-only operation results back into the session.

Gate: all required operations execute end to end with no model-visible secret value.

### E6 — Conformance, adversarial proof, and launch assets

- Implement certification runner and persistence.
- Add random-canary tests.
- Run full local verification.
- Exercise documented workflows.
- Review/update all launch assets.

Gate: every locally actionable Definition of Done item has real evidence; external prerequisites are named precisely.

## 19. Required tests

### 19.1 Unit tests

- route canonicalization and digest stability;
- unknown/missing/duplicate route input rejection;
- exact model identity and certification drift;
- response envelope duplicate-key/trailing/unknown rejection;
- each typed proposal codec;
- proposal authorization and ambiguity denial;
- ingress scanner bounds, findings, panic, timeout, and error;
- protected intake state transitions;
- metadata-safe result and receipt codecs;
- cleanup idempotence.

### 19.2 Integration tests

Using fake local processes/services:

- successful startup order;
- failure and rollback at every startup stage;
- managed Ollama readiness and shutdown;
- external loopback Ollama acceptance;
- non-loopback and redirect rejection;
- exact model/digest enforcement;
- proxy capability and session binding;
- request/response guard enforcement;
- Hermes readiness/session/create/prompt/multi-turn/complete;
- malformed/oversized/timed-out gateway behavior;
- list/search/inspect/create/rotate/revoke/audit/help/exit;
- confirmation decline;
- protected-intake cancellation and mismatch;
- expiry and revocation;
- model unload;
- disposable-home removal;
- terminal restoration;
- receipt finalization.

### 19.3 Adversarial tests

- high-confidence secret paste before Hermes;
- secret introduced in history/template before Ollama;
- secret emitted by model before terminal/reuse;
- hidden or multiple proposals;
- fabricated success;
- model-selected model/provider/context;
- shell/file/MCP/plugin/profile/provision attempts;
- prompt injection in stored metadata;
- wrong/stale/replayed proxy capability;
- same-host unauthenticated proxy client;
- giant input and resource exhaustion bounds;
- crash/EOF/Ctrl-C at each lifecycle stage.

### 19.4 Random-canary invariant

An end-to-end test MUST generate a new credential-shaped canary, enter it only through protected intake, perform credential operations, close the session, and assert plaintext absence from:

- Hermes stdin/stdout/stderr captures;
- gateway event captures;
- proxy request/response captures;
- fake Ollama requests;
- model messages;
- transcripts and state;
- logs and errors;
- audit and receipts;
- stdout/stderr;
- argv and environment captures;
- database metadata/plaintext scans;
- temporary files;
- disposable runtime homes after cleanup.

Tests MUST never use real credentials.

## 20. Verification requirements

The implementation session MUST run:

- focused tests during development;
- shell syntax and release regression tests;
- Go formatting;
- Go build;
- complete Go tests;
- race tests for affected concurrent/lifecycle code, preferably all packages;
- `go vet`;
- configured vulnerability scanning;
- documented no-key workflows that are locally safe;
- targeted lifecycle and canary tests.

Default verification MUST NOT:

- access cloud inference;
- download models;
- read real credentials;
- alter normal Hermes/Ollama state;
- replace the installed Aegis executable;
- publish or mutate remote repository state.

If the environment requires a runtime-recognized ad-hoc verifier, it MUST use an OS-safe `/tmp/hermes-verify-*` path, run the targeted changed behavior, clean itself up, and be reported as ad-hoc rather than canonical suite evidence.

A real Hermes/Ollama result MUST be reported only if it was actually executed. Missing installed models or unsupported upstream behavior MUST be reported as concrete blockers.

## 21. Launch-asset impact review

Before completion, inspect every asset required by `AGENTS.md`:

- root `README.md`;
- `LICENSE`;
- `SECURITY.md`;
- `CONTRIBUTING.md`;
- `CODE_OF_CONDUCT.md`;
- `CHANGELOG.md`;
- threat model;
- architecture diagram;
- five-minute quickstart;
- no-key demonstration;
- terminal recording;
- release binaries/checksums/workflow material;
- contributor issue material.

Update each affected asset in the same change. Report unaffected assets explicitly. Do not touch unrelated assets merely to claim review.

Do not fabricate command output, recordings, certifications, model results, release artifacts, checksums, releases, or issue links.

## 22. Definition of Done

The feature is complete only when every locally actionable statement below is true.

### 22.1 User experience

- Bare interactive `aegis` starts the real manager when prerequisites are valid.
- Bare non-TTY behavior remains noninteractive and compatible.
- The UI names Aegis, Hermes, Ollama, the exact model digest, the built-in context, local-only routing, and no-fallback policy.
- Required read and mutation operations complete end to end.
- Degraded mode is honest and deterministic.

### 22.2 Authority

- The principal authenticates outside the model.
- The model only proposes.
- Aegis authorizes, previews, confirms, executes, audits, and reports.
- Prompt content cannot change security context, route, model, provider, runtime, or permissions.

### 22.3 Secret boundary

- Protected secret values never enter Hermes/Ollama/model paths.
- High-confidence ordinary secret paste is blocked before Hermes.
- Exact serialized requests and complete model responses are guarded.
- Secret values are absent from retained and observable non-secret surfaces.
- Scanner/proxy/codec failure denies.

### 22.4 Runtime

- Hermes runs in safe mode with disposable isolated state and no ambient extensions/credentials.
- Hermes reaches Ollama only through the session proxy.
- The route permits exactly one local pinned model/digest.
- No cloud fallback or model switching exists.
- Exit/failure invalidates capabilities, stops Hermes/proxy, unloads the model, stops managed Ollama, and removes disposable state.

### 22.5 Protocol

- Every model response is one valid closed envelope.
- Every proposal uses a closed typed codec.
- Unknown, malformed, ambiguous, expired, revoked, drifted, replayed, or unsupported input fails closed.
- Model narration never becomes authoritative state.

### 22.6 Evidence

- Hermetic unit/integration/adversarial tests pass.
- The random-canary invariant passes.
- Build, race, vet, and vulnerability checks have real output.
- Every locally runnable documented workflow is exercised.
- Live certification is either genuinely passed or identified as blocked by an exact missing prerequisite.
- Launch assets match only implemented and tested behavior.

## 23. `/loop` execution contract

A `/loop` implementation session using this file MUST:

1. run `cat specs/BASE_MANAGER_END_TO_END.md` and read the entire file before editing;
2. read the higher-authority documents and parent manager specification;
3. inspect and preserve the current working tree;
4. trace existing symbols and usages before adding interfaces;
5. work through E0–E6 in dependency order;
6. reuse shared application services rather than shelling out to Aegis;
7. add tests with each behavior change;
8. use fake local services for default tests;
9. avoid model downloads and external state changes without explicit approval;
10. preserve fail-closed behavior when integration is blocked;
11. continue until every locally actionable Definition of Done item has real evidence;
12. avoid stopping at analysis, plans, scaffolding, TODOs, unused components, or fixture-only certification claims;
13. perform the launch-asset review;
14. not commit, tag, push, publish, release, or create remote issues without explicit operator authorization.

If supported Hermes or Ollama behavior makes a requirement impossible, the session MUST produce a minimal reproducible finding, preserve the stronger boundary, update the blocker documentation, and finish every unaffected requirement. It MUST NOT substitute direct TTY pass-through, one-shot mode, cloud fallback, prompt-only restrictions, or fabricated output.
