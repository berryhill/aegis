# Frontier models, local inference, Hermes integration, and secret protection for Aegis

- Status: Complete with documented evidence limitations
- Date: 2026-07-17
- Prepared for: Aegis
- Research question: How should Aegis support the July 2026 frontier-model landscape, local inference, Hermes provider routing, and fail-closed credential protection while preserving its identity-first trust and audit architecture?
- Recommended decision: Add an Aegis-owned, versioned model/provider registry and resolve every primary, fallback, and auxiliary model call into a signed `RoutePlan` before launch; bind the route-plan digest into the mandate; route Hermes through an Aegis egress/secret gateway; permit automatic routing only inside the pre-authorized route set; and make local-only, cloud-denied operation a first-class privacy mode.

## Executive summary

Aegis should remain the identity, authorization, and audit control plane and should not become another agent framework or inference engine. Hermes already supplies broad model-provider integration, local OpenAI-compatible support, tool calling, provider plugins, session model switching, fallback providers, and auxiliary model routing. The current Aegis adapter, however, exposes only a narrow and incomplete portion of that surface: a stanza carries raw `provider` and `model` strings, validation requires one `provider:<provider>` credential scope, and launch recognizes only `openai`, `anthropic`, and `openrouter` credential environments. This is adequate for an explicit MVP but not for privacy-aware routing or a durable provider abstraction.

The principal architectural finding is that a model choice is not merely a performance preference. It selects a data processor, endpoint, jurisdiction, authentication mechanism, retention policy, cost schedule, context limit, tool protocol, and failure behavior. Hermes can also route compression, vision, web extraction, approval scoring, and other auxiliary calls independently. An unrecorded model override or fallback can therefore invalidate the trust assumptions under which Aegis approved a session.

The recommended design is an Aegis-owned `RoutePlan`, generated from a versioned registry and explicit policy. A plan includes immutable model revisions or controlled aliases, endpoints, provider identities, local/cloud classification, data classes allowed, tool-call support, context and output limits, auxiliary routes, fallbacks, cost ceilings, and credential references. Aegis signs or hashes the resolved plan into the mandate. Hermes receives a generated, session-scoped configuration with discovery, fallback, and auxiliary `auto` behavior disabled unless each candidate is present in the approved plan.

For local inference, Aegis should standardize on OpenAI-compatible HTTP rather than couple its control plane to every engine. Use llama.cpp or Ollama/LM Studio for developer workstations and GGUF portability; vLLM or SGLang for production GPU serving; TensorRT-LLM where NVIDIA-only optimization justifies the operational cost. ExLlamaV2 remains useful for narrow NVIDIA/GPTQ workflows but is a less general strategic interface. The strongest practical workstation models are currently Qwen3.6-27B and Qwen3.6-35B-A3B; Qwen3-Coder-Next is attractive for 48–64 GB coding nodes. Very large open weights such as GLM-5.2 and Kimi K3 are enterprise-cluster, not single-workstation, targets. Published maximum contexts are not realistic local defaults because KV-cache memory grows with sequence length.

“Prevent secrets from ever reaching an LLM” cannot be guaranteed by regex alone or by a prompt instruction. The security boundary must be the outbound byte stream. Hermes and tools should run in an egress-restricted sandbox; provider credentials should live in a broker/gateway rather than in the agent process; every model request, attachment, tool result, retrieval chunk, and auxiliary request should pass through a fail-closed local gateway before any external connection is made. Use layered structured detectors, provider-specific regex, entropy and contextual heuristics, encoded-content scanning, and optional local classification. Block high-confidence findings, redact only under an explicit policy, and quarantine uncertain sensitive content for local-only processing or human approval. Active secret validation is valuable for incident response but is itself an outbound disclosure and must never occur synchronously on raw user content without authorization.

The 6–12 month priority order is:

1. Model/provider registry and immutable route-plan contract.
2. Generated Hermes session configuration and complete credential mapping.
3. Aegis egress/secret gateway plus network deny-by-default.
4. Local/cloud/privacy route policies and local inference conformance tests.
5. Cost, quality, latency, and leak-prevention telemetry without raw prompts.
6. Policy-constrained routing, evaluation harnesses, and plugin SDK only after the security boundary is enforceable.

## Research question and scope

This report covers:

- frontier models visible by 2026-07-17, including the models named in the assignment;
- open-weight status, API/local availability, license, context, tool use, modalities, maturity, and available benchmark evidence;
- realistic inference on 24, 32, 48, and 64 GB GPUs and 128 GB RAM CPU-only hosts;
- Hermes v0.18.2 provider, routing, authentication, local-endpoint, tool, and orchestration behavior;
- pre-egress credential detection, redaction, and containment;
- a concrete Aegis architecture and 6–12 month roadmap.

It does not claim that cross-vendor benchmark numbers are directly comparable. Vendor model cards generally use different prompts, agent scaffolds, context limits, judges, and task subsets. It also does not recommend treating a model catalog entry as proof that weights were published or that a license permits a particular use.

Evidence labels used below:

- **Verified**: directly supported by inspected repository source or cited primary/authoritative material.
- **Vendor-reported**: a first-party claim not independently reproduced for this report.
- **Inference**: an engineering conclusion from verified facts, explicitly identified.
- **Unresolved**: evidence was absent, conflicting, or too new for a mature conclusion.

## Aegis codebase context

### Current architecture

Aegis is an identity-first Go control plane. Its implemented flow is:

1. Create or import an office.
2. Define a charter and trust stanzas.
3. Validate charter syntax and references.
4. Add and authenticate agents.
5. Select a stanza and construct a mandate.
6. Approve the mandate.
7. Provision a workspace/deployment.
8. Launch an explicit runtime adapter.
9. Record lifecycle events in the append-only audit stream.

The charter controls filesystem, network, toolset, credential, workspace, and approval scope. Runtime output is treated as untrusted. Aegis owns the contract and lifecycle; Hermes owns the interactive agent loop.

### Current provider and model behavior

Verified from `internal/core/model.go`, `internal/app/service.go`, and `internal/runtime/hermes/hermes.go`:

- `TrustStanza` stores `Provider` and `Model` as raw strings.
- Validation requires exactly one credential scope named `provider:<provider>`.
- The selected stanza is copied into the mandate, so the current provider/model choice is indirectly approval-bound.
- The Hermes adapter invokes `hermes --safe-mode --tui --toolsets ... --model <model> --provider <provider>`.
- `CredentialBinding` maps a source environment variable to a target variable.
- The adapter constructs a minimal child environment rather than forwarding the parent environment wholesale.
- Only OpenAI, Anthropic, and OpenRouter have hard-coded target environment mappings.
- The runtime interface is deliberately small (`Name`, `Launch`, `Stop`) and has no provider discovery, capability negotiation, route planning, or usage telemetry.
- There is no proxy between Hermes and a provider and no prompt/attachment/tool-output preprocessor in the data path.

### Security boundaries and consequences

Aegis already makes several good boundary choices:

- explicit adapter launch rather than shell execution;
- safe mode and a minimal environment;
- authenticated agent identity and approved mandate before launch;
- append-only audit records;
- runtime output treated as untrusted;
- charter-scoped network, tools, filesystem, and credentials.

The gaps relevant to this research are:

1. **Provider identity is underspecified.** A raw provider string does not identify endpoint, jurisdiction, account, retention terms, or whether the endpoint is local.
2. **One credential scope cannot describe every egress.** Primary, fallback, vision, compression, extraction, approval, and plugin calls may use different providers.
3. **Hermes behavior can mutate after approval.** Hermes documents session model switching and fallback behavior. An in-session switch changes the approved processor/model unless Aegis interposes.
4. **Network scope is not an egress enforcement mechanism by itself.** The current subprocess can make outbound calls once launched.
5. **Secrets are present in the agent process.** Minimal environment is better than ambient inheritance, but an injected provider key can still be read or exfiltrated by a compromised process or dependency.
6. **There is no model capability contract.** Context, modality, structured output, tool calls, and local-engine parser support are assumed rather than checked.
7. **No quality/cost evaluation loop exists.** Automatic routing would be ungrounded until Aegis can measure task success, tool-call validity, latency, and cost.

