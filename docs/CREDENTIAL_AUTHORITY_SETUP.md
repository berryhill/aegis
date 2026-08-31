# Credential Authority Setup

Credential-authority setup is a principal operation separate from manager-model onboarding and certification. Aegis does not ask a model to configure or initialize it.

For a bare local installation, onboarding defaults to an owner-only host KEK at `~/.aegis/state/credentials/authority.kek` and a database at `~/.aegis/state/credentials/authority.db`. The displayed decision states the trade-off: mode `0600` prevents other host accounts from reading the key, but the configured principal and same-account processes can. This is the built-in custody route qualified for automatic rootless user-service installation because the separately started gateway can reopen it without receiving a secret through argv, environment, or an unprotected pipe.

Advanced passphrase custody protects a copied KEK envelope against offline disclosure without its passphrase. After the exact authority plan and separate confirmation, Aegis uses protected pinentry or no-echo terminal fallback, Argon2id, and XChaCha20-Poly1305. The passphrase is never persisted and must unlock authority in each process. Because bootstrap cannot securely transfer that process-local unlock into a separate rootless systemd service, automatic gateway installation denies passphrase custody; run `aegis serve` interactively or provide an externally integrated service-custody design instead.

## Protected prompt selection and troubleshooting

Aegis resolves one helper deterministically: an explicit `--pinentry-executable /ABSOLUTE/PATH` wins and must name an executable regular file; otherwise Aegis resolves conventional `pinentry` on `PATH`. It executes that file directly, never through a shell, GPG, or `gpg-agent`, and does not read or edit `gpg-agent.conf`. No passphrase, authority path, key, or credential is placed in argv or the child environment. The child receives only the bounded locale, home/path, terminal, display, Wayland/Xauthority/runtime-directory, and desktop-session-bus values needed by common pinentry implementations; provider keys, application tokens, proxy variables, and Hermes variables are not inherited.

Pinentry must be usable in the current desktop session (`DISPLAY` or `WAYLAND_DISPLAY`, and often `DBUS_SESSION_BUS_ADDRESS`/`XDG_RUNTIME_DIR`). If discovery or initialization fails before `GETPIN`, Aegis may use the no-echo fallback only when stdin and diagnostic stderr are real terminals. Pinentry cancellation is final. A crash, malformed response, timeout, or other failure after `GETPIN` fails closed instead of unexpectedly asking in the terminal. If neither mechanism is available, run from an appropriate desktop session or real terminal; never put the passphrase in argv, environment, chat, or an unprotected pipe.

Every passphrase-file authority open uses this shared path, including onboarding resumption, principal-only `aegis secret` administration, manager authority startup, and `aegis serve` when its configured broker needs the authority. A wrong passphrase retries in a fresh protected request at most three times. Missing, malformed, insecurely permissioned, unsupported, deployment-drifted, or structurally invalid artifacts do not retry. A successful unlock is process-local and lasts only as long as the existing custodian/authority lifecycle; Aegis adds no daemon, cache file, keyring record, GPG secret, or passphrase verifier.

Pinentry changes the protected input/display route only. It is not a desktop keyring, GPG-agent cache, hardware-backed store, sandbox, or recovery mechanism. Same-account malware, a compromised desktop session or helper executable, root, the kernel, process-memory inspection, and Go/runtime copies remain residual risks. Headless services must use the gateway-ready host-file route or an externally integrated systemd credential unit rather than pretending GUI pinentry is available.

Systemd custody remains available only for an actual service deployment that supplies `CREDENTIALS_DIRECTORY` and integrates the credential into its own unit. The built-in automatic user-service generator rejects this custody mode because it has no authoritative encrypted-credential source path to bind. Bare onboarding does not pretend that an ordinary shell can deliver a systemd credential.

The owner-only host-file mode is the default gateway-compatible local route. It is weaker against compromise of the configured host account than passphrase or externally delivered systemd custody. Aegis resolves the local home before filesystem use and never stores a tilde path.

## Gateway-compatible host-file path

Choose deployment-specific absolute paths below the configured Aegis state directory. Add this block under `credentials` in the existing mode-`0600` Aegis configuration:

```yaml
authority:
  database: /ABSOLUTE/AEGIS/STATE/credentials/authority.db
  deployment_id: REPLACE_WITH_STABLE_DEPLOYMENT_ID
  custody: host-file
  kek_file: /ABSOLUTE/AEGIS/STATE/credentials/authority.kek
```

