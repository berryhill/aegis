# Aegis manager session, Ollama lifecycle, and security-model report

- Status: Research complete; implementation not yet started
- Date: 2026-07-17
- Scope: the built-in principal-facing Aegis manager, pre-Hermes secret protection, local model candidates, Ollama lifecycle and security, and the work required for the bare `aegis` command to become a complete product experience
- Canonical command: `aegis`. The spelling `aegus` is treated as a typo unless the project deliberately adds an alias.
- Primary decision: build the principal-facing Aegis manager first. Use Ollama for the local conversational model, but keep authorization and high-confidence secret detection in deterministic Aegis code. Keep the Ollama service available and unload large model weights after a short idle period or immediately when the Aegis session closes.

## Executive decision

The first complete Aegis experience should be:

```text
$ aegis

Aegis authenticates the configured principal
  -> initializes the installation if needed
  -> resolves the built-in secrets-manager security context
  -> verifies a pinned local Ollama model and endpoint
  -> starts one attached Hermes conversation
  -> guards every model-bound message
  -> exposes only typed Aegis management operations
  -> collects secret values through protected no-echo intake
  -> audits deterministic outcomes without secret values
  -> unloads the conversational model when the session closes
```

This requires three distinct decision layers:

1. **Deterministic Aegis policy and secret detection** — authoritative and always available.
2. **Optional compact discriminative classifiers** — local, advisory, and only allowed to make a decision more restrictive.
3. **A local conversational model through Hermes** — useful for intent, explanation, clarification, and typed tool proposals; never an authority and normally never a recipient of reusable secret plaintext.

There is no mature public fine-tuned model that can safely answer the general question “does this arbitrary message contain a reusable credential?” across all credential formats. Public specialist models are useful for narrower tasks such as prompt-injection detection or PII tagging. None should replace provider-specific rules, structured-field policy, protected secret intake, or exact-byte egress enforcement.

Ollama is a good first workstation runtime because it provides a local API, model management, tool calling, structured output, and explicit load/unload controls. It is not itself a security sandbox. Its local API is unauthenticated, its model names/tags can be mutable, and current Ollama includes optional cloud functionality. Aegis must pin the exact local model artifact, require loopback/private routing, disable cloud functionality for the managed deployment, and enforce the session route independently of Hermes and Ollama.

## Research method and evidence limits

Primary sources inspected for this report include:

- current Ollama API, FAQ, OpenAI compatibility, and tool-calling documentation;
- the current Ollama model library and repository license;
- live Hermes Agent v0.18.2 documentation and its 2026-07-16 model catalog;
- official or publisher-provided Hugging Face metadata and model cards;
- the 2025 paper and repository for LLM-assisted hard-coded credential detection;
- the current Aegis command, Hermes adapter, secret command, and existing broader model-routing research.

The environment did not expose a subagent orchestration tool. The existing broad Aegis report records that four parallel Hermes one-shot research workers were attempted and all failed before returning usable reports because of stale-stream/provider failures. This focused investigation therefore parallelized primary-source retrieval directly. No successful subagent result is claimed.

Download counts reported by the Hugging Face API are volatile adoption signals, not model quality evidence. Vendor benchmarks and model-card evaluations are not accepted as Aegis acceptance results. Every selected artifact, quantization, context size, prompt template, Ollama version, and Hermes version must pass an Aegis-owned local conformance suite.

## Product boundary: the built-in Aegis manager

### What the base agent is

The base agent is a built-in, principal-facing Aegis manager:

```text
logical identity: aegis
security context: secrets-manager
principal: authenticated configured operator
runtime: Hermes Agent
inference: pinned local Ollama model
cloud fallback: forbidden
model switching: forbidden
secret plaintext in model context: forbidden by normal workflow
```

It is lateral to user-created agents. It is not a user-editable Hermes `default` profile, and it does not derive foundational authority from a prompt or model-generated charter. Aegis owns its operations and policy; Hermes supplies the conversational loop.

### Initial supported jobs

The first manager should support:

- initialize local encrypted credential storage;
- create secret metadata and begin protected value intake;
- list, search, inspect, tag, and organize secret records;
- rotate and revoke records or versions;
- show metadata-only history and authoritative audit events;
- propose exact credential bindings;
- explain what a proposed binding permits;
- deny plaintext reveal;
- block likely secret values pasted into ordinary conversation;
- function through deterministic CLI commands if local inference is unavailable.

It should not initially support:

- arbitrary shell or filesystem access;
- arbitrary MCP servers, plugins, or user-installed skills;
- cloud model fallback;
- in-session model switching;
- generic credential reveal;
- autonomous approval;
- self-modification or self-provisioning;
- fleet administration;
- unrestricted downstream credential use.

### Future wrapped agents