## Methodology

### Repository-first inspection

The investigation read Aegis architecture, product, deployment, runtime, core model, app service, configuration, storage, specification, example-charter, test, and existing research material before external research. Repository searches covered provider, model, credential, secret, route, offline, plugin, toolset, and Hermes symbols.

### External evidence

Primary sources were preferred:

- live Hermes documentation and source at the inspected v0.18.2 checkout;
- model-provider documentation and official Hugging Face model cards/configurations;
- official inference-engine repositories and release metadata;
- official TruffleHog, Gitleaks, detect-secrets, GitHub secret-scanning, OWASP, and research-paper sources;
- the live OpenRouter models API as an availability and aggregator-metadata source.

OpenRouter fields are not treated as proof of licenses or independent reproduction. GitHub stars are volatile adoption signals, not quality measures. Vendor benchmark scores are labeled as such.

### Subagents and validation

Four parallel Hermes one-shot workers were launched for frontier models, local inference, Hermes integration, and credential detection. All failed with the same OpenAI Codex stale-stream timeout before producing usable reports. An alternate Nous Portal probe failed because that provider was not authenticated. These failures are recorded in the appendix rather than concealed. The architecture synthesis was consequently performed directly from repository inspection and directly fetched sources. Important source URLs were checked for successful retrieval, the live Hermes catalog was fetched, selected OpenRouter records were extracted, official model cards/configs were inspected, and current release metadata was queried.

## Key findings

### Frontier model landscape as of 2026-07-17

| Model/family | Weights and license | Availability | Context and modality | Agent/coding position | Evidence quality and Aegis implication |
|---|---|---|---|---|---|
| GPT-5.5 / 5.5 Pro | Proprietary; no weights | OpenAI API and aggregators | OpenRouter reports 1.05M; text/image/file input; tools | Frontier general reasoning/coding; Pro is materially more expensive | API metadata verified; quality remains provider/vendor dependent. Use only through pinned API route and cost ceiling. |
| Claude Opus 4.6–4.8 | Proprietary; no weights | Anthropic API and aggregators | Opus 4.8: 1M text/image/file and tools in current metadata | Strong long-horizon agent/coding position; fast mode trades price for throughput | API/Hermes metadata verified; benchmarks vary by harness. Best fit for high-value cloud coding under explicit privacy policy. |
| Qwen3.7-Max / Plus | Max is proprietary API; no public Max weights found | Alibaba APIs/aggregators | Max: 1M text; Plus: 1M text/image; tools | Agent-centric, coding and office work | Availability and metadata verified through live catalogs; avoid inferring open-weight status from the Qwen brand. |
| Qwen3.6 Max/Plus/Flash | Proprietary service variants | API/aggregators | 262K–1M depending variant; Flash supports text/image/video | Cost/latency-oriented cloud routing candidates | Distinguish these from the open 27B/35B releases. |
| Qwen3.6-27B | Open weight, Apache-2.0 | Hugging Face; local engines; APIs | Native 262,144; vendor says extensible to 1,010,000; text/image/video | Vendor reports strong agentic coding; dense 27B is workstation-feasible when quantized | High-confidence local baseline for 24–32 GB. Extended context is not a free memory feature. |
| Qwen3.6-35B-A3B | Open weight, Apache-2.0; 35B total/3B active (name and card) | Hugging Face; SGLang/vLLM; local ecosystem | Native 262,144; extensible; text/image/video | Strong local agent/coding candidate; MoE reduces compute, not weight storage | Preferred 32 GB class candidate and 24 GB at aggressive quantization/short context. |
| Qwen3-Coder / Coder-Next | Open weight, Apache-2.0 for inspected cards | Hugging Face and local/API engines | Coder-Next: 80B total, 3B active, 262,144 text | Purpose-built coding agent; native tool-call formats require engine parser support | Coder-Next is attractive at 48–64 GB quantized; total weights still must fit. |
| GLM-5.2 | Open weight, MIT according to official card | Hugging Face and API; multi-GPU serving | Native 1,048,576 text; tools | Vendor reports strong long-horizon coding/agent scores | Frontier open model but not a consumer single-GPU recommendation. Validate engine/version and cluster capacity. |
| DeepSeek V4 Pro/Flash | No official open-weight repository was verified in this investigation; current entries are APIs | DeepSeek/aggregator APIs | OpenRouter reports 1,048,576 text and tools | Very low listed API pricing; reasoning/coding candidate | Treat as proprietary hosted routes until an official weight release and license are verified. Do not extrapolate from earlier open DeepSeek releases. |
| Kimi K3 | OpenRouter describes 2.8T open weight, but no Hugging Face ID or official weight repository was verified on access date | API verified; local artifact unresolved | 1,048,576 text/image and tools in aggregator metadata | Long-horizon, multimodal, coding claim | Mark weight/license/local deployment unresolved. Even if published, 2.8T total weights imply cluster-scale storage. |
| Seed 2.1 Pro | Proprietary status/API details were not independently verified; the accessible ByteDance page documented Seed 2.0, and no matching live OpenRouter entry was found | Unresolved | Unresolved | Unresolved | Do not add to production registry until first-party model ID, endpoint, terms, context, and tool schema are verified. |
| gpt-oss-120b / 20b | Open weight, Apache-2.0 | Hugging Face/local engines | 120b: 131K; text; Harmony response format | Agentic tools and configurable reasoning | Mature open alternative. Official card says 120b fits one 80 GB accelerator in MXFP4 and 20b within 16 GB. |
| Gemini 3.x, Grok 4.5, MiniMax M3, MiMo 2.5 Pro, Nemotron 3 | Mostly hosted, with openness varying by family | APIs/aggregators | Current Hermes catalog lists them | Significant additional candidates | Aegis registry should be extensible rather than hard-code the assignment’s model list. Each still requires primary-source onboarding. |

#### Benchmark evidence and cautions

| Model | Metric | Reported result | Source class | Important caveat |
|---|---:|---:|---|---|
| GLM-5.2 | SWE-bench Pro | 62.1 | Vendor model card | OpenHands, tailored prompt, 400K context. |
| GLM-5.2 | Terminal-Bench 2.1, Terminus-2 | 81.0 | Vendor model card | 256K context; vendor-defined run settings. |
| GLM-5.2 | MCP-Atlas public | 76.8 | Vendor model card | Gemini judge and ten-minute timeout. |
| Qwen3.6-35B-A3B | SWE-bench Verified | 75.0 | Vendor model card | Internal bash/file-edit scaffold and 200K context. |
| Qwen3.6-35B-A3B | SWE-bench Pro | 51.2 | Vendor model card | Vendor says it corrected problematic public tasks and used a refined benchmark. |
| Kimi K3 | Artificial Analysis coding index | 76.2 | Third-party value surfaced by OpenRouter | Aggregated, time-varying methodology; not an Aegis workload score. |
| Claude Opus 4.8 | Artificial Analysis coding index | 74.3 | Third-party value surfaced by OpenRouter | API endpoint/settings may differ from Aegis. |
| GLM-5.2 | Artificial Analysis coding index | 68.8 | Third-party value surfaced by OpenRouter | Does not establish local quantized performance. |
| Qwen3.7-Max | Artificial Analysis coding index | 66.0 | Third-party value surfaced by OpenRouter | Hosted model only; routing and prompt differences matter. |
| Qwen3.6-35B-A3B | Artificial Analysis coding index | 41.9 | Third-party value surfaced by OpenRouter | Conflicts in impression with high vendor SWE scores illustrate harness sensitivity. |

