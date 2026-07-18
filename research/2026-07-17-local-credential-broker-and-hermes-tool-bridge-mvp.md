# Local credential broker and Hermes tool bridge: minimum viable enforcement boundary

- Status: Complete
- Date: 2026-07-17
- Prepared for: Aegis
- Research question: What is the minimum safe architecture that connects Aegis’s implemented encrypted credential authority to a trust-stanza-bound Hermes session without revealing reusable credentials to Hermes or the model?
- Recommended decision: Implement a local Unix-socket credential broker plus an Aegis-owned, generated, single-purpose tool bridge. Prove the boundary first with one typed, read-only GitHub `get repository` action. Do not expose raw secret retrieval, arbitrary HTTP, caller-selected credential references, arbitrary destinations, or a generic credential-injection API.

## Executive summary

Aegis now has an encrypted bbolt credential authority, immutable secret versions, exact agent/stanza/deployment/scope bindings, no-echo administration, and key custody. It does not yet have the runtime enforcement edge that makes those credentials safely useful to an agent. Operational Hermes sessions still receive selected provider credentials through environment variables, and `docs/ARCHITECTURE.md` correctly marks the authority-to-adapter edge as unimplemented.

The missing boundary is not just a Unix socket. It consists of four pieces:

1. **A mandate-bound session capability** issued by Aegis and unusable outside the intended runtime boundary.
2. **A narrow model-visible tool schema** containing only business arguments, never secret references, headers, tokens, arbitrary URLs, or methods.
3. **A local broker** that derives agent, stanza, deployment, credential scope, destination, and secret version from trusted Aegis state; reauthorizes every action; decrypts only inside a bounded callback; applies the credential; and returns a sanitized result.
4. **An OS/network boundary** preventing Hermes or its tools from reaching the protected downstream service through an alternate credential or bypass path.

The recommended first action is:

```text
github.get_repository(owner, repository)
```

It is useful, read-only, easy to type strictly, and can be constrained to `GET https://api.github.com/repos/{owner}/{repository}`. It proves that a private credential can be applied without being returned to Hermes. It does not require accepting a generic URL or proxying an arbitrary request. The first credential should be a low-risk, read-only, repository-scoped credential created specifically for the test deployment. A GitHub App installation token is the preferred end state; a fine-grained read-only personal access token is an acceptable bounded pilot if its blast radius and rotation are explicit.

The main integration blocker is Hermes launch mode. Aegis currently starts Hermes v0.18.x with `--safe-mode` and effectively disables MCP/plugins. Hermes’s documented extension point for a narrow external system is MCP, but simply enabling an inherited MCP configuration would violate Aegis’s no-ambient-authority invariant. The safe direction is an Aegis-generated, disposable Hermes home containing exactly one Aegis-owned stdio bridge and an exact tool allowlist, with no inherited profile, tokens, plugins, skills, memories, or project rules. Aegis must verify the effective tool registration before accepting prompts. If Hermes cannot support that while preserving the required isolation, Aegis should not expose the broker to the model until the adapter contract is extended or Hermes adds an explicit safe-mode tool-injection mechanism.

## Scope

This report covers:

- the broker trust boundary and request protocol;
- session authentication and capability binding;
- credential resolution and use;
- a single typed GitHub read operation;
- Hermes tool exposure and the current safe-mode conflict;
- response sanitization, audit, revocation, concurrency, and failure behavior;
- implementation phases and acceptance gates.

It does not design:

- generic secret retrieval;
- a generic HTTP proxy;
- model-provider proxying;
- arbitrary GitHub API operations;
- write actions;
- cross-stanza delegation;
- remote/fleet projection transport;
- a general MCP security platform.

## Verified Aegis context

### Implemented credential authority

Verified from `internal/credentials/` and `docs/ARCHITECTURE.md`:

- `SecretRecord` and immutable `EncryptedSecretVersion` types exist.
- Per-version values use XChaCha20-Poly1305 envelope encryption.
- The repository is deployment-bound and stores exact `CredentialBindingKey` fields: agent, stanza, deployment, and scope.
- Bindings include an exact secret record, current or pinned version policy, mode, destinations, enabled state, and revision.
- The bbolt authority validates storage structure and key custody before use.
- Administration supports secret creation/rotation/metadata without model invocation or value display.
- The encrypted authority is an administrative path only; there is no broker wired to Hermes.

