# Aegis MVP

Aegis is a Go control plane for authenticated, trust-stanza-bound sessions over an explicit Hermes Agent runtime. It does not hide Hermes, infer authority from prompts, or treat the model as an approver or provisioner.

Its security contract is: identity and authority are established outside the model; prompts, profile names, model conclusions, and stanza requests are not authentication. Each runtime session binds to exactly one authenticated trust stanza. Trust stanzas are security contexts, not personalities: zero or multiple authorized matches deny, grants are never unioned, and a stanza or material-authority change requires a new mandate and clean session.

Start with the [five-minute quickstart](docs/QUICKSTART.md) or executable [no-key demonstration](docs/DEMO_NO_KEY.md). Normative behavior is defined in the [Markdown specifications](specs/README.md). Security boundaries are detailed in the [security policy](SECURITY.md), [threat model](docs/THREAT_MODEL.md), and [architecture](docs/ARCHITECTURE.md). See [CONTRIBUTING.md](CONTRIBUTING.md), [CHANGELOG.md](CHANGELOG.md), and the repository-local [early contributor backlog](docs/contributing/ISSUE_BACKLOG.md).

## Install and update

Tagged releases use stable Semantic Versioning tags (`vMAJOR.MINOR.PATCH`). Install directly with Go:

```sh
go install github.com/berryhill/aegis/cmd/aegis@latest
```

Alternatively, download the matching archive and `SHA256SUMS` from the GitHub release. Release archives support Linux and macOS on amd64 and arm64. A release-archive installation can check for or atomically install the latest release:

```sh
aegis update --check
aegis update
aegis --update
```

`aegis --update` is a strict root-only alias for `aegis update`; ambiguous combinations with subcommands or other root actions are rejected. Both forms use the same checksum-verifying updater before atomically replacing the exact current executable. Package-manager-owned or non-writable executables should be updated through their original installation method.

The updater selects only the latest exact published stable GitHub release and fails closed if its repository identity, publication metadata, archive, or matching `SHA256SUMS` entry is unavailable or invalid. A local or remote Git tag alone is not an available update; the tag-triggered workflow must finish publishing the GitHub release.

## Build

```sh
go build -o aegis ./cmd/aegis
```

Go 1.26.5 or newer is required. `aegis version` and `aegis --version` print the same build version without requiring configuration. The application uses Cobra, isolated Viper instances, Echo v5, context cancellation, and injected `log/slog` loggers. Development builds and the Hermes adapter report `dev`; tagged release builds inject and share the tag version, and versioned `go install` builds recover the stable module version from embedded Go build information.

## Configure

For a new local installation, run bare `aegis` in a terminal or run `aegis init`. The literal default root is `~/.argis`: configuration is `~/.argis/aegis.yaml`, state is `~/.argis/state`, and XDG variables do not scatter local defaults. Aegis verifies the current UID/username through host-native account APIs, displays the exact resolved paths plus complete configuration content, and writes only after the operator types `yes`. The root is mode `0700` and configuration is atomically published mode `0600`. Initialization does not invoke Hermes, Ollama, a model, cloud service, credential creation, provisioning, or profile modification. See [Local Path Layout](docs/PATH_LAYOUT.md).

For an explicit example configuration, copy it, set restrictive permissions, replace the principal placeholders, and make the matching replacement in `examples/office-charter.json`:

```sh
cp examples/aegis.yaml .aegis.yaml
chmod 600 .aegis.yaml
```

The binding is mandatory. Precedence is:

1. CLI flags
2. `AEGIS_*` environment variables
3. Explicit config file
4. compiled defaults

The API token is redacted by `aegis config`. It authenticates API transport; it does not replace local OS principal authentication for principal operations.

## End-to-end workflow

Bare interactive `aegis` immediately renders a deterministic bootstrap screen, initializes a genuinely absent installation only after exact preview and conventional `[Y/n]` confirmation, derives progress from authoritative artifacts, and resumes the first incomplete prerequisite. It enters the built-in manager only after principal, credential authority, Hermes, loopback Ollama route, exact model artifact, and certification checks all pass. Bare non-TTY use never prompts or mutates: it emits a structured state/reason/next-command payload and exits 2. Existing malformed, insecurely permissioned, partial, drifted, or ambiguous artifacts enter a repair-required path and are never treated as absent or silently overwritten. The manager authenticates the configured OS principal, renders startup stages plus persistent principal/agent/trust/authority/policy context, labels model text as untrusted, consumes local slash directives and exact `quit`/`exit` aliases, and blocks high-confidence credential pastes before any model path. Ordinary prose containing those words remains a normal turn. EOF, session expiry, runtime failure, SIGINT, and SIGTERM converge on one bounded idempotent cleanup path; the first termination signal requests graceful shutdown and a second restores the operating system's default immediate action. Startup failures retain exact reason codes and degrade only to the administration that is actually available, without cloud/model fallback or unsupported secret commands.

