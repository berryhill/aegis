# Terminal Recording

The recording source is `scripts/demo-no-key.sh`. It uses ignored disposable paths inside the checkout, initializes an empty generation-managed Badger authority only in its disposable workspace, derives only the local UID/username, sets the copied configuration and charter to mode `0600`, prints no credential values, and removes its workspace and demonstration executable.

The retained recording is:

- `docs/assets/aegis-no-key.typescript`
- `docs/assets/aegis-no-key.timing`

Replay it with:

```sh
scriptreplay --timing docs/assets/aegis-no-key.timing docs/assets/aegis-no-key.typescript
```

It was captured from an older revision of the real script with Hermes Agent 0.18.2, replayed locally, and reviewed for usernames, home paths, temporary paths, bearer tokens, and key-shaped values. Local identity and home paths are placeholders, configuration displays `[REDACTED]`, and the expected no-provider result is not presented as model success. The current script now additionally reports `aegis version dev` and uses the enforced checkout profile path, so these retained files are historical and must be regenerated with supported Hermes before publication. They are not current launch evidence.

To record with asciinema when available:

```sh
asciinema rec --idle-time-limit 2 --command './scripts/demo-no-key.sh' aegis-no-key.cast
```

Before publishing any recording, replay it, inspect it for usernames, hostnames, paths, tokens, API keys, and provider output, and regenerate it whenever CLI output or commands change. Do not edit a recording to imply a successful provider-backed turn.

The retained no-key recording does not enter the interactive manager, open a credential authority, or invoke pinentry. It therefore does not demonstrate the Core 15 slash surface or protected authority prompt. It is currently stale because the recording source and no-key CLI output changed. A future authority recording must use a fake pinentry and generated canary, never a real passphrase or operator helper. Any future manager recording must capture both rich and `AEGIS_ACCESSIBLE=1` paths, show the permanent untrusted-model origin label and trust surface, and use only fake local runtime fixtures unless an already-installed live artifact is separately authorized.

The opt-in [operator acceptance POC](OPERATOR_ACCEPTANCE_POC.md) is separate from this publishable terminal recording. It retains bounded sanitized JSONL evidence from the real accessible/plain manager PTY, never a protected value, and fails if its generated canary reaches capture or metadata. Its evidence can contain local model prose and credential metadata and must not be committed or published without manual review. A hermetic fake-PTY run proves the recorder but is not a live transcript.

The installed-MVI verifier used by CI and the tag-triggered release workflow is non-interactive and intentionally not represented by this no-key Hermes recording. Its canonical local evidence is the exit-checked output of `./scripts/verify-installed-mvi.sh`: four release-shaped archives each constrained to exactly one root `aegis` regular file with mode `0755`, verified checksums/version, fail-closed first run, an extracted-binary Registry/Loop/Graph/Queue/evidence/disposition proof with durable rejection and historical readback, and an extracted-binary console proof covering the embedded self-hosted Datastar asset plus authenticated authoritative-empty fleet rendering. It uses and removes isolated repository-local proof state even when an explicit empty archive-output directory is retained. This does not exercise a browser engine, make the historical recording current, or prove release publication.

The pre-publication release-candidate verifier is also non-interactive and does not make the historical recording current. Its retained evidence manifest, exact digests, and workspace-only rollback/withdrawal rehearsal are the relevant surfaces; do not edit or regenerate the no-key recording to imply candidate approval or publication.