**Inference:** Aegis should not route from public leaderboards. It should maintain a workload-specific evaluation corpus: charter interpretation, tool-call JSON validity, repository editing, long-session recovery, secret non-disclosure, and office-specific acceptance tests. Public results are onboarding priors only.

### Local inference

#### Engine comparison

| Engine | Best use | Strengths | Risks/limits | Aegis recommendation |
|---|---|---|---|---|
| llama.cpp | Portable workstation and CPU/GPU hybrid | GGUF, broad hardware support, quantization ecosystem, simple OpenAI-compatible server | Peak multi-user GPU throughput may trail specialized servers; model feature support varies by build | Default portable/offline backend. Pin build and model template. |
| vLLM | Production GPU API | High throughput, continuous batching, tensor parallelism, broad model support, OpenAI-compatible API | GPU-centric; rapid releases; tool/reasoning parser must match model | Default enterprise GPU backend after conformance tests. |
| SGLang | Agentic/structured high-throughput serving | Strong structured generation, prefix caching, tensor parallelism, model-specific parsers | Fast-moving compatibility surface | Preferred alternative when its parser/runtime leads for a selected model. |
| ExLlamaV2 | NVIDIA consumer GPU and GPTQ/EXL2 | Fast quantized inference | Narrower formats/platforms; latest inspected release older than vLLM/SGLang | Optional specialist adapter, not registry contract. |
| Ollama | Developer UX and managed local models | Easy lifecycle, broad adoption, local HTTP API | Tags can drift; defaults may hide quant/context/template choices | Supported developer backend only with digest-pinned artifacts and explicit context. |
| LM Studio | Desktop evaluation and OpenAI-compatible local service | Strong GUI, easy model testing, Apple MLX support | Desktop lifecycle and manual state are weaker for unattended production | Supported for development, not authoritative production deployment. |
| TensorRT-LLM | NVIDIA enterprise serving | NVIDIA-specific kernel and multi-GPU optimization | Highest build/ops coupling; compatibility and licenses need scrutiny | Use for stable, high-volume NVIDIA fleets where measured gains justify it. |

Current release/adoption signals on access date: llama.cpp build b10064, vLLM v0.25.1, SGLang v0.5.15.post1, Ollama v0.32.1, TensorRT-LLM v1.2.1, and ExLlamaV2 v0.3.2. Release numbers are operational evidence, not compatibility guarantees.

#### Memory model

For a dense or MoE checkpoint, a first-order raw weight estimate is:

`weight_GB ≈ total_parameters_billions × quantization_bits / 8`

MoE activation sparsity reduces compute per token but does **not** remove the need to store all experts unless the engine offloads or shards them. Real memory also includes quantization metadata/scales, runtime buffers, vision encoder, allocator reserve, and KV cache. A planning allowance of 10–20% over raw weights is only an estimate; KV cache must then be budgeted separately. Context length, batch size, KV precision, hidden size/layers, and concurrency can dominate the remaining memory.

Illustrative calculations with a 15% non-KV allowance:

| Configuration | Raw weights | Weights + 15% | Memory remaining before KV/runtime |
|---|---:|---:|---:|
| 27B at 4-bit on 24 GB | 13.5 GB | 15.5 GB | 8.5 GB |
| 35B at 4-bit on 24 GB | 17.5 GB | 20.1 GB | 3.9 GB |
| 35B at 6-bit on 32 GB | 26.25 GB | 30.2 GB | 1.8 GB; generally too little for long context |
| 80B at 4-bit on 48 GB | 40 GB | 46 GB | 2 GB; context must be short or offloaded |
| 80B at 5-bit on 64 GB | 50 GB | 57.5 GB | 6.5 GB |
| 120B at 4-bit in 128 GB RAM | 60 GB | 69 GB | 59 GB |

These are capacity estimates, not performance benchmarks.

#### Hardware recommendations

| Hardware | Strong realistic model target | Deployment | Expected trade-off |
|---|---|---|---|
| 24 GB GPU | Qwen3.6-27B Q4; Qwen3.6-35B-A3B Q4 only with conservative context | llama.cpp/GGUF or a supported quant in vLLM/SGLang | 27B is safer. 35B leaves little KV headroom; 262K is not realistic. |
| 32 GB GPU | Qwen3.6-35B-A3B Q4/Q5; Qwen3.6-27B at higher precision | llama.cpp for workstation; vLLM/SGLang if quant supported | Best balance for local Aegis agent use. Start at 32K–64K context and measure. |
| 48 GB GPU | Qwen3-Coder-Next 80B Q4 at short/moderate context; Qwen3.6-35B at high precision/large KV | vLLM/SGLang or llama.cpp | 80B Q4 fit is tight. Coding quality may justify it; multimodal 35B offers more headroom. |
| 64 GB GPU | Qwen3-Coder-Next 80B Q4/Q5; gpt-oss-120b only if an engine-specific representation demonstrably fits | vLLM/SGLang/TensorRT-LLM | Better context and batching. Do not assume an “80B, 3B active” model consumes 3B memory. |
| 128 GB RAM CPU-only | Qwen3.6-35B-A3B Q4/Q5 for usable latency; Qwen3-Coder-Next Q4 or gpt-oss-120b as slower quality/capacity options | llama.cpp/GGUF; memory-map where appropriate | Memory bandwidth dominates. Large MoE can fit but may be too slow for interactive multi-agent use; benchmark tokens/s locally. |

For multi-GPU, tensor parallelism is appropriate for one model/request stream; pipeline parallelism is generally less attractive for low-latency interactive sessions; data parallel replicas maximize throughput where each model fits one node. High-bandwidth interconnect materially affects tensor-parallel latency. Enterprise deployments should separate model-serving nodes from the Aegis control plane, pin container/model digests, expose only a private authenticated endpoint, and meter queue time, time-to-first-token, tokens/second, KV utilization, OOMs, and tool-call validity.

### Hermes integration

Verified against Hermes v0.18.2 documentation/source and the live 2026-07-16 model catalog:

- Hermes has native or plugin-backed support for OpenRouter, Nous Portal, OpenAI, Anthropic, Google/Gemini, Azure, AWS Bedrock, Vertex, NVIDIA, GitHub Copilot, llama.cpp, Ollama, LM Studio, custom OpenAI-compatible endpoints, and other providers.
- Model and provider may be selected by CLI/config; model aliases and provider prefixes exist.
- Local endpoints are supported through first-class LM Studio and custom OpenAI-compatible configuration; provider plugins can define base URL, API key, model discovery, transforms, and capability behavior.
- Provider fallback is per turn. Provider metadata is stored in message history, and switching providers can reset cache behavior.
- In-session `/model` switching exists.
- Auxiliary routes can be independently selected for vision, compression, web extraction, approval, and related tasks; `auto` may choose routes not visible in Aegis’s current stanza.
- Hermes tool calls are model/provider-sensitive. OpenAI-compatible does not imply identical tool-call, reasoning, image, or streaming semantics.
- Hermes has orchestration/subagent features, but Aegis should authorize their effective tool/network/credential envelope rather than reimplement their loop.

#### Ideal Hermes configuration for Aegis

1. Generate an isolated Hermes home/config for every deployment/session.
2. Set explicit provider and immutable model identifier; never rely on Hermes’s silent default.
3. Disable model discovery and remote catalog mutation during a running mandate.
4. Disable arbitrary `/model` changes, or force a reauthorization/restart through Aegis.
5. Disable fallback by default. If enabled, render only the pre-approved fallback list in order.
6. Set every auxiliary model explicitly. `auto` is forbidden in privacy-bound or offline sessions.
7. Point cloud providers at the Aegis egress gateway using an OpenAI-compatible/custom provider where protocol allows.
8. Give Hermes only an ephemeral, session-scoped gateway token—not the upstream provider key.
9. For local mode, use a loopback/private endpoint and deny external egress at the OS/network layer.
10. Pin Hermes within the supported compatibility range and run an adapter conformance suite before upgrade.
11. Record resolved Hermes version, provider plugin version, model digest/revision, template/parser, endpoint identity, and route-plan digest in launch/audit events.
12. Keep Aegis’s explicit runtime adapter boundary; add capabilities rather than importing Hermes internals.

