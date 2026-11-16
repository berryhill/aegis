# Native-profile provider reuse: partial source result

## Status

**The requested registration-to-provider-reuse feature is not complete.** This
candidate fixes direct Loop session bootstrap lane selection and adds explicit
registration model snapshots. The [resumed connection composition](native-profile-provider-connection.md) now
adds independent native-account review and exact successor approval. Antigravity
inference remains unimplemented, and Codex live qualification remains blocked.
No installed runtime/profile, credential, daemon, main deployment or historical
Agent/charter revision is changed. No commit, publication or deployment is made.

## Explicit registration snapshot slice

The authenticated manager now supports:

```text
/agents import hermes default with-models
/agents import hermes default with-models confirm REVISION_DIGEST
```

This is explicit consent to read bounded configured model metadata, unlike the
unchanged metadata-only command without `with-models`. The preview discloses
default provider/model and all selectable lanes, `authentication: not_connected`
and `qualification: not_checked`. It performs no writes. Exact confirmation saves
the canonical charter, registers its exact Agent reference and verifies readback.
This registration command never migrates an existing metadata-only registration.
The separate reviewed `providers AGENT models` successor is now supported.

Only structured `model.provider`/`model.default` and explicit provider/model pairs
in `fallback_providers` (or the legacy single `fallback_model`) are extracted.
The latter become selectable lanes, not automatic provider fallbacks. The closed
provider set is `codex`, `openai-codex`, `antigravity-oauth`; the bounded model-ID
grammar accepts gpt-number, o1/o3/o4, codex, gemini-number and Claude
opus/sonnet/haiku family spellings. Values are not normalized. Unknown providers,
implicit/auto selection, unsafe identifiers and malformed entries fail closed;
supported metadata is not refused merely for lack of a qualified transport.
Unrelated keys are discarded. No endpoint, account, path, credential, prompt,
plugin, MCP or tool setting is included in preview or charter.

The fixed no-follow, owner-safe, single-link regular config file is bounded to
1 MiB; YAML rejects duplicate keys throughout, aliases/anchors, custom tags,
multiple documents, excessive depth/nodes and more than 16 lanes. No environment
expansion occurs. Errors contain no source values. Before/after descriptor and
directory-entry checks detect file drift. The fingerprint hashes sanitized
selections and device/inode/owner/mode/size/mtime/ctime, **not raw config bytes**.
Same-size edits with restored mtime still invalidate an unconsumed confirmation.
These checks are not CAS against a malicious same-UID writer or a host sandbox.

The charter uses `provider_authentication.mode: configured-provider.v1` as an
explicit **non-executable selection marker**, not a usable authentication claim.
Both application and runtime provider resolvers reject this mode before reading
credentials or qualifying transport. It grants zero Agent credentials/tools.
Existing Loop step and Graph node admission resolve against these immutable
approved selections. Later profile changes never feed that authority. Consumed
confirmation and bootstrap replay reconstruct the exact stored initial charter;
supported lifecycle/successor history remains intact and no profile is reopened.

Synthetic tests exercise real application registration and manager slash dispatch,
exact persisted readback, per-step/per-node publication/default/override/denial,
profile drift, historical no-migration, orphan recovery, lifecycle replay, unsafe
files/configuration and credential-resolution denial. They do not execute real
provider inference or establish complete Queue Run/Graph output acceptance.

## Preserved session bootstrap boundary

`prepareQueuedLoop` selects a Hermes action's provider/model from the exact Loop
and passes it through the shared session service to `Adapter.LaunchLane`.
Both layers resolve the pair against the original immutable mandate. The selected
lane controls provider authentication, disposable runtime preparation and the
local-inference route; an unused default Codex login is not resolved. All required
step lanes still pass readiness and every actual attempt independently repeats
lane resolution and fresh authority/provider admission.

Explicit bootstrap overrides are bounded to credential-free, tool-free authority.
They repeat admission before preparation and after gateway probing, use the
headless empty-toolset gateway, and remain caller/deadline-bound. Local inference
retains its existing process-owned transport and guards. Ordinary session starts
keep the historical default path, including its legacy credential semantics.

Session `launch_provider` and `launch_model` retain explicit resolved bootstrap
provenance. They are observational metadata, not authority or subsequent attempt
provenance. Historical absent fields remain absent. Charter, mandate, full
allowed-lane authority and import confirmation digests are not rewritten.

Tests cover default authentication still failing when absent, successful explicit
credential-free bootstrap with a Codex default, exact persisted session/mandate
readback, unapproved lanes, foreign subjects, expiry, selected Codex authentication,
qualification denial, runtime drift, tool/credential denial, fresh revocation after
probe, process deadline/cancellation and disposable-home removal. Gateway and
tokens are synthetic. The local-lane negative asserts its own fresh-admission gate
before any listener/process, not a successful Ollama inference.

## Inspected runtime evidence and unresolved adapter boundary

