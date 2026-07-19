# Credential Authority Setup

Credential-authority setup is a principal operation separate from manager-model onboarding and certification. Aegis does not ask a model to configure or initialize it.

For a bare local installation, onboarding defaults to a passphrase-encrypted KEK at `~/.argis/state/credentials/authority.kek.enc` and a database at `~/.argis/state/credentials/authority.db`. After the exact authority plan and separate `[Y/n]` mutation confirmation, it asks for and confirms the authority passphrase through two fresh protected pinentry requests, generates the KEK, stores only an Argon2id plus XChaCha20-Poly1305 envelope, initializes the database, verifies the deployment-bound sentinel, and continues onboarding in the same process. The 12-to-1024-byte policy is byte based. A mismatch or policy failure retries the complete pair at most three times; cancel, timeout, helper/protocol failure, or retry exhaustion mutates nothing and leaves onboarding resumable. Enter accepts the displayed plan defaults. The passphrase is never persisted and must unlock the authority again in a later process.

This local encrypted mode protects a copied credential file against offline disclosure without its passphrase. A compromised logged-in account, root, kernel, terminal, or active Aegis process can still capture the passphrase or plaintext KEK. It is not equivalent to externally delivered systemd service custody, and losing the passphrase makes the authority unavailable without a separately designed recovery mechanism.

## Protected prompt selection and troubleshooting

Aegis resolves one helper deterministically: an explicit `--pinentry-executable /ABSOLUTE/PATH` wins and must name an executable regular file; otherwise Aegis resolves conventional `pinentry` on `PATH`. It executes that file directly, never through a shell, GPG, or `gpg-agent`, and does not read or edit `gpg-agent.conf`. No passphrase, authority path, key, or credential is placed in argv or the child environment. The child receives only the bounded locale, home/path, terminal, display, Wayland/Xauthority/runtime-directory, and desktop-session-bus values needed by common pinentry implementations; provider keys, application tokens, proxy variables, and Hermes variables are not inherited.

Pinentry must be usable in the current desktop session (`DISPLAY` or `WAYLAND_DISPLAY`, and often `DBUS_SESSION_BUS_ADDRESS`/`XDG_RUNTIME_DIR`). If discovery or initialization fails before `GETPIN`, Aegis may use the no-echo fallback only when stdin and diagnostic stderr are real terminals. Pinentry cancellation is final. A crash, malformed response, timeout, or other failure after `GETPIN` fails closed instead of unexpectedly asking in the terminal. If neither mechanism is available, run from an appropriate desktop session or real terminal; never put the passphrase in argv, environment, chat, or an unprotected pipe.

Every passphrase-file authority open uses this shared path, including onboarding resumption, principal-only `aegis secret` administration, manager authority startup, and `aegis serve` when its configured broker needs the authority. A wrong passphrase retries in a fresh protected request at most three times. Missing, malformed, insecurely permissioned, unsupported, deployment-drifted, or structurally invalid artifacts do not retry. A successful unlock is process-local and lasts only as long as the existing custodian/authority lifecycle; Aegis adds no daemon, cache file, keyring record, GPG secret, or passphrase verifier.

Pinentry changes the protected input/display route only. It is not a desktop keyring, GPG-agent cache, hardware-backed store, sandbox, or recovery mechanism. Same-account malware, a compromised desktop session or helper executable, root, the kernel, process-memory inspection, and Go/runtime copies remain residual risks. Headless and systemd services should use the systemd custody mode below rather than pretending GUI pinentry is available.

Systemd custody remains available only for an actual service deployment that already supplies `CREDENTIALS_DIRECTORY`. Bare onboarding does not pretend that an ordinary shell can deliver a systemd credential. If an earlier incomplete systemd selection has no database and no delivered credential, the wizard offers a digest-bound switch to passphrase-encrypted local custody and removes the obsolete `kek_credential` setting.

The explicitly weaker plaintext host-file mode remains available for development. Aegis resolves the local home before filesystem use and never stores a tilde path.

## Development host-file path

Choose deployment-specific absolute paths below the configured Aegis state directory. Add this block under `credentials` in the existing mode-`0600` Aegis configuration:

```yaml
authority:
  database: /ABSOLUTE/AEGIS/STATE/credentials/authority.db
  deployment_id: REPLACE_WITH_STABLE_DEPLOYMENT_ID
  custody: host-file
  kek_file: /ABSOLUTE/AEGIS/STATE/credentials/authority.kek
```

The configuration must remain owned by the configured principal with mode `0600`. Parent directories must be owned by that principal and must not be writable by group or others. The database and KEK paths must not be symlinks. The host-file KEK is a weaker development fallback: never store or back it up with `authority.db`.

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