The configuration must remain owned by the configured principal with mode `0600`. Parent directories must be owned by that principal and must not be writable by group or others. The database and KEK paths must not be symlinks. The host-file KEK is the gateway-compatible local default but remains weaker against same-account compromise: never store or back it up with `authority.db`.

Validate the complete configuration before any creation:

```sh
aegis --config /ABSOLUTE/PATH/aegis.yaml config
```

Then run the authenticated initializer:

```sh
aegis --config /ABSOLUTE/PATH/aegis.yaml secret initialize
```

Aegis prints the database path, deployment identity, custody mode, redacted KEK source, required ownership/modes, startup checks, and custody warning. It creates or opens the authority only after the operator types the literal confirmation `yes`. Decline, EOF, or cancellation performs no authority initialization. A successful startup check opens the mode-`0600` database, validates schema and structure, loads the mode-`0600` external KEK, and verifies the deployment-bound encrypted sentinel.

Verify metadata-only readiness with:

```sh
aegis --config /ABSOLUTE/PATH/aegis.yaml secret list
```

## Production systemd custody

Production should use `custody: systemd`, a basename-only `kek_credential`, and an encrypted systemd service credential delivered through `CREDENTIALS_DIRECTORY`. The bootstrap records the exact deployment/database/credential names only after its digest-bound confirmation, then remains at a resumable incomplete prerequisite; absence of externally delivered material is not corruption and is not reported as a systemd authority selection when custody is empty. The interactive `secret initialize` command deliberately does not create systemd credentials. The deployment administrator must create and provision that encrypted credential and service unit outside Aegis, then rerun `aegis init` with the configured credential available. Aegis displays the exact database effect and requires `initialize systemd authority DEPLOYMENT_ID` before creating and validating the deployment-bound database. It never copies or modifies the delivered KEK and does not report manager credential administration as ready unless the database and credential both pass authority startup validation.

Keep KEK/recovery material separate from database backups. Disable core dumps and use distinct production service/runtime identities where required by the threat model. See `research/2026-07-17-embedded-bbolt-credential-authority.md` for the normative production custody and recovery design.

## Browser credentials surface

The authenticated Aegis console renders a dedicated `#/credentials` workspace as native same-origin HTML. It performs authoritative repository-side search and active/revoked filtering, reports matching and vault-wide counts, pages deterministically, and resolves exact deep links even when the target is beyond the first 100 records. Cards and inspectors remain metadata-only: record provenance, status, versions, algorithms, KEK version identifiers, verification digests, binding counts, vault metadata, and backup status may be shown; secret values, ciphertext, wrapped DEKs, nonces, and KEK bytes are never projected.

The browser supports reviewed create, rotate, revoke, exact-binding, and policy-selected ciphertext-backup operations. Strict bounded forms accept no caller-supplied principal, authority context, or backup path. Review requires an authenticated principal session plus exact Host/Origin/CSRF admission, validates the exact target, and retains the bounded operation payload only behind a short-lived, one-use receipt bound to that session and the credential-operation purpose. Confirm consumes the receipt, decodes it strictly, reloads the exact authoritative target and version, and returns metadata-only readback. Cancel is also an authenticated same-origin CSRF-protected POST: it consumes the exact receipt and immediately wipes the retained payload. Cross-session, wrong-purpose, malformed, expired, replayed, stale-target, version-drifted, and missing-CSRF requests deny without mutation.

The separately protected local Unix API exposes the same typed principal-only create, rotate, revoke, bind, and backup application operations under `/v1/credentials`. Create and rotate use strict one-use request intake capped at 1 MiB and return metadata only. The backup request must be exactly an empty JSON object; a caller cannot select a host path. Browser and API operations do not add initialize, unlock, reveal, restore, KEK administration, generic secret reads, or arbitrary-path backups.

The credential surface is intentionally non-gating for the rest of the
fleet-control vertical: a missing, locked (`ErrPassphraseAuthentication`),
corrupt, or unavailable authority surfaces an `unconfigured` / `locked` /
`degraded_repair_required` / `unavailable` readiness state for credentials
without changing registry, loops, graphs, or queue readiness. The
`/v1/fleet/readiness` aggregate returns `ready` for non-credential failures
only.
