# Publish a Loop to the owning gateway

If the console shows Agents but an offline CLI lists none, do not initialize a second instance or register duplicate Agents. Select the existing gateway's configuration and console URL explicitly. Commands below use placeholders supplied by the authenticated owner; no token copying is required.

```sh
aegis loops example > loop.json
aegis --config OWNER_CONFIG --target CONSOLE_URL agents list
```

Review `loop.json`, replace `SELECT_EXISTING_ENABLED_AGENT` with the exact owner-selected enabled Agent ID and choose a fresh idempotency key. Then, after explicit publication authorization:

```sh
aegis --config OWNER_CONFIG --target CONSOLE_URL loops validate loop.json
aegis --config OWNER_CONFIG --target CONSOLE_URL loops publish loop.json
aegis --config OWNER_CONFIG --target CONSOLE_URL loops show basic-implementation 1
```

Match returned immutable revision and validation digests and publisher/workspace provenance; lifecycle must remain draft. Validation does not persist a Loop or promise publication: publish repeats admission, ownership, predecessor and idempotency checks. The target origin must match both selected and running configuration. Transport uses the configured protected Unix socket and existing peer plus bearer authentication, never the console HTTP URL. Unavailable/mismatched/denied service fails closed without local store fallback. State/runtime/pinentry overrides and the update flag are rejected online. Online activation, queue, run and all other operations are unsupported.

## Authoring support, not execution guarantee

The `basic-implementation` example is deliberately only a structurally valid publication template. The current Step schema has no executable instruction or command binding. Its required string ports do not impose bounded task text or acceptance criteria values. Its single action-to-success edge does not implement focused verification. `max_attempts: 2` does not implement one corrective attempt after failed verification, and empty evidence claims/requirements cannot prove success.

Required evidence can pin a media type, expected content digest, verifier ID and policy version, but those require a real independently established contract. Inventing a digest or policy to make a generic code-task example appear verified would fabricate evidence. No such claims are supplied. Implementing smallest-change instructions, focused checks, success only after checks, one corrective pass and failed exhaustion needs an authorized executable binding/worker contract beyond this publication adapter. Do not activate or run this template as if that contract existed.

Tests exercise synthetic disposable state, installed CLI, Unix authentication, canonical validation/publication and exact inactive readback. They do not prove live owner publication, runtime/model execution or dynamic code verification.