### Current Hermes runtime behavior

Verified from `internal/runtime/hermes/hermes.go`:

- Operational sessions get a fresh disposable home.
- Hermes is launched with `--safe-mode --tui` and explicit built-in toolsets.
- Empty tool selection resolves to `no_mcp`.
- Project plugins are disabled.
- Selected credentials are currently inserted into the child environment.
- Runtime output is discarded rather than copied into Aegis logs.
- The adapter reports no broker or externally injected tool capability.

### Existing architectural requirements

The credential and deployment reports already require:

- active mandate, exact stanza, deployment, scope, operation, and destination checks at every broker use;
- Unix pathname, ownership/mode, `SO_PEERCRED`, and a session capability;
- no authorization from profile name, prompt text, caller-selected stanza, or secret reference;
- zero or multiple bindings to deny;
- revoked/expired mandate, binding, projection, or secret version to deny;
- credential application without returning the value to Hermes.

This report specifies the missing concrete interaction.

## Security objective and non-objectives

### Security objective

After implementation and successful acceptance tests, Aegis should be able to claim:

> An authenticated Hermes session bound to agent A, stanza X, deployment D, and mandate M can request one explicitly granted structured operation. Aegis independently resolves the approved credential scope and destination, applies the credential to that operation, and returns a bounded sanitized result without placing the reusable credential in Hermes environment, prompt context, tool arguments, tool results, transcript, logs, or audit.

### Claims Aegis must not make

The MVP does not prove:

- safety against root compromise on the active host;
- perfect Go memory zeroization;
- inability of an authorized model to misuse the allowed read operation;
- correctness of GitHub data returned to the model;
- prevention of data leakage through every other Hermes tool;
- guaranteed erasure from backups or downstream services;
- general protection for arbitrary broker plugins.

## Threat model

### Assets

- GitHub credential value and private repository metadata;
- credential binding and destination policy;
- mandate and session capability;
- bbolt authority and KEK;
- broker audit integrity;
- sanitized downstream result;
- trust-stanza and deployment isolation.

### Threat actors and abuse cases

| Actor or failure | Abuse case | Required control |
|---|---|---|
| Prompt-injected model | Supplies a secret reference, arbitrary URL, header, method, or path traversal | Tool schema has only owner/repository; broker derives every security field. |
| Same-UID local process | Connects to broker socket and replays a captured request | Socket permissions plus peer credentials plus unexported session capability plus freshness/replay checks. |
| Compromised Hermes process | Reads environment/files or calls broker directly | No upstream credential in environment; capability grants only exact broker operations; OS isolation limits peer access and alternate egress. |
| Malicious tool bridge | Changes operation or response | Aegis-owned pinned bridge; strict protocol; broker independently validates operation and canonical arguments. |
| SSRF attacker | Uses redirects, alternate hosts, encoded paths, DNS tricks, or userinfo | Destination and scheme are constants; no generic URL; redirects disabled or strictly revalidated. |
| Cross-stanza attacker | Reuses a teamwide capability to access principal binding | Capability and broker lookup bind exact agent/stanza/deployment/mandate; no caller-selected binding. |
| Replay attacker | Repeats a valid operation after expiry or revocation | Request ID, deadline, bounded sequence/replay cache, and reauthorization on every call. |
| Downstream compromise | Returns malicious or oversized content | Strict status/content type/size handling and response field allowlist; result remains untrusted data. |
| Broker crash | Leaves plaintext, token, or ambiguous action state | No plaintext files, bounded callback, no open DB transaction during network, safe panic/error rendering. |
| Audit/log operator | Learns token or private repository data | Metadata-only events; result body and credential absent. Repository name retention follows data policy. |

## Recommended architecture

