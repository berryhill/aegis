# Verified implementation Loop authoring

`aegis loops implementation FILE [--output NEW_FILE]` builds a canonical Loop v3 publication draft, not the structural-only `loops example` template. It does not open authority stores, verify an Agent exists, run tests, publish, activate, or queue work. Stdout contains publication JSON; stderr identifies the exact implementation-contract digest. `--output` creates a mode-0600 file and refuses overwrite.

Build source before invoking commands; do not use `go run ./cmd/aegis` (it runs a temporary executable outside the development root, breaking source-build provenance):

```sh
./scripts/build-source.sh ./aegis
./aegis loops implementation --help
```

Create an authoring JSON file using the owner's actual enabled Agent ID and absolute workspace. This shape is illustrative; do not substitute its placeholder Agent, workspace or required test identity for real owner-selected values:

```json
{
  "agent_id": "OWNER_SELECTED_EXISTING_AGENT",
  "loop_id": "bounded-add",
  "revision": 1,
  "idempotency_key": "OWNER_SELECTED_UNIQUE_KEY",
  "implementation": {
    "schema_version": "aegis.loop.verified-implementation.v1",
    "task": "Implement addition in add.go",
    "acceptance": "The existing TestAdd passes without changing tests",
    "workspace": "/ABSOLUTE/OWNER_SELECTED/WORKSPACE",
    "writable_files": ["add.go"],
    "max_passes": 2,
    "policy": {
      "kind": "go-test.v1",
      "packages": ["."],
      "required_tests": [{"package": "REAL_MODULE_IMPORT_PATH", "name": "TestAdd"}],
      "timeout_seconds": 60
    }
  }
}
```

Task and acceptance are nonempty and bounded to 32768 bytes each. Use one or two passes, timeout 1–120 seconds, explicit local non-test Go source paths, local Go package selectors, and exact package import path/top-level test-name identities from the trusted existing tests. Test files are not writable. For later contiguous revisions include the exact `previous_digest`; the builder carries it into both predecessor fields. No shell command is accepted in this contract.

```sh
./aegis loops implementation authoring.json --output publication.json
./aegis --config OWNER_CONFIG --target CONSOLE_URL agents list
./aegis --config OWNER_CONFIG --target CONSOLE_URL loops validate publication.json
# Only after explicit authorization to publish this reviewed file:
./aegis --config OWNER_CONFIG --target CONSOLE_URL loops publish publication.json
./aegis --config OWNER_CONFIG --target CONSOLE_URL loops show bounded-add 1
```

Online validation resolves the named latest enabled registered Agent under authenticated owner authority. Match the exact revision digest on readback and require lifecycle `draft` with no lifecycle history. Publication is inactive and creates no Queue item. Online support does not include activation or Queue execution. Do not stop a daemon or fall back to opening its stores.

## Separate controller authorization and execution

Publication grants no host authority. The controller defaults to no implementation execution. After separate explicit operator approval of a configuration change, `implementation.go_binary` must name an absolute trusted Go executable and `implementation.authorized_contracts` must contain the exact contract digest returned by the draft builder. That digest binds workspace, writable files, task, acceptance, bounds and test policy. It is not the Loop revision digest. Never add it automatically, change owner configuration, restart a service, provision an Agent or execute work as part of authoring/publication.

The integrated Queue worker additionally requires normal exact Graph/Loop bindings, fresh controller runtime authority/admission and a Hermes participant with no tools or credentials. The controller admits each effect, records implementation passes, obtains bounded patch proposals through Hermes, and independently reloads the implementation evidence before terminal success. Process exit or model narration alone cannot complete the Queue item.

**Native Go tests are trusted code, not a sandbox.** Tests and imported code execute as the controller's OS user. Writable-path restrictions constrain patch application, not arbitrary effects of native code. Only approve trusted repositories/tests and a suitably isolated host account. This feature does not provide host confinement, automatic scheduling, GitHub merge, deployment or automatic service restart.

## Evidence

`go test ./internal/command -run TestImplementationDraftCLI -count=1` exercises real command construction, typed drafting, file output and refusal paths. `go test ./internal/api -run TestInstalledOnlineLoopPublication -count=1` builds and executes the CLI against a real authenticated Unix service and disposable Badger store, validates and publishes the builder's v3 draft, checks exact inactive readback and idempotency, denies invalid input and verifies no Queue item or duplicate Agent. Its Agent provenance is synthetic; it is not live owner publication or live-model acceptance.