## Security and threat analysis

### Assets and threat actors

Assets include provider/API credentials, user secrets, source code, attachments, tool results, office policy, mandate integrity, model prompts/responses, audit integrity, local model artifacts, and cost budgets.

Threat actors include malicious users, compromised repositories/documents, prompt-injected web content, malicious or compromised tools/plugins, compromised model providers, supply-chain attackers, tenants attempting cross-office access, and accidental user disclosure.

### Trust boundaries

1. User/client → Aegis API/CLI.
2. Aegis control plane → workspace/provisioner.
3. Aegis → Hermes process.
4. Hermes/model tools → local egress gateway.
5. Gateway → local inference endpoint or cloud provider.
6. Plugins/models/artifacts → runtime supply chain.
7. Runtime events → audit store and observability systems.

### STRIDE-oriented analysis

| Threat | Abuse case | Current exposure | Required mitigation |
|---|---|---|---|
| Spoofing | A local endpoint impersonates an approved provider | Provider is a string; endpoint identity is not mandate-bound | Endpoint identity, TLS/pinning where appropriate, deployment identity, session token, route-plan digest. |
| Tampering | Model alias or Ollama tag changes after approval | Mutable identifiers | Pin provider model revision or artifact digest; signed registry snapshots. |
| Repudiation | Fallback/auxiliary call is absent from audit | No route-level telemetry | Gateway emits metadata-only decision/usage events correlated to session and route ID. |
| Information disclosure | Secret in prompt, file, tool output, URL, encoded blob, image/OCR, or log reaches cloud | No pre-egress interception | Fail-closed gateway, multi-layer scanning, egress deny-by-default, local quarantine, safe logging. |
| Denial of service | Huge context, detector worst case, MoE OOM, fallback loop | No explicit budgets | Byte/token/context/time/concurrency ceilings, bounded regex, circuit breakers, admission control. |
| Elevation of privilege | Plugin/tool reads provider key or calls arbitrary endpoint | Key is in Hermes environment; tool/network scopes are broad abstractions | Credential broker, no upstream key in agent, sandbox network allowlist, signed plugin policy. |

### Prompt injection and agent manipulation

Secret filtering does not solve prompt injection. Untrusted content may instruct Hermes to read environment variables, upload files, invoke a network tool, alter model selection, or encode data to evade detectors. Controls must be structural:

- do not expose upstream credentials to the agent process;
- separate instructions from untrusted data in tool adapters;
- attach provenance and trust labels to retrieved/tool content;
- require policy checks for sensitive tool actions;
- restrict network egress independently of the model;
- scan outbound tool arguments and model requests after transformations;
- limit recursive decoding, archive depth, and payload size;
- treat model output as untrusted before it reaches tools;
- do not permit content to alter the route plan.

### Credential-detection pipeline

A strict pre-egress path should be:

1. **Canonicalize safely:** parse request structure; normalize line endings and Unicode; identify URL/form/header/JSON fields; decode bounded base64/hex/percent encodings; expand bounded archives; extract text/OCR locally when policy permits. Preserve byte spans and never log raw values.
2. **Structured policy checks:** block known sensitive fields (`authorization`, private-key blocks, credential files, `.env` values), Aegis credential references, and data labels disallowed by route policy.
3. **High-precision detectors:** provider-specific prefixes/checksums, private-key formats, JWTs, connection strings, cloud keys, and secret-specific regex/keywords.
4. **Context and entropy:** use entropy only on plausible candidate spans; combine assignment keywords, nearby identifiers, path/provenance, character classes, length, and allowlisted test fixtures. Entropy alone has excessive false positives.
5. **Optional local classifier:** a compact MiniLM/DistilBERT-style classifier may rank ambiguous candidates using surrounding context. It must run locally, must not receive upstream credentials, and must never override a deterministic high-confidence block. Build/evaluate an Aegis-specific classifier; no authoritative public artifact named `DeepPass2-BERT` was found in arXiv or GitHub searches on the access date.
6. **Decision:** block, redact/tokenize, local-only reroute, or require explicit approval. Unknown detector errors fail closed for cloud egress.
7. **Redaction/tokenization:** replace with typed stable placeholders such as `<AEGIS_SECRET:aws:7f2c…>`; store reversible mappings only in a short-lived encrypted vault when task semantics require reinsertion. Never include full values in findings, logs, spans, fingerprints, or telemetry.
8. **Post-transform rescan:** scan the exact serialized bytes after prompt templates, tool wrapping, compression, and provider transforms.
9. **Egress enforcement:** only the gateway can reach providers. DNS/IP and process sandboxing prevent bypass.
10. **Response/tool scan:** prevent models or tools from reflecting secrets into logs, chat, audit events, or another provider.

#### Tool comparison

| Approach | Strength | Weakness | Correct Aegis role |
|---|---|---|---|
| TruffleHog v3.95.9 | Hundreds of classified detectors and active verification; broad formats/sources | AGPL-3.0 integration implications; verification sends candidate material to services; optimized for discovery, not inline latency | Offline/asynchronous repository and artifact scanning; reuse detector ideas or invoke as isolated optional service after legal review. Never auto-verify inline user prompts. |
| Gitleaks v8.30.1 | Fast Go regex/entropy/keyword engine, stdin and redacted output, configurable allowlists/composite proximity | Maintainer states feature-complete/security-fixes-only; mostly deterministic and code-oriented | Good CI/pre-commit baseline and possible bounded inline component; evaluate Betterleaks trajectory before strategic coupling. |
| detect-secrets | Plugin architecture, entropy/keyword detectors, baselines and heuristics | Python dependency; baseline workflow is repository-centric | Useful reference and developer pre-commit option. |
| Regex/prefix/checksum | Fast, explainable, high precision for structured formats | Misses generic passwords and novel formats | Mandatory first stage. |
| Entropy | Catches unknown random-looking values | IDs, hashes, compressed data, and generated code cause false positives | Candidate feature, never sole verdict. |
| MiniLM/DistilBERT classifier | Local contextual ranking, low relative model size | Dataset shift, adversarial evasion, opaque errors, maintenance burden | Optional second-pass triage after Aegis corpus evaluation. |
| Active validation | Strong false-positive reduction and incident prioritization | Discloses candidate to a service, may create audit events/rate limits, and can be abused | Asynchronous, explicitly authorized incident workflow only. |

“No secrets ever reach an LLM” is achievable only within a stated threat model. A local classifier is itself an LLM-like model; therefore the product requirement should say “no secret reaches an unauthorized model or external processor.” If even local models are forbidden, use deterministic scanning and block rather than classify.

### False positives and false negatives

- False positive: blocks legitimate code/hash/test fixtures and interrupts agents. Mitigate with typed findings, exact-path/provenance exceptions, expiring reviewed allowlists, local-only reroute, and deterministic reproducible rules.
- False negative: discloses a live credential. Mitigate with no credential in agent memory, exact-byte post-transform scan, egress enforcement, canary credentials, periodic red-team corpora, and immediate revoke/rotate playbooks.
- Redaction semantic failure: replacing a credential may make the task impossible or cause the model to invent one. Expose a typed placeholder and use a privileged tool that can perform the operation without revealing the secret to the model.
- Validation failure: network outage produces “unknown,” not “safe.” Fail closed for external egress.