```text
Aegis control plane
  -> authenticate identity
  -> select exactly one stanza
  -> issue mandate
  -> create disposable Hermes home
  -> issue session-bound broker capability
  -> launch Aegis-owned tool bridge + Hermes

Hermes model
  -> tool call: github_get_repository(owner, repository)
  -> Aegis-owned stdio tool bridge
  -> bounded local Unix socket request
  -> broker authenticates peer + session capability
  -> broker loads trusted mandate/session state
  -> broker derives binding key and destination
  -> authority resolves encrypted version
  -> broker decrypts inside callback
  -> broker sends fixed GitHub REST request with auth
  -> broker validates and sanitizes response
  -> bridge returns bounded structured result
  -> Hermes treats result as untrusted content
```

The model never chooses:

- `SecretRecord` or secret reference;
- credential scope;
- binding version policy;
- deployment;
- token type or authorization header;
- host, scheme, port, method, or arbitrary path;
- redirect policy;
- whether authorization/audit is required.

## Why the first action should be GitHub repository metadata

### Recommended operation

```json
{
  "operation": "github.get_repository.v1",
  "arguments": {
    "owner": "approved-owner",
    "repository": "approved-repository"
  }
}
```

The broker constructs:

```text
GET https://api.github.com/repos/{canonical-owner}/{canonical-repository}
```

It sets fixed headers required by the reviewed GitHub API version and adds authorization internally.

### Benefits

- Read-only and naturally idempotent.
- Small typed input surface.
- Fixed destination and method.
- Easy to test against a mock server and a low-risk real repository.
- Demonstrates access to a private resource without returning a credential.
- Produces a response that can be aggressively field-filtered.
- Avoids generic proxy and SSRF complexity.

### Sanitized result

The model should receive only required fields, for example:

```json
{
  "owner": "approved-owner",
  "name": "approved-repository",
  "private": true,
  "default_branch": "main",
  "archived": false,
  "visibility": "private",
  "updated_at": "2026-07-17T00:00:00Z"
}
```

Do not return:

- response headers;
- raw body;
- URLs not required by the task;
- permissions unless explicitly needed and reviewed;
- organization/security metadata unrelated to the operation;
- credential or safe fingerprint;
- provider error body.

### Credential choice

Preferred production direction:

1. GitHub App installation credential with repository-limited permissions.
2. Broker creates/refreshes short-lived installation access tokens.
3. Long-lived app private key remains inside credential authority/broker boundary.

Acceptable pilot:

- a fine-grained personal access token;
- read-only repository metadata/content permission as narrowly as GitHub supports;
- dedicated test owner/repository;
- short expiration and documented rotation;
- no reuse for unrelated tools or agents.

A classic broad personal access token is not an acceptable default.

## Broker transport and authentication

### Unix socket

Use a pathname Unix domain socket under an Aegis-owned runtime directory, not `/tmp`, for example:

```text
/run/aegis/credential-broker.sock
```

Required checks:

- parent directory owner/group/mode and no symlink traversal;
- existing path must be a socket owned by the expected service;
- socket mode permits only the intended runtime bridge group/user;
- finite accept/read/write deadlines;
- bounded connections and concurrency;
- Linux `SO_PEERCRED` PID/UID/GID captured by the broker;
- peer identity is necessary but not sufficient.

A same-UID process can often access the same local resources. Production isolation should use a dedicated broker user and per-session/runtime identity or cgroup/container boundary. The MVP must state when it is operating in a weaker same-user development mode.

### Session capability

The capability should contain or reference:

- random identifier with at least 256 bits of entropy;
- session and mandate IDs/digests;
- agent, stanza, deployment, and charter/projection generation;
- exact allowed operation IDs;
- issue and expiry times;
- replay/sequence state;
- broker audience.

Prefer delivery to the Aegis-owned bridge through a sealed inherited file descriptor or already-connected socket. Do not place it in model-visible configuration, command arguments, transcript, or logs. An environment variable is a weaker compatibility mode because the runtime may inspect it.

The broker must load authoritative session/mandate state by server-side capability ID. Caller-supplied claims are not authority. Capability validation resembles proof-of-possession principles, but RFC 9449 DPoP is not directly adopted for the local socket protocol.

### Request framing