The bootstrap wizard defaults bare local custody to a passphrase-encrypted random KEK that it can create and verify in the current terminal; the passphrase is collected twice with echo disabled and is never persisted. Host-file authority is labeled development-only. Systemd custody is an advanced option only usable when an actual service has delivered credential material; an incomplete undelivered selection can switch safely to encrypted local custody. Every mutation has a complete preview, explicit effects/non-effects, `[Y/n]` confirmation, digest/artifact revalidation, and post-write verification. Model acquisition remains limited to the closed no-default candidate registry. Choosing `pull` displays name/source/size/route/destination implications before confirmation. Download progress is streamed, cancellation is safe to resume, and the downloaded registry name is not trusted as identity: Aegis rediscovers the artifact, binds its exact digest, then runs certification separately. No cloud fallback, silent model switching, normal Hermes-profile mutation, or external authority/systemd provisioning occurs.

`aegis reset` is the destructive, principal-authenticated development/testing path for replaying first run. It prints a deterministic path-by-path plan and preservation list, requires a real terminal plus explicit `yes` at an `[y/N]` prompt, reauthenticates and revalidates the complete plan immediately before deletion, and removes the configuration last. It inventories only recognized, securely owned artifacts under a validated operator-home scope; unknown files, symlinks, hard links, ownership/mode failures, repository paths, identity drift, and plan drift deny before deletion. Exact legacy defaults are also reset safely beneath group-writable XDG parents using descriptor-anchored no-follow operations; unsafe external parents are never chmodded or treated as artifacts, and an empty legacy child may be truthfully retained. `aegis migrate-layout` performs the separate real-TTY, digest-bound Linux migration from exact legacy defaults and accepts Enter at its displayed `[Y/n]` default; canonical and legacy coexistence fails closed. External authority paths and all systemd credentials are preserved. The executable, checkout, normal Hermes profiles, Hermes/Ollama installations, operator-managed Ollama daemon, external model stores, and downloaded model data—including Aegis's managed model store—are preserved.

Local-model onboarding is deterministic application code. `aegis manager model candidates` lists the closed candidate registry and confirms there is no default; `model route` previews managed versus external-local ownership; `model discover --endpoint LOOPBACK_URL` inspects only already-installed Ollama artifacts; and `model configure CANDIDATE_ID --endpoint LOOPBACK_URL` prints the exact digest-bound mutation and ownership/copy implications, then requires literal `yes` before atomically replacing an existing secure valid configuration. It never pulls, downloads, imports, or copies a model and never activates an uncertified artifact. Bootstrap visibly auto-selects an exact sole approved installed candidate instead of presenting a one-item menu; multiple installed candidates have no selection default. `model status` reports configuration, artifact, and certification drift with the next exact command.

Live certification remains deliberately opt-in and is never part of default tests or startup. After selecting one discovered official candidate, explicitly run `aegis manager certify CANDIDATE_ID`. The command exercises the complete Hermes → authenticated proxy → Ollama path at the configured 64K context and publishes the mode-`0600` certification only if every semantic case passes. When configuration names that exact installed digest and matching certification record, startup verifies Hermes/Ollama versions, artifact digest, certification identity, and 64K context, then starts the authenticated proxy and disposable safe-mode Hermes gateway. No real artifact was downloaded or certified by this repository change, so examples remain deliberately unconfigured.

```sh
./aegis --config .aegis.yaml runtime
./aegis --config .aegis.yaml charter validate examples/office-charter.json

# Structured Hermes gateway design turn, safe mode, disposable HERMES_HOME,
# no provisioning authority. The file supplies principal requirements.
./aegis --config .aegis.yaml design --draft examples/office-charter.json

# Non-interactive minimal design turn; without configured provider credentials
# this reaches the provider boundary and fails honestly.
./aegis --config .aegis.yaml design --smoke

./aegis --config .aegis.yaml charter import examples/office-charter.json
./aegis --config .aegis.yaml plan preview office --revision 1
./aegis --config .aegis.yaml approval request PLAN_ID --ttl 5m
./aegis --config .aegis.yaml approval approve APPROVAL_ID
./aegis --config .aegis.yaml provision PLAN_ID APPROVAL_ID
./aegis --config .aegis.yaml session preview office --revision 1 --stanza principal
./aegis --config .aegis.yaml session start MANDATE_ID
./aegis --config .aegis.yaml session show SESSION_ID
./aegis --config .aegis.yaml session revoke SESSION_ID --reason operator_request
./aegis --config .aegis.yaml audit verify
```

