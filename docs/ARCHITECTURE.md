# Aegis MVP Architecture

```mermaid
flowchart TB
  CLI[Cobra CLI] --> Service[Shared application service]
  API[Echo v5 API] -->|Bearer + Unix SO_PEERCRED| Service
  OS[Local OS identity] --> Service
  Service --> Validator[Strict charter validation and canonical digest]
  Service --> Selector[Single-stanza selector]
  Service --> Approval[Exact single-use approval transaction]
  Service --> Provisioner[Typed deterministic provisioner]
  Service --> Mandate[Short-lived mandate issuer]
  Mandate --> Adapter[Hermes 0.18.x adapter]
  Adapter -->|safe mode, explicit toolsets and credentials| Hermes[Fresh Hermes process + disposable home]
  Service --> State[(Charters, plans, approvals, receipts, sessions)]
  Service --> Audit[(Hash-linked audit)]
  Audit --> Checkpoint[(Ed25519 checkpoints)]
  Design[Hermes design gateway] -. proposal only .-> Validator
  Credentials[Configured environment bindings] -->|selected provider only| Adapter
  SecretCLI[Principal-only secret administration] --> Authority[(Encrypted bbolt authority)]
  Custody[systemd credential or weaker host KEK file] -->|wraps per-record DEKs| Authority
  Authority --> Broker[Session-bound GitHub repository broker]
  Broker -->|SO_PEERCRED + capability; sanitized result| Bridge[Future verified Hermes bridge]
  Bridge -. Hermes 0.18 safe-mode gate .-> Adapter
  Updater[CLI self-updater] -->|GitHub release + SHA256SUMS| Binary[Aegis executable]
  Init[Deterministic first-run initializer] -->|host UID/user + confirmation; atomic 0600| Config[(Aegis configuration)]
  Terminal[Principal terminal] --> Manager[Built-in secrets-manager shell]
  Manager --> Ingress[Source-aware secret guard]
  Manager --> ManagerGateway[Structured multi-turn Hermes client]
  ManagerGateway --> Proxy[Ephemeral authenticated inference proxy]
  Proxy --> Ollama[Exact loopback Ollama model]
  Onboarding[Authenticated installed-only onboarding] -->|preview + yes; atomic config| Config
  Onboarding -->|version/tags only; no pull| Ollama
```

The model proposes; it never authenticates, approves, or provisions. Design uses a disposable Hermes gateway process and returns an enveloped charter proposal. Aegis strictly decodes, validates, canonicalizes, digests, and persists it.

Provisioning currently supports only atomic creation of deterministic Aegis-owned mapping files. File modification, Hermes profile creation, MCP/plugin configuration, gateways, services, cron, and external network effects are explicitly classified and denied.

Operational launch resolves one stanza into one mandate, one credential binding, one set of Hermes toolset arguments, and one clean process/home. Selection evaluates verified subject, method, issuer, freshness, and trusted environment data; a requested stanza only filters already-authorized matches. Zero matches, overlapping policy, stale authentication, and multiple matches fail closed. Stored charter bytes and digests are revalidated before use, and mandate authority is compared exactly with the selected stanza before launch. `toolset_verification: launch_arguments` records argument-level verification rather than individual-tool runtime attestation.

The optional credential authority is a separate administrative data path. It stores independently encrypted immutable versions, exact agent/stanza/deployment/scope bindings, revocations, and metadata in one deployment-bound bbolt file. It validates schema, structural integrity, filesystem ownership/mode, and a KEK-authenticated sentinel before serving administration. Secret intake is outside the model and avoids argv; inspection returns metadata only. Consistent backups use bbolt read transactions and do not include the KEK.