Use a versioned, length-prefixed binary frame carrying strict JSON or a similarly strict codec. Minimum properties:

- maximum frame size, recommended initial limit 64 KiB;
- one request and response schema version;
- reject unknown fields;
- request ID generated by the bridge and deduplicated by the broker;
- deadline and monotonic/freshness policy;
- exact operation enum;
- UTF-8 validation and conservative owner/repository grammar;
- no arbitrary maps, headers, URLs, paths, or serialized HTTP requests;
- no credential reference field.

Illustrative envelope:

```go
type BrokerRequest struct {
    SchemaVersion uint16
    RequestID     string
    CapabilityID  string
    Operation     string
    Arguments     json.RawMessage
    Deadline      time.Time
}
```

`CapabilityID` is an opaque lookup key, not a bearer secret sufficient without peer/session checks.

## Authorization algorithm

For every request:

1. Authenticate socket path and peer credentials.
2. Enforce frame, rate, deadline, and schema bounds.
3. Resolve capability from broker-owned state.
4. Verify active session, mandate, process/runtime association, expiry, and revocation.
5. Verify operation is explicitly present in the selected stanza’s effective capabilities/tools.
6. Derive `CredentialBindingKey` from trusted state:

```text
AgentID      = mandate.AgentID
StanzaID     = mandate.StanzaID
DeploymentID = active deployment
Scope        = operation policy’s fixed scope, e.g. github/read
```

7. Resolve exactly one enabled binding; zero or ambiguity denies.
8. Verify binding mode is `brokered`, destination includes the exact policy destination, and binding revision/projection generation is active.
9. Resolve current/pinned encrypted version; reject revoked record/version.
10. Canonicalize and validate owner/repository against operation and stanza policy.
11. Close all bbolt transactions before decrypting or making network calls.
12. Decrypt through a callback; construct and send the fixed downstream request.
13. Remove authorization material and release plaintext buffers as far as practical.
14. Validate status, content type, response size, JSON schema, and allowed fields.
15. Emit metadata-only decision/execution audit.
16. Return sanitized structured result.

Authorization is per call. A successful previous call or live connection does not authorize the next call.

## Destination and SSRF controls

The first adapter should not accept a URL. Destination policy is code/config reviewed with the operation:

```text
scheme = https
host = api.github.com
port = 443
method = GET
path template = /repos/{owner}/{repository}
```

Controls:

- canonical owner/repository grammar and segment escaping;
- no slash, dot-segment, userinfo, percent-encoded separator, host, port, query, or fragment from the model;
- DNS resolution through the normal trusted resolver with network policy allowing only required GitHub endpoints where operationally feasible;
- proxy environment ignored unless explicitly approved;
- redirects disabled for the MVP; a rename/move is returned as a typed error;
- TLS verification mandatory with controlled trust roots;
- bounded response and decompression size;
- no forwarding of downstream error bodies to the model.

A generic allowlist layered over arbitrary URLs is not equivalent to a fixed typed destination.

## Hermes tool bridge decision

### Verified compatibility issue

Hermes documents MCP as the extension mechanism for connecting narrow local or remote systems. It also supports stdio MCP, tool allowlists, and generated tool names. However, current Aegis launch deliberately uses safe mode and disables ambient MCP/plugins. Current `supportedToolsets` contains only built-in Hermes toolsets and `no_mcp`.

Therefore:

- implementing a broker socket alone does not expose a safe model tool;
- adding a normal user-profile MCP entry would reintroduce ambient mutable authority;
- putting a GitHub token in an MCP server environment would defeat the broker;
- relying on the terminal tool to invoke `curl` or a general `aegis broker` CLI would create a larger and less reviewable action surface.

### Recommended bridge

Add an Aegis-owned executable such as:

```text
aegis-hermes-tool-bridge
```

It should:

- expose exactly the operation schemas authorized for the selected stanza;
- communicate with Hermes over stdio MCP or a future explicit Hermes tool protocol;
- connect to the broker over an inherited authenticated channel;
- contain no upstream credential;
- accept no secret reference or generic destination;
- enforce request/result size and time bounds;
- avoid persistence, network access other than the broker socket, and model calls;
- be pinned to the Aegis binary/build and included in adapter verification.