All command results are JSON on stdout. Diagnostics and the explicit design-mode warning go to stderr. A stanza flag is only a narrowing request and never identity evidence. `charter explain` and denied session previews retain the shared machine-readable decision on stdout; `charter effective` authenticates and authorizes the caller before returning only the selected stanza's capabilities, tools, memory and credential scopes, session/approval limits, and Hermes mapping.

## Charter representation

Charters are strict JSON. Unknown fields, trailing data, and omitted policy blocks or nested policy fields are rejected. Every selector must explicitly constrain issuer and the MVP's trusted `local` control-plane environment and anchor a subject, principal, or exact claim; unsupported authentication methods and wildcard/duplicate selector values fail validation. Approval binds SHA-256 over deterministic Go JSON serialization of the typed charter and complete typed provisioning plan; stored charter bytes/digests and plan digests are recomputed before use. Review explicitly shows charter/plan digests, runtime details, complete effects, per-stanza toolsets, memory and credential scopes, approval semantics, warnings, and the full previous-revision diff. Revisions are immutable on disk. Duplicate stanza IDs, overlapping enabled selectors, wildcard authority/scope, delegation, cross-stanza flow, persistent profiles/homes, runtime extensions, and mismatched Hermes tool grants/toolsets are rejected.

In the MVP, a charter `grant.tools` entry is a Hermes toolset ID because toolsets are the hard runtime-registration boundary Hermes exposes. `grant.tools` and `hermes.toolsets` must match exactly. MCP and plugin provisioning is rejected. Operational launches pass only the selected model-provider environment binding (`provider:<provider>`). The separately enforced `github/read` scope is accepted only with `github.get_repository.v1` and is consumed by the optional local broker; it is not injected as a Hermes environment credential.

## Encrypted credential authority

Aegis now includes the local administrative foundation for storing reusable secrets without persisting plaintext values. Bare onboarding defaults to a passphrase-encrypted local KEK: it derives a wrapping key with Argon2id, stores only an XChaCha20-Poly1305 credential envelope, initializes and verifies the deployment-bound bbolt database, and keeps the passphrase out of persistent configuration. Actual service deployments may instead use an externally delivered encrypted systemd credential; plaintext host-file KEK custody remains an explicitly weaker development fallback. Each immutable secret version is encrypted with a fresh DEK and XChaCha20-Poly1305 nonce; the DEK is independently wrapped by a versioned KEK. The database and custody files are mode `0600`, use fixed schema buckets and a finite writer-lock timeout, perform startup schema/structural/key checks, and support exact bindings, rotation, logical revocation, and consistent ciphertext backups. Follow the deterministic [credential-authority setup path](docs/CREDENTIAL_AUTHORITY_SETUP.md).

Principal-only administration is available through `aegis secret initialize|put|metadata|list|rotate|bind|revoke|backup`. The shared authority service also supplies bounded metadata list/search/history and the model-backed manager's closed proposal flow for create, rotate, revoke, and binding. Aegis renders the exact metadata-only preview, requires operator confirmation, and collects create/rotate values through confirmed no-echo intake outside Hermes and Ollama. `put` and `rotate` retain deterministic CLI operation and protected `--stdin` support. Values are never accepted in argv or returned by the CLI. `metadata` and history do not decrypt. Host-file initialization is intended for development and is explicitly reported as weaker; production service configuration should use `LoadCredentialEncrypted` and `custody: systemd`. Keep KEK/recovery material separate from database backups.

The optional Linux [session credential broker](docs/CREDENTIAL_BROKER.md) now proves one narrow downstream path: `github.get_repository.v1`. It combines pathname-socket `SO_PEERCRED`, a short-lived exact-session capability, live mandate/runtime checks, and one `github/read` authority binding; it applies the credential inside Aegis and returns only sanitized repository metadata. It is not GetSecret, a generic proxy, or a claim that all credentials are brokered. Existing Hermes provider authentication remains environment-backed, and the broker is not model-visible until an exact Aegis-owned bridge can be verified under Hermes safe-mode constraints. Fleet projections, production unit/identity provisioning, TPM recovery, and Infisical migration remain separate boundaries.

## Runtime behavior and limits