Future user-created agents should run behind the same Aegis ingress, route, capability, broker, and audit controls. They should not “own their keys” in the sense of receiving reusable plaintext. They may request operations through bindings that identify one logical agent, one trust stanza, one deployment, one credential scope, permitted operations, destinations/resources, and a version policy.

The built-in manager remains the place where the principal creates, organizes, rotates, revokes, and binds credentials. A wrapped agent receives typed broker capabilities, not secret-administration authority by default.

## Correct message path

The desired “is there a secret in this message?” guard must not be a single model call.

```text
operator input
  -> bounded canonicalization
  -> deterministic structured and high-confidence secret rules
       -> block and discard if high confidence
       -> offer protected intake without retaining the pasted value
  -> optional local ambiguity classifier
       -> may block, quarantine, or force local-only handling
       -> may never downgrade a deterministic block
  -> Aegis route-policy check
  -> exact Hermes/model request construction
  -> exact serialized egress scan
  -> pinned local model route
```

Protected secret intake is a separate path:

```text
no-echo terminal input
  -> deterministic Aegis intake transaction
  -> immediate encryption
  -> credential authority
  -> metadata-only result returned to the conversation
```

Secret bytes must not be sent through Hermes tool arguments merely because the selected model is local. Local inference reduces external disclosure but does not remove model context, runtime logs, prompt caches, transcripts, crash dumps, malformed tool calls, or compromised-process risk.

## Fine-tuned model landscape

### Finding: no production-ready generic credential classifier was identified

Live Hugging Face searches for “secret detection,” “credential detection,” and “api key detection” did not return a mature, broadly adopted, clearly licensed general credential classifier. Searches did return narrow prompt-injection and PII models.

A 2025 paper, *Detecting Hard-Coded Credentials in Software Repositories via LLMs* (arXiv:2506.13090), evaluates contextual model embeddings plus a classifier on the CredData benchmark and reports a 13% F1 improvement over the compared state of the art. Its own limitations include modest benchmark size, uneven category frequency, possible failure on novel credential forms, and the need to complement ML with other controls. It addresses source-repository observations, not arbitrary interactive messages or an authorization decision. It is useful research evidence for a future Aegis-specific classifier, not a drop-in security boundary.

### Candidate specialist models

| Candidate | Task | Size/type | License/access | Evidence and limitations | Recommended Aegis role |
|---|---|---|---|---|---|
| Meta Llama Prompt Guard 2 22M | Prompt attack/jailbreak input classification | 22M mDeBERTa text classifier | Gated; Meta license (`other` in HF metadata) | Eight languages listed. Official repository says it guards LLM inputs against prompt attacks and jailbreaks. It is not credential detection. | Optional prompt-injection signal after license and local evaluation; never a secret detector or authority. |
| Meta Llama Prompt Guard 2 86M | Same task with larger classifier | 86M mDeBERTa text classifier | Gated; Meta license | More downloaded than the 22M variant in live metadata. Same task mismatch. | Optional higher-quality candidate if measured benefit justifies it. |
| Protect AI DeBERTa-v3 small prompt-injection v2 | English prompt-injection classification | Fine-tuned DeBERTa-v3-small; ONNX and safetensors metadata | Apache-2.0; gated automatically in current metadata | Model family is explicitly prompt injection, not secrets. Small card retrieval was gated during this investigation. | Evaluation candidate only. Prefer direct ONNX inference, not Ollama. |
| Protect AI DeBERTa-v3 base prompt-injection v2 | English prompt-injection classification | Fine-tuned DeBERTa-v3-base | Apache-2.0 | Card says benign/injection labels; explicitly does not detect jailbreaks or non-English prompts and warns of system-prompt false positives. No populated results were present in its model-index metadata. | Optional prompt-injection signal; not sufficient alone. |
| Gravitee BERT-small PII detection | English token-level PII tagging | BERT-small token classifier; ONNX/safetensors | Apache-2.0 | Labels include password, credit card, SSN, address, and related PII. Card reports F1 0.8686, precision 0.8182, recall 0.9256 on its evaluation and documents domain/language drift. API tokens and private keys are not its core task. | Optional PII feature after Aegis evaluation; not a credential boundary. |
| Llama Guard 3 1B/8B | General content-safety classification | Generative guard model | Llama license applies | Ollama library supports input/output safety categories in eight languages. It is much larger than necessary and its taxonomy is harm safety, not credential leakage. | Reject for inline secret detection; potentially separate content-safety feature later. |
| ShieldGemma 2B/9B/27B | General harm-policy classification | Generative instruction-tuned guard | Gemma terms apply | Four broad harm categories; not prompt injection or credential detection. | Reject for this feature. |

### Why prompt-injection, PII, and secret detection remain separate

These are independent classifications:

