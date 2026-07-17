# Hermes Runtime Integration Research for Aegis

## Executive summary

Aegis can run a Hermes-backed, authenticated design session without creating a named Hermes profile.

The strongest design is:

1. Aegis authenticates the human user itself.
2. Aegis starts Hermes explicitly as an isolated worker, preferably over:
   - the TUI gateway JSON-RPC protocol for maximum lifecycle and approval control, or
   - direct `AIAgent` library integration for a Python-only deployment.
3. The worker receives a disposable `HERMES_HOME`, set before Hermes is imported or started.
4. The design phase exposes only a narrowly selected, read-only tool surface and no MCP servers or third-party plugins.
5. The model produces a declarative provisioning proposal, not filesystem changes.
6. Aegis validates and displays the proposal.
7. Only an authenticated, explicit approval transition invokes a separate deterministic provisioner.
8. The provisioner—not the model—creates any real profile, skills, MCP configuration, plugin configuration, or credentials.

This is materially safer than telling Hermes “do not provision until approved.” Prompt instructions can guide behavior, but they cannot establish a security boundary. The hard boundary must be implemented through process isolation, tool registration, filesystem permissions, API authentication, and an Aegis-owned approval state machine.

Important distinctions:

- Hermes messaging pairing authenticates messaging users. It is not API-server authentication.
- `API_SERVER_KEY` authenticates callers to the Hermes HTTP API, but it is one bearer secret rather than a per-user identity system.
- ACP “authentication” confirms that Hermes has usable model-provider credentials. It does not authenticate an untrusted local ACP client; the implementation explicitly assumes local stdio trust.
- Provider authentication authorizes Hermes to call an LLM provider. It does not authenticate the Aegis user.
- A Hermes profile isolates Hermes state, but it is not a filesystem sandbox.
- CLI one-shot mode, `hermes -z`, is unsuitable for an approval-sensitive design phase because it explicitly enables YOLO mode and auto-bypasses tool approvals.

## Research basis

The local Hermes checkout inspected was:

- Repository: a local Hermes Agent checkout (operator-specific path omitted)
- Commit: `594308d4bbe95548c9fe418bb10c449099426f93`
- Commit subject: `fix(credential-pool): throttle "no available entries" log to stop Windows log-lock storm (contributes to #62698) (#66338)`
- Commit date reported by Git: `2026-07-17 13:08:46 -0400`

The checkout already contained an untracked `.install_method` marker. No files were changed during this research.

The current official documentation site was reachable. All principal URLs cited below returned HTTP 200 when checked.

This report originated before the Aegis implementation existed and records the Hermes integration contracts used during design. Aegis now implements the selected TUI-gateway and process-launch integrations; current behavior is documented in the repository `README.md` and `internal/runtime/hermes`.

---

## 1. Integration surfaces

Hermes documents three external protocols plus direct Python embedding.

| Surface | Transport | Authentication properties | Best fit for Aegis |
|---|---|---|---|
| TUI gateway | JSON-RPC over stdio; WebSocket also exists | Stdio normally inherits host-process trust; no API bearer boundary documented | Best full-control local worker protocol |
| HTTP API server | HTTP + SSE | Mandatory `API_SERVER_KEY` bearer token | Best remote/language-neutral integration |
| ACP | JSON-RPC over stdio | “Auth” validates provider readiness; local-trust client model | Good if Aegis already implements ACP |
| Python library | In-process Python calls | Aegis must supply both user auth and provider auth boundaries | Simplest Python embedding, highest coupling |

Official overview:

- https://hermes-agent.nousresearch.com/docs/developer-guide/programmatic-integration
- Local: `<hermes-checkout>/website/docs/developer-guide/programmatic-integration.md:7-17`
- Core constructor: `<hermes-checkout>/run_agent.py:418-567`

### 1.1 TUI gateway JSON-RPC

The TUI gateway exposes the richest host-control surface:

- Session create, activate, close, interrupt, branch, compress, history, and status
- Prompt submission and mid-turn steering
- Approval, clarification, sudo, and secret responses
- MCP reload
- Streaming tool lifecycle events
- Slash-command dispatch
- Active process and subagent control

Relevant documented methods include:

```text
prompt.submit
session.create
session.activate
session.close
session.interrupt
session.history
session.branch
approval.respond
clarify.respond
sudo.respond
secret.respond
reload.mcp
```

Relevant events include:

```text
message.delta
message.complete
tool.start
tool.progress
tool.complete
approval.request
clarify.request
sudo.request
secret.request
gateway.ready
```

References:

- Local: `<hermes-checkout>/website/docs/developer-guide/programmatic-integration.md:36-60`
- Source: `<hermes-checkout>/tui_gateway/server.py`
- WebSocket wrapper: `<hermes-checkout>/tui_gateway/ws.py`

This is the strongest candidate if Aegis wants:

- an explicit Hermes subprocess,
- a private stdio control channel,
- streaming progress,
- session branching,
- interactive approvals,
- and minimal dependence on Python internals.

The stdio transport can itself be a hard local capability boundary if Aegis alone owns the child process and pipe. It should not be treated as remotely authenticated merely because JSON-RPC is used.

### 1.2 HTTP API server

The HTTP API offers:

- OpenAI Chat Completions
- OpenAI Responses API
- Long-running Runs API with SSE
- Run cancellation
- Run approval resolution
- Session CRUD and branching
- Skills and toolsets discovery
- Health and capability discovery

Official documentation:

