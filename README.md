# Aegis MVP

Aegis is a Go control plane for authenticated, trust-stanza-bound sessions over an explicit Hermes Agent runtime. It does not hide Hermes, infer authority from prompts, or treat the model as an approver or provisioner.

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
```

`aegis update` verifies the archive against the release checksum before replacing the current executable. Package-manager-owned or non-writable executables should be updated through their original installation method.

## Build

```sh
go build -o aegis ./cmd/aegis
```

Go 1.26.5 or newer is required. The application uses Cobra, isolated Viper instances, Echo v5, context cancellation, and injected `log/slog` loggers. Development builds and the Hermes adapter report `dev`; tagged release builds inject and share the tag version, and versioned `go install` builds recover the stable module version from embedded Go build information.

## Configure

Copy `examples/aegis.yaml`, replace the principal UID and username placeholders with the designated operator's local account, and make the matching replacement in `examples/office-charter.json`. The binding is mandatory. Precedence is:

1. CLI flags
2. `AEGIS_*` environment variables
3. Explicit config file
4. compiled defaults

The API token is redacted by `aegis config`. It authenticates API transport; it does not replace local OS principal authentication for principal operations.

## End-to-end workflow

```sh
./aegis --config examples/aegis.yaml runtime
./aegis --config examples/aegis.yaml charter validate examples/office-charter.json

# Structured Hermes gateway design turn, safe mode, disposable HERMES_HOME,
# no provisioning authority. The file supplies principal requirements.
./aegis --config examples/aegis.yaml design --draft examples/office-charter.json

# Non-interactive minimal design turn; without configured provider credentials
# this reaches the provider boundary and fails honestly.
./aegis --config examples/aegis.yaml design --smoke