```text
benign message + live API token      -> secret leak, no prompt injection
malicious prompt + no sensitive data -> prompt injection, no secret
home address                         -> PII, not necessarily a reusable credential
private key                          -> reusable credential, may not be recognized as ordinary PII
```

Aegis should preserve separate detector identifiers and policy outcomes rather than emit one vague `unsafe` label.

### Recommended classifier runtime

Ollama is optimized around generative model APIs and GGUF-style local serving. The strongest tiny candidates above are discriminative BERT/DeBERTa classifiers with ONNX or Transformers artifacts. Running them through Ollama would add conversion and semantic uncertainty without a useful benefit.

Recommended future implementation:

```text
Aegis Go process
  -> deterministic detectors in Go
  -> optional isolated ONNX classifier worker
       - pinned model digest
       - no network
       - no provider credentials
       - no prompt logging
       - bounded input and deadline
       - label/score output only
```

The classifier may stay resident because a 22M–100M-class classifier has a materially smaller footprint and avoids per-message cold starts. That is a performance choice to make after measurement. The large conversational model should not stay resident indefinitely.

### Fine-tuning recommendation

Do not fine-tune a security classifier before building the deterministic baseline and evaluation corpus.

A future Aegis classifier could use MiniLM, DistilBERT, ModernBERT, or DeBERTa-class encoders and should classify spans/context into explicit labels such as:

```text
no_sensitive_candidate
known_structured_credential
possible_generic_password
possible_api_token
private_key_material
pii_only
synthetic_or_documentation_example
ambiguous_sensitive_content
```

Required corpus properties:

- synthetic and revoked credentials only;
- provider-specific formats and nearby context;
- generic passwords and assignment statements;
- UUIDs, hashes, package locks, public keys, fixtures, and generated code as hard negatives;
- split, encoded, Unicode, and structured-message cases;
- source provenance and content-type labels;
- held-out provider families and time-based drift tests;
- no retained production plaintext.

Deployment requires threshold calibration at a fixed false-negative target. The classifier cannot downgrade a deterministic block or grant a cloud route.

## Conversational model candidates

The Aegis manager needs a small instruction model with reliable tool calls and conversation, not a “security model.” Security comes from Aegis policy and capabilities.

### Shortlist

| Candidate | Evidence | Advantages | Risks | Recommendation |
|---|---|---|---|---|
| Qwen3-4B-Instruct-2507 | Official card: Apache-2.0, 4.0B parameters, non-thinking, native 262K context, improved tool usage, local Ollama support | Text-only, mature, explicit agent/tool guidance, no hidden thinking stream | Exact Ollama artifact/template and 64K behavior still require testing | Strong conservative baseline. |
| Qwen3.5 4B | Official metadata/card: Apache-2.0, 4B, multimodal, tool benchmark section; Ollama library advertises tools/thinking and 4B tag | Newer capabilities and tool support; likely fits this host at quantized weight size with constrained context | Multimodal complexity is unnecessary; current card recommends very large context for full thinking behavior; exact Hermes tool reliability unverified | Primary evaluation candidate, not automatic production selection. |
| Qwen3.5 2B | Apache-2.0, 2B; Ollama library advertises a 2B tag with tools/thinking | Lower memory and cold-load cost | Official card describes this size as suited to prototyping/task-specific fine-tuning; likely lower tool reliability | Evaluate as low-resource/fallback candidate. |
| Qwen3 4B | Ollama library advertises tools and 4B; official Qwen family has agent/tool claims | Established Ollama integration and broad adoption | Thinking/template behavior can complicate deterministic tool loops | Secondary baseline. |
| Granite 3.3 2B | Ollama library advertises tools; IBM family is instruction-tuned | Small and simple alternative | Older and must be measured against Qwen on Aegis-specific tasks | Benchmark comparison, not default. |

Community fine-tunes with tiny download counts, unclear licenses, ablation/“abliterated” safety modifications, or missing model cards should not be selected for the built-in security manager. In particular, the locally installed `lukey03/Qwen3.5-9B-abliterated:latest` is inappropriate for this role. A security manager should use a first-party or strongly traceable artifact with intact alignment, explicit licensing, and a pinned digest.

### Host-specific observation

The inspected development host has:

- Intel Core i9-13900H, 20 logical CPUs;
- 30 GiB RAM;
- NVIDIA RTX A1000 Laptop GPU with 6 GiB VRAM;
- Ollama 0.32.0 active on `127.0.0.1:11434`;
- no model currently loaded;
- two local 9B model artifacts, each roughly 5.6–6.6 GB on disk.

A quantized 4B model is a more suitable first benchmark than 9B on this 6 GiB GPU, especially because Hermes currently documents a 64K minimum context for agent use. Context/KV memory, not only weight size, must be measured. A 2B model may be necessary if 4B at the required context cannot deliver acceptable latency and tool-call reliability.

