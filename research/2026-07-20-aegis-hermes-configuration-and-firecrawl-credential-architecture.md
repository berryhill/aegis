# Aegis as the security entry point for Hermes configuration and Firecrawl access

- Status: Architecture research and recommendation
- Date: 2026-07-20
- Prepared for: Aegis
- Scope: Aegis-controlled Hermes sessions, effective configuration projection, secret delivery, and Firecrawl integration
- Does not authorize: provisioning, activation, modification of the operator's normal Hermes profile, or use of a real Firecrawl credential

## Executive decision

Aegis should be the mandatory entry point for every **Aegis-controlled** Hermes session and the sole authority for that session's effective security configuration. It should not replace Hermes, hide Hermes, or silently take ownership of the operator's independent `~/.hermes` installation.

The precise product statement should be:

> Aegis authenticates the principal, selects exactly one trust stanza, resolves an approved session projection, and launches one clean Hermes runtime with exactly that projection. Hermes remains the visible runtime and owns the agent loop; Aegis owns identity, authority, credential use, lifecycle, verification, and audit.

For Firecrawl, Aegis should not put the reusable `FIRECRAWL_API_KEY` in a Hermes profile, generated config, command line, or Hermes process environment. The preferred architecture is an Aegis-owned, destination-locked Firecrawl broker that performs narrowly typed operations with the upstream key inside Aegis and returns bounded results. A practical intermediate architecture may preserve Hermes's native `web_search` and `web_extract` tools by pointing them at an Aegis loopback gateway and giving Hermes only a short-lived, session-bound capability. The gateway replaces that capability with the upstream Firecrawl credential after reauthorization.

Direct environment injection is technically simple and already resembles Aegis's model-provider credential path, but it only prevents persistence; it does not keep the secret out of Hermes. It should be treated as a compatibility fallback, not the target security boundary.

## Research question

Should Aegis effectively manage Hermes configuration, and how should Aegis provide Firecrawl to Hermes without reproducing the plaintext-secret behavior of a normal Hermes profile?

This report answers five narrower questions:

1. Which parts of Hermes configuration must Aegis own?
2. Which parts may remain ordinary Hermes preferences?
3. What does Hermes currently support for Firecrawl and external secret sources?
4. Which credential-delivery patterns preserve Aegis's trust-stanza invariants?
5. What implementation sequence fits the current Aegis code rather than an imagined greenfield system?

## Method and evidence labels

Repository code and retained specifications were inspected before external sources. External research used current first-party Hermes, Firecrawl, MCP, systemd, Linux man-pages, and OWASP material retrieved on 2026-07-20.

Evidence labels:

- **Verified:** directly supported by inspected Aegis code or a cited first-party source.
- **Vendor statement:** first-party behavior or security property not independently reproduced here.
- **Recommendation:** proposed Aegis behavior.
- **Unresolved:** requires a prototype or compatibility test before commitment.

No real credential was read, transmitted, or used. No Hermes profile, Aegis configuration, runtime service, or external account was modified.

## Current Aegis and Hermes facts

### Aegis already behaves like a session control plane

The current adapter already implements important parts of the desired boundary:

- `internal/runtime/hermes/hermes.go:118` resolves an approved toolset and launches Hermes.
- `internal/runtime/hermes/hermes.go:130` uses Hermes safe mode for ordinary operational sessions.
- `internal/runtime/hermes/hermes.go:153` constructs the process directly rather than invoking a shell.
- `internal/runtime/hermes/hermes.go:161` sets the disposable home as the working directory.
- `internal/runtime/hermes/hermes.go:162` constructs a minimal environment instead of inheriting all ambient credentials.
- `internal/runtime/hermes/hermes.go:278` generates the sole current Aegis-owned MCP bridge configuration in the disposable home.
- `internal/runtime/hermes/hermes.go:230` verifies the active broker-enabled gateway rather than assuming that writing configuration was sufficient.

The current credential path is deliberately narrower than a general secret injector:

- `internal/app/service.go:67` resolves only the explicitly selected `provider:<provider>` credential.
- `internal/config/config.go:235` represents environment-to-environment credential bindings.
- `internal/config/config.go:359` validates environment binding names and rejects reserved targets.
- `internal/runtime/hermes/hermes.go:49` states that a resolved credential is process input and must not be logged, persisted, or returned.

