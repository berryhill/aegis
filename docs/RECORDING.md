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

The retained no-key recording does not enter the interactive manager, open a credential authority, or invoke pinentry. It therefore does not demonstrate the Core 15 slash surface or protected authority prompt; it was reviewed as unaffected because the recording source and no-key CLI output did not change. A future authority recording must use a fake pinentry and generated canary, never a real passphrase or operator helper. Any future manager recording must capture both rich and `AEGIS_ACCESSIBLE=1` paths, show the permanent untrusted-model origin label and trust surface, and use only fake local runtime fixtures unless an already-installed live artifact is separately authorized.