No model was downloaded or loaded during this research. Model adoption should be a deliberate implementation/evaluation action because it consumes multiple gigabytes and modifies host runtime state.

## Ollama assessment

### Why Ollama is a good first runtime

Verified current capabilities include:

- local HTTP API at port 11434;
- OpenAI-compatible API support usable by Hermes as a custom endpoint;
- native chat and generate APIs;
- function/tool definitions and multi-turn tool calling;
- JSON and JSON-schema structured outputs;
- model discovery and model details;
- explicit context configuration;
- explicit load, keep-alive, stop, and unload behavior;
- broad quantized workstation model availability;
- MIT-licensed Ollama server code.

Hermes documentation explicitly supports local Ollama through:

```text
provider: custom
base URL: http://localhost:11434/v1
API key: none
```

Hermes also warns that local tool behavior depends on the model/server combination and that Ollama context defaults can be too small for reliable Hermes tool use.

### Should models run all the time?

No—not the large conversational model.

Separate the daemon from resident model weights:

```text
Ollama service process: may remain running
model files: remain on disk
model weights/KV cache: load on first Aegis request
model weights/KV cache: unload after idle or session close
```

Ollama’s current FAQ states:

- default model keep-alive is five minutes;
- `keep_alive` accepts a duration, seconds, a negative value for indefinite residency, or `0` for immediate unload;
- `ollama stop <model>` unloads a model;
- an empty native API request can preload a model;
- a per-request `keep_alive` overrides `OLLAMA_KEEP_ALIVE`.

Recommended Aegis policy:

```text
interactive session starts:
  preflight endpoint and pinned digest
  preload conversational model
  use a short keep-alive, initially 5m

session active:
  ordinary requests refresh residency

session idle:
  allow Ollama to unload after 5m
  accept cold-start latency on the next message

session exits/revokes/expires:
  explicitly request keep_alive: 0 or invoke an equivalent controlled unload
  verify the model disappears from Ollama's running-model list
```

Do not use indefinite keep-alive for a personal secrets manager unless measurement shows that cold-load delay is unacceptable and the operator explicitly prefers latency over memory reclamation.

The daemon itself can stay active because it is much cheaper than keeping model weights resident and lets `aegis` start without service-management privileges. A future embedded/single-user mode may start and stop a dedicated Ollama instance, but that increases lifecycle, upgrade, and crash-recovery complexity.

### Concurrent sessions

Ollama can load multiple models or process parallel requests when memory permits. Its FAQ says insufficient-memory model requests queue until prior models unload, and parallel requests increase context memory. Current server knobs include:

- `OLLAMA_MAX_LOADED_MODELS`;
- `OLLAMA_NUM_PARALLEL`;
- `OLLAMA_MAX_QUEUE`;
- `OLLAMA_CONTEXT_LENGTH`;
- `OLLAMA_KV_CACHE_TYPE` with Flash Attention support.

For the first personal Aegis manager:

```text
max loaded models: 1
parallel requests per model: 1
bounded queue: small
context: explicit and certified
```

This avoids two model copies or parallel KV allocations exhausting a 6 GiB GPU. Multi-agent scheduling should be deferred until the base manager is reliable.

### Cold-start trade-off

Immediate unload after every turn minimizes residency but gives poor conversation latency because every user message reloads the model. Indefinite residency wastes GPU/RAM while the manager is unused. A five-minute idle timer is a sensible initial compromise.

The acceptance suite should measure:

- cold load time;
- warm time-to-first-token;
- tokens per second;
- memory at 8K, 32K, and the Hermes-required context;
- tool-call validity;
- unload completion time;
- memory reclaimed after unload;
- behavior after model or daemon crash.

### Ollama security posture

Ollama is a runtime, not a secret boundary.

Verified current facts:

- the native API specification declares `security: []`;
- the server binds `127.0.0.1:11434` by default;
- network exposure is controlled through `OLLAMA_HOST`;
- current Ollama includes optional cloud models and web search;
- local-only operation can be requested with `disable_ollama_cloud: true` in the server configuration or `OLLAMA_NO_CLOUD=1`;
- local model files are stored under Ollama’s model directory;
- model tags can name cloud-capable variants and should not be treated as proof of locality.

Required Aegis controls:

1. Require loopback or an explicitly approved private authenticated endpoint.
2. Reject `ollama-cloud`, cloud model tags, and public endpoint resolution for the built-in manager.
3. Require Ollama cloud functionality to be disabled for an Aegis-managed local-only deployment and verify the effective state.
4. Pin Ollama version and the exact model digest; do not approve a mutable `latest` tag alone.
5. Record the digest, quantization/details, context, template, Ollama version, Hermes version, and route-policy digest in session receipts.
6. Do not expose the Ollama port on `0.0.0.0` for the personal manager.
7. Keep browser origins narrow; do not add wildcard extension origins.
8. Do not assume localhost authenticates a caller. Other same-host processes can normally call an unauthenticated loopback API.
9. Use Aegis-owned session policy and, where feasible, a dedicated service identity/network namespace or authenticated Aegis inference proxy.
10. Verify logging behavior empirically and ensure prompts are absent from Aegis audit/general logs.
11. Never store upstream/cloud provider credentials in the local manager’s Hermes environment.
12. Deny Hermes fallback and auxiliary `auto` routes; pin every auxiliary route or disable the feature.