- https://hermes-agent.nousresearch.com/docs/user-guide/features/api-server
- Local: `<hermes-checkout>/website/docs/user-guide/features/api-server.md`
- Source: `<hermes-checkout>/gateway/platforms/api_server.py`

Particularly useful endpoints for Aegis are:

```text
GET  /v1/capabilities
POST /v1/runs
GET  /v1/runs/{run_id}
GET  /v1/runs/{run_id}/events
POST /v1/runs/{run_id}/approval
POST /v1/runs/{run_id}/stop

GET  /api/sessions
POST /api/sessions
POST /api/sessions/{id}/fork
POST /api/sessions/{id}/chat
POST /api/sessions/{id}/chat/stream

GET  /v1/skills
GET  /v1/toolsets
```

References:

- Runs and approval: local API documentation lines 240-295
- Session API: lines 332-360
- Capability discovery: lines 201-222
- Skills/toolset discovery: lines 362-377

The Runs API is more suitable than plain Chat Completions when Aegis needs:

- attach/detach progress streaming,
- durable run status,
- cancellation,
- and human approval resolution.

#### API authentication

The API server requires a bearer secret:

```http
Authorization: Bearer <API_SERVER_KEY>
```

The official documentation says `API_SERVER_KEY` is required even for loopback deployments:

- Local: `<hermes-checkout>/website/docs/user-guide/features/api-server.md:400-424`

The source uses a timing-safe comparison:

- `<hermes-checkout>/gateway/platforms/api_server.py:1234-1264`
- `hmac.compare_digest()` is used at line 1254.

This is a real transport-level enforcement point.

However, it is not a complete Aegis user-authentication system:

- It is one shared bearer credential unless Aegis runs separate instances.
- It does not itself express Aegis users, roles, tenant membership, or approval authority.
- Aegis should keep `API_SERVER_KEY` server-side and authenticate users using its own identity/session mechanism.
- Browsers should not normally receive the Hermes key.
- If browser-to-Hermes access is unavoidable, CORS must be a narrow explicit allowlist.

### 1.3 ACP

ACP offers:

- session creation/load/resume/fork/list,
- prompt submission,
- tool events,
- cancellation,
- model switching,
- and dangerous-command permission requests.

References:

- https://hermes-agent.nousresearch.com/docs/developer-guide/acp-internals
- Local: `<hermes-checkout>/website/docs/developer-guide/acp-internals.md`
- Source:
  - `<hermes-checkout>/acp_adapter/server.py`
  - `<hermes-checkout>/acp_adapter/session.py`
  - `<hermes-checkout>/acp_adapter/permissions.py`
  - `<hermes-checkout>/acp_adapter/auth.py`

ACP sessions create an agent with the `hermes-acp` toolset:

- `<hermes-checkout>/website/docs/developer-guide/acp-internals.md:114-129`
- `<hermes-checkout>/acp_adapter/session.py:623-629`

Dangerous terminal approvals are bridged into ACP permissions. Timeouts and bridge failures deny by default:

- Local ACP internals: lines 91-102.

#### ACP authentication is not caller authentication

ACP reuses the active Hermes provider credentials rather than implementing a separate authentication store:

- `<hermes-checkout>/website/docs/developer-guide/acp-internals.md:143-152`

The source confirms that:

- `detect_provider()` succeeds when runtime provider credentials exist:
  `<hermes-checkout>/acp_adapter/auth.py:11-38`
- ACP advertises the provider and a terminal `hermes-setup` method:
  `<hermes-checkout>/acp_adapter/auth.py:41-79`
- `authenticate()` merely checks that the requested method corresponds to the configured provider:
  `<hermes-checkout>/acp_adapter/server.py:899-919`
- The source comment describes ACP as “stdio-only, local-trust”:
  `<hermes-checkout>/acp_adapter/server.py:900-905`

Therefore, ACP authentication answers:

> “Can this Hermes instance call a configured model provider?”

It does not answer:

> “Is this remote person an authenticated Aegis user authorized to provision artifacts?”

Aegis must answer the latter.

### 1.4 Direct Python library

The direct library API is:

```python
from run_agent import AIAgent

agent = AIAgent(
    provider=...,
    model=...,
    api_key=...,
    quiet_mode=True,
    enabled_toolsets=[...],
    skip_context_files=True,
    skip_memory=True,
    ephemeral_system_prompt=...,
)

result = agent.run_conversation(
    user_message=...,
    system_message=...,
    conversation_history=...,
    task_id=...,
)
```

Official guide:

- https://hermes-agent.nousresearch.com/docs/guides/python-library
- Local: `<hermes-checkout>/website/docs/guides/python-library.md`

Exact source signatures:

- `AIAgent.__init__`: `<hermes-checkout>/run_agent.py:418-491`
- `run_conversation`: `<hermes-checkout>/run_agent.py:6171-6223`
- `chat`: `<hermes-checkout>/run_agent.py:6228-6240`

Useful constructor controls include:

- `api_key`
- `provider`
- `model`
- `enabled_toolsets`
- `disabled_toolsets`
- `ephemeral_system_prompt`
- `session_id`
- `platform`
- `skip_context_files`
- `skip_memory`
- `session_db`
- callback hooks for streaming, tool lifecycle, clarification, and events

This is convenient, but it couples Aegis to internal Python APIs more tightly than the documented wire protocols. A new `AIAgent` should be created per concurrent task; instances are not thread-safe.