The encrypted credential authority and broker are also real but intentionally narrow:

- `internal/app/broker.go:109` implements one broker authorization and execution path.
- `internal/app/broker.go:184` requires the exact GitHub operation and credential scope.
- `internal/app/broker.go:201` resolves the binding by exact agent, stanza, deployment, and scope.
- `internal/app/broker.go:204` decrypts and uses the credential only inside the authorized callback.
- `docs/CREDENTIAL_BROKER.md:3` states that `github.get_repository.v1` is the only implemented operation.
- `docs/CREDENTIAL_BROKER.md:10` explicitly rejects a generic proxy or secret-reading API.

Therefore, the architecture is not starting from zero. Aegis already controls process creation, a disposable Hermes home, selected environment credentials, exact toolsets, a generated MCP bridge, session capabilities, and one typed downstream credential use.

### Hermes has two relevant Firecrawl interfaces

Current Hermes documentation identifies native Firecrawl support:

- The `web` toolset provides `web_search` and `web_extract`.
- `FIRECRAWL_API_KEY` selects/authenticates the Firecrawl backend.
- `FIRECRAWL_API_URL` can select a self-hosted or alternate endpoint.
- `web.backend`, `web.search_backend`, and `web.extract_backend` choose providers explicitly.
- Normal persistent configuration lives under `~/.hermes`, with settings in `config.yaml` and API keys in `.env`.

Hermes also supports remote and stdio MCP servers. Its MCP configuration can contain URLs, headers, command arguments, and subprocess environment values. Hermes's OAuth support persists client tokens under its home. That is useful for ordinary personal Hermes use, but Aegis must decide whether such state is authorized, session-scoped, retained, and revocable.

### Hermes secret-source plugins improve retrieval, not process isolation

Hermes's first-party secret-source plugin documentation says that secret sources resolve credentials from a vault, password manager, OS keystore, or custom backend into environment variables at process startup. Bitwarden and 1Password are bundled; additional sources are plugins. The framework controls ordering, protected bootstrap tokens, timeouts, precedence, provenance labels, and environment writes.

This is useful for direct Hermes and for deployments where Hermes is trusted to possess a credential. It does not satisfy Aegis's stronger goal of keeping reusable downstream credentials outside the runtime process, because the terminal state is still an environment variable inside Hermes. An Aegis secret-source plugin would also create a second authorization path inside a runtime Aegis is supposed to constrain, and plugin loading itself is disabled by Aegis safe mode today.

### Firecrawl offers REST API-key and MCP OAuth modes

Firecrawl's v2 REST API uses an HTTP bearer API key against `https://api.firecrawl.dev`. Search and scrape are distinct operations. Search supports explicit limits and domain filters; the current schema documents a query maximum of 500 characters and a result limit from 1 to 100.

Firecrawl's remote MCP endpoint offers two authentication patterns:

- API key embedded in the MCP URL.
- Keyless `https://mcp.firecrawl.dev/v2/mcp` with OAuth.

Firecrawl recommends OAuth when supported. Its documentation states that the client receives a scoped, short-lived access token rather than the raw API key, access tokens expire after one hour, and refresh tokens rotate. However, the current consent text grants full Firecrawl API access for the selected account. Thus OAuth reduces reusable-key exposure but does not automatically provide Aegis trust-stanza scope, per-operation URL policy, or Aegis-authoritative audit.

## The correct ownership boundary

“Aegis manages all Hermes configuration” is too broad if read literally. The correct boundary is **effective authority**, not every preference.

### Aegis must own or approve

For each Aegis-controlled session, Aegis must be authoritative for:

- authenticated subject and principal provenance;
- exactly one selected trust stanza;
- canonical charter revision and digest;
- mandate identity, issue time, expiry, and revocation;
- Hermes executable, version range, and adapter version;
- model provider, model, endpoint, fallback, and auxiliary routes;
- toolsets and exact effective tool registrations;
- MCP servers, commands, packages, URLs, headers, and tool filters;
- plugin identities, versions, and permissions;
- credential references, destinations, modes, and operation scopes;
- memory namespaces and retained state;
- filesystem and network authority;
- session home retention and cleanup;
- gateway, cron, service, hook, and automation authority;
- approvals and consequential-action policy;
- launch receipt, runtime verification, termination, and audit.