Aegis should generate the complete MCP configuration inside the disposable session home and verify that only the intended bridge/tool is registered. The runtime must not inherit normal `~/.hermes` MCP config or token files.

### Adapter gate

Before implementation, prove one of these paths against the pinned Hermes version:

1. Hermes supports an explicit command-line or safe-mode mechanism for one Aegis-owned tool bridge while all ambient MCP/plugins remain disabled; **preferred**.
2. Aegis can omit `--safe-mode` yet construct a truly empty disposable home and explicitly disable every ambient input, then register only the generated bridge; acceptable only after conformance tests.
3. Hermes adds a narrow host-injected tool API usable through the TUI gateway; future option.

If none can be demonstrated, do not weaken isolation merely to make the broker model-callable. Keep the broker integration behind Aegis CLI/API tests until the runtime adapter can enforce exact tool registration.

## Multi-agent and trust-stanza behavior

A broker capability is never profile-wide or agent-wide. It is session-specific and stanza-specific.

Example:

```yaml
agent: office
stanzas:
  principal:
    tools: [github_get_repository]
    credentials: [github/read]
  teamwide:
    tools: [github_get_repository]
    credentials: [github/read]
  public:
    tools: []
    credentials: []
```

Principal and teamwide may map `github/read` to different secret records or repository sets. The broker derives the binding from the active mandate. A public session cannot acquire the tool or binding by naming it. Two simultaneously running profiles or sessions receive different capability IDs and no shared writable bridge state.

If two agents intentionally share one record, each requires an exact binding and the audit attributes each use independently. Separate downstream credentials remain the preferred blast-radius boundary.

Subagents inherit no credential authority automatically. A delegated Hermes child may use the parent’s broker tool only if Aegis explicitly defines that orchestration as part of the same session/mandate and can attribute the call. Otherwise, child sessions require independently issued capabilities. This remains an open integration question and should default to deny.

## Failure handling

| Failure | Required behavior |
|---|---|
| Broker unavailable | Tool returns typed unavailable error; no environment credential fallback. |
| Capability unknown/expired/replayed | Deny and audit safe reason. |
| Peer mismatch | Deny before parsing sensitive operation details. |
| Session/mandate revoked | Deny every new request immediately; close active bridge connection. |
| Missing/ambiguous binding | Deny; never choose first or merge. |
| Wrong destination/mode | Deny before decrypt. |
| KEK/authority failure | Broker not ready; no plaintext recovery fallback. |
| GitHub timeout/rate limit | Return typed bounded error; do not expose response headers/token. |
| Redirect | Return typed moved/redirect error in MVP. |
| Oversized/malformed response | Abort and return safe validation error. |
| Audit sink required but unavailable | Fail closed according to action policy. |
| Client disconnect/cancellation | Cancel HTTP request, release plaintext, emit outcome. |
| Broker restart | Existing capabilities are invalid unless reconstructed from authoritative active session state with replay-safe generation; safest MVP invalidates and requires session restart. |

No failure path may fall back to injecting the secret into Hermes.

## Audit and observability

Record:

- request/event ID;
- session, mandate, agent, stanza, deployment, and projection IDs/digests;
- operation ID and broker/bridge version;
- binding revision and secret record/version identifiers;
- destination policy ID, not arbitrary URL;
- decision/outcome/reason;
- latency buckets, response byte count, downstream status class, and retry count;
- peer UID/GID and a protected process identity reference where operationally appropriate.

Do not record:

- credential value or fragments;
- authorization header;
- capability secret;
- raw downstream body;
- complete private repository data unless separately authorized;
- raw tool prompt or model reasoning;
- low-entropy unkeyed fingerprints.

Metrics should include authorization denies by reason, broker readiness, resolve/decrypt/downstream/sanitize latency, active connections, rate-limit outcomes, cancellation, and bridge registration verification. Labels must be bounded to avoid repository/record cardinality and metadata leakage.

## Options considered

