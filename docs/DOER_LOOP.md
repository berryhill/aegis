# Bounded Doer Loop v4

`aegis loops doer FILE [--output NEW_FILE]` builds a canonical immutable Loop revision v4 for one task. It does not publish, activate, authorize, configure Laya, queue or run the task. The existing authenticated `loops publish`, `loops activate`, Graph submission and Queue processing paths own those separate actions.

An authoring document has `agent_id`, `loop_id`, positive `revision`, optional exact `previous_digest`, `idempotency_key`, and `doer`:

```json
{
  "agent_id": "EXISTING_AGENT_ID",
  "loop_id": "selected-file-task",
  "revision": 1,
  "idempotency_key": "OWNER_SELECTED_UNIQUE_KEY",
  "doer": {
    "task": "Create result.txt containing hello",
    "workspace": "/ABSOLUTE/OPERATOR_APPROVED/WORKSPACE",
    "writable_files": ["result.txt"],
    "verify_file": "result.txt",
    "expected_text": "hello",
    "max_attempts": 3
  }
}
```

Omit `expected_text` for a presence-only regular-file check; an explicit empty string means exact empty UTF-8 text after trimming outer whitespace. The workspace must already exist and the selected file should not already satisfy the assertion. Writable paths are explicit relative paths beneath the workspace. The builder seals the task, paths, check and attempt bound in the exact Loop digest and prints the separate Doer-contract digest to stderr. Use `./scripts/build-source.sh ./aegis` from the checkout and `./aegis loops doer --help` to inspect the installed source command; do not use `go run` as a build-only substitute. A real publication requires an enabled exact Agent, fresh authority and separately reviewed activation. Authoring and publication are not runtime grants.

The controller defaults to no implementation execution. After separate operator approval, configure an absolute trusted `implementation.go_binary`, `implementation.authorized_contracts` containing the exact printed Doer-contract digest, and absolute `implementation.laya_python` plus existing private `implementation.laya_home` with a prepopulated offline Laya checkpoint. The controller does not download Laya or change the operator's Hermes profile. The participant must be registered with a tool-free, credential-free Hermes authority. A Graph pins exactly one Agent/Loop node; the Queue submission permits only one Queue attempt, while the Loop permits up to three internal Hermes proposals. The actual Hermes model/provider remain those sealed in that authority context for both implementation and diagnosis/completion; the upstream Doer skill's experimental family routing is not supported here.

The closed step sequence is eligibility (typed local Laya) → implement (bounded tool-free Hermes proposal; controller applies only allowlisted file edits) → judgment (Laya reads the report) → selected-file verification (controller reads the file independently) → either terminal assessment or bounded Hermes diagnosis and another proposal. A negative gate durably denies the Queue item as `doer_needs_input` before any claim/attempt. An already-satisfying target denies before claim so old bytes cannot count as new work. Both judgment and file check are observed on every pass; a model verdict never mints a receipt. The file verifier denies unsafe paths, symlinks, nonregular files, oversized output and mismatched text; content-addressed artifact, independently reloaded receipt, pinned contract and fresh authority are required for successful disposition. Failed or uncertain effects never become success.

Append-only digest-linked step checkpoints live in the existing fleet writer. Authenticated Queue readback exposes ordered content-free `doer_steps` metadata keyed by attempt ID; raw model text and file bytes are not copied into this response. Interrupted nonterminal cursors fail closed: an uncertain model turn or file write is never replayed automatically. The exact claimed attempt remains owned until its bounded lease expires; the existing authenticated `aegis queue expire` path then records an evidence-free expired disposition, subject to fresh authority and exact claim admission. If authority is no longer available, expiration needs separately authorized authority recovery; the model cannot force it. This is a bounded single-node Doer Loop, not a general multi-node Graph scheduler, host sandbox, distinct Sol/Luna model mandates, Jev adapter, token/cost ledger, arbitrary shell executor, automatic Queue retry, or live-model qualification. Native code/tests (if separately authorized in v3) and the local Laya subprocess remain host processes under the operator account; tool-free Hermes turns are capability-limited but not a host sandbox.

Focused synthetic proof: `GOMAXPROCS=2 go test -p 1 ./internal/loop ./internal/evidence ./internal/implementation ./internal/looprun ./internal/orchestration -run 'TestDoer|TestSelectedFile|TestStepCheckpoint|TestImplementationQueueNativeCompletion/v4-' -count=1`. It exercises fake Laya/Hermes transports and actual controller-selected file checks; live provider/model operation and an operator's deployment are separate acceptance gates.
