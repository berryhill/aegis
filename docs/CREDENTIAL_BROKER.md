# Local session credential broker

Aegis can optionally run a Linux pathname-Unix-socket broker alongside `aegis serve`. The only implemented operation is the typed, read-only `github.get_repository.v1` action:

```text
POST /v1/broker/actions/github-get-repository
{"schema_version":1,"request_id":"<fresh 128-bit lowercase hex>","deadline":"<RFC3339 deadline>","capability":"<session capability>","owner":"approved-owner","repository":"approved-repository"}
```

This is not a generic HTTP proxy or secret-reading API. Unknown fields are rejected. The caller cannot name a URL, method, header, credential scope, secret record, binding version, agent, stanza, deployment, destination, profile, or prompt. Aegis derives the fixed `github/read` scope and `github-api` destination and reauthorizes the complete tuple from current state for every call.

## Authorization boundary

A request is allowed only when all of these remain true:

- Linux `SO_PEERCRED` matches the configured bridge UID and GID before HTTP reads the body;
- the 256-bit capability digest resolves to one in-memory capability and its raw value matches no persisted session state;
- the caller supplies a fresh 128-bit request ID and a deadline no later than the capability and per-request timeout; duplicate IDs and stale requests are denied;
- the exact session is running and its authenticated subject, mandate, charter revision/digest, agent, stanza, local environment, deployment, runtime PID/start token, issue time, and expiry still match;
- the current mandate still matches the current canonical charter and grants `github.get_repository.v1` plus `github/read`;
- exactly one active `brokered` binding exists for agent + stanza + deployment + `github/read`;
- that binding permits only `github-api`, and its current or pinned record version remains active;
- owner and repository are conservative single path segments.

Zero/ambiguous resolution, termination, failure, expiry, replay, request-budget exhaustion, mandate revocation, PID reuse/loss, binding disablement, rotation-policy mismatch, or record/version revocation denies. A capability accepts at most 4096 distinct request IDs, bounding its in-memory replay cache. Capability state is process-local and is invalid after broker restart.

The broker stores only SHA-256 capability digests in memory. The raw capability and socket pathname are written mode 0600 to `aegis-broker-capability.json` in the fresh disposable session home after the runtime PID is known. They are not placed in argv, environment, the charter, mandate, session JSON, audit, logs, or model context. Session cleanup removes the file and capability. The disposable file is session authentication material, not a reusable downstream credential.

## Socket deployment

The socket is never abstract. Its pre-existing parent directory must be owned by the Aegis service process, must not be writable by group/other, and must not be owned by the configured runtime UID. Aegis rejects symlinks and unsafe stale paths, creates the socket mode 0660, and verifies socket type/owner/mode after creation. Production therefore needs distinct Aegis service and Hermes bridge identities. A same-user production mode is intentionally rejected.

Example authority fragment (IDs and custody paths are deployment-specific):

```yaml
credentials:
  authority:
    database: /var/lib/aegis/credentials.db
    deployment_id: local-production
    custody: systemd-credential
    kek_credential: /run/credentials/aegis.service/aegis-credential-kek
    broker:
      socket: /run/aegis/credential-broker.sock
      allowed_uid: 991
      allowed_gid: 991
      capability_ttl: 2m
      max_body_bytes: 65536
      timeout: 10s
      destinations:
        github-api:
          url: https://api.github.com
          repositories:
            - approved-owner/approved-repository
```

`github-api` is the only accepted destination identifier, and at least one exact `owner/repository` entry is required. Redirects and proxy-environment use are disabled. The broker always constructs `GET /repos/{owner}/{repository}` for an exact configured repository, applies the `Authorization` header internally, bounds the response to 64 KiB, requires a successful JSON response, and returns only owner, name, private, default branch, archived, visibility, and update time. Downstream headers, error bodies, URLs, permissions, and credential material are never returned.

## Current integration boundary

Aegis materializes session authentication for a future Aegis-owned bridge, but Hermes 0.18.x safe mode does not currently provide a verified way to register exactly that bridge while keeping ambient MCP/plugins disabled. The broker is therefore not advertised as a model-visible Hermes tool. Do not expose it through terminal/curl or inherited MCP configuration. A separately pinned bridge and exact Hermes tool-registration verification remain a deployment blocker.

Environment-backed provider authentication remains unchanged. This GitHub operation does not replace model-provider credentials and Aegis does not claim that all runtime credentials are brokered.

The broker also does not provide fleet projection, Tailscale enrollment, TPM recovery, systemd unit provisioning, Infisical migration, network confinement, or protection from root/kernel compromise.