| Option | Benefits | Risks | Recommendation |
|---|---|---|---|
| Continue environment injection | Already works | Hermes/runtime can read reusable credential; no per-action destination enforcement | Deprecate for each brokered integration. |
| Return secret through broker | Simple integration | Directly exposes reusable credential to model/runtime; copy/exfiltration cannot be controlled | Reject. |
| Generic credential lease/file | Broad compatibility | Runtime disclosure, cleanup uncertainty, broad reuse | Compatibility-only, not first broker proof. |
| Generic HTTP proxy | Flexible | SSRF, header smuggling, arbitrary destinations/methods, huge policy surface | Reject for MVP. |
| Aegis-owned typed GitHub read tool | Narrow, testable, no value disclosure | Requires Hermes bridge and response policy | Adopt first. |
| Existing third-party GitHub MCP with token env | Easy | Token resides in child environment/profile; third-party supply chain; weak mandate binding | Reject as broker architecture. |
| Terminal invokes broker CLI | Minimal bridge work | Terminal is broad; capability exposure and argument surface | Use only for operator testing, not model-facing MVP. |
| Model-provider reverse proxy first | Removes provider key and enables prompt scan | Streaming/tool protocol complexity, high availability, every prompt depends on it | Important next project, not first broker action. |

## Implementation plan

### Phase 1 — Contracts and denial tests

- Add versioned operation registry and strict `github.get_repository.v1` schema.
- Add broker request/result/error types and reason codes.
- Add a `CredentialAuthorizer` deriving binding key from trusted mandate/session state.
- Add tests proving caller secret reference, scope, destination, agent, stanza, and deployment fields do not exist or are ignored/rejected.
- Build mock authority and mock GitHub endpoint tests before socket integration.

### Phase 2 — Local broker service

- Implement Aegis-owned runtime directory and Unix socket lifecycle.
- Add `SO_PEERCRED`, permission checks, deadlines, bounds, concurrency limits, capability state, replay protection, and cancellation.
- Resolve/decrypt outside bbolt transactions.
- Add metadata-only audit and readiness.
- Run broker under a distinct service boundary where deployment supports it.

### Phase 3 — GitHub read adapter

- Implement fixed request construction, TLS, redirect denial, proxy policy, response cap, JSON decode, field allowlist, and typed errors.
- Test with a mock server for redirect/SSRF/error/oversize cases.
- Pilot against one low-risk private test repository and dedicated read-only credential.
- Rotate/revoke the pilot credential after tests.

### Phase 4 — Hermes bridge proof

- Prototype the Aegis-owned stdio bridge against pinned Hermes v0.18.x.
- Determine whether exact bridge registration can coexist with safe mode.
- Generate disposable configuration and verify effective tools before prompt acceptance.
- Pass capability via inherited channel, not argv/profile.
- Keep inherited MCP/plugins/tokens disabled.
- If conformance fails, stop and document the Hermes adapter gap rather than weakening isolation.

### Phase 5 — Cutover and hardening

- Remove the pilot GitHub credential from Hermes environment/config.
- Add OS/network policy preventing alternate credentialed GitHub paths where feasible.
- Add restart/revocation/rotation/race/fuzz/failure tests.
- Document operator recovery and rollback without environment fallback.
- Only then certify the tool for non-test stanzas.

## Testing and acceptance criteria

### Protocol and authentication

1. Unauthorized UID/GID and wrong socket path/mode fail.
2. Peer credentials alone are insufficient without an active session capability.
3. Capability alone is insufficient from the wrong peer/runtime boundary.
4. Unknown fields, oversized frames, malformed JSON, stale deadlines, duplicate request IDs, and excessive concurrency fail safely.
5. Capability/session/broker restart behavior matches documented invalidation semantics.

### Authorization and isolation

6. Agent A/stanza X cannot use agent A/stanza Y, agent B, another deployment, or another projection generation.
7. Prompt/profile/tool arguments cannot select secret reference, scope, destination, token type, or binding version.
8. Zero or multiple bindings deny.
9. Revoked/expired mandate, binding, record, version, or projection denies on every call.
10. Subagent/child access defaults to deny unless explicitly attributable to the same authorized session contract.

### Secret containment