If any of these can be changed inside a running Hermes session without Aegis reauthorization, the approved security context is no longer authoritative.

### Hermes may own non-authority presentation preferences

Aegis may permit a reviewed subset of non-security preferences, for example:

- color and terminal theme;
- animation and presentation density;
- accessibility rendering;
- local keybindings that do not invoke broader actions;
- result formatting that does not enable persistence or external delivery.

Even these should be copied from a typed allowlist rather than merging arbitrary `config.yaml`, because apparently cosmetic hooks or commands can acquire authority over time as Hermes evolves.

### Direct Hermes remains independent

The operator should retain two explicit modes:

```text
Direct personal use:
operator -> hermes -> ~/.hermes

Aegis-controlled use:
authenticated operator -> Aegis -> approved projection -> disposable Hermes
```

Aegis must not rewrite or treat the normal profile as authoritative. Conversely, an Aegis-controlled session must not inherit the normal profile merely because both run under the same human account.

The user-facing distinction should be obvious in the command, banner, session receipt, runtime home, and audit trail. A session started directly with `hermes` is not Aegis-controlled and must not be represented as such.

## Configuration projection model

### Separate desired policy, projection, and observed runtime

Aegis should maintain three distinct objects:

1. **Charter policy:** human-reviewable, runtime-independent authority.
2. **Hermes session projection:** deterministic adapter-specific configuration derived from one approved charter revision and mandate.
3. **Observed runtime state:** what the launched Hermes process actually registered and used.

These objects must not collapse into one mutable YAML file.

A proposed projection contains at least:

```text
schema version
projection ID and digest
charter digest
mandate ID
agent, stanza, subject, deployment
Hermes adapter and supported-version constraint
resolved executable identity and observed version
model/provider/endpoint route
exact toolsets
exact MCP/plugin set and tool allowlist
credential-use modes and destinations
memory/state policy
session home and retention policy
launch arguments and generated-file digests
expected runtime verification assertions
expiry and cleanup requirements
```

The principal approves the canonical charter and complete material plan. At launch, Aegis generates the projection into a fresh home, launches Hermes, queries the runtime where Hermes provides an introspection protocol, and compares observed state to the projection. A mismatch denies activation and terminates the process.

### Merge semantics should be “construct,” not “overlay”

Aegis should construct configuration from a closed schema. It should not:

- merge the operator's `~/.hermes/config.yaml`;
- copy `~/.hermes/.env`;
- inherit `mcp-tokens` or provider credential pools;
- merge project plugins, rules, hooks, or skills;
- honor model-generated requests to enable a component;
- preserve unknown Hermes keys “for convenience.”

Unknown keys in an adapter projection should fail validation. When a new Hermes version introduces a security-relevant key or changes defaults, the adapter compatibility suite must classify it before the supported version range advances.

### Runtime verification is mandatory

A config file proves only intended input. It does not prove effective behavior. Aegis already follows the stronger pattern for its GitHub bridge by requiring one exact live tool registration.

That pattern should expand to verify, where observable:

- Hermes version and executable;
- active model and provider route;
- active toolsets;
- exact MCP servers and tools;
- absence of unapproved tools;
- plugin and skill absence or exact approved versions;
- clean home and no inherited token/config paths;
- gateway readiness;
- egress gateway identity;
- session PID/start token and process ancestry.

When Hermes cannot expose a property reliably, Aegis should record the verification limit rather than treating launch arguments as equivalent to observed enforcement.

## Firecrawl integration options

| Pattern | Reusable key in Hermes? | Persistent plaintext? | Aegis operation control | Native Hermes web tools | Recommendation |
|---|---:|---:|---:|---:|---|
| Write `~/.hermes/.env` | Yes | Yes | Weak | Yes | Reject for Aegis sessions |
| Inject `FIRECRAWL_API_KEY` in child env | Yes | No | Session/stanza gate at launch only | Yes | Compatibility fallback |
| Hermes secret-source plugin | Yes, after resolution | Avoidable | Split between Aegis and plugin | Yes | Direct-Hermes feature, not Aegis target |
| Firecrawl remote MCP with API key in URL | Yes | Likely in config | Weak | MCP tools | Reject |
| Firecrawl remote MCP OAuth in disposable home | Short-lived token, not raw key | Token state exists during session | Firecrawl/client scopes, not exact Aegis scopes | MCP tools | Better for direct Hermes; limited Aegis fit |
| Aegis loopback Firecrawl gateway | No upstream key; only session capability | No reusable plaintext | Strong if destination/operation locked | Yes | Recommended intermediate path |
| Typed Aegis Firecrawl broker tools | No | No reusable plaintext | Strongest | Aegis MCP tools rather than native tools | Recommended target |