Installed Hermes source was observed at
`acdc8f1ba65c0231ae8277e846a4d55928913991`; stable-tag Git description was
`v0.23.0-1339-gacdc8f1ba6`. The isolated public launcher `hermes --version` refused
with `no dependency environment is committed for this install`; this was not
repaired by borrowing the native profile. The diagnostic namespace omits PM
per-install dependency state, including the owning-install selection supported
by `pm/environments.py`; its failure does not prove the installed launcher broken.
The source identity is not proof of a successful isolated launcher or current
Aegis provider qualification.

The existing actual-Hermes synthetic Codex pool probe was executed through its
installed `venv/bin/python3`, with disposable HOME/HERMES_HOME and network denied.
It passed access-only pool selection/resolution, rejected a token inside the
120-second refresh margin, and rejected an unknown pool source. **Those parser
checks do not qualify the whole transport.** Aegis's 0.21.3/five-source gate remains
unchanged; no version/hash-only promotion was made.

The resumed [native full-gateway diagnostic](native-codex-gateway-boundary.md)
now exercises real AIAgent and SDK HTTP/SSE behavior in a networkless namespace.
It reproduces truncated-stream false success and auxiliary-route/tool-boundary
failures. Its opt-in terminal-stream regression is red; the existing runtime
qualification gate remains unchanged. This is a concrete independently owned
Hermes prerequisite, not completed registration/provider reuse.