## Options considered

| Option | Benefits | Risks | Complexity | Performance | Recommendation |
|---|---|---|---|---|---|
| Continue raw stanza provider/model strings | Minimal implementation | No capability, endpoint, auxiliary, fallback, license, privacy, or cost contract | Low | No added overhead | Reject beyond MVP. |
| Let Hermes own discovery/routing/fallback | Reuses Hermes features; rapid model availability | Routing can violate approved processor/privacy/credential boundary and weaken reproducibility | Low–medium | Potential availability/cost gains | Reject as policy authority; use Hermes only to execute an Aegis-resolved plan. |
| Aegis registry + immutable `RoutePlan` | Explicit trust binding, auditability, controlled routing, local/cloud parity | Registry freshness and compatibility maintenance | Medium | Negligible control-plane cost | Adopt. |
| Direct provider credentials in Hermes environment | Simple and currently implemented | Compromised process/plugin can read keys; no central egress scan | Low | Lowest latency | Deprecate. |
| Aegis egress gateway with credential broker | Exact-byte scan, centralized policy, no upstream key in agent, unified metrics | New critical service; protocol streaming/tool compatibility work | High | Added scan and proxy latency | Adopt incrementally; fail closed. |
| Regex/entropy-only secret filter | Fast, explainable | Generic secrets and encoded/contextual cases escape; entropy noise | Low | Low latency | Mandatory baseline but insufficient alone. |
| Hybrid deterministic + local classifier | Better ambiguous-case recall/precision | Dataset/model supply chain and adversarial drift | Medium–high | Additional local latency | Pilot only after deterministic gateway and corpus exist. |
| Cloud classifier for secret detection | Easy scaling | Sends possibly secret content to another external model | Medium | Network latency/cost | Reject. |
| Local-first routing with approved cloud escalation | Privacy/cost resilience; offline capability | Local quality/latency variance; routing complexity | Medium–high | Often slower per token, zero network latency | Adopt for sensitive offices with explicit escalation authorization. |
| Fully automatic quality/cost routing | Operational savings | Non-determinism, hidden processor changes, benchmark gaming | High | Can optimize throughput | Defer until evaluation/telemetry; constrain to pre-approved candidates. |

## Recommended approach

### Decision principles

1. **Identity and authorization precede optimization.** Route eligibility derives from office, agent, stanza, data class, and mandate—not from a global “best model.”
2. **Resolve before launch.** All model-bearing egress, including auxiliary calls, must be in a route plan.
3. **Processor changes are authorization changes.** An endpoint/provider switch requires a pre-approved route or a new mandate.
4. **Local is a trust property, not a provider name.** Validate endpoint locality and enforce network isolation.
5. **No upstream credential in the agent process.** Prefer brokered credentials and ephemeral gateway identity.
6. **Measure Aegis tasks.** Route from controlled evaluations and production metadata, not generic leaderboards.
7. **Fail closed on policy uncertainty.** Availability fallback must not silently override privacy or credential controls.

## Proposed Aegis integration

### Architecture

```text
CLI/API
  -> Aegis application service
       -> Charter + agent + approval checks
       -> Registry snapshot + policy resolver
       -> immutable RoutePlan + digest
       -> Mandate (includes RoutePlan digest)
       -> Provisioner
            -> isolated Hermes config/home
            -> sandbox + network policy
            -> Hermes runtime
                 -> localhost Aegis Egress Gateway
                      -> canonicalize / classify / redact / authorize
                      -> credential broker adds upstream auth
                      -> approved local inference endpoint OR approved cloud route
       -> metadata-only audit and usage events
```

### Components affected

- `internal/core`: add `ProviderDefinition`, `EndpointDefinition`, `ModelDefinition`, `ModelCapability`, `RoutePolicy`, `RoutePlan`, `RouteCandidate`, `AuxiliaryRoute`, `PrivacyMode`, and digests/revisions.
- `internal/app`: add registry lookup, route resolution, policy evaluation, reauthorization, and route-specific audit events.
- `internal/runtime`: extend launch requests with a resolved plan/config reference and adapter capability report without coupling the interface to Hermes internals.
- `internal/runtime/hermes`: render isolated Hermes configuration; complete provider/endpoint mapping; disable unapproved mutation; point eligible calls to the gateway.
- `internal/config`: registry source, refresh/signature policy, gateway bind address, classifier mode, privacy defaults, and budgets.
- `internal/store`: persist immutable registry snapshots/route plans or content-addressed references.
- `specs`: version the charter/mandate schema for route-policy references and route-plan digest.
- New `internal/egress` or separate `cmd/aegis-egress`: protocol proxy, secret pipeline, broker, and metadata events.

### Data flow

1. Stanza specifies intent and constraints: workload class, privacy mode, allowed providers/endpoints/models, minimum capabilities, maximum cost, and escalation policy.
2. Resolver evaluates agent/office policy and current signed registry snapshot.
3. Resolver produces an ordered, immutable `RoutePlan` for primary, fallback, and auxiliary functions.
4. Approval covers the exact plan digest. Any material route change creates a new plan and approval event.
5. Provisioner creates isolated Hermes state and an ephemeral gateway token.
6. Hermes sends a serialized request to the gateway.
7. Gateway validates token/session/route, scans exact payload, applies decision, and adds upstream authentication.
8. Gateway emits only hashes, lengths, detector IDs, route IDs, timing, token/cost metadata, and decision—never raw secret or prompt.
9. Local/cloud response is scanned before logging/tool use.

### Configuration

Suggested policy shape (illustrative, not a final schema):

```yaml
model_policy:
  privacy_mode: local_preferred   # cloud_allowed | local_preferred | local_only | offline
  capability_requirements:
    tools: true
    structured_output: true
    min_context_tokens: 32768
  allowed_routes:
    - model: qwen/Qwen3.6-35B-A3B@<artifact-digest>
      endpoint: local-gpu-pool
    - model: anthropic/claude-opus-4.8@2026-05-28
      endpoint: anthropic-production
      data_classes: [public, internal]
  cloud_escalation: require_approval
  auxiliary:
    compression: local-small-model@<digest>
    vision: local-multimodal@<digest>
  budgets:
    max_input_tokens: 100000
    max_output_tokens: 32000
    max_usd_per_session: 20
```

`offline` means DNS and non-loopback/private egress are denied, catalog refresh is disabled, and every required model/tool artifact has been preflighted locally.

### Error handling

- Registry stale/signature invalid: reject new session; existing pinned session may continue according to policy.
- Capability mismatch: fail before launch with exact missing capability.
- Local endpoint unavailable: fail or use an already-approved fallback; never auto-cloud in local-only/offline mode.
- Detector crash/timeout: block cloud egress; permit local route only if policy explicitly allows unscanned local processing.
- Secret high confidence: block and return typed remediation without echoing content.
- Secret ambiguous: local-only reroute or approval queue.
- Provider rate limit/outage: use only the next authorized candidate and record transition.
- Cost/context ceiling: stop before provider call or request explicit budget change.
- Tool-call parse failure: retry within bounded policy or route to an authorized compatible candidate.

### Observability

Record:

- session/mandate/route-plan/registry digests;
- Hermes/provider-plugin/engine/model artifact versions;
- route candidate selected and reason code;
- local/cloud classification and endpoint identity;
- input/output token counts, time to first token, total latency, retry/fallback count, and estimated/actual cost;
- tool-call parse/validation outcome and task acceptance result;
- detector IDs, confidence bucket, action, payload byte count, and irreversible keyed fingerprint;
- local serving queue, tokens/s, KV use, OOM, GPU utilization, and cache hit rate.

Never record raw prompts, model responses, secret values, reversible hashes of low-entropy secrets, authorization headers, or redaction-vault mappings in general audit logs.

### Privacy and security controls