The optional Linux broker is an active authority-to-downstream edge, but not yet a model-visible Hermes edge. It exposes only `github.get_repository.v1`, derives the exact binding and `github-api` destination from current Aegis state, applies the credential internally, and returns a bounded field allowlist. Its pathname socket authenticates a distinct runtime identity with `SO_PEERCRED`; a 256-bit capability is bound to the exact live session, mandate, charter, deployment, stanza, PID/start token, and expiry. Fresh 128-bit request IDs and bounded deadlines are deduplicated in a finite per-capability replay cache. Session cleanup revokes the capability and removes its file. Hermes 0.18.x safe mode has not yet demonstrated exact Aegis bridge registration without ambient MCP, so the dotted bridge-to-adapter edge remains gated. Operational provider authentication continues through the configured environment-binding path. See `CREDENTIAL_BROKER.md`.

The API uses the same services as Cobra. Bearer authentication is transport-only; Linux Unix peer credentials create the Aegis subject. TCP TLS is optional transport encryption and does not map a principal identity.

Application services depend on a narrow audit-authority interface for append, inspection, and verification. The local MVP injects the file/checkpoint store; hardened deployment must place the same boundary behind a separately supervised process or OS account. Hermes processes receive neither that interface nor an audit credential. This service boundary does not by itself make the default same-account deployment externally tamper-proof.

Provisioning intent is persisted before approval consumption. Startup recovery finalizes interrupted receipts and removes only artifacts whose decoded content still matches the approved effect digest; mismatching files are retained and reported for manual intervention.

Self-update is an installation operation outside the application service and agent authority model. It accepts only published non-draft stable SemVer releases with exact metadata from the fixed Aegis GitHub repository, rejects API and untrusted or multi-hop download redirects and downgrades, follows only GitHub's bounded HTTPS release-asset redirect, bounds and validates the single-file archive, verifies its published SHA-256 checksum, and atomically replaces the current executable when its directory is writable. Local and remote Git tags are not updater discovery inputs.

Release publication is a separate deterministic operator transaction. Fresh publication performs signing preflight before its changelog-only commit and signed tag. If atomic push fails, recovery verifies the immutable tag object/signature/target, reproduces the exact release commit from its parent, re-verifies tagged source, compares local and remote objects explicitly, and publishes only missing refs without force. Hermes review remains advisory and has no publication authority.

Root dispatch inspects configuration before constructing operational services. Help, version, update, and initialization do not require principal configuration. A genuinely absent interactive installation enters the deterministic initializer and then the manager; non-TTY absence returns structured remediation without reading input. Malformed, insecure, partial, and ambiguous artifacts remain distinct fail-closed states. The initializer authenticates through host account APIs, previews exact paths/content, and uses a synced no-replace atomic publication rather than model, Hermes, Ollama, credential, provisioning, or profile authority.

The manager orchestrator owns one explicit lifecycle transaction: authenticate the principal; inspect authority/model/certification/Hermes readiness; verify exact certification and route identity; establish managed or external-local Ollama; load the pinned artifact; start an expiring authenticated proxy; launch a disposable safe-mode Hermes stdio gateway with no ambient extensions; execute closed typed proposals through shared credential services; and clean up in reverse order. Terminal cancellation, expiry, runtime failure, EOF, and operator exit all transition through bounded idempotent cleanup. The executable boundary gives the first termination signal to that lifecycle and restores default handling so a second signal cannot be trapped. Hermetic fake-process and PTY tests exercise managed readiness/shutdown, multi-turn gateway behavior, proposal confirmation, protected-intake restoration, signal/EOF behavior, capability replay/expiry, model unload, receipt finalization, and disposable-state removal.

Candidate onboarding is separate deterministic application code, not a model turn. It lists a closed no-default registry, compares only local Ollama `/api/version` and `/api/tags` metadata at a loopback endpoint, previews managed versus external-local ownership, and applies an exact digest-bound external-local route only after principal authentication and literal confirmation. Atomic validated configuration publication does not pull/copy an artifact and does not certify or activate it. No real candidate has been downloaded or certified, so unconfigured or drifted hosts remain in truthful deterministic degraded mode.
