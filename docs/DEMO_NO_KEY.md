# No-Key Demonstration

Run from a clean checkout with Go 1.26.5+ and supported Hermes installed:

```sh
./scripts/demo-no-key.sh
```

The script creates an OS-safe temporary workspace, builds Aegis, binds a mode-`0600` copied explicit configuration and copied charter to the current UID/username, discovers the real Hermes installation, validates a strict charter, verifies configuration redaction, and invokes the real disposable design boundary. Its repository-local `.aegis` paths are explicit demonstration fixtures, not local defaults; bare local defaults are under literal `~/.argis`. It removes its workspace on exit and never points `HERMES_HOME` at the user's normal profile or touches real canonical/legacy state.

Without an explicitly configured design provider credential, the final design turn is expected to fail. The script prints the real failure and explicitly does not claim a model-generated charter. If the operator deliberately configures a provider in the copied temporary configuration and source environment, the turn may succeed and Aegis will validate the returned proposal.

This demonstration proves control-plane behavior and a real provider boundary; it does not prove host sandboxing, external audit anchoring, or individual-tool runtime attestation.

The bare manager is intentionally not recorded as a successful model conversation: no candidate artifact is downloaded or certified by this no-key workflow. Its non-TTY fail-closed dispatch and production rich/plain terminal path are covered by repository tests, including all Core 15 names through an isolated authenticated degraded manager, unknown/malformed local routing, literal `//` ingress, multiline/history/paste/help input, adversarial control-sequence sanitization, random-canary presentation absence, signals, cleanup, and terminal restoration. These tests do not claim host scanning or watching: the core scan is Aegis-native, and endpoint profiles plus `/watch` remain typed unavailable. `aegis manager model candidates` and `model route` can describe the no-default, no-download paths without changing state; discovery/configuration are intentionally omitted because they require an operator-owned local Ollama artifact.

A sanitized replayable capture and review instructions are retained in [RECORDING.md](RECORDING.md).