### Option 1: environment injection

Aegis can resolve a Firecrawl record and pass it as `FIRECRAWL_API_KEY` only to the child process. This avoids writing the value to `~/.hermes/.env` and is compatible with Hermes's native web backend.

It does not keep the secret from Hermes. Linux documents `/proc/<pid>/environ` as exposing the initial environment subject to ptrace access checks. Child processes inherit environment variables by default unless the parent constructs a new environment. Hermes code, dependencies, terminal tools, crash handling, process inspection, or a compromised same-identity process may expose it.

OWASP likewise advises that environment variables are generally accessible to processes and may appear in logs or dumps, recommending other delivery methods when possible.

If retained as a fallback, Aegis must:

- require an exact `firecrawl/*` credential scope in the selected stanza;
- inject only for sessions that grant the Firecrawl-backed toolset;
- reject terminal, code, or file authority combinations unless policy explicitly accepts the exposure;
- never persist the value in projection, receipt, logs, audit, or errors;
- use a separate restricted Firecrawl account/key where the provider permits;
- document that Hermes possesses the key;
- terminate the clean session on scope or binding changes.

### Option 2: Firecrawl MCP OAuth

Using Firecrawl's keyless MCP endpoint avoids copying the raw API key into Hermes. Firecrawl states that its access token lasts one hour and refresh tokens rotate. MCP authorization requires audience/resource binding and forbids tokens in URI query strings; MCP security guidance also rejects token passthrough and requires tokens intended for the MCP server.

This is materially safer than an API key embedded in an MCP URL. Nevertheless, it is not the preferred Aegis control plane because:

- Hermes still holds bearer and refresh material;
- OAuth state persistence conflicts with disposable clean sessions unless deliberately projected or reauthorized;
- Firecrawl's current consent is broad account-level API access;
- Aegis cannot infer that a Firecrawl OAuth grant equals a charter scope;
- direct remote MCP gives the remote server's tool surface to Hermes unless Aegis pins and verifies an allowlist;
- OAuth metadata discovery adds SSRF and redirect-validation obligations;
- authoritative use audit would be split across Aegis, Hermes, and Firecrawl.

It remains a good recommendation for independent personal Hermes use when users prefer short-lived OAuth tokens over a reusable API key in configuration.

### Option 3: Aegis loopback Firecrawl gateway

This path preserves Hermes's native `web_search` and `web_extract` integration while keeping the upstream credential inside Aegis:

```text
Hermes native web tool
    -> loopback Aegis Firecrawl endpoint
       Authorization: Bearer <session capability>
    -> exact live mandate and operation check
    -> Aegis resolves encrypted firecrawl binding
    -> Aegis calls the fixed Firecrawl v2 search or scrape endpoint
       Authorization: Bearer <upstream key>
    -> bounded, sanitized response
```

Aegis would project:

```text
web.backend = firecrawl
FIRECRAWL_API_URL = fixed Aegis loopback endpoint
FIRECRAWL_API_KEY = short-lived session capability, not upstream key
```

The capability is still sensitive, but its blast radius can be constrained by session, stanza, operation, destination, deadline, request count, and runtime process identity. Revocation can disable it immediately without rotating the Firecrawl account key.

This must not be a generic reverse proxy. It should expose only exact Firecrawl operations and enforce a closed request schema. The client must not choose the upstream host, path, method, authorization header, redirect policy, proxy, or credential record. Aegis must strip inbound forwarding headers, disable ambient proxy variables, reject redirects unless explicitly safe, enforce response limits, and audit metadata rather than content.

**Unresolved compatibility test:** confirm that Hermes v0.18.x's Firecrawl SDK accepts the projected custom base URL and sends the session capability in the expected authorization header for both search and extraction. The Aegis gateway must reproduce only the response shapes Hermes actually consumes. This should be proven with a loopback fake Firecrawl server before any real credential test.

### Option 4: typed Aegis Firecrawl broker tools

