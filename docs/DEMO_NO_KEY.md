# No-Key Demonstration

Run from a clean checkout with Go 1.26+ and supported Hermes installed:

```sh
./scripts/demo-no-key.sh
```

The script creates an OS-safe temporary workspace, builds Aegis, binds copied examples to the current UID/username, discovers the real Hermes installation, validates a strict charter, verifies configuration redaction, and invokes the real disposable design boundary. It removes its workspace on exit and never points `HERMES_HOME` at the user's normal profile.

Without an explicitly configured design provider credential, the final design turn is expected to fail. The script prints the real failure and explicitly does not claim a model-generated charter. If the operator deliberately configures a provider in the copied temporary configuration and source environment, the turn may succeed and Aegis will validate the returned proposal.

This demonstration proves control-plane behavior and a real provider boundary; it does not prove host sandboxing, external audit anchoring, or individual-tool runtime attestation.

A sanitized replayable capture and review instructions are retained in [RECORDING.md](RECORDING.md).
