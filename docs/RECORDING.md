# Terminal Recording

The recording source is `scripts/demo-no-key.sh`. It uses temporary paths, derives only the local UID/username, sets the copied configuration to mode `0600`, prints no credential values, and removes its workspace.

The retained recording is:

- `docs/assets/aegis-no-key.typescript`
- `docs/assets/aegis-no-key.timing`

Replay it with:

```sh
scriptreplay --timing docs/assets/aegis-no-key.timing docs/assets/aegis-no-key.typescript
```

It was captured from the real script with Hermes Agent 0.18.2, replayed locally, and reviewed for usernames, home paths, temporary paths, bearer tokens, and key-shaped values. Local identity and home paths are placeholders, configuration displays `[REDACTED]`, and the expected no-provider result is not presented as model success.

To record with asciinema when available:

```sh
asciinema rec --idle-time-limit 2 --command './scripts/demo-no-key.sh' aegis-no-key.cast
```

Before publishing any recording, replay it, inspect it for usernames, hostnames, paths, tokens, API keys, and provider output, and regenerate it whenever CLI output or commands change. Do not edit a recording to imply a successful provider-backed turn.