The strongest boundary exposes Aegis-owned tools such as:

```text
firecrawl.search.v1
firecrawl.extract.v1
```

Each tool has a bounded JSON schema. Aegis applies the upstream credential itself and returns a stable, sanitized result independent of Firecrawl's full API surface.

Suggested initial contracts:

```text
firecrawl.search.v1
- query: required string, 1..500 bytes/runes under one documented rule
- limit: 1..10 initially, never provider maximum by default
- include_domains: optional bounded allowlist intersected with charter policy
- exclude_domains: optional bounded list if policy allows
- no arbitrary scrape options in v1

firecrawl.extract.v1
- url: one HTTPS URL
- domain must match the stanza's destination policy
- no userinfo, fragments, private/reserved IP destinations, or redirects to them
- output format fixed to markdown or a small approved enum
- maximum upstream and returned bytes
- deadline and cost/request budget
```

The broker authorization key should extend the existing exact tuple:

```text
subject + agent + stanza + deployment + charter digest + mandate + session
+ operation + credential scope + destination + binding revision + request ID
```

The model never selects a secret record or raw destination URL beyond contract fields. Aegis resolves exactly one active binding, emits an allow audit before downstream use, performs the operation, and emits a success/failure use event without query text, page content, headers, or credential material.

The principal drawback is integration breadth. The current bridge verifies exactly one GitHub tool and requires exactly the `aegis` toolset. Supporting Firecrawl requires a versioned Aegis bridge manifest and exact-set verification rather than simply adding arbitrary MCP. That is desirable work because it generalizes controlled capabilities without weakening the no-ambient-MCP invariant.

## Recommended target architecture

### Four planes

Aegis should make four planes explicit:

1. **Authority plane**
   - authentication, stanza selection, charter, mandate, approval, revocation.
2. **Projection plane**
   - deterministic Hermes config, exact runtime home, tool manifest, routes, session capability.
3. **Execution plane**
   - visible Hermes runtime and agent loop with only projected authority.
4. **Broker/egress plane**
   - credentials, fixed destinations, typed downstream operations, rate/cost limits, authoritative audit.

```text
Authenticated operator
        |
        v
Aegis authority plane
  identity -> one stanza -> mandate -> approval
        |
        v
Aegis projection plane
  canonical Hermes projection + digest + expected assertions
        |
        v
Disposable Hermes runtime
  visible runtime; no ambient ~/.hermes; no reusable Firecrawl key
        |
        v
session-bound Aegis bridge/gateway
  operation + destination + live mandate reauthorization
        |
        v
Aegis credential authority -> fixed Firecrawl API
```

### Credential modes should be explicit

Every credential scope should declare one delivery/use mode:

- `provider-gateway`: Aegis owns upstream provider key; Hermes gets an ephemeral gateway capability.
- `brokered-action`: Aegis performs one typed operation; Hermes never receives upstream key.
- `oauth-client`: runtime receives a scoped OAuth token under an explicit persistence policy.
- `environment-compat`: runtime receives reusable plaintext; exceptional and visibly warned.
- `none`: no credential available.

A scope must not silently fall back from brokered use to environment injection. Changing mode is authority-relevant, changes the plan digest, requires approval, and starts a clean session.

### Tool and credential scopes must compose exactly

A Firecrawl grant should require all corresponding elements:

```text
capability: firecrawl.search.v1
Hermes/Aegis tool: exact projected search tool
credential scope: firecrawl/search
operation destination: firecrawl-api
network policy: Aegis broker only from Hermes; Firecrawl HTTPS only from broker
```

A credential scope without the operation must not expose anything. A tool without the scope must fail closed. Search authority must not imply scrape, crawl, browser control, monitoring, or account administration.

### Network enforcement remains necessary

A process boundary and disposable home are not a network sandbox. If Hermes can contact `api.firecrawl.dev` directly, it can bypass the Aegis gateway whenever it obtains a credential or alternate route.

The production design should therefore separate runtime and broker identities and enforce egress so that:

- Hermes can reach only the required local Aegis endpoints and approved model gateway;
- only Aegis can reach Firecrawl;
- metadata, loopback exceptions, redirects, DNS rebinding, and proxy variables are handled deliberately;
- direct personal Hermes remains outside this policy and is not confused with an Aegis session.