- OS-level process sandbox and deny-by-default egress.
- Upstream keys only in broker memory or external secret manager.
- Ephemeral session-scoped gateway token with route-plan audience.
- Signed/content-addressed model and plugin artifacts; SBOM and vulnerability scanning.
- Registry source allowlist and signature verification.
- Exact-byte outbound and inbound scanning.
- Typed provenance/data-class labels.
- Non-bypassable route-plan enforcement.
- Explicit cloud processor/jurisdiction/retention metadata.
- No active validation of candidate secrets without incident authorization.
- Red-team canaries and leak-response runbook.

## Implementation plan

### Phase 1 — 0–3 months: make routing explicit

- Define registry and `RoutePlan` domain types and schema versioning.
- Add static built-in registry entries for current three cloud providers plus custom OpenAI-compatible/local endpoint.
- Bind route-plan digest into mandate and launch/audit events.
- Generate isolated Hermes configuration and set all auxiliary routes explicitly.
- Reject model switching and unapproved fallback for Aegis-managed sessions.
- Add provider/model capability preflight and Hermes conformance tests.
- Add Gitleaks or equivalent to CI/pre-commit with fully redacted output.

Acceptance milestone: two cloud models and one local OpenAI-compatible model can run under immutable, auditable plans; an unapproved model/fallback is denied.

### Phase 2 — 3–6 months: enforce the data boundary

- Implement local egress gateway and credential broker.
- Move upstream provider keys out of Hermes environment.
- Enforce process/network egress so provider traffic can only traverse gateway.
- Implement structured/prefix/regex/entropy/context scanning and bounded decoding.
- Add block, typed redaction, local-only reroute, and approval decisions.
- Add local-only and offline privacy modes.
- Integrate llama.cpp and one enterprise engine (vLLM or SGLang) through OpenAI-compatible conformance.
- Add metadata-only route/cost/latency/detector telemetry.

Acceptance milestone: canary secrets in every modeled request channel never appear at a mock external provider, logs, or audit store; bypass attempts fail at the network boundary.

### Phase 3 — 6–12 months: evaluate and optimize safely

- Build Aegis workload benchmark and adversarial corpus.
- Add policy-constrained routing across pre-approved candidates using measured quality, cost, latency, capacity, and privacy.
- Add approved failover and cloud escalation workflow.
- Pilot a local compact classifier on ambiguous findings; deploy only if it improves precision/recall at fixed security thresholds.
- Add signed provider/model plugin SDK and compatibility certification.
- Add enterprise serving profiles, model artifact lifecycle, capacity scheduler, and cost budgets.
- Add route recommendation UI/API that explains every decision.

Acceptance milestone: routing improves measured cost/latency without reducing acceptance or leak-prevention thresholds, and every route decision is reproducible from stored metadata.

## Testing and evaluation plan

### Unit tests

- Registry validation, canonical IDs, alias expiry, signature/digest determinism.
- Route eligibility by privacy/data class/capability/cost.
- No local-only/offline plan contains cloud or public endpoint.
- Hermes config rendering includes every auxiliary route and excludes `auto`/unapproved fallbacks.
- Detector span/redaction correctness across Unicode and encodings.
- Log serializers reject sensitive fields.

### Integration tests

- Mock OpenAI-compatible local and cloud servers.
- Hermes tool calls, streaming, structured output, reasoning blocks, image/file requests, compression, and fallback.
- Credential broker injects upstream auth only after scan and only to approved endpoint.
- Network namespace denies direct provider/tool bypass.
- Route-plan mutation or session `/model` switch is rejected/requires new mandate.
- llama.cpp plus vLLM/SGLang conformance for selected pinned models.

### Adversarial tests

- Prompt injection requesting environment/file exfiltration.
- Secrets split across JSON fields/messages/tool calls or Unicode confusables.
- Base64/hex/percent/nested archive and QR/OCR cases within bounded policy.
- Secret embedded in URL, image metadata, source map, binary, and compressed tool output.
- Plugin attempts direct socket/DNS and local metadata-service access.
- Provider alias/tag drift and malicious registry update.
- Detector ReDoS/memory bombs, giant context, fallback loops, and model OOM.

### Performance tests

- Gateway p50/p95/p99 scan latency by payload size and detector set.
- Streaming time-to-first-token overhead.
- Local tokens/s and concurrency at 8K/32K/64K contexts.
- Multi-GPU scaling efficiency and queue behavior.
- KV-cache pressure, OOM recovery, and cold model load.
- Cost and cache effects of fallback/model switching.

### False-positive tests

- UUIDs, checksums, package locks, minified assets, test fixtures, public keys, synthetic examples, and code snippets.
- Measure block and review rates by language/path/provenance.
- Require reviewed exceptions to be typed, scoped, expiring, and regression-tested.

### False-negative tests

- Seed corpus of real formats using revoked/synthetic values.
- Novel/generic passwords, split tokens, transformed encodings, and nearby misleading context.
- Every provider credential family supported by registry.
- Canary secret end-to-end assertions at mock provider, logs, traces, and audit.

### Regression tests

- Pin detector rules, model templates, parser versions, engine versions, and registry snapshots.
- Replay an encrypted/redacted labeled corpus before upgrades.
- Golden route plans and Hermes configs.
- Compatibility matrix for Hermes supported range and every certified backend.

### Acceptance criteria

- Zero canary leakage to an unauthorized mock provider or telemetry sink in the complete adversarial suite.
- 100% block rate for supported high-confidence structured credentials.
- Detector failure cannot result in cloud allow.
- Local-only/offline mode has zero unauthorized network connections under packet/process observation.
- No route outside mandate-bound plan can execute.
- False-positive target is set from a representative Aegis corpus before release; recommended initial gate is below 1% of benign outbound messages blocked, with no relaxation of high-confidence rules.
- Gateway p95 added latency target: under 50 ms for ordinary text requests up to 256 KiB, excluding optional classifier/OCR/archive processing.
- Route-plan decision is reproducible from registry/policy inputs.
- Local model target meets office-specific acceptance and interactive latency thresholds measured on supported hardware.

## Risks and mitigations

| Risk | Mitigation |
|---|---|
| Frontier catalogs change daily | Signed snapshots, explicit effective dates, aliases resolved to revisions, scheduled re-certification. |
| Vendor benchmark inflation or incompatibility | Aegis corpus and harness; record prompts, scaffold, context, quantization, and engine. |
| Local quantization reduces tool reliability | Evaluate each quant/artifact, not just base model; require parser/schema tests. |
| Gateway becomes high-value chokepoint | Minimal code/privilege, memory-safe implementation, fuzzing, HA where needed, fail closed, no raw logging. |
| Secret classifier supply-chain/model risk | Optional local-only tier, pinned artifact, SBOM/signature, adversarial evaluation, deterministic rules remain authoritative. |
| AGPL obligations from TruffleHog | Legal review; isolated invocation/service or alternative implementation; do not copy detector code casually. |
| Gitleaks maintenance shift | Pin v8.30.1/security updates and evaluate Betterleaks/alternatives before deep embedding. |
| Provider credential still leaks through gateway compromise | Least-privilege short-lived provider credentials, secret manager, route-bound broker, rotation and canaries. |
| Automatic routing breaks reproducibility | Persist inputs/plan/reason codes; route only inside approved candidate set. |
| Offline mode silently uses cloud auxiliary model | Explicit complete auxiliary plan plus network deny; integration test all Hermes features. |
| Model/plugin identifier collision | Registry namespace includes provider, endpoint, artifact revision, protocol, and signer. |
| Audit metadata enables inference | Minimize fields, bucket lengths/cost where needed, keyed fingerprints, access controls and retention. |

## Open questions