- Supported adapter range: Hermes Agent `>=0.18.0,<0.19.0`; installed version constraints in each charter are enforced.
- Design uses real `hermes --safe-mode --tui --toolsets no_mcp`, never `-z`/one-shot.
- Every operational session gets a new process and disposable `HERMES_HOME`.
- Safe mode disables inherited user config, project rules, memories, plugins, and MCP.
- Aegis resolves and records the exact Hermes toolset IDs placed in the launched process arguments and fails closed if those arguments differ from the approved mandate. Hermes 0.18.x does not expose a stable post-launch API for enumerating the fully registered individual-tool surface, so `toolset_verification: launch_arguments` must not be interpreted as individual-tool runtime attestation.
- Revocation updates the mandate and terminates the recorded runtime PID.
- Stanza changes and material authority changes require a newly issued mandate and a clean runtime session; Aegis does not switch or expand authority inside a running session.
- Hermes-home/process isolation is not a host filesystem, network, container, or VM sandbox. A charter granting `terminal` or `file` intentionally grants broad host-facing authority and must be reviewed accordingly.
- Persistent named Hermes profiles, gateways, services, cron, arbitrary plugins/MCP, and external provisioning actions are denied by the MVP provisioner.
- Provisioning creates only new deterministic Aegis-owned mapping files atomically. Durable in-progress receipts are recovered on the next command: matching owned artifacts are removed and the receipt is finalized as failed; non-matching artifacts are preserved for manual review.
- Operational provider authentication must be named by the stanza as `provider:<provider>`, configured under `credentials.provider_auth.<provider>`, and present in its configured source environment variable. Aegis injects only that resolved binding into Hermes; ambient provider keys are not inherited. Design receives no provider credential unless `credentials.design_provider` explicitly selects a configured provider binding. Credential values are not persisted or included in command output, receipts, audit events, errors, or model prompts.
- The optional bbolt credential authority is an administrative store plus the backing authority for the implemented local broker's one typed GitHub action. It is not a generic operational secret source and does not replace environment-backed model-provider authentication.
- A live design turn requires an explicitly selected provider credential available to the disposable Hermes process. Discovery and expected provider-authentication failure remain testable without a key.

## API

`aegis serve` runs Echo v5 in the foreground. It exposes `/livez`, `/readyz`, and protected `/v1` routes for the charter, design, plan, approval, provisioning, session, inspection, configuration, and audit workflows. Cobra and Echo call the same application services. The HTTP bearer token authenticates transport, not principal identity. Unix-socket mode derives caller identity from Linux `SO_PEERCRED` and maps it through the configured UID; bearer-only TCP callers cannot acquire principal authority. Optional TCP TLS 1.2+ is enabled by configuring both `api.tls_cert_file` and `api.tls_key_file`, but TLS without an identity mapper still does not grant principal authority. Source-level pre-authentication and subject-level post-authentication rate limits are applied without trusting `Forwarded` or `X-Forwarded-*` headers. The server has explicit header/read/write/idle/shutdown timeouts, request limits, request IDs, safe errors, panic recovery, structured logging, and authorization in shared services. The default listener is loopback-only.

Audit records are cross-process locked, hash-linked, and checked against Ed25519-signed checkpoints under the separately configurable `audit.checkpoint_dir`. Application services write through a narrow injectable audit-authority interface that is never passed to Hermes; the default implementation remains in-process. For stronger append separation, deploy that interface behind a separately supervised process/account and retain checkpoints on separately protected storage. The default local deployment is not an external transparency service.

## Verification

```sh
gofmt -w cmd internal
go build ./cmd/aegis
go test ./...
go test -race ./...
go vet ./...
govulncheck ./...
```

Tests cover OS principal mapping, prompt/profile/flag non-authentication, successful/zero/ambiguous/requested/disabled/stale/wrong-method/wrong-issuer/wrong-environment selection, selector-overlap rejection, no authority union, authority-relevant digest mutation, CLI/API decision parity, exact approval mutation/expiry/replay, clean runtime homes, revocation/termination, design non-provisioning, secret redaction, audit tamper detection, strict config, credential envelope-context mutation, wrong keys, exact binding and destination denial, concurrent authority reads/rotation, backup/restore, and bounded credential codecs. Hermetic fake Ollama/Hermes tests cover installed-only discovery and lifecycle rollback; Linux PTY subprocess tests cover cancellation during ordinary and protected intake, echo restoration, EOF, aliases, first-signal cleanup, and second-signal forced termination. `cmd/aegis/e2e_test.go` builds and exercises the real CLI against an isolated Hermes fixture without using the installed Hermes profile or external services. GitHub Actions runs formatting, build, tests, race tests, vet, and `govulncheck`.

## License

Aegis is licensed under the [Apache License 2.0](LICENSE).