Ollama documentation states that locally run prompts are not sent to ollama.com. That does not prove that no compromised local process, plugin, browser extension, log configuration, or cloud-tagged model can disclose data. Aegis’s enforcement remains necessary.

### Context compatibility with Hermes

Current Hermes documentation says:

- local Ollama is configured as an OpenAI-compatible custom endpoint;
- Ollama tool calling is enabled by default when the model supports it;
- Hermes requires at least 64,000 context tokens for agent use with tools;
- smaller contexts are rejected because system prompts and tool schemas consume substantial space;
- explicit `context_length`/Ollama context configuration is necessary.

This creates a real MVP risk: a tiny model that fits easily at 4K may not fit or remain reliable at 64K on supported workstation hardware.

The first manager should minimize Hermes tool schemas and disable all unrelated toolsets, but it still must satisfy Hermes’s supported public contract rather than patching around the minimum silently. If no 2B–4B model can meet the 64K requirement with acceptable latency on target hardware, options are:

1. improve the Hermes/Aegis narrow-tool integration through a supported lower-overhead protocol;
2. use CPU/GPU hybrid inference with more system RAM and accept slower responses;
3. document a higher minimum hardware tier;
4. use a separate compact conversational loop rather than claiming a full Hermes session—this is less preferred because Hermes visibility is a product invariant.

The choice must be made from measured conformance, not estimated fit.

## Required `aegis` command behavior

### Current state

The current root command has subcommands but no bare-command manager action. `aegis` therefore renders command help rather than starting the built-in manager.

The existing Hermes adapter launches managed sessions with stdin/stdout/stderr pipes, then discards both stdout and stderr. It records lifecycle but provides no attached human conversation. A separate design foreground path attaches Hermes directly, but direct attachment would not by itself give Aegis a pre-Hermes message guard or typed protected intake transition.

The current secret CLI already provides useful foundations:

- principal authentication;
- no-echo value and confirmation prompts;
- bounded intake;
- independently encrypted version creation/rotation;
- metadata-only inspection;
- revocation and exact binding records;
- audit operations.

The full manager should reuse these application services rather than asking a model to reproduce credential logic.

### Bare-command state machine

```text
aegis
  -> load strict configuration
  -> if uninitialized: run authenticated initialization wizard
  -> authenticate principal
  -> verify encrypted authority and key custody
  -> discover supported Hermes version
  -> verify Ollama endpoint is local-only and cloud-disabled
  -> resolve exact model digest and certified configuration
  -> build immutable secrets-manager route plan
  -> start Aegis-owned terminal UI
  -> start isolated Hermes protocol session behind the UI
  -> guard every user/model/tool boundary
  -> execute typed proposals only after deterministic validation/confirmation
  -> terminate Hermes and ephemeral home
  -> unload Ollama model
  -> close audit/session receipt
```

### Aegis-owned UI, not blind TTY pass-through

To detect a pasted secret before Hermes receives it, Aegis must own prompt input. Merely running an attached Hermes TUI transfers input directly to Hermes and prevents a reliable protected-intake interception layer.

Recommended architecture:

```text
Aegis terminal UI
  <-> Aegis ingress/security state machine
  <-> Hermes documented structured TUI-gateway stdio protocol
  <-> pinned local Ollama endpoint
```

When Hermes proposes `secret.begin_intake`, Aegis pauses conversational input, displays the deterministic no-echo prompt, encrypts the result, and returns metadata only. The conversational transcript contains an operation/result record but no value.

### Typed management operations

Initial model-visible operations should be exact and narrow:

```text
secret.list
secret.search
secret.metadata
secret.propose_create
secret.begin_intake
secret.propose_rotate
secret.propose_revoke
secret.propose_metadata_update
secret.propose_binding
secret.history
secret.audit
```

Mutation operations should produce proposals. Deterministic Aegis code authenticates, validates, confirms, executes, and audits them. Tool output must be treated as untrusted data and scanned before re-entering model context.

### Initialization UX

A complete first run should:

1. establish the configured local principal outside the model;
2. choose or verify Hermes explicitly;
3. discover local Ollama without exposing it on the network;
4. offer only vetted candidate models, with download size and license visible;
5. require explicit consent before downloading a multi-gigabyte artifact;
6. configure local-only/cloud-disabled operation;
7. initialize credential authority and explain key-custody trade-offs;
8. run model/session conformance tests;
9. create the built-in Aegis manager artifacts;
10. start the first manager session.