1. Should route plans be embedded fully in mandates or content-addressed in an immutable registry store?
2. Which processor/jurisdiction/retention fields must be approval-visible for Aegis’s target customers?
3. Can Hermes-managed `/model` and every auxiliary `auto` path be disabled through stable public configuration in the entire supported v0.18.x range, or must the gateway independently reject them?
4. Which protocol should the first gateway implement: OpenAI Responses, Chat Completions, Anthropic Messages, or a normalized internal envelope with provider transforms?
5. Does the project accept an AGPL executable/service dependency, or should TruffleHog remain an optional operator tool?
6. What representative benign and secret-bearing corpus can Aegis legally retain for detector evaluation?
7. What are office-specific quality and latency acceptance thresholds for local routing?
8. Which exact Qwen/GLM/Kimi quantizations have verified GGUF and tool-parser support in the pinned engines? Catalog support must not be assumed.
9. Kimi K3’s official weight repository/license and Seed 2.1 Pro’s first-party model/API documentation remained unresolved on the access date.
10. Should local model endpoints be office-scoped, host-scoped, or shared through a multi-tenant inference pool with cryptographic workload identity?
11. How should reversible placeholders be reinserted without exposing secrets to Hermes or the model—privileged tool RPC, gateway template, or application-specific broker?
12. What is the compatibility and migration policy when a provider retires an immutable dated model?

## Conclusion

Aegis should not chase the frontier-model market by adding ad hoc provider conditionals. Its durable advantage is the identity and trust contract around an agent session. Model routing must become part of that contract.

The practical design is a versioned registry plus an immutable, mandate-bound `RoutePlan`, executed by Hermes under generated configuration and enforced by a local egress/credential gateway. This architecture supports proprietary frontier APIs, local open weights, cost optimization, fallback, and future providers without allowing those features to bypass Aegis’s approval and privacy boundaries.

For the immediate local baseline, Qwen3.6-27B and 35B-A3B are realistic and permissively licensed; Qwen3-Coder-Next is a strong larger coding target. GLM-5.2 and prospective Kimi K3 weights are cluster-scale. Public benchmark tables are useful priors but not routing policy. Aegis must evaluate exact model artifact, quantization, engine, prompt/tool scaffold, and hardware.

For credential protection, detection is defense in depth, not the boundary. The boundary is a process that lacks upstream credentials and cannot make unauthorized network connections, combined with exact-byte scanning before a credential broker sends an approved request. Build that boundary before automatic model routing.

## Repository files reviewed

Materially informing this report:

- `AGENTS.md`
- `BIG_IDEA.md`
- `MVP_FEATURE_SET.md`
- `GO_RESEARCH.md`
- `DEPLOYMENT_PROJECTION_ARCHITECTURE.md`
- `research/HERMES_RUNTIME_RESEARCH.md`
- `research/SECURITY_CONTROL_PLANE_RESEARCH.md`
- `specs/README.md`
- `specs/charter.go`
- `specs/runtime.go`
- `internal/core/model.go`
- `internal/app/service.go`
- `internal/config/config.go`
- `internal/store/store.go`
- `internal/runtime/hermes/hermes.go`
- `examples/office-charter.json`
- Relevant `*_test.go` files located by provider/model/credential/environment searches.

## Sources

All sources accessed 2026-07-17 unless stated otherwise.

1. **Hermes Agent documentation: Model Providers** — Nous Research; live documentation for Hermes v0.18.x; https://hermes-agent.nousresearch.com/docs/integrations/providers — supports provider/authentication/local/custom endpoint findings.
2. **Configuring Models** — Nous Research; live documentation; https://hermes-agent.nousresearch.com/docs/user-guide/configuring-models — supports model/provider selection and model overrides.
3. **Provider Routing** — Nous Research; live documentation; https://hermes-agent.nousresearch.com/docs/user-guide/features/provider-routing — supports auxiliary route and provider-resolution findings.
4. **Fallback Providers** — Nous Research; live documentation; https://hermes-agent.nousresearch.com/docs/user-guide/features/fallback-providers — supports per-turn fallback and cache/history implications.
5. **Provider Runtime** — Nous Research; live developer documentation; https://hermes-agent.nousresearch.com/docs/developer-guide/provider-runtime — supports runtime architecture and transformation behavior.
6. **Model Provider Plugin** — Nous Research; live developer documentation; https://hermes-agent.nousresearch.com/docs/developer-guide/model-provider-plugin — supports custom provider/plugin capabilities.
7. **Hermes model catalog JSON** — Nous Research; updated 2026-07-16; schema v1; https://hermes-agent.nousresearch.com/docs/api/model-catalog.json — supports current Hermes curated identifiers only.
8. **OpenRouter Models API** — OpenRouter; live API metadata; https://openrouter.ai/api/v1/models — supports current aggregator availability, context, modality, tool parameter, price, and surfaced benchmark metadata. Not used as license proof.
9. **Qwen3.6-27B model card and configuration** — Qwen Team/Alibaba; updated 2026-04-24; https://huggingface.co/Qwen/Qwen3.6-27B — supports Apache-2.0, context, multimodality, serving, and vendor benchmark claims.
10. **Qwen3.6-35B-A3B model card and configuration** — Qwen Team/Alibaba; updated 2026-04-24; https://huggingface.co/Qwen/Qwen3.6-35B-A3B — supports Apache-2.0, MoE naming, context, tool-serving examples, and vendor benchmark claims.
11. **Qwen3-Coder-Next model card and configuration** — Qwen Team/Alibaba; updated 2026-02-03; https://huggingface.co/Qwen/Qwen3-Coder-Next — supports Apache-2.0, 80B total/3B active, 262K context, agent/tool claims, and serving guidance.
12. **GLM-5.2 model card and configuration** — Z.ai; updated 2026-07-02; https://huggingface.co/zai-org/GLM-5.2 — supports MIT license, 1M context, architecture/serving and vendor benchmark claims.
13. **gpt-oss-120b model card** — OpenAI; updated 2025-08-26; https://huggingface.co/openai/gpt-oss-120b — supports Apache-2.0, 117B/5.1B active, Harmony format, 131K configuration, agent tools, and 80 GB MXFP4 claim.
14. **llama.cpp repository/releases** — ggml-org; build b10064 published 2026-07-17; https://github.com/ggml-org/llama.cpp — supports GGUF/portable local inference and current maintenance signal.
15. **vLLM repository/releases** — vLLM project; v0.25.1 published 2026-07-14; https://github.com/vllm-project/vllm — supports production high-throughput serving recommendation.
16. **SGLang repository/releases** — SGLang project; v0.5.15.post1 published 2026-07-14; https://github.com/sgl-project/sglang — supports structured/high-performance serving recommendation.
17. **Ollama repository/releases** — Ollama; v0.32.1 published 2026-07-16; https://github.com/ollama/ollama — supports developer local model lifecycle recommendation.
18. **ExLlamaV2 repository/releases** — turboderp-org; v0.3.2 published 2025-07-13; https://github.com/turboderp-org/exllamav2 — supports specialist consumer NVIDIA inference characterization.
19. **TensorRT-LLM repository/releases** — NVIDIA; v1.2.1 published 2026-04-20; https://github.com/NVIDIA/TensorRT-LLM — supports NVIDIA enterprise optimization characterization.
20. **TruffleHog README and releases** — Truffle Security; v3.95.9 published 2026-07-09; https://github.com/trufflesecurity/trufflehog — supports discovery/classification/verification, detector scale, active-validation behavior, AGPL-3.0, redaction, and scanning features.
21. **Gitleaks README and releases** — Gitleaks maintainers; v8.30.1 published 2026-03-21; https://github.com/gitleaks/gitleaks — supports regex/entropy/keywords, composite rules, decoding, stdin/redaction, MIT license, and feature-complete maintenance notice.
22. **detect-secrets README** — Yelp; current repository; https://github.com/Yelp/detect-secrets — supports plugin, entropy, keyword, heuristic, audit, and baseline findings.
23. **About secret scanning** — GitHub; current documentation; https://docs.github.com/en/code-security/secret-scanning/introduction/about-secret-scanning — supports provider-pattern and push-protection context.
24. **MiniLM: Deep Self-Attention Distillation for Task-Agnostic Compression of Pre-Trained Transformers** — Wang et al.; 2020; https://arxiv.org/abs/2002.10957 — supports MiniLM as a compact local classifier architecture, not a secret-specific detector.
25. **DistilBERT, a distilled version of BERT** — Sanh et al.; 2019; https://arxiv.org/abs/1910.01108 — supports DistilBERT as a compact classifier architecture, not a secret-specific detector.
26. **OWASP Top 10 for LLM Applications** — OWASP Foundation; current project; https://owasp.org/www-project-top-10-for-large-language-model-applications/ — supports prompt-injection, sensitive-information-disclosure, supply-chain, and excessive-agency threat categories.
27. **OpenAI model documentation** — OpenAI; current; https://platform.openai.com/docs/models — supports authoritative model/API onboarding requirement.
28. **Claude model overview** — Anthropic; current; https://docs.anthropic.com/en/docs/about-claude/models/overview — supports authoritative Claude capability/context onboarding requirement.
29. **DeepSeek API pricing/quick start** — DeepSeek; current; https://api-docs.deepseek.com/quick_start/pricing — supports first-party API endpoint/pricing validation requirement.
30. **Kimi API guide** — Moonshot AI; current; https://platform.moonshot.ai/docs/guide/start-using-kimi-api — supports first-party Kimi API onboarding requirement.
31. **Z.ai GLM guide** — Z.ai; current; https://docs.z.ai/guides/llm/glm-5 — supports first-party GLM API/model onboarding.
32. **Seed model site** — ByteDance Seed; accessible page documented Seed 2.0 on access date; https://seed.bytedance.com/en/seed2_0 — supports the unresolved status of the requested Seed 2.1 Pro details.

