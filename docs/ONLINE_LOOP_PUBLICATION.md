# Publish a Loop to the owning gateway

For real bounded code-task semantics, use [`loops implementation FILE`](VERIFIED_IMPLEMENTATION.md) instead of the structural `loops example` below. The installed publication test now proves exact v3 builder output survives authenticated validation/publication and inactive readback; it does not execute code.

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

Match returned immutable revision and validation digests and publisher/workspace provenance; lifecycle must remain draft. Validation does not persist a Loop or promise publication: publish repeats admission, ownership, predecessor and idempotency checks. The target origin must match both selected and running configuration. Transport uses the configured protected Unix socket and existing peer plus bearer authentication, never the console HTTP URL. Unavailable/mismatched/denied service fails closed without local store fallback. State/runtime/pinentry overrides and the update flag are rejected online. The online allowlist also supports Agent registration/history/charter approval/enable/disable/retire, Loop activation/retirement, Graph list/show/publication/submission and Queue list/show/runtime binding/processing/cancel/revoke/expired-lease reclaim. Graph lifecycle application and general scheduling remain unsupported. See [executable hello](EXECUTABLE_HELLO.md).

## Authoring support, not execution guarantee

The `basic-implementation` example is deliberately only a structurally valid publication template. The current Step schema has no executable instruction or command binding. Its required string ports do not impose bounded task text or acceptance criteria values. Its single action-to-success edge does not implement focused verification. `max_attempts: 2` does not implement one corrective attempt after failed verification, and empty evidence claims/requirements cannot prove success.

Required evidence can pin a media type, expected content digest, verifier ID and policy version, but those require a real independently established contract. Inventing a digest or policy to make a generic code-task example appear verified would fabricate evidence. No such claims are supplied. Implementing smallest-change instructions, focused checks, success only after checks, one corrective pass and failed exhaustion needs an authorized executable binding/worker contract beyond this publication adapter. Do not activate or run this template as if that contract existed.

Tests exercise synthetic disposable state, installed CLI, Unix authentication, canonical validation/publication and exact inactive readback. They do not prove live owner publication, runtime/model execution or dynamic code verification.

## Online lifecycle qualification

`agents enable|disable|retire AGENT FILE` forwards an exact expected revision/digest to the owning service. Read the returned successor using `agents show AGENT REVISION` and recover unknown outcomes through exact history; stale or retired mutations deny. Registration exact replay returns the original record; changed content conflicts.

`queue cancel FILE`, `queue revoke FILE`, and `queue retry FILE` use the same owning service. Retry supports only canonical expired-lease reclaim (`reclaimed: true`, `reason_code: "lease_reclaimed"`); ordinary retry remains unavailable without authoritative durable runtime-stop acknowledgment. Reclaim preserves Queue, GraphRun, LoopExecution, immutable attempt records and consumed budget; the next claim consumes the next attempt. Live leases, terminal items, exhausted budgets and wrong authority deny. Online expire/exhaust remain unsupported; do not open a second writer or construct a temporary authenticated client to bypass that limit.

`TestInstalledOnlineLifecycle` builds the candidate with the provenance-checking source script and invokes fresh CLI subprocesses against real Serve and isolated Badger stores. Setup uses existing application services and canonical abandoned-claim fixture records through the sole fixture store, with a synchronized controlled clock. It qualifies transport/lifecycle behavior, not a runtime crash, installed release package, independent tester, live provider or workstation deployment. Runtime stop semantics are unchanged.