Possible Linux mechanisms include a dedicated runtime user plus systemd/network namespace policy, nftables owner/cgroup rules, or a container/VM boundary. The selected mechanism must be tested on supported platforms; none should be described as complete confinement without enforcement evidence.

## Security analysis

### Threats addressed

The target architecture reduces:

- plaintext API keys in `~/.hermes/.env` or generated YAML;
- key leakage through process environments;
- cross-stanza credential reuse;
- tool-driven requests to unapproved Firecrawl operations;
- stale-session use after revocation;
- arbitrary remote MCP tool expansion;
- missing attribution for credential use;
- direct selection of secret records or headers by model output;
- persistent OAuth/API state inherited by later sessions.

### Threats not solved by configuration projection alone

It does not by itself solve:

- root, kernel, or Aegis-process compromise;
- malicious content returned by Firecrawl and subsequent prompt injection;
- Firecrawl provider compromise or retention behavior;
- denial of service or credit exhaustion without budgets;
- data exfiltration through an allowed query or URL;
- direct network bypass without OS-level egress controls;
- complete plaintext zeroization in Go;
- supply-chain compromise in Hermes, Aegis, or dependencies.

Firecrawl output is untrusted remote content. The broker should label provenance and preserve source URLs, while downstream prompt assembly must keep retrieved content distinct from instructions. Search/extract authorization does not make page content trusted.

### MCP-specific obligations

MCP's security guidance is directly relevant because Aegis's bridge is an MCP server and may proxy downstream APIs:

- avoid token passthrough;
- bind tokens/capabilities to the intended resource;
- prevent confused-deputy behavior;
- require exact user consent for local command installation;
- validate redirects and OAuth metadata retrieval;
- mitigate SSRF, including private, loopback, link-local, and metadata endpoints;
- use short-lived credentials and replay-resistant request identifiers;
- expose the minimum tool surface.

Aegis's current generated local bridge is stronger than one-click arbitrary MCP installation because Aegis writes a fixed command and verifies one live tool. Generalization should preserve that property through signed/pinned bridge manifests and exact-set verification.

## Implementation roadmap

### Phase 0: contract and terminology

1. Adopt the precise product statement: Aegis owns effective security configuration for Aegis-controlled sessions.
2. Define `HermesSessionProjection` separately from charter and observed runtime.
3. Define credential-use modes and make them authority-relevant.
4. Add explicit Firecrawl operations and scopes to specifications before implementation.
5. Define a stable destination ID such as `firecrawl-api`; never authorize a raw URL as the destination identity.

Exit gate: strict schemas, canonical digest tests, mutation tests, and documentation agree on the boundary.

### Phase 1: hermetic Firecrawl compatibility prototype

1. Build a fake loopback Firecrawl v2 server with generated canary credentials.
2. Launch the supported Hermes version with a disposable home, native `web` toolset, explicit Firecrawl backend, and custom API URL.
3. Verify search and extraction request paths, headers, request bodies, response shapes, timeouts, cancellation, and error handling.
4. Prove the upstream canary is absent from Hermes home, argv, logs, audit, output, and retained state.
5. Test whether the Hermes process can read its injected session capability and document that this is expected.

Exit gate: real Hermes-to-fake-gateway execution, not mocked adapter assumptions.

### Phase 2: destination-locked loopback gateway

1. Extend the existing session capability model with operation and destination restrictions.
2. Add only fixed search and extract endpoints.
3. Resolve Firecrawl from the encrypted credential authority by exact binding.
4. Replace the session capability with the upstream bearer key inside Aegis.
5. Add URL/domain policy, SSRF checks, redirect policy, byte limits, request budgets, deadlines, and sanitized errors.
6. Audit authorization/use metadata without query, URL path beyond approved policy metadata, content, or secrets.
7. Terminate/revoke on mandate, stanza, binding, process, or session changes.

Exit gate: adversarial broker tests and one opt-in live test using a dedicated restricted test account, only after explicit operator authorization.

### Phase 3: versioned Aegis bridge manifest

1. Replace the hard-coded one-GitHub-tool assumption with a typed manifest of approved Aegis tools.
2. Render only manifest tools into the disposable Hermes home.
3. Query Hermes and require exact equality between expected and registered tool names/schemas.
4. Add `firecrawl.search.v1` first; add extraction separately after URL-policy tests.
5. Keep resources, prompts, sampling, arbitrary MCP methods, and unapproved tools disabled.