A further caveat: even with `session_db=None`, initialization creates the active Hermes home’s `sessions/` directory:

- `<hermes-checkout>/agent/agent_init.py:1272-1299`

Therefore, an in-process design session is not guaranteed to leave the normal Hermes home untouched unless Aegis sets a disposable `HERMES_HOME` before Hermes modules are imported.

---

## 2. Profiles and profile-free execution

### 2.1 What a Hermes profile is

A profile is a separate `HERMES_HOME` with its own:

- `config.yaml`
- `.env`
- `SOUL.md`
- memories
- sessions and `state.db`
- skills
- cron state
- plugins
- MCP configuration
- gateway state and logs

Official documentation:

- https://hermes-agent.nousresearch.com/docs/user-guide/profiles
- Local: `<hermes-checkout>/website/docs/user-guide/profiles.md:5-12`

The default profile is simply the root Hermes home, normally `~/.hermes`:

- Local profile docs: lines 270-302
- Source: `<hermes-checkout>/hermes_cli/profiles.py:1-19`
- Profile resolution: `<hermes-checkout>/hermes_cli/profiles.py:264-291,367-380`

A named profile normally lives under:

```text
~/.hermes/profiles/<name>/
```

and `hermes -p <name>` sets the corresponding profile scope.

### 2.2 Profiles are not sandboxes

This is a crucial hard boundary distinction.

The official profile documentation states:

- A profile isolates Hermes state.
- A working directory is separate.
- A sandbox controls filesystem access.
- On the local terminal backend, profiles retain the OS user’s filesystem access.
- `SOUL.md` cannot enforce a workspace boundary.

Reference:

- `<hermes-checkout>/website/docs/user-guide/profiles.md:125-149`

Therefore:

> Creating a profile does not prevent Hermes from reading or modifying Aegis, other profiles, or the user’s home when terminal or file tools are available.

Aegis should not use profile isolation as a substitute for:

- process isolation,
- filesystem permissions,
- containerization,
- or tool removal.

### 2.3 Running without creating a named profile

There are three interpretations of “without creating a Hermes profile”:

#### Option A: Use the existing default Hermes home

This does not create a new named profile, but it uses and may modify the existing default profile’s state.

Potential writes include:

- sessions,
- state database rows,
- cache files,
- memory,
- logs,
- plugin state.

This is appropriate only if Aegis is intentionally integrated into the user’s normal Hermes installation.

#### Option B: Use a disposable custom `HERMES_HOME`

Start a child process with:

```text
HERMES_HOME=/private/aegis-runtime/<session-id>
```

This is not a named profile under `~/.hermes/profiles/`. It is an isolated Hermes home selected directly by environment.

Advantages:

- No named profile is created.
- The user’s normal `~/.hermes` state is not used.
- Session state and runtime directories can be deleted after the design session.
- Provider credentials can be supplied directly as process secrets.
- Aegis can generate a minimal config containing no MCPs or enabled plugins.

Important implementation detail: set `HERMES_HOME` before importing Hermes. Some modules bind paths at import time. The local profile-builder design document records a concrete case where the skills hub captures `SKILLS_DIR` at module import:

- `<hermes-checkout>/docs/design/profile-builder.md:42-61`

For that reason, a fresh subprocess per disposable home is safer than changing `HERMES_HOME` inside a long-running Python process.

#### Option C: Run in a disposable container or restricted worker account

This provides the strongest boundary:

- temporary Hermes home,
- no mount of the real `~/.hermes`,
- no mount of the Aegis repository during design,
- no normal user home credentials,
- tightly controlled network egress.

This is the recommended approach for untrusted or multi-tenant design sessions.

### 2.4 Profiles should be a provisioning result, not a design prerequisite

Aegis does not need to create the final profile to discuss or design it.

The design session can operate on a declarative object such as:

```json
{
  "kind": "hermes-profile-proposal",
  "name": "researcher",
  "description": "Research-oriented Hermes agent",
  "model": {
    "provider": "openrouter",
    "model": "anthropic/claude-sonnet-4.6"
  },
  "toolsets": ["web", "session_search"],
  "skills": [
    {"id": "research-arxiv", "source": "bundled"}
  ],
  "mcp_servers": [],
  "plugins": [],
  "gateway": {
    "enabled": false
  }
}
```

Aegis can validate and render this without running:

```text
hermes profile create
hermes skills install
hermes mcp add
hermes plugins install
```

This cleanly separates design from provisioning.

---

## 3. Prompt assembly and keeping Hermes explicit

Official documentation:

- https://hermes-agent.nousresearch.com/docs/developer-guide/prompt-assembly
- Local: `<hermes-checkout>/website/docs/developer-guide/prompt-assembly.md`
- Main source:
  - `<hermes-checkout>/agent/system_prompt.py`
  - `<hermes-checkout>/agent/prompt_builder.py`
  - `<hermes-checkout>/run_agent.py`

### 3.1 Prompt tiers

Hermes assembles the cached prompt in three tiers:

1. Stable:
   - Hermes identity from `SOUL.md` or default identity
   - tool/model guidance
   - skills index
   - environment and platform hints
2. Context:
   - caller-supplied `system_message`
   - project context files
3. Volatile:
   - memory snapshot
   - user profile snapshot
   - external memory provider
   - time, session, model, and provider metadata

Reference:

- `<hermes-checkout>/website/docs/developer-guide/prompt-assembly.md:27-42`

API-call-time additions include:

- `ephemeral_system_prompt`
- prefill messages
- gateway session overlays
- `pre_llm_call` plugin context

Reference:

- Local prompt assembly: lines 238-249.

### 3.2 Context-file discovery

Project instructions are loaded in priority order, first match wins:

1. `.hermes.md` / `HERMES.md`
2. `AGENTS.md`
3. `CLAUDE.md`
4. `.cursorrules` or Cursor rule files

Reference:

- `<hermes-checkout>/website/docs/developer-guide/prompt-assembly.md:186-236`

For an Aegis-controlled design session, use:

```python
skip_context_files=True
skip_memory=True
```

This avoids inheriting unrelated project rules, user memory, and normal-profile persona state.

When context files are skipped, Hermes uses the hardcoded default identity unless `load_soul_identity=True` is requested. The documented default begins:

> “You are Hermes Agent, an intelligent AI assistant created by Nous Research.”

Reference:

- `<hermes-checkout>/website/docs/developer-guide/prompt-assembly.md:156-184`
- Constructor controls: `<hermes-checkout>/run_agent.py:478-480`

### 3.3 “Keep Hermes explicit”

There are two separate requirements:

#### Model-facing explicitness

Keep the built-in Hermes identity instead of replacing it with an anonymous Aegis persona. Add an Aegis-specific ephemeral instruction such as:

> “You are Hermes Agent operating inside Aegis’ design-session workflow. Identify the runtime as Hermes when relevant. You may design Hermes artifacts, but you do not have provisioning capabilities in this phase.”

This is useful, but it remains prompt-only behavior.

#### User-facing explicitness

Aegis itself should present non-model-generated provenance:

```text
Runtime: Hermes Agent
Integration: Aegis design session
Provider: <resolved provider>
Mode: Design only — no provisioning capability
```

Where possible, obtain this from protocol metadata:

- ACP advertises `agent_info.name="hermes-agent"`:
  `<hermes-checkout>/acp_adapter/server.py:884-897`
- API `/v1/capabilities` reports the Hermes platform and supported features.
- API `/v1/models` advertises `hermes-agent` for the default home or the profile name for a named profile.

This UI provenance is a hard Aegis behavior. Asking the model to mention Hermes is not.

---

## 4. Gateway authentication and pairing

### 4.1 Messaging authorization

The messaging gateway uses multiple authorization sources:

1. Platform-specific allow-all flag
2. Platform allowlist
3. Pairing approval store
4. Global allow-all
5. Default deny

Official reference:

- https://hermes-agent.nousresearch.com/docs/developer-guide/gateway-internals
- Local: `<hermes-checkout>/website/docs/developer-guide/gateway-internals.md:90-109`
- Source: `<hermes-checkout>/gateway/authz_mixin.py:279-288`

The implementation also has specialized trusted-upstream and adapter-auth cases:

- Home Assistant events
- HMAC-authenticated webhooks
- authenticated relay delivery
- platform-specific role and chat authorization

See:

- `<hermes-checkout>/gateway/authz_mixin.py:290-378`

### 4.2 Pairing storage and security

Pairing records:

- pending pairing requests,
- approved users,
- rate-limit and lockout state.

Source:

- `<hermes-checkout>/gateway/pairing.py`

Notable hard properties in the local implementation:

- Codes are generated with `secrets.choice`.
- Codes are stored only as salted SHA-256 hashes.
- Comparisons use `secrets.compare_digest`.
- Pending requests expire.
- Pairing requests are rate-limited.
- Repeated failed approvals trigger platform lockout.
- Approved records are persisted with secure writes.
- Revocation removes the grant.

Exact references:

- Store layout and profile scoping:
  `<hermes-checkout>/gateway/pairing.py:235-282`
- Approval lookup:
  `<hermes-checkout>/gateway/pairing.py:344-406`
- Code generation and hashing:
  `<hermes-checkout>/gateway/pairing.py:408-469`
- Constant-time verification:
  `<hermes-checkout>/gateway/pairing.py:471-537`
- Rate limiting and lockout:
  `<hermes-checkout>/gateway/pairing.py:583-623`

In multiplex mode, pairing can be profile-scoped:

```text
<HERMES_HOME>/profiles/<name>/pairing/
```

Without a profile, the global pairing store is used.

### 4.3 Pairing is not appropriate as Aegis web authentication

Pairing is designed for messaging identities such as Telegram or Discord user IDs. Aegis should not repurpose it as its main web/session authentication mechanism.

Recommended boundary:

- Aegis authenticates the human with its normal identity provider.
- Aegis maps the authenticated principal to an Aegis design session.
- Aegis calls Hermes through a private child-process pipe or server-side API token.
- Hermes pairing remains relevant only if Aegis provisions a messaging gateway later.

---

## 5. Toolsets and capability restriction

Official documentation:

- https://hermes-agent.nousresearch.com/docs/reference/toolsets-reference
- Local: `<hermes-checkout>/website/docs/reference/toolsets-reference.md`

Toolsets are named groups of tools. Hermes supports:

- core toolsets,
- composite toolsets,
- platform toolsets,
- dynamic MCP toolsets,
- plugin toolsets,
- custom toolsets.

The hard runtime tool list is generated during agent initialization:

- `<hermes-checkout>/agent/agent_init.py:1206-1233`

The model can call only tools registered in that list. This is substantially stronger than a prompt saying not to use a tool.

### 5.1 Relevant platform defaults

Documented defaults include:

- `hermes-cli`: broad interactive surface including file, terminal, memory, skills, delegation, cron, and code execution.
- `hermes-acp`: coding-focused; still includes meaningful local capabilities.
- `hermes-api-server`: nearly full surface, minus clarify and TTS.

Reference:

- `<hermes-checkout>/website/docs/reference/toolsets-reference.md:89-118`

Neither `hermes-acp` nor `hermes-api-server` should be accepted unchanged for a pre-approval design phase.

### 5.2 Recommended design-phase tool policy

Prefer a dedicated custom toolset containing only purpose-built Aegis design tools, for example:

```text
aegis_catalog_search
aegis_get_provider_catalog
aegis_get_skill_metadata
aegis_get_mcp_catalog_entry
aegis_validate_profile_proposal
aegis_submit_profile_proposal
```

These should:

- return data only,
- not invoke shells,
- not write files,
- not create profiles,
- not install skills or plugins,
- not connect arbitrary MCP servers,
- not expose raw credentials.

If external research is required, add only read-oriented web tools. Do not assume the built-in `safe` bundle is perfectly side-effect-free: it includes image generation in the current reference.

During design, do not enable:

- `file`
- `terminal`
- `code_execution`
- `skills`
- `cronjob`
- `computer_use`
- `kanban`
- arbitrary plugin toolsets
- arbitrary MCP toolsets
- broad `all` or `*`

Also apply OS-level restrictions. Tool filtering reduces the callable surface, but plugins and implementation defects remain part of the process’s trusted computing base.

### 5.3 Do not use CLI one-shot mode

`hermes -z` explicitly:

- sets `HERMES_YOLO_MODE=1`,
- auto-bypasses shell/tool approvals,
- loads normal rules, memory, and project context by default,
- and uses the caller’s working directory.

Source:

- `<hermes-checkout>/hermes_cli/oneshot.py:1-20`
- YOLO setup: `<hermes-checkout>/hermes_cli/oneshot.py:218-221`

Therefore:

> `hermes -z` is convenient for trusted automation, but it is the wrong entrypoint for an approval-gated Aegis design session.

---

## 6. MCP integration

Official references:

- https://hermes-agent.nousresearch.com/docs/user-guide/features/mcp
- https://hermes-agent.nousresearch.com/docs/reference/mcp-config-reference
- https://hermes-agent.nousresearch.com/docs/guides/use-mcp-with-hermes
- Local config reference:
  `<hermes-checkout>/website/docs/reference/mcp-config-reference.md`

Hermes supports:

- stdio MCP servers via `command` and `args`,
- HTTP MCP servers via `url`,
- headers,
- TLS verification,
- client certificates,
- OAuth 2.1 PKCE,
- include/exclude tool filters,
- MCP resources and prompts,
- per-server parallel-call declarations.

Each MCP server generates a dynamic toolset:

```text
mcp-<server>
```

and tool names:

```text
mcp_<server>_<tool>
```

References:

- Toolset generation:
  `<hermes-checkout>/website/docs/reference/toolsets-reference.md:120-138`
- Tool naming:
  `<hermes-checkout>/website/docs/reference/mcp-config-reference.md:245-274`

### 6.1 MCP defaults are broader than Aegis should assume

Hermes’ platform configuration normally makes all globally enabled MCP servers available unless:

- an explicit server allowlist is present, or
- the special `no_mcp` sentinel is used.

Source:

- `<hermes-checkout>/hermes_cli/tools_config.py:1888-1916`

ACP also explicitly expands configured MCP servers into the session’s enabled toolsets:

- `<hermes-checkout>/acp_adapter/session.py:129-133,623-629`

Therefore, merely selecting `hermes-acp` is not a guarantee that no MCP tools will appear.

For Aegis design sessions:

- use a disposable config with no `mcp_servers`, or
- explicitly configure `no_mcp`,
- and verify the resolved tool list before accepting prompts.

### 6.2 MCP filters are not semantic safety controls

`include` and `exclude` filter by server-native tool name. They do not establish whether a tool is read-only, reversible, tenant-safe, or non-provisioning.

For example, allowing a tool named `preview_configuration` is safe only if the MCP server implementation actually keeps it read-only.

Hard recommendation:

- Do not connect user-selected MCP servers during design.
- Treat “add this MCP server” as proposal data.
- Connect and discover the server only after approval, ideally in a staging environment.
- Revalidate the discovered tool inventory before enabling it.
- Never let model text directly become an MCP `command`, `args`, URL, header, certificate path, or environment map without schema and policy validation.

### 6.3 MCP OAuth token persistence

OAuth tokens are persisted under:

```text
~/.hermes/mcp-tokens/<server>.json
```

Reference:

- `<hermes-checkout>/website/docs/reference/mcp-config-reference.md:276-292`

A disposable `HERMES_HOME` ensures design-time tests do not write OAuth state into the user’s real Hermes home.

---

## 7. Plugins

Official references:

- https://hermes-agent.nousresearch.com/docs/user-guide/features/plugins
- https://hermes-agent.nousresearch.com/docs/developer-guide/plugins
- Local:
  `<hermes-checkout>/website/docs/user-guide/features/plugins.md`

Plugins can:

- register arbitrary tools,
- register lifecycle hooks,
- add slash and CLI commands,
- inject messages,
- register gateway platforms,
- register providers,
- run host-owned LLM calls,
- provide memory and context engines.

A plugin is Python code executing inside the Hermes process. It must be treated as trusted code, not as passive configuration.