If initialization or inference fails, deterministic `aegis secret ...` operations remain available and the error must not trigger cloud fallback.

### Session banner

Every session should make the boundary visible:

```text
Aegis manager
Principal: <authenticated principal>
Runtime: Hermes Agent <version>
Inference: Ollama local / <model>@<digest>
Security context: secrets-manager
Cloud fallback: disabled
Model switching: disabled
Secret values visible to model: no by normal workflow
```

Do not claim “sandboxed” merely because Hermes uses a disposable home or Ollama binds loopback.

### Accidental paste behavior

For a high-confidence credential candidate:

```text
Aegis blocked a possible credential.
The message was not sent to Hermes and was not retained in the transcript.
Start protected intake instead? [Y/n]
```

The pasted bytes should be wiped/discarded, not silently imported. Protected intake should ask for the value again. Findings and audit should contain detector ID, action, byte-length bucket, and an irreversible scoped fingerprint only where safe—never the value.

### Model unavailable behavior

```text
The local Aegis management model is unavailable.
No cloud fallback was attempted.

Available deterministic commands:
  aegis secret put
  aegis secret metadata
  aegis secret rotate
  aegis secret revoke
  aegis audit verify
```

A broken model must not make secret storage unavailable or weaken policy.

## Implementation sequence

### Phase 0: decisions and contracts

- Confirm `aegis` as canonical spelling.
- Define built-in manager identity and immutable secrets-manager security context.
- Define the message/operation protocol between the Aegis UI and Hermes.
- Define route-plan and model-artifact identity fields.
- Define secret-finding labels and fail-closed outcomes.
- Define threat statement: reusable secret values normally reach no model; sensitive non-secret text may reach only an approved local model.

### Phase 1: make bare `aegis` useful without ML

- Add a bare-root `RunE` that dispatches initialization or manager start.
- Add initialization status and deterministic wizard.
- Build an Aegis-owned terminal input loop.
- Reuse current protected `readSecret` behavior through application services rather than shelling out.
- Add typed list/search/metadata/create/rotate/revoke/history operations.
- Add deterministic ingress scanning and accidental-paste blocking.
- Add metadata-only transcript/audit handling.
- Preserve all existing non-interactive commands.

Acceptance: a user can initialize, store, list, organize, rotate, revoke, and audit secrets without starting Hermes.

### Phase 2: Ollama and attached Hermes conversation

- Add strict Ollama endpoint/model configuration.
- Add `/api/tags`/details preflight and digest pinning.
- Verify cloud-disabled/local-only state where an Aegis-managed service configuration is used.
- Add preload/unload lifecycle control and running-model verification.
- Generate isolated Hermes configuration with custom endpoint, exact model, explicit context, no fallback, explicit/disabled auxiliary routes, no model switching, and only approved tool bridge.
- Implement the structured Hermes protocol attach path instead of discarding output.
- Add typed proposal/result correlation and cancellation.
- Add route and model metadata to authoritative receipts.

Acceptance: `aegis` starts a visible local-only Hermes conversation, successfully exercises every narrow operation, closes cleanly, unloads the model, and never exposes a canary value to Hermes/model/mock egress/log/audit.

### Phase 3: model certification

Benchmark exact candidates, initially:

- Qwen3-4B-Instruct-2507 quantized Ollama artifact;
- Qwen3.5 4B official Ollama artifact;
- Qwen3.5 2B official Ollama artifact;
- one non-Qwen 2B tool-capable baseline such as Granite 3.3 2B.

Test:

- exact JSON/tool schema validity;
- safe clarification;
- proposal versus execution behavior;
- refusal to request plaintext in chat;
- no fabricated success;
- untrusted metadata/prompt-injection resistance;
- multi-turn operation completion;
- 64K context startup and memory;
- cold/warm latency and unload;
- quantization-specific regressions.

Select a default only after results are reproducible. Pin the artifact digest, not only the family name.

### Phase 4: optional classifiers

- Add prompt-injection model evaluation separately from secret detection.
- Build the Aegis-specific credential/sensitivity corpus.
- Prototype a direct ONNX worker.
- Compare hybrid performance against deterministic-only baseline.
- Deploy only if it improves measured outcomes without weakening deterministic rules.

The first fully working `aegis` manager must not depend on this phase.

### Phase 5: wrapped user agents

Only after the personal manager is reliable:

- expose principal-approved credential binding proposals;
- wrap one user-created agent behind identical ingress and route policy;
- issue typed broker capabilities for one operation/destination;
- prove no reusable plaintext reaches the wrapped model;
- prove cross-stanza and unbound requests fail closed.

## Acceptance criteria for “the `aegis` command fully works”

### Functional