Exit gate: zero, missing, renamed, duplicate, schema-mutated, and additional tools all fail launch.

### Phase 4: stronger process and network separation

1. Run the Aegis authority/broker and Hermes runtime under distinct OS identities.
2. Deliver Aegis KEK material through encrypted systemd credentials where supported.
3. Enforce Hermes egress to Aegis gateways only and Aegis egress to fixed providers.
4. Test systemd hardening and network policy rather than claiming sandboxing from configuration.
5. Add revocation, restart, crash, and broker-unavailability campaigns.

Exit gate: direct Hermes-to-Firecrawl bypass fails under the deployed policy, while brokered operations work.

### Phase 5: external and OAuth credential sources

After the local authority path is stable, add source adapters for externally managed secret stores or OAuth grants. These adapters should populate or lease Aegis authority records; they should not create an alternate Hermes-side authorization path. OAuth refresh state should remain in Aegis when Aegis is the client, with resource/audience binding and explicit revocation.

## Acceptance criteria

The Firecrawl feature is complete only when tests prove:

- direct and Aegis-controlled Hermes modes are distinguishable;
- the normal `~/.hermes` profile is neither read nor modified;
- the upstream Firecrawl key never enters Hermes argv, environment, home, config, token store, model context, output, logs, or audit;
- one authenticated subject selects exactly one stanza;
- the charter and plan explicitly grant the exact Firecrawl operation and scope;
- missing, ambiguous, stale, revoked, or wrong-destination bindings deny;
- the live session, mandate, PID/start token, and process ancestry remain valid per request;
- search does not imply scrape, crawl, browser, monitor, or administration;
- URL and domain restrictions survive redirects, DNS changes, alternate encodings, and private-address targets;
- request count, cost, deadline, and response bytes are bounded;
- broker restart invalidates process-local capabilities cleanly;
- rotation affects new uses according to an explicit binding policy;
- Firecrawl and transport errors cannot leak headers, bodies, or credentials;
- exact runtime tools match the approved projection;
- cleanup removes disposable capability and runtime artifacts;
- audit reconstructs subject, agent, stanza, charter, mandate, session, operation, destination, record/version metadata, decision, and outcome without secret or content values.

## Alternatives rejected

### Treat `~/.hermes` as the Aegis source of truth

Rejected because persistent profile state is mutable outside Aegis approval, mixes direct and managed sessions, and can contain MCP, plugins, tokens, memories, hooks, and credentials that were never selected by a trust stanza.

### Copy the Firecrawl key into each disposable `.env`

Rejected because disposal limits duration but still creates plaintext filesystem artifacts and grants the runtime the reusable credential.

### Use an Aegis Hermes secret-source plugin

Rejected as the primary design because Hermes plugins are runtime extensions, safe mode disables them, and the resolved value still becomes a Hermes environment variable. It may be useful for independent direct Hermes use but is weaker than brokered use.

### Put the API key in a remote MCP URL

Rejected because URLs are copied, persisted, logged, diagnosed, and shared easily. Firecrawl itself recommends OAuth for clients that support it.

### Give Hermes a generic `GetSecret` MCP tool

Rejected categorically. It would move authorization into model-selected secret retrieval, destroy destination binding, and make exfiltration a normal tool result.

### Build a generic authenticated HTTP proxy

Rejected because a model-controlled URL/method/header proxy is a confused deputy and SSRF primitive. The gateway must expose fixed operations and destinations.

### Let Aegis absorb the Hermes agent loop

Rejected because Aegis is an identity, authority, projection, and lifecycle layer—not a replacement agent framework. Hermes must remain explicit and independently supportable.

## Product and UX consequences

The CLI should make the boundary visible:

```text
Runtime: Hermes Agent 0.18.x
Control plane: Aegis
Session: clean/disposable
Trust stanza: research
Effective tools: firecrawl.search.v1
Credential mode: brokered-action
Credential visible to Hermes: no
Destination: firecrawl-api
Expires: <time>
```

Before launch, the principal should review configuration effects, not values:

- Firecrawl search allowed or denied;
- extraction allowed or denied;
- domain constraints;
- request/cost limits;
- whether reusable credentials enter Hermes;
- retained state policy;
- exact Hermes runtime and tool projection.

