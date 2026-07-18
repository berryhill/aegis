# Credential Authority Setup

Credential-authority setup is a principal operation separate from manager-model onboarding and certification. Aegis does not ask a model to configure or initialize it.

## Development host-file path

Choose deployment-specific absolute paths below the configured Aegis state directory. Add this block under `credentials` in the existing mode-`0600` Aegis configuration:

```yaml
authority:
  database: /ABSOLUTE/AEGIS/STATE/authority.db
  deployment_id: REPLACE_WITH_STABLE_DEPLOYMENT_ID
  custody: host-file
  kek_file: /ABSOLUTE/AEGIS/STATE/custody/authority-kek.json
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

Production should use `custody: systemd`, a basename-only `kek_credential`, and an encrypted systemd service credential delivered through `CREDENTIALS_DIRECTORY`. The interactive `secret initialize` command deliberately does not create systemd credentials. The deployment administrator must create and provision that encrypted credential and service unit outside Aegis, then start Aegis with the configured credential available. Aegis does not report manager credential administration as ready unless the configured database and delivered credential are both present and pass authority startup validation.

Keep KEK/recovery material separate from database backups. Disable core dumps and use distinct production service/runtime identities where required by the threat model. See `research/2026-07-17-embedded-bbolt-credential-authority.md` for the normative production custody and recovery design.