### 7.1 Plugin activation behavior

General user plugins are opt-in through:

```yaml
plugins:
  enabled:
    - plugin-name
```

Project-local plugins are disabled unless:

```text
HERMES_ENABLE_PROJECT_PLUGINS=true
```

References:

- Project plugins:
  `<hermes-checkout>/website/docs/user-guide/features/plugins.md:90-93`
- General plugin opt-in:
  lines 145-182.

However, several categories bypass `plugins.enabled`, including bundled platform and backend infrastructure. Provider, memory, and context-engine plugins have their own selectors.

Therefore, for a strong Aegis design boundary:

- use a disposable, minimal config,
- do not mount user plugin directories,
- keep project-plugin loading disabled,
- do not enable user-installed providers/backends except those explicitly required,
- and preferably run a controlled Hermes build/container whose bundled plugin set is known.

### 7.2 Pre-LLM hooks can alter the prompt

A `pre_llm_call` plugin can add context to the current user message:

- Prompt assembly reference:
  `<hermes-checkout>/website/docs/developer-guide/prompt-assembly.md:238-249`
- Plugin hook catalog:
  `<hermes-checkout>/website/docs/user-guide/features/plugins.md:188-203`

This means prompt provenance is not controlled solely by `system_message` or `ephemeral_system_prompt`. Loading untrusted plugins can alter design behavior even if their tools are not selected.

---

## 8. Sessions and persistence

Official references:

- https://hermes-agent.nousresearch.com/docs/user-guide/sessions
- https://hermes-agent.nousresearch.com/docs/developer-guide/session-storage
- Local:
  - `<hermes-checkout>/website/docs/user-guide/sessions.md`
  - `<hermes-checkout>/website/docs/developer-guide/session-storage.md`

Hermes stores:

- session metadata,
- system-prompt snapshot,
- messages,
- tool calls and outputs,
- token and billing data,
- model configuration,
- lineage.

The canonical store is:

```text
<HERMES_HOME>/state.db
```

Source:

- `<hermes-checkout>/hermes_state.py`

### 8.1 Implications for Aegis

A design session may contain:

- user requirements,
- proposed credentials or endpoint names,
- internal infrastructure plans,
- selected plugins and MCP servers,
- generated profile identity.

Aegis must explicitly choose whether this should be:

1. persisted in the user’s normal Hermes history,
2. persisted in an Aegis-owned session store,
3. or discarded after the design flow.

Recommended default:

- Use a disposable Hermes home and Aegis-owned transcript storage.
- Persist only the sanitized proposal and approval audit in Aegis.
- Delete the disposable Hermes state after completion.
- Do not rely on `/compress` as deletion; compression reduces context but is not a privacy delete.

The sessions documentation explicitly notes that compression is not privacy deletion:

- `<hermes-checkout>/website/docs/user-guide/sessions.md:60-66`

### 8.2 Session IDs versus stable memory scopes

The API supports:

- `X-Hermes-Session-Id` for transcript correlation,
- `X-Hermes-Session-Key` for stable long-term memory scope.

Reference:

- `<hermes-checkout>/website/docs/user-guide/features/api-server.md:379-390`

For a design-only session with `skip_memory=True`, Aegis generally should not supply a durable memory key. If long-term memory is intentionally enabled later, bind the key to the authenticated Aegis tenant/user—not to user-controlled input.

---

## 9. Recommended Aegis architecture

## 9.1 Phase 1: authenticated design

```text
Authenticated user
       |
       v
Aegis application
  - verifies identity
  - creates design-session record
  - owns authorization and CSRF/replay controls
       |
       v
Hermes design worker
  - explicit Hermes provenance
  - private stdio RPC or loopback bearer API
  - disposable HERMES_HOME
  - no real profile
  - no user plugins
  - no MCP
  - no file/terminal/code-execution tools
       |
       v
Validated provisioning proposal
```

Recommended transport order:

1. TUI gateway stdio for a local Aegis host needing full event and approval control.
2. HTTP Runs API for a remote or language-neutral deployment.
3. Direct `AIAgent` for a tightly version-pinned Python deployment.
4. ACP only if Aegis already speaks ACP and accepts its local-trust/auth semantics.

### Worker setup

Set before process start:

```text
HERMES_HOME=<disposable directory>
HERMES_ENABLE_PROJECT_PLUGINS=false
```

Provide provider credentials through:

- a process secret,
- direct constructor arguments,
- or a trusted secret-source mechanism.

Do not copy the user’s normal `.env`, `config.yaml`, plugins, MCP tokens, memory, or sessions into the disposable home.

Use:

```python
skip_context_files=True
skip_memory=True
quiet_mode=True
```

Use a narrow, Aegis-specific toolset rather than `hermes-cli`, `hermes-acp`, or `hermes-api-server`.

### Authentication layering

Keep these layers separate:

| Layer | Mechanism |
|---|---|
| Human identity | Aegis authentication |
| Approval authority | Aegis RBAC/policy |
| Aegis-to-Hermes transport | Private stdio capability or `API_SERVER_KEY` |
| Hermes-to-model provider | Provider API key/OAuth/credential pool |
| Future messaging user access | Hermes allowlists and pairing |
| Future MCP server access | MCP headers, OAuth, mTLS, or subprocess env |

Do not treat one layer as proof of another.

## 9.2 Proposal contract

Require Hermes to return a structured proposal rather than commands.

Suggested fields:

```json
{
  "schema_version": 1,
  "runtime": "hermes-agent",
  "mode": "design",
  "proposal_id": "uuid",
  "profile": {
    "name": "string",
    "description": "string",
    "soul": "string|null"
  },
  "model": {
    "provider": "string",
    "model": "string"
  },
  "toolsets": ["string"],
  "skills": [
    {
      "id": "string",
      "source": "bundled|optional|hub",
      "version": "string|null"
    }
  ],
  "mcp_servers": [
    {
      "name": "string",
      "transport": "stdio|http",
      "catalog_id": "string|null",
      "requested_tools": ["string"],
      "requires_credentials": true
    }
  ],
  "plugins": [
    {
      "name": "string",
      "source": "bundled|repository|package",
      "enabled": false
    }
  ],
  "gateway": {
    "enabled": false,
    "platforms": []
  },
  "warnings": ["string"]
}
```

Do not accept raw executable MCP `command` or plugin installation shell fragments from model output unless a separate policy maps them to a trusted catalog entry.

## 9.3 Approval transition

The Aegis approval should be a real state transition:

```text
DRAFT
  -> VALIDATED
  -> AWAITING_APPROVAL
  -> APPROVED
  -> PROVISIONING
  -> PROVISIONED | FAILED | ROLLED_BACK
```

Approval should be bound to:

- authenticated user,
- tenant,
- proposal ID,
- canonical proposal digest,
- expiration time,
- target environment,
- and expected prior state.

The provisioner should reject:

- changed proposals,
- stale approvals,
- replayed approval tokens,
- unapproved secret requirements,
- unknown skills/plugins/MCP catalog entries.

This is a hard guarantee when enforced in code and storage. A chat message saying “approved” is not sufficient unless Aegis maps it through authenticated UI/action semantics and binds it to the exact proposal.

## 9.4 Phase 2: deterministic provisioning

The provisioner should be a separate component with the required write capability. The design worker should never acquire that capability after an approval message.

Recommended flow:

1. Re-read the approved proposal by ID.
2. Verify its canonical digest.
3. Resolve all catalog references.
4. Validate profile name and destination.
5. Create a staging Hermes home/profile.
6. Write model and tool configuration.
7. Add MCP configuration in disabled state first.
8. Install skills.
9. Install plugins but leave third-party plugins disabled unless enablement was separately approved.
10. Run validation and health checks.
11. Atomically publish the completed directory where feasible.
12. Record exact artifacts, versions, and resulting paths.
13. Return a provisioning receipt.

A separate process is preferable because it can:

- run under a distinct OS identity,
- receive narrow filesystem permissions,
- hold provisioning secrets only briefly,
- and expose only typed provisioning operations.

### Current profile-builder atomicity caveat

The local design proposal for the dashboard profile builder is explicitly marked:

> “Status: design proposal (not yet implemented)”

Reference:

- `<hermes-checkout>/docs/design/profile-builder.md:1-5`

It also documents that profile creation plus hub-skill installation is not fully atomic:

- synchronous profile/config/MCP/skill steps commit first,
- hub skill installs run asynchronously,
- failures may leave a partially provisioned profile.

Reference:

- `<hermes-checkout>/docs/design/profile-builder.md:63-79,81-107`

Aegis should not assume a future Hermes builder provides transactionality. If Aegis needs all-or-nothing semantics, use staging and rollback outside the current profile-create flow.

---

## 10. Hard guarantees versus prompt-only behavior

| Requirement | Hard enforcement mechanism | Prompt-only if implemented as… |
|---|---|---|
| User must be authenticated | Aegis auth middleware/session verification | “Only authorized users may continue” in the system prompt |
| Only approver may provision | Aegis RBAC and signed approval transition | Asking Hermes whether the user seems authorized |
| No profile before approval | No profile-create capability in design worker; disposable `HERMES_HOME`; separate provisioner | “Do not create a profile yet” |
| No artifact writes before approval | Read-only tool list, filesystem ACLs, no mounted real Hermes home | “Do not write files” |
| No MCP side effects | No MCP connections or MCP tool registration | Asking the model not to call MCP tools |
| No plugin side effects | No untrusted plugins mounted/enabled; isolated controlled build | Telling plugins or Hermes to behave safely |
| Hermes remains explicit | Aegis UI provenance and protocol metadata | Asking the model to mention Hermes |
| Exact approved plan is provisioned | Digest-bound proposal and deterministic provisioner | Natural-language “yes, use the plan above” |
| Provider credentials are protected | Secret injection and process isolation | Instructing the model not to reveal keys |
| Session data is not retained | Disposable home deletion and storage policy | Asking Hermes to forget the conversation |
| Dangerous commands require approval | Interactive approval bridge and deny-by-default callback | Prompt instruction to ask before commands |
| Workspace isolation | Container/OS permissions/no mount | Profile isolation or `SOUL.md` instruction |
| Plugin/MCP allowlist | Code-level catalog and configuration validation | Tool descriptions claiming read-only behavior |

### Hermes approval subsystem limits

Hermes has a substantial dangerous-command approval system, including:

- per-session state,
- smart approval,
- permanent allowlists,
- user deny rules,
- a small unconditional hardline blocklist.

Source:

- `<hermes-checkout>/tools/approval.py`

Some catastrophic commands are blocked even under YOLO or `approvals.mode=off`:

- `<hermes-checkout>/tools/approval.py:333-349`

This is useful defense in depth, not a general transaction boundary:

- It primarily detects known dangerous command patterns.
- It does not make arbitrary plugins or MCP tools safe.
- It does not ensure that every filesystem write corresponds to an approved Aegis proposal.
- Non-interactive modes can auto-approve unless the host integrates the correct approval path.
- One-shot mode deliberately enables YOLO.

For Aegis, Hermes tool approval should be secondary to capability separation.

---

## 11. Recommended concrete choice

### Preferred: TUI gateway stdio worker

Use when Aegis can manage a subprocess.

Properties:

- Hermes is an explicit child runtime.
- Stdio pipe is private to Aegis.
- Rich streaming and session lifecycle.
- Approval events can be surfaced in Aegis.
- No network listener or shared API key is required.
- Disposable `HERMES_HOME` can be set before process start.
- Aegis can terminate the process and delete its runtime directory.

The TUI gateway approval channel should be used for incidental design-tool approvals if any remain, but final artifact provisioning should still occur through the separate Aegis provisioner.

### Alternative: authenticated HTTP Runs API

Use when Aegis needs a remote/language-neutral service.

Requirements:

- Bind to loopback or a private service network.
- Keep `API_SERVER_KEY` server-side.
- Aegis authenticates users separately.
- Use `/v1/capabilities` for feature negotiation.
- Use `/v1/runs` and SSE events.
- Use `/v1/runs/{id}/approval` only for Hermes runtime tool approvals.
- Do not equate that endpoint with final Aegis provisioning approval.
- Start the API server from a disposable or dedicated Hermes home with a narrowed `api_server` tool configuration and no MCPs.

### Direct library option

Use only if Aegis is Python-based and pins the Hermes version.

Requirements:

- Set `HERMES_HOME` before importing Hermes.
- Prefer a worker subprocess rather than importing Hermes into the main Aegis web process.
- Create one `AIAgent` per task.
- Supply a custom `session_db` only if Hermes persistence is desired.
- Remember that initialization still creates runtime session directories.
- Keep plugin discovery and ambient process environment tightly controlled.

---

## 12. Final conclusions

1. **A named Hermes profile is not required for design.** A disposable `HERMES_HOME` is sufficient and avoids touching the user’s normal profile.

2. **Some Hermes home is still operationally required.** Even direct `AIAgent` initialization creates runtime directories. If “no Hermes artifacts before approval” means no persistent user-visible artifacts, use a temporary home and delete it. If it means literally no filesystem writes at all, Hermes must run inside a disposable in-memory or temporary filesystem.

3. **Aegis should own human authentication and approval.** Hermes API bearer auth, messaging pairing, ACP provider auth, and model-provider credentials solve different problems.

4. **The TUI gateway is the best full-control local integration.** The Runs API is the best authenticated network integration. Direct library use is viable but more tightly coupled.

5. **Hermes should remain explicit in both prompt and UI.** Preserve the default Hermes identity, add Aegis-specific ephemeral context, and render non-model provenance in the Aegis interface.

6. **Do not use `hermes -z` for this flow.** It explicitly enables YOLO and bypasses interactive approvals.

7. **Tool removal is stronger than tool instructions.** Design sessions should not receive profile, file, terminal, skill-management, plugin-management, MCP, cron, or code-execution capabilities.

8. **MCP and plugins must be treated as executable integrations.** MCP tool-name filters are not semantic safety guarantees; plugins are arbitrary in-process Python code.

9. **Provisioning must be a separate deterministic phase.** Bind approval to a canonical proposal digest and execute it with a dedicated provisioner. Do not grant the design agent write capability after it sees the word “approved.”

10. **Profiles isolate Hermes state, not host access.** Any final provisioned profile that receives local terminal/file tools still needs a real sandbox if filesystem isolation matters.

## Official URLs

- Programmatic integration:  
  https://hermes-agent.nousresearch.com/docs/developer-guide/programmatic-integration
- Prompt assembly:  
  https://hermes-agent.nousresearch.com/docs/developer-guide/prompt-assembly
- Gateway internals:  
  https://hermes-agent.nousresearch.com/docs/developer-guide/gateway-internals
- ACP internals:  
  https://hermes-agent.nousresearch.com/docs/developer-guide/acp-internals
- Session storage:  
  https://hermes-agent.nousresearch.com/docs/developer-guide/session-storage
- User sessions:  
  https://hermes-agent.nousresearch.com/docs/user-guide/sessions
- Profiles:  
  https://hermes-agent.nousresearch.com/docs/user-guide/profiles
- Multi-profile gateways:  
  https://hermes-agent.nousresearch.com/docs/user-guide/multi-profile-gateways
- API server:  
  https://hermes-agent.nousresearch.com/docs/user-guide/features/api-server
- Toolsets reference:  
  https://hermes-agent.nousresearch.com/docs/reference/toolsets-reference
- MCP feature guide:  
  https://hermes-agent.nousresearch.com/docs/user-guide/features/mcp
- MCP config reference:  
  https://hermes-agent.nousresearch.com/docs/reference/mcp-config-reference
- MCP integration guide:  
  https://hermes-agent.nousresearch.com/docs/guides/use-mcp-with-hermes
- Plugins:  
  https://hermes-agent.nousresearch.com/docs/user-guide/features/plugins
- Plugin development:  
  https://hermes-agent.nousresearch.com/docs/developer-guide/plugins
- Python library:  
  https://hermes-agent.nousresearch.com/docs/guides/python-library
- Hermes source repository:  
  https://github.com/NousResearch/hermes-agent