Installed `agy --version` under a clean scratch environment reported `1.5.8`.
Its `--help` advertises `--settings` (session-only JSON/file), `--mode`, `--sandbox`,
stream-json input/output, `--disable-slash-commands` and `--print-timeout`.
These controls must not be treated as evidence of a verified zero-tool runtime.
The upstream [permission documentation](https://antigravity.google/docs/permissions/)
describes explicit deny rules; it does not make the currently installed Hermes
plugin automatically use or attest them.

Read-only installed plugin source inspection found:

- `__init__.py` registers `antigravity-oauth` as an external-process provider;
  it is not a built-in Aegis/Hermes controller access-token transport.
- `client.py:269–291,512–543` creates its child home and starts a worker without
  a controller-owned settings/deny policy by default.
- `process.py:506–561` links the selected native token into that home, with
  hardlink/copy fallback; it does not export an access-only account-pinned grant.
- `process.py:599–608` copies the parent environment.
- `stream.py:780–808` reacts to native tool `step_update` by terminating the
  worker. Receiving a stream event is not a pre-effect authorization handshake.

The retained probe `internal/runtime/hermes/testdata/antigravity_boundary_probe.py`
loads the **actual plugin**, uses only a synthetic token in a new private directory,
and drives a real synthetic child process. It explicitly forbids network, real
credential/config reads and subprocesses other than that child. Observed result:
`native_token_linked=true`, `ambient_inherited=true`, `settings_present=false`,
`effect_preceded_stream_neutralization=true`, `qualification=not_qualified`.
This proves that plugin event termination alone cannot prevent a child effect;
it is **not** a claim that real agy ignored an explicit deny rule, nor a live
provider test. A safe independent adapter might be possible, but has not been
implemented or qualified here. Do not enable ambient plugins or copy profiles to
fill that gap.

Observed source/binary SHA-256 identities:

| Input | SHA-256 |
|---|---|
| Plugin `client.py` | `980d60dee224bdc366a3131f96f5286de37d6345ee2c8a55c92086580053ac77` |
| Plugin `process.py` | `c0c402e6fd876f9a964624c6358b87ec5fdcc8abb46bf5d4f923f7572890fa20` |
| Plugin `stream.py` | `90358d5e531215731567a15f98d3804286ed9a9607e0446f9f50bd5b47df6b78` |
| agy executable | `9db59b6371107b939befa9c507c0a24989fda974e75d81d8529f3689ef7322e5` |

## Reproduction

Use the Makefile's `GOTOOLCHAIN=go1.26.10`, `GOMAXPROCS=2`, `GOFLAGS=-p=1`,
`GOMEMLIMIT=512MiB` and task-owned TMPDIR. No test here starts an Aegis listener.

```sh
go test ./internal/app ./internal/slash -run 'TestLocalHermesModelImport' \
  -count=1 -timeout=60s
go test -race ./internal/app ./internal/slash \
  -run 'TestLocalHermesModelImport(SnapshotAndImmutableUse|UnconsumedDrift|ExplicitReviewAndReplay|RejectsUnsafeConfiguration)$' \
  -count=1 -timeout=60s
go test ./internal/runtime/hermes ./internal/core -run 'TestConfiguredProvider' \
  -count=1 -timeout=60s
go test ./internal/runtime/hermes ./internal/app \
  -run 'Test(SessionLane|SessionLocalLane|CredentialFreeSessionLane|QueuedSession|QueuedLoopSession|ProviderAuthentication|ProviderLaunch|ProviderTransport|ProviderToken|LegacyLaunch|AttemptTurnResolvesCharterLaneEnvelope|LocalInferenceCannotFallThroughGenericRuntime)' \
  -count=1 -timeout=60s
# Repeat the same selection with -race.
go test ./internal/core ./internal/architecture -count=1 -timeout=60s
```

The actual-plugin diagnostic is deliberately opt-in. Supply an explicitly reviewed
plugin source and a **new, nonexistent** directory under private task scratch:

```sh
/absolute/installed/python -B \
  internal/runtime/hermes/testdata/antigravity_boundary_probe.py \
  /absolute/reviewed/antigravity-oauth /absolute/private/task/new-probe
```

The earlier lane-only candidate's normal/race focused session/provider tests and
full core/architecture packages passed. Broader focused app checks covered step-lane publication/host-review,
local import and session preparation history; focused `go vet` passed. The
original nested linked-worktree build and no-key demo unit suite were blocked by
`vcs expected git, got missing`. A private standalone Git snapshot under task
scratch, with the same HEAD and exact candidate files overlaid, passed the
provenance-verified build and both no-key demo tests. No commit or shared Git
configuration change was used. The demo uses a synthetic Hermes executable and
claims no inference. Exact source/binary identities are in the candidate handoff,
not a claim of clean committed release readiness.

## Existing connection-composition seam

The [resumed source implementation](native-profile-provider-connection.md) now
composes this seam through `/agents providers` and owning-service CLI/API. The
following identifies the reused boundaries, not an outstanding parser task.

`internal/app/controller_codex_connection.go` already owns
`InspectControllerCodexCandidatesAs` → `PreviewControllerCodexAs` →
`ApplyControllerCodexAs` → `ReadControllerCodexAs`. The registration follow-up
composes that independent source/account/entry review without treating the
registration digest as connection consent. `ApproveAgentCharterAs` in
`internal/app/agent_charter_successor.go` is the existing exact successor boundary:
only separately approved Codex lanes may move from `configured-provider.v1` to
`controller-codex-access-token.v1`. Preserve untouched lanes, old revisions and
zero Agent credential scope. `resolveControllerProviderAuthentication` then
consumes the exact operational binding and the runtime resolver separately
qualifies transport. Antigravity has no corresponding qualified connection/adapter
in this slice; do not relabel its selection marker or enable ambient plugins.

## Remaining feature work and acceptance

1. Registration model/lane snapshot is implemented and covered by synthetic
   service/slash regressions; independent candidate acceptance remains separate.
2. File-free independent source/account connection and exact successor composition
   is implemented in the resumed slice. Synthetic tests do not establish live
   native qualification or actual operational-account consent.
3. Leave Antigravity honestly unsupported unless separately authorized research
   establishes a supported bounded mechanism. Existing plugin evidence does not
   establish pre-effect tool denial or safe access-only custody; it also does not
   establish blanket technical impossibility. No consumer-token experiment,
   ambient-plugin enablement or paid Gemini substitute is part of this slice.
4. Resolve the reproduced native Hermes stream/launch prerequisites and qualify
   actual supported transports, not just pool parsing, version ranges or hashes.
5. Independently exercise fresh registration, default model, per-step/per-node
   overrides, drift/replay/denial and real Loop/Graph output/evidence journeys.

Full executable registration/provider reuse: **BLOCKED**. Snapshot and file-free
connection/successor composition are source-implemented; see the [resumed slice](native-profile-provider-connection.md). Antigravity Aegis adapter:
**UNAVAILABLE**. Real Codex/Antigravity inference, real Ollama execution, complete
Queue Run/Graph integration, installed-target acceptance and fresh independent
candidate testing: **NOT EXERCISED**. Existing auth/host/runtime gates remain gates.

## Launch-asset impact review

| Asset | Review/result |
|---|---|
| Root README | Updated with narrow lane fix, opt-in snapshot and explicit non-completion link. |
| LICENSE | Apache-2.0 reviewed; no dependency or licensing change. |
| SECURITY | Updated exact-lane, immutable snapshot, credential denial and Antigravity limits. |
| CONTRIBUTING | Existing synthetic/live and low-concurrency requirements retained. |
| CODE_OF_CONDUCT | Unchanged; its missing owner-designated private reporting route remains a launch blocker. |
| CHANGELOG | Unreleased partial correction only; no provider-reuse completion claim. |
| Threat model | Added explicit bounded config-read, drift and configuration-only boundary; no new trust principal. |
| Architecture/diagrams | Documented additive snapshot path; existing service→adapter diagram remains valid, no new daemon. |
| Five-minute quickstart | Default registration unchanged; opt-in manager syntax documented in BASE_SLASH_COMMANDS, no automatic reuse claim. |
| No-key demo | Script/interface unchanged; earlier lane-only candidate's synthetic unit tests passed in its private standalone source snapshot. New snapshot build/test results belong to its separate evidence directory; no live provider demo claim. |
| Terminal recording | Historical 0.18.2 capture inspected, unchanged; not this candidate or provider proof. |
| Release binaries/checksums | Release source gates inspected; none created/published or represented as current candidate evidence. Exact release verification remains unexercised. |
| Early-contributor issues | Existing local backlog inspected; no remote issue created, no live qualification inferred. |

No installed skills or shared configuration were edited. The probe recipe is
retained here because this task's write authority is restricted to its worktree.