11. Credential value is absent from Hermes environment, argv, home, MCP config, tool arguments/results, transcript, logs, audit, panic output, metrics, and temporary files.
12. The bridge and model never receive the upstream token.
13. Wrong KEK/AAD/ciphertext prevents downstream request.
14. Cancellation and every injected error path release plaintext without logging it.
15. No bbolt transaction remains open during decrypt, network, or model work.

### GitHub adapter

16. Only HTTPS `api.github.com:443`, GET, and canonical repository path are possible in production policy.
17. Model input cannot inject slash, host, scheme, port, query, fragment, userinfo, headers, or encoded path separators.
18. Redirects, malformed content type, oversized/compression-bomb body, invalid JSON, and unexpected fields fail safely.
19. Returned result contains only approved fields and is marked untrusted.
20. A real low-risk private repository request succeeds with a dedicated read-only credential.

### Hermes integration

21. Fresh session home contains exactly the generated bridge configuration and no inherited MCP, plugins, tokens, memory, skills, project rules, or profile state.
22. Effective tool registration is verified before prompt acceptance.
23. A stanza without the tool cannot call it.
24. Two concurrent sessions receive independent capabilities and bridge state.
25. Removing/revoking the binding prevents subsequent tool calls without falling back to environment credentials.

### Performance

26. Measure p50/p95/p99 authorization, resolve/decrypt, downstream, sanitize, and total latency under concurrent sessions.
27. Broker bounds connections, request rate, payload, response, and total duration without starvation.
28. Rotation and metadata administration do not hold long bbolt transactions or corrupt active reads.

## Risks and mitigations

| Risk | Mitigation |
|---|---|
| Enabling MCP weakens current safe-mode boundary | Generated disposable config, exact allowlist, adapter conformance, or defer integration. |
| Same-user processes steal capability | Distinct service/runtime identities, inherited FD, peer/cgroup checks, short expiry; label weaker dev mode. |
| Read-only GitHub data is still sensitive | Exact repo policy, response minimization, stanza data-flow controls, no audit body. |
| Fine-grained PAT remains reusable | Dedicated low-risk pilot, short expiry, rotation; migrate to GitHub App token. |
| Model abuses permitted reads at scale | Per-session/action rate and repository bounds, audit, revocation, budgets. |
| Redirect or proxy creates SSRF path | Disable redirects; fixed host/scheme/method; ignore unapproved proxy env. |
| Tool bridge supply-chain compromise | Aegis-owned pinned binary, digest verification, no package-manager runtime install. |
| Broker becomes availability bottleneck | Readiness, bounded concurrency, typed errors, supervised restart; never insecure fallback. |
| Generic abstraction creeps into MVP | One operation enum and one adapter; add operations only through review and tests. |
| Credential is exposed after host root compromise | Do not overclaim; separate high-trust deployment or external broker/HSM for stronger boundary. |

## Open questions

1. Can pinned Hermes v0.18.x register one Aegis-owned stdio MCP server while preserving safe-mode guarantees, or is an upstream Hermes change required?
2. Should the bridge be a subcommand of the Aegis binary or a separately installed, independently sandboxed binary?
3. What runtime identity boundary is supportable for the first deployment: separate Unix user, systemd transient unit, container, or same-user development mode?
4. How should Hermes subagents be attributed to the parent mandate, and should they ever share a broker capability?
5. Which GitHub permissions and sanitized response fields are necessary for the first real Aegis workload?
6. Is a dedicated fine-grained PAT acceptable for the pilot, or should the first implementation start directly with GitHub App installation tokens?
7. What network enforcement prevents direct GitHub access from broad Hermes `web`, `browser`, or `terminal` tools while still permitting intended model-provider traffic?
8. Should broker capabilities be reconstructed after broker restart or always force a fresh Aegis/Hermes session?
9. What audit metadata about private repository names is acceptable?
10. After the GitHub proof, should the next integration be a model-provider egress proxy or another typed tool action?

## Repository files reviewed

