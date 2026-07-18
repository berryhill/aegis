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
```

`aegis update` verifies the archive against the release checksum before replacing the current executable. Package-manager-owned or non-writable executables should be updated through their original installation method.

Release deployment status: the public repository currently has the `v0.1.0` tag but no published GitHub release. [Workflow run 29627074637](https://github.com/berryhill/aegis/actions/runs/29627074637) failed in the test step before building or publishing assets; the release-tag E2E defect was fixed on `main` afterward. Consequently, `aegis update --check` honestly reports that no published release is visible. A maintainer must publish a new fixed release tag (without moving `v0.1.0`) before self-update can succeed; this repository does not claim that the missing release or assets exist.

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

All command results are JSON on stdout. Diagnostics and the explicit design-mode warning go to stderr. A stanza flag is only a narrowing request and never identity evidence. `charter explain` and denied session previews retain the shared machine-readable decision on stdout; `charter effective` authenticates and authorizes the caller before returning only the selected stanza's capabilities, tools, memory and credential scopes, session/approval limits, and Hermes mapping.

## Charter representation

Charters are strict JSON. Unknown fields, trailing data, and omitted policy blocks or nested policy fields are rejected. Every selector must explicitly constrain issuer and the MVP's trusted `local` control-plane environment and anchor a subject, principal, or exact claim; unsupported authentication methods and wildcard/duplicate selector values fail validation. Approval binds SHA-256 over deterministic Go JSON serialization of the typed charter and complete typed provisioning plan; stored charter bytes/digests and plan digests are recomputed before use. Review explicitly shows charter/plan digests, runtime details, complete effects, per-stanza toolsets, memory and credential scopes, approval semantics, warnings, and the full previous-revision diff. Revisions are immutable on disk. Duplicate stanza IDs, overlapping enabled selectors, wildcard authority/scope, delegation, cross-stanza flow, persistent profiles/homes, runtime extensions, and mismatched Hermes tool grants/toolsets are rejected.

In the MVP, a charter `grant.tools` entry is a Hermes toolset ID because toolsets are the hard runtime-registration boundary Hermes exposes. `grant.tools` and `hermes.toolsets` must match exactly. MCP and plugin provisioning is rejected. Operational launches pass only the selected model-provider environment binding (`provider:<provider>`). The separately enforced `github/read` scope is accepted only with `github.get_repository.v1` and is consumed by the optional local broker; it is not injected as a Hermes environment credential.

## Encrypted credential authority

Aegis now includes the local administrative foundation for storing reusable secrets without persisting plaintext values. The optional `credentials.authority` configuration selects one deployment-bound bbolt database and either an encrypted systemd service credential or an explicitly weaker host-file KEK. Each immutable secret version is encrypted with a fresh DEK and XChaCha20-Poly1305 nonce; the DEK is independently wrapped by a versioned KEK. The database is mode `0600`, uses fixed schema buckets and a finite writer-lock timeout, performs startup schema/structural/key checks, and supports exact bindings, rotation, logical revocation, and consistent ciphertext backups.

Principal-only administration is available through `aegis secret initialize|put|metadata|rotate|bind|revoke|backup`. `put` and `rotate` default to confirmed no-echo terminal intake; `--stdin` reads exact bytes from a protected pipe. Values are never accepted in argv or returned by the CLI. `metadata` does not decrypt. Host-file initialization is intended for development and is explicitly reported as weaker; production service configuration should use `LoadCredentialEncrypted` and `custody: systemd`. Keep KEK/recovery material separate from database backups.

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

Tests cover OS principal mapping, prompt/profile/flag non-authentication, successful/zero/ambiguous/requested/disabled/stale/wrong-method/wrong-issuer/wrong-environment selection, selector-overlap rejection, no authority union, authority-relevant digest mutation, CLI/API decision parity, exact approval mutation/expiry/replay, clean runtime homes, revocation/termination, design non-provisioning, secret redaction, audit tamper detection, strict config, credential envelope-context mutation, wrong keys, exact binding and destination denial, concurrent authority reads/rotation, backup/restore, and bounded credential codecs. `cmd/aegis/e2e_test.go` builds and exercises the real CLI against an isolated Hermes fixture without using the installed Hermes profile or external services. GitHub Actions runs formatting, build, tests, race tests, vet, and `govulncheck`.

## License

Aegis is licensed under the [Apache License 2.0](LICENSE).