## Appendix: Subagent assignments and findings

### Subagent 1 — Frontier LLM landscape

- **Scope investigated:** requested and additional frontier models through 2026-07-17; openness, license, availability, context, tools, modalities, benchmarks, adoption, strengths, weaknesses.
- **Execution:** launched in parallel via Hermes one-shot with web tools; failed before report generation with an OpenAI Codex stale-stream timeout.
- **Validated findings recovered directly:** live Hermes catalog identifiers; OpenRouter availability/capability metadata; Qwen, GLM, and gpt-oss official model cards; benchmark caveats and unresolved Seed/Kimi weight evidence.
- **Repository files supplied/inspected:** Aegis architecture summary based on files listed in “Repository files reviewed.”
- **Assumptions:** live APIs represent availability at access time; model cards represent vendor claims.
- **Conflicting evidence:** Qwen3.6 vendor SWE results and lower third-party coding index are not directly comparable; Kimi K3 is described as open-weight by an aggregator but no official artifact was verified.
- **Open questions:** official Seed 2.1 Pro documentation; Kimi K3 weight/license artifact; exact model revision retirement terms.
- **Confidence:** high for catalog/API availability and inspected licenses; medium for contexts exposed through aggregators; low-to-medium for cross-model quality rankings.
- **Next step:** implement Aegis workload evaluation before routing.

### Subagent 2 — Local inference

- **Scope investigated:** engines, quantization/GGUF, memory, CPU/GPU/MoE, multi-GPU, workstation tiers, enterprise serving.
- **Execution:** launched in parallel; failed with the same stale-stream timeout.
- **Validated findings recovered directly:** engine repositories/releases; official Qwen/GLM/gpt-oss configurations and serving guidance; transparent memory estimates.
- **Repository files supplied/inspected:** runtime adapter, configuration, deployment architecture, and charter/runtime specifications.
- **Assumptions:** 15% non-KV overhead is an explicit planning estimate, not a benchmark.
- **Conflicting evidence:** vendor maximum context versus feasible local KV capacity; active MoE parameters versus total storage.
- **Open questions:** exact GGUF/quant/tool-parser compatibility for each pinned artifact and measured tokens/s on target hardware.
- **Confidence:** high for first-order memory math and official parameter/context data; medium for tier recommendations; low for performance until benchmarked.
- **Next step:** benchmark 27B, 35B-A3B, and Coder-Next artifacts at 32K/64K contexts on target nodes.

### Subagent 3 — Hermes integration

- **Scope investigated:** providers, identifiers, authentication, configuration, local/API providers, routing, overrides, tool calls, orchestration, Aegis best practices.
- **Execution:** launched in parallel; failed with the same stale-stream timeout. A Nous Portal fallback probe failed because the local Hermes installation was not logged into Portal.
- **Validated findings recovered directly:** Hermes v0.18.2 docs/source, live model catalog, and Aegis Hermes adapter.
- **Repository files inspected:** `internal/runtime/hermes/hermes.go`, `internal/core/model.go`, `internal/app/service.go`, `internal/config/config.go`, runtime specs, and existing Hermes research.
- **Assumptions:** documented v0.18.x behavior is the supported integration contract; upgrades require conformance testing.
- **Conflicting evidence:** Hermes’s broad dynamic routing is operationally useful but conflicts with Aegis’s immutable approval semantics when unconstrained.
- **Open questions:** stable disablement of every model/auxiliary mutation path across the supported version range.
- **Confidence:** high.
- **Next step:** generate isolated config and add route conformance tests.

### Subagent 4 — Credential detection

- **Scope investigated:** TruffleHog, Gitleaks, DeepPass2-BERT, compact classifiers, entropy/regex/hybrid pipelines, false positives, redaction, classification, validation.
- **Execution:** launched in parallel; failed with the same stale-stream timeout.
- **Validated findings recovered directly:** tool READMEs/releases/licenses; MiniLM/DistilBERT papers; no arXiv or GitHub result for the exact name `DeepPass2-BERT`; Aegis credential/runtime code.
- **Repository files inspected:** credential-scope/model validation, Hermes environment mapping, tests, security/control-plane research.
- **Assumptions:** the desired security property is no secret reaching an unauthorized/external processor; deterministic rules remain authoritative.
- **Conflicting evidence:** active validation reduces false positives but can itself disclose credentials and create side effects.
- **Open questions:** legal acceptance of AGPL; labeled Aegis corpus; reversible placeholder design.
- **Confidence:** high for architecture and deterministic tool behavior; medium for classifier value; no confidence assigned to an unverified DeepPass2-BERT artifact.
- **Next step:** build deterministic fail-closed gateway and corpus before classifier pilot.

### Subagent 5 — Aegis architecture synthesis

- **Scope investigated:** provider abstraction, registry, routing, local/cloud/cost/privacy/offline, boundaries, prompt preprocessing, orchestration, plugins, extensibility, performance, and roadmap.
- **Execution:** scheduled after the evidence streams by design; independent Hermes execution was blocked by provider authentication/timeouts, so direct source-grounded synthesis produced this report.
- **Key findings:** mandate-bound `RoutePlan`; gateway/broker boundary; complete auxiliary routing; local OpenAI-compatible contract; phased roadmap.
- **Repository files inspected:** all files listed above, with primary emphasis on core model, app service, Hermes adapter, config, store, specs, and architecture research.
- **Assumptions:** Aegis remains the control plane and Hermes remains the interactive runtime.
- **Conflicting evidence:** dynamic routing improves availability/cost but breaks trust reproducibility unless constrained before launch.
- **Open questions:** route-plan persistence/schema and gateway protocol priority.
- **Confidence:** high for architectural direction; medium for implementation sequencing until protocol prototypes are measured.
- **Recommended next step:** approve Phase 1 design and write the route-plan ADR/schema before production code.