- `docs/ARCHITECTURE.md`
- `DEPLOYMENT_PROJECTION_ARCHITECTURE.md`
- `specs/DEPLOYMENT_PROJECTION.md`
- `research/2026-07-17-embedded-bbolt-credential-authority.md`
- `research/2026-07-17-secure-prompt-ingress-and-secret-intake-mvp.md`
- `research/2026-07-17-frontier-models-local-inference-hermes-secret-protection-research.md`
- `research/HERMES_RUNTIME_RESEARCH.md`
- `internal/credentials/model.go`
- `internal/credentials/authority.go`
- `internal/credentials/crypto.go`
- `internal/credentials/custody.go`
- `internal/credentials/bbolt/store.go`
- `internal/credentials/bbolt/store_test.go`
- `internal/command/secret.go`
- `internal/config/config.go`
- `internal/runtime/hermes/hermes.go`
- `internal/api/peercred_linux.go`
- `internal/app/service.go`

## Sources

All external sources accessed 2026-07-17.

1. **unix(7) — Linux manual page** — Linux man-pages project / Michael Kerrisk; current rendered manual; https://man7.org/linux/man-pages/man7/unix.7.html — supports Unix-domain socket pathname permissions and peer credential behavior.
2. **RFC 9449: OAuth 2.0 Demonstrating Proof of Possession (DPoP)** — IETF; September 2023; https://www.rfc-editor.org/rfc/rfc9449.html — supports the distinction between bearer possession and request-bound proof/freshness; used as a design reference, not directly adopted.
3. **Model Context Protocol security best practices** — Model Context Protocol project; specification dated 2025-06-18; https://modelcontextprotocol.io/specification/2025-06-18/basic/security_best_practices — supports confused-deputy, token handling, audience, and local-server security considerations.
4. **Use MCP with Hermes** — Nous Research; Hermes v0.18.x live documentation; https://hermes-agent.nousresearch.com/docs/guides/use-mcp-with-hermes — supports MCP as Hermes’s external tool adapter, stdio operation, and exact include filtering.
5. **MCP Config Reference** — Nous Research; Hermes v0.18.x live documentation; https://hermes-agent.nousresearch.com/docs/reference/mcp-config-reference — supports stdio MCP configuration, tool allowlists, token persistence behavior, and tool naming.
6. **Programmatic Integration** — Nous Research; Hermes v0.18.x live documentation; https://hermes-agent.nousresearch.com/docs/developer-guide/programmatic-integration — supports TUI gateway JSON-RPC scope and events; confirms it is a host/session protocol rather than documented arbitrary tool injection.
7. **REST API endpoints for repositories: Get a repository** — GitHub; API version 2022-11-28; https://docs.github.com/en/rest/repos/repos?apiVersion=2022-11-28#get-a-repository — supports the chosen fixed read-only endpoint and response contract.
8. **About authentication to GitHub** — GitHub; current documentation; https://docs.github.com/en/authentication/keeping-your-account-and-data-secure/about-authentication-to-github — supports scoped token and GitHub App authentication choices.
9. **Server-Side Request Forgery Prevention Cheat Sheet** — OWASP Foundation; current; https://cheatsheetseries.owasp.org/cheatsheets/Server_Side_Request_Forgery_Prevention_Cheat_Sheet.html — supports fixed destinations, strict allowlisting, and avoiding user-controlled URL resolution.

## Conclusion

The encrypted authority is necessary but not sufficient. Aegis does not obtain a meaningful secret-security boundary until the model can request a useful operation without receiving either the credential or a generic proxy primitive.

The minimum defensible bridge is one Aegis-owned typed tool, one mandate-bound local broker protocol, and one fixed read-only downstream adapter. GitHub repository metadata is the appropriate proof operation. It exercises exact agent/stanza/deployment/scope authorization, encrypted credential resolution, destination enforcement, response sanitization, audit, revocation, and concurrent sessions while keeping the credential outside Hermes.

The Hermes integration must not be hand-waved. Current safe mode and no-MCP behavior intentionally exclude ambient extensions. Aegis should prove exact generated tool registration against the pinned runtime before enabling the model-facing bridge. If that cannot be done without weakening isolation, the correct MVP behavior is to defer the model bridge—not to return the secret, enable inherited MCP, or expose a generic HTTP proxy.