- Bare `aegis` initializes or starts the manager without requiring knowledge of internal subcommands.
- Existing subcommands remain scriptable and stable.
- Store, list, search, inspect, organize, rotate, revoke, bind-propose, and audit flows work end to end.
- The user can exit and restart without losing authoritative state.
- Model outage degrades to deterministic CLI rather than blocking administration.

### Security

- The principal is authenticated outside the model.
- Every manager session has exactly one built-in security context and one immutable local route.
- No cloud provider, cloud Ollama model, fallback, auxiliary auto-route, or model switch can execute.
- Protected-intake canaries never appear in Hermes input, model server requests, transcripts, logs, errors, audit events, or receipts.
- Known high-confidence credentials pasted into chat are blocked before Hermes.
- Detector failure cannot result in allow.
- Models cannot directly invoke shell, file, MCP, plugin, profile, or provisioning authority.
- Secret mutation requires deterministic validation and appropriate confirmation.
- Ollama is loopback/private, cloud-disabled for managed local-only use, and exact artifacts are pinned.
- Session close terminates Hermes, removes disposable state according to policy, unloads Ollama weights, and records a receipt.

### Model/runtime

- Hermes supported-version discovery passes.
- The exact model/quant/template supports Hermes tool calls at the configured context.
- Conformance tests pass at declared hardware minimums.
- Cold-load and warm latency meet documented targets.
- Memory pressure and queue overload produce bounded errors, not fallback.
- Artifact/tag drift causes preflight failure or explicit re-certification.

### Audit

- Events reconstruct principal, manager identity, security context, operation, secret record ID, session, Hermes version, Ollama version, model digest, route digest, and result.
- No raw prompt, response, credential value, authorization header, low-entropy reversible hash, or protected-intake buffer is recorded.
- Audit verification remains usable when Ollama and Hermes are stopped.

## Testing plan

### Unit tests

- deterministic detector formats, spans, Unicode, and bounded decoding;
- deterministic blocks cannot be downgraded;
- local-only route rejects public/cloud identifiers;
- route/model digest determinism;
- model tag/digest mismatch;
- protected-intake buffers are bounded and wiped where Go permits;
- proposal validation and confirmation requirements;
- safe metadata/log serialization.

### Integration tests

- fake Ollama endpoint: discovery, preload, chat, tool call, unload, timeout, digest drift, and cloud-tag rejection;
- Hermes structured protocol: startup, attached conversation, tool proposals, cancellation, malformed output, and process death;
- protected intake with pseudo-terminal/no echo;
- mock model server asserts canary absence;
- root first-run and returning-run workflows;
- deterministic CLI fallback with Ollama stopped;
- session expiry/revocation terminates runtime and unloads model.

### Adversarial tests

- provider tokens, private keys, authorization headers, JWTs, connection strings, generic passwords, and split tokens;
- Base64, hex, percent encoding, Unicode confusables, JSON-field splitting, and tool-result reflection;
- prompt injection in secret names, tags, model output, and tool results;
- attempts to invoke hidden tools, change model, enable fallback, or call Ollama cloud;
- direct Ollama API bypass attempts under the declared host threat model;
- malicious/mutable model tag and altered Modelfile/template;
- giant input, regex worst cases, queue exhaustion, model OOM, and daemon crash.

## Risks and explicit non-claims

- Ollama loopback binding is not same-process authentication or a sandbox.
- A disposable Hermes home is state isolation, not host filesystem confinement.
- A local model may still process or retain data in memory; local does not mean trusted with reusable credentials.
- A prompt-injection classifier does not detect secrets.
- A PII classifier does not cover general API tokens/private keys.
- A fine-tuned classifier cannot authorize egress or credential use.
- Quantized model behavior can differ materially from the base model card.
- A model family’s tool claim does not prove Hermes/Ollama compatibility.
- Immediate memory unload does not guarantee physical erasure from all GPU/RAM/cache layers.
- “Fully works” should not be claimed until the end-to-end acceptance suite passes on declared supported hardware.

## Final recommendation

Build the principal-facing Aegis manager now, around the existing encrypted authority and deterministic CLI. Make the bare `aegis` command own initialization, authentication, UI, protected intake, session lifecycle, and audit.

Use Ollama as the first local conversational runtime with these defaults:

```text
endpoint: loopback only
cloud: disabled
model: official/traceable 2B–4B tool-capable artifact
artifact: digest pinned
context: explicit and Hermes-conformant
loaded models: one
parallelism: one
keep-alive: five minutes
session close: explicit unload and verification
fallback: none
auxiliary routes: disabled or explicitly local and pinned
```

Evaluate Qwen3-4B-Instruct-2507 and Qwen3.5 4B first, with Qwen3.5 2B and Granite 3.3 2B as lower-resource comparisons. Do not use an abliterated community model for the manager.