Aegis should never claim control over a directly started Hermes process. Conversely, a managed session should not offer a runtime command that silently converts it into direct-profile mode.

## Launch-asset impact review

This work adds a research report only; it does not change implemented behavior, command syntax, dependencies, configuration schema, architecture enforcement, demonstrations, or release artifacts.

Reviewed as unaffected:

- root `README.md`;
- `LICENSE`;
- `SECURITY.md`;
- `CONTRIBUTING.md`;
- `CODE_OF_CONDUCT.md`;
- `CHANGELOG.md`;
- `docs/THREAT_MODEL.md`;
- `docs/ARCHITECTURE.md` and its diagrams;
- `docs/QUICKSTART.md`;
- `docs/DEMO_NO_KEY.md`;
- `docs/RECORDING.md` and retained recording;
- release binaries and checksums;
- `docs/contributing/ISSUE_BACKLOG.md`.

No launch asset should describe the recommended Firecrawl design as implemented until code and verification satisfy the acceptance criteria above. No GitHub release or issue was created.

## Sources

Accessed 2026-07-20 unless otherwise stated.

### Hermes Agent

- Configuration: https://hermes-agent.nousresearch.com/docs/user-guide/configuration
- Environment variables: https://hermes-agent.nousresearch.com/docs/reference/environment-variables
- MCP guide: https://hermes-agent.nousresearch.com/docs/guides/use-mcp-with-hermes
- MCP configuration reference: https://hermes-agent.nousresearch.com/docs/reference/mcp-config-reference
- Secret-source plugins: https://hermes-agent.nousresearch.com/docs/developer-guide/secret-source-plugin
- Tool reference: https://hermes-agent.nousresearch.com/docs/reference/tools-reference

### Firecrawl

- Documentation index: https://docs.firecrawl.dev/llms.txt
- API v2 introduction and authentication: https://docs.firecrawl.dev/api-reference/v2-introduction
- Search endpoint: https://docs.firecrawl.dev/api-reference/endpoint/search
- Scrape endpoint: https://docs.firecrawl.dev/api-reference/endpoint/scrape
- MCP OAuth: https://docs.firecrawl.dev/developer-guides/mcp-setup-guides/oauth
- Nous Research/Hermes quickstart: https://docs.firecrawl.dev/quickstarts/nous-research

### MCP and OAuth

- MCP security best practices: https://modelcontextprotocol.io/docs/tutorials/security/security_best_practices
- MCP authorization specification (2025-11-25): https://modelcontextprotocol.io/specification/2025-11-25/basic/authorization
- SEP-1024, local MCP installation security: https://modelcontextprotocol.io/seps/1024-mcp-client-security-requirements-for-local-server-
- OAuth 2.0 Security Best Current Practice, RFC 9700: https://www.rfc-editor.org/rfc/rfc9700
- OAuth 2.0 Resource Indicators, RFC 8707: https://www.rfc-editor.org/rfc/rfc8707
- OAuth 2.0 Protected Resource Metadata, RFC 9728: https://www.rfc-editor.org/rfc/rfc9728

### Host credential and secret handling

- systemd system and service credentials: https://systemd.io/CREDENTIALS/
- Linux `/proc/<pid>/environ`: https://man7.org/linux/man-pages/man5/proc_pid_environ.5.html
- OWASP Secrets Management Cheat Sheet: https://cheatsheetseries.owasp.org/cheatsheets/Secrets_Management_Cheat_Sheet.html

## Final recommendation

Proceed with Aegis as the mandatory security entry point for Aegis-controlled Hermes sessions, but describe the boundary accurately: Aegis owns the complete effective authority projection; Hermes remains the visible runtime and owns execution.

For Firecrawl:

1. Do not modify or consume the normal Hermes profile.
2. Do not put the reusable API key in Hermes config, URL, or environment in the target design.
3. First prove Hermes native Firecrawl compatibility against an Aegis loopback fake gateway.
4. Implement destination-locked, session-bound search separately from extraction.
5. Generalize the existing exact Aegis bridge only through a versioned manifest and live exact-set verification.
6. Add OS/network egress enforcement before claiming that Hermes cannot bypass the broker.
7. Treat environment injection and remote MCP OAuth as explicit compatibility choices with weaker, documented guarantees—not silent fallbacks.