./aegis --config examples/aegis.yaml charter import examples/office-charter.json
./aegis --config examples/aegis.yaml plan preview office --revision 1
./aegis --config examples/aegis.yaml approval request PLAN_ID --ttl 5m
./aegis --config examples/aegis.yaml approval approve APPROVAL_ID
./aegis --config examples/aegis.yaml provision PLAN_ID APPROVAL_ID
./aegis --config examples/aegis.yaml session preview office --revision 1 --stanza principal
./aegis --config examples/aegis.yaml session start MANDATE_ID
./aegis --config examples/aegis.yaml session show SESSION_ID
./aegis --config examples/aegis.yaml session revoke SESSION_ID --reason operator_request
./aegis --config examples/aegis.yaml audit verify
```

All command results are JSON on stdout. Diagnostics and the explicit design-mode warning go to stderr. A stanza flag is only a requested stanza and never identity evidence.

## Charter representation

Charters are strict JSON. Unknown fields and trailing data are rejected. Approval binds SHA-256 over deterministic Go JSON serialization of the typed charter and complete typed provisioning plan; stored plan digests are recomputed before use. Review explicitly shows charter/plan digests, runtime details, complete effects, per-stanza toolsets, memory and credential scopes, approval semantics, warnings, and the full previous-revision diff. Revisions are immutable on disk. Duplicate stanza IDs, implicit wildcard identity selectors, identical enabled selectors, wildcard authority/scope, delegation, cross-stanza flow, unsupported runtime extensions, and mismatched Hermes tool grants/toolsets are rejected.

In the MVP, a charter `grant.tools` entry is a Hermes toolset ID because toolsets are the hard runtime-registration boundary Hermes exposes. `grant.tools` and `hermes.toolsets` must match exactly. MCP and plugin provisioning is rejected. Credential scope currently supports exactly the selected model-provider credential (`provider:<provider>`); operational launches still pass only that provider's configured environment binding.

## Encrypted credential authority

Aegis now includes the local administrative foundation for storing reusable secrets without persisting plaintext values. The optional `credentials.authority` configuration selects one deployment-bound bbolt database and either an encrypted systemd service credential or an explicitly weaker host-file KEK. Each immutable secret version is encrypted with a fresh DEK and XChaCha20-Poly1305 nonce; the DEK is independently wrapped by a versioned KEK. The database is mode `0600`, uses fixed schema buckets and a finite writer-lock timeout, performs startup schema/structural/key checks, and supports exact bindings, rotation, logical revocation, and consistent ciphertext backups.

Principal-only administration is available through `aegis secret initialize|put|metadata|rotate|bind|revoke|backup`. `put` and `rotate` default to confirmed no-echo terminal intake; `--stdin` reads exact bytes from a protected pipe. Values are never accepted in argv or returned by the CLI. `metadata` does not decrypt. Host-file initialization is intended for development and is explicitly reported as weaker; production service configuration should use `LoadCredentialEncrypted` and `custody: systemd`. Keep KEK/recovery material separate from database backups.

This is not yet the runtime credential broker or fleet projection system. Existing Hermes launches continue to use configured environment-backed provider bindings, and the new authority does not make stored credentials available to Hermes. The local broker, session capabilities, downstream credential application, systemd unit, TPM/recovery workflow, signed selective projections, and Infisical migration remain unimplemented acceptance gates. bbolt supplies persistence only; Aegis supplies encryption and policy, and neither protects values from fully compromised root while they are in use.

## Runtime behavior and limits

- Supported adapter range: Hermes Agent `>=0.18.0,<0.19.0`; installed version constraints in each charter are enforced.
- Design uses real `hermes --safe-mode --tui --toolsets no_mcp`, never `-z`/one-shot.
- Every operational session gets a new process and disposable `HERMES_HOME`.
- Safe mode disables inherited user config, project rules, memories, plugins, and MCP.
- Aegis resolves and records the exact Hermes toolset IDs placed in the launched process arguments and fails closed if those arguments differ from the approved mandate. Hermes 0.18.x does not expose a stable post-launch API for enumerating the fully registered individual-tool surface, so `toolset_verification: launch_arguments` must not be interpreted as individual-tool runtime attestation.
- Revocation updates the mandate and terminates the recorded runtime PID.
- Hermes-home/process isolation is not a host filesystem, network, container, or VM sandbox. A charter granting `terminal` or `file` intentionally grants broad host-facing authority and must be reviewed accordingly.
- Persistent named Hermes profiles, gateways, services, cron, arbitrary plugins/MCP, and external provisioning actions are denied by the MVP provisioner.
- Provisioning creates only new deterministic Aegis-owned mapping files atomically. Durable in-progress receipts are recovered on the next command: matching owned artifacts are removed and the receipt is finalized as failed; non-matching artifacts are preserved for manual review.
- Operational provider authentication must be named by the stanza as `provider:<provider>`, configured under `credentials.provider_auth.<provider>`, and present in its configured source environment variable. Aegis injects only that resolved binding into Hermes; ambient provider keys are not inherited. Design receives no provider credential unless `credentials.design_provider` explicitly selects a configured provider binding. Credential values are not persisted or included in command output, receipts, audit events, errors, or model prompts.
- The optional bbolt credential authority is an administrative store and exact-binding foundation only. It is not consulted by operational Hermes launches until the separately authorized local broker is implemented.
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

Tests cover OS principal mapping, prompt/stanza non-authentication, zero and ambiguous selection, no authority union, exact approval mutation/expiry/replay, clean runtime homes, revocation/termination, design non-provisioning, secret redaction, audit tamper detection, strict config, credential envelope-context mutation, wrong keys, exact binding and destination denial, concurrent authority reads/rotation, backup/restore, and bounded credential codecs. `cmd/aegis/e2e_test.go` builds and exercises the real CLI against an isolated Hermes fixture without using the installed Hermes profile or external services. GitHub Actions runs formatting, build, tests, race tests, vet, and `govulncheck`.

## License

Aegis is licensed under the [Apache License 2.0](LICENSE).