Implement high-confidence secret protection deterministically before Hermes. Treat compact Prompt Guard/DeBERTa/PII models as optional, narrow secondary signals. If Aegis later needs a generic contextual secret classifier, train and calibrate an Aegis-specific discriminative model on synthetic/revoked data and run it through an isolated ONNX worker—not as the authority and not as a prerequisite for the first working manager.

Only after the personal manager is complete should Aegis wrap user-created agents and give them typed, principal-approved broker capabilities.

## Sources

All sources accessed 2026-07-17 unless otherwise stated.

1. Ollama, **Generate API specification** — `keep_alive`, structured output, explicit load/unload examples, unauthenticated native API specification: https://docs.ollama.com/api/generate.md
2. Ollama, **Chat API specification** — tool definitions, structured output, `keep_alive`: https://docs.ollama.com/api/chat.md
3. Ollama, **FAQ** — five-minute default residency, preload/unload, concurrency, context, loopback binding, cloud disablement, storage: https://docs.ollama.com/faq.md
4. Ollama, **Tool calling** — single, parallel, multi-turn, and streaming tool loops: https://docs.ollama.com/capabilities/tool-calling.md
5. Ollama, **OpenAI compatibility** — OpenAI-compatible endpoint behavior: https://docs.ollama.com/openai.md
6. Ollama, **Qwen3.5 library** — official Ollama tags/capability labels including tools and 2B/4B sizes: https://ollama.com/library/qwen3.5
7. Ollama, **Qwen3 library** — tool capability and small-size tags: https://ollama.com/library/qwen3
8. Ollama, **Granite 3.3 library** — 2B/8B and tool capability: https://ollama.com/library/granite3.3
9. Ollama, **Llama Guard 3 library** — 1B/8B safety classifier scope: https://ollama.com/library/llama-guard3
10. Ollama, **ShieldGemma library** — general safety-policy scope and sizes: https://ollama.com/library/shieldgemma
11. Ollama repository, **MIT license**: https://github.com/ollama/ollama/blob/main/LICENSE
12. Nous Research, **Hermes Agent AI Providers** — self-hosted/custom providers, Ollama configuration, context and tool guidance: https://hermes-agent.nousresearch.com/docs/integrations/providers
13. Nous Research, **Provider Routing** — primary/auxiliary route behavior: https://hermes-agent.nousresearch.com/docs/user-guide/features/provider-routing
14. Nous Research, **Fallback Providers** — fallback implications: https://hermes-agent.nousresearch.com/docs/user-guide/features/fallback-providers
15. Nous Research, **Hermes model catalog** — live catalog metadata: https://hermes-agent.nousresearch.com/docs/api/model-catalog.json
16. Meta, **Llama Prompt Guard 2 repository** — mDeBERTa prompt-attack/jailbreak task: https://github.com/meta-llama/PurpleLlama/tree/main/Llama-Prompt-Guard-2
17. Hugging Face metadata, **Llama Prompt Guard 2 22M**: https://huggingface.co/meta-llama/Llama-Prompt-Guard-2-22M
18. Hugging Face metadata, **Llama Prompt Guard 2 86M**: https://huggingface.co/meta-llama/Llama-Prompt-Guard-2-86M
19. Protect AI, **DeBERTa-v3-small prompt-injection v2**: https://huggingface.co/protectai/deberta-v3-small-prompt-injection-v2
20. Protect AI, **DeBERTa-v3-base prompt-injection v2** — intended use and explicit limitations: https://huggingface.co/protectai/deberta-v3-base-prompt-injection-v2
21. Gravitee, **BERT-small PII detection** — labels, metrics, external-corpus results, domain/language limitations: https://huggingface.co/gravitee-io/bert-small-pii-detection
22. Qwen Team, **Qwen3-4B-Instruct-2507 model card** — Apache-2.0, parameters, context, local runtime and tool guidance: https://huggingface.co/Qwen/Qwen3-4B-Instruct-2507
23. Qwen Team, **Qwen3.5-2B model card**: https://huggingface.co/Qwen/Qwen3.5-2B
24. Qwen Team, **Qwen3.5-4B model card**: https://huggingface.co/Qwen/Qwen3.5-4B
25. Biringa and Kul, **Detecting Hard-Coded Credentials in Software Repositories via LLMs**, arXiv:2506.13090: https://arxiv.org/abs/2506.13090
26. PADLab, **M2 credential-detection research repository**: https://github.com/PADLab/M2
27. Microsoft Presidio, **Analyzer** — rule/recognizer/context/NLP combination for PII: https://data-privacy-stack.github.io/presidio/analyzer/
28. Aegis, **Frontier models, local inference, Hermes integration, and secret protection for Aegis**: `research/2026-07-17-frontier-models-local-inference-hermes-secret-protection-research.md`
