# Bounded Doer Loop v4

## Exact owner-principal Run preparation (source candidate)

For a published selected-file v4 Loop, one fresh authenticated owner-principal
`loops queue` request now prepares its **same immutable Loop** without separate
setup consent when the selected stanza has zero capabilities, tools/toolsets,
memory and credential scopes and the runtime target is `aegis-owned-ephemeral`.
Hermes builds a disposable home directly from the exact mandate; this path does
not create the persistent provisioned mapping, a plan, a generic approval or a
provisioning receipt. `required_operations: ["provision"]` is not rewritten or
waived for actual provisioning. Generic session entry still requires a verified
provisioning receipt. Readiness marks the receipt `not_required` with reason
`zero_authority_ephemeral_doer`, not falsely verified.

The exact authenticated Run is also the owner-principal's write-scope action:
the controller persists the existing signed exact host grant with truthful
`run_request_id` provenance, bounded by authentication expiry/freshness. It does
not infer a grant from publication, a draft, readiness, workspace ownership or
UID alone. The durable intent fence, exact latest enabled Registry/stable-owner
checks, immutable charter and Loop bindings, protected workspace/path checks,
provider admission, fresh per-effect host admission and independent selected-file
verification remain enforced. New authority, foreign owners, ordinary workspace
actors and persistent runtime targets do not qualify. Explicit protected setup
remains available when genuinely independent new authority is required; it is
not a mandatory detour for this zero-authority Run.

The controller does **not** install or choose Laya/model/checker dependencies.
An absent execution controller/Laya configuration returns
`doer_runtime_unconfigured` / `configure_controller_doer_runtime` inline with the
same retained request identity and no new Graph/Queue/claim. Configured helpers
still undergo the real availability and task-eligibility checks. Tests use
synthetic Hermes/Laya protocol fixtures; they do not qualify a live provider.

Implementation turns receive the exact selected-file assertion (`verify_file`, optional `expected_text`, `exact_bytes`) and retry bound alongside task prose and allowlisted paths. Raw-byte assertions do not imply a trailing newline. This is input to the tool-free implementer, not model-written acceptance evidence; the controller still independently reloads and verifies the file. Host workspace paths are not included in that turn. Publication and exact same-Loop execution remain distinct supported actions; no approval or credential authority is inferred from these inputs.

### Retained create-and-execute parity

`loops task-execute` derives the same exact Run intent from the authenticated
retained task and shares the published-Loop Run preparation/admission checks.
Genuine setup blockers leave the proposal editable and do not reserve a Run.
Once setup is eligible, the owning-store version guard freezes the exact task
before reserving intent or preparing signed host scope. Later material edits
deny; recovery keeps the task, publication key, immutable Loop and Run identity.
Publication is read back before execution. Failed history and cumulative
implementation-pass limits remain unchanged.

Execution qualification uses the bounded execution context, not the public
ten-second observational readiness probe. Public readiness, including the typed
owning-service probe, remains bounded and grants no execution authority. Expired
authority, unapproved scope, drift and unqualified runtime/provider/helper state
still deny. A successful command response is not completion: inspect the exact
Queue artifact, passed verifier receipt and succeeded disposition.

## Real-daemon protected setup negative regression

On Linux, in a checkout under the operator home with no group/other-writable executable ancestors:

```sh
go test ./internal/api -run '^TestInstalledDaemonDoerProtectedSetupNoDisplay$' -count=1 -v
```

This builds a private source-root executable, closes fixture stores before real `serve`, and requests public successor/host/provision setup through the default pinned companion without DISPLAY/WAYLAND_DISPLAY. It requires `unavailable` and independently reloads unchanged durable draft/Agent state and absence of authority grants. Fake fixture enrollment is test-only; there is no human approval or provider inference. Untrusted checkout ancestry produces an explicit prerequisite skip, not acceptance; review directory custody before hardening permissions. See `docs/implementation/offline-doer-public-success-launch-review.md` for actual execution evidence and scope.

## Controller-owned Codex transport and typed Doer proposal adapters (source candidate)

The optional charter field `hermes.provider_authentication: {"mode":"controller-codex-access-token.v1"}` separates controller-owned inference transport from Agent credential authority. It accepts only an explicit Codex provider/model with zero Agent tools, toolsets, credential scopes, plugins, MCP servers or persistent/profile home. Historical omitted fields preserve canonical encoding; existing provider credential scopes are not reclassified or removed. An exact successor, authenticated approval, provisioning receipt and fresh session admission remain independent gates.

The legacy resolver uses the explicitly configured `credentials.provider_auth` environment binding: `type: environment`, an operator-chosen `source_env`, and `target_env: AEGIS_CODEX_ACCESS_TOKEN`. The target is a binding discriminator, not an exported environment variable. The controller validates bounded access-token shape and expiry, materializes only disposable OAuth transport state, and supplies no refresh token, Agent credential tool or management grant. This access-only transport is qualified narrowly for Hermes 0.21.3 with the exact source fingerprints in the adapter; other versions or changed transport sources deny `doer_provider_auth_runtime_unqualified`. An offline synthetic-token test exercises the real pool loader and provider resolver without network access; it proves schema consumption, not provider authentication or entitlement. The token must remain outside the 120-second refresh margin throughout the approved session. Controller-authenticated processes repeat admission immediately before start and terminate on caller cancellation or the authority deadline. This is tool/model isolation, not a host-process sandbox: the trusted Hermes process necessarily handles the access token. Missing, invalid, expired or unauthorized bindings fail closed with fixed diagnostics. These controls are not a host sandbox or cryptographic JWT issuer verification. Independent security review and installed/live provider qualification remain required. No live binding, secret, charter, Agent, Loop or service was modified for this candidate.


### Explicit native Hermes connection (Linux source candidate)

The configured principal may explicitly select their canonical shared Hermes
root (`<operator-home>/.hermes`) as well as a named profile. Shared-root access
is not automatic fallback: preview, connection metadata and every runtime use
pin that exact selected source and account. Another operator's root, arbitrary
paths, implicit borrowing, source writes and refresh remain denied.

Use the owning daemon's typed online connection action, never handcraft binding
fields or extract a token/account ID:

```sh
aegis --config OWNER_CONFIG --target CONSOLE_URL controller-codex candidates PROFILE_HOME
aegis --config OWNER_CONFIG --target CONSOLE_URL controller-codex preview PROFILE_HOME [--credential ID]
aegis --config OWNER_CONFIG --target CONSOLE_URL controller-codex connect REVIEW_ID
aegis --config OWNER_CONFIG --target CONSOLE_URL controller-codex show
```

`PROFILE_HOME` is the explicitly approved canonical `~/.hermes` or exact named
profile under the principal's OS home, never an implicit shared-source fallback.
Preview returns only source/account/principal metadata and a five-minute one-use
review ID; connect rechecks the same account/source and exact configuration bytes.
The existing fresh kernel-authenticated Unix principal authorizes this operational
connection; it is not foundational charter approval and creates no Agent grant.
No independent password decision or desktop is required for this operation.
Unknown/caller-authored identity fields, foreign/expired reviews, unsafe sources,
changed accounts and competing environment bindings deny. The daemon alone
publishes a private mode-0600 controller-only operational record beside the
owning config at `OWNER_CONFIG.controller-codex.json` (private owner-only parent).
It contains the reviewed config SHA-256 and exact source/optional credential ID/account/principal only,
never credentials. Publication is atomic no-replace with fsync and readback;
existing records conflict. The owning YAML is never rewritten, backed up or
rolled back, so concurrent operator bytes/comments are preserved. Every runtime
resolution and `show` revalidate the record and config digest; any drift denies,
not fallback to another account. This is not CAS against arbitrary same-UID
writers. Legacy explicit YAML bindings remain supported without a sidecar;
conflicting bindings deny. Read `show` after an
interrupted outcome; do not replay a consumed review. The existing daemon's
runtime resolver adopts the exact persisted binding without mutating shared
startup configuration or requiring a restart. No local-store fallback exists.
`approved_by` is derived server-side, never an input. Live account entitlement and
same-Run execution must still be verified after independent candidate testing.

The native connection retains the exact existing Hermes 0.21.3/five-source
qualification gate. A private read-only descriptor is opened through no-follow
owner/mode-checked ancestry into only that local profile's store. A bounded,
no-inherited-environment child invokes the qualified native Hermes
`_load_auth_store` and `_provider_state_in` APIs, never a fallback-capable account
selector. Hermes owns the store decoding, including its supported legacy format;
Aegis suppresses the parser's corruption-backup behavior and raw diagnostics.
Only a nonempty profile-local native OAuth pool is eligible; no singleton is
required. `PooledCredential.from_dict` decodes each entry. Exact token aliases
are one grant; without an explicit selector multiple distinct grants deny as ambiguous without choosing by
priority, label, singleton, or reviewed account. Each entry must have a local
native device-code source and no external custody reference. Borrowed/global or
external references return `doer_provider_auth_native_source_approval_required`
without opening their sources; independent exact source approval is required.
Suppressed, dead, exhausted, non-OAuth and reference-only entries deny. Empty
local pools cannot establish effective profile-local login and never trigger
root fallback. Symlinks, hardlinks, wrong owners,
unsafe modes, oversized stores, account mismatch and expired/missing tokens deny.
The source is never modified. The private pipe returns only the access token to
the controller, which validates account and the existing 120-second margin;
refresh tokens never enter runtime transport. Non-Linux native resolution is
unqualified. A pre-existing environment binding still behaves as before, but a
native connection cannot coexist with either Codex environment alias or be used
for the design provider. Authentication approval remains distinct from custody,
Agent, charter, Loop, Run and host-write authority; none is changed by connection.

### Inspect and select an exact local native entry

`controller-codex candidates PROFILE_HOME` calls only the owning service's strict
`POST /v1/controller/codex/candidates` with `{"profile_home": PROFILE_HOME}`.
It is credential-free output, not credential-free internal parsing: the same
qualified native APIs inspect the protected pinned descriptor, offline and
read-only. At most 64 entries and a 1 MiB source are accepted. Labels, raw native
IDs, source JSON, exceptions, token hashes and credentials are never returned.
Each row has optional `credential_id`, optional `account_fingerprint`, `eligible`
and a closed `reason`. Reasons are `eligible`, `id_missing_or_invalid`,
`id_duplicate`, `source_approval_required`, `unsupported_auth_type`,
`unusable_status`, `token_absent`, `token_invalid` and `token_expired`.
Source-wide malformed/suppressed/oversized stores fail with existing closed errors.
`distinct_eligible_grants` counts validated distinct access grants, not accounts;
matching account fingerprints can still identify different grants. No native
current account is inferred or advertised. Inspection creates no review or grant.

The opaque ID is `nc-` plus a domain-separated SHA-256 digest of the bounded
explicit native entry ID. It is stable across token refresh, not across native
entry replacement; missing IDs never use native `from_dict`'s generated default.
After operator selection use `preview PROFILE_HOME --credential ID`. Its strict
API adds optional `credential_id` to the preview input, never caller account,
approval or secret fields. The five-minute single-use review binds exact source,
ID, account, authenticated principal and config bytes. Connect rechecks all of
these and persists the optional ID in the operational sidecar; `show` returns it.
Every subsequent resolution requires exactly that native entry ID and account.
Missing, duplicate, replaced, ineligible or account-drifted entries deny without
selecting another entry. A refresh outside Aegis may advance the same entry's
valid access token for the same account. Empty selector retains unique-only
behavior, including exact aliases, and never picks by priority, label or model
inference. An unsupported entry is not proof of multiple accounts or bad remote
authentication. Provider-side authentication and entitlement remain unchanged.

Offline regression command (synthetic stores only; no production credential read):

```sh
AEGIS_TEST_PROVIDER_HERMES_INSTALL=/path/to/qualified/hermes/source \
  GOMAXPROCS=2 GOFLAGS=-p=1 GOMEMLIMIT=2GiB \
  go test -race ./internal/config ./internal/runtime/hermes ./internal/app
```

This proves deterministic source resolution and the installed native parser,
not provider entitlement or the published Loop's live execution. An independent
tester must verify the frozen source/binary before an operator connects dev and
retries the same retained Run. Do not create replacement definitions or output.

`loops readiness FILE [--probe]`, `draft-save FILE`, `draft-show ID` and `draft-continue FILE` use strict typed CLI/API adapters and the configured authenticated owning-service Unix transport with `--target`. Readiness distinguishes `can_author` from `can_execute`; inspect before execution-intended publication, while intentional inactive registry-only publication remains possible. Plain inspection never invokes the helper; explicit probing is bounded to 10 seconds and does not judge task eligibility or create authority/work. Draft updates require exact version CAS; compatible continuation requires the old exact Agent and an already-approved immediate successor charter, archives the old binding and preserves contract/lifetime and original publication identities. None of these operations publishes, approves, provisions, activates or executes. Fresh worker admission and exact host-write approval still apply. See the canonical Loop skill for request fields.


## Exact reusable Run readiness

`loops readiness FILE [--probe]` and authenticated `POST /v1/loops/doer/readiness` also accept `{ "agent": EXACT_AGENT_REF, "loop": EXACT_V5_LOOP_REF, "inputs": TYPED_RUN_INPUTS }`, without `candidate`. They resolve the published immutable revision and use the same pure direct-contract binder as Run. Optional `idempotency_key` and `activate` describe the intended Run payload for principal-scoped request identity and retained-payload conflict checks; readiness never reserves that key or activates the Loop. Omit them when only inspecting the contract. Default inspection never calls the helper; `--probe` explicitly requests bounded protocol qualification, not task execution.

Blocked Run responses retain `request_id`, `readiness.contract_digest`, ordered prerequisite statuses and a closed `required_action`. Readiness exclusively accepts a v4 `candidate` or an exact published v5 `loop` with normalized `inputs` and optional intent key/activation choice; mixed shapes, partial references, null and unknown fields deny before effects. Existing zero omitted typed structs remain compatible. V4 absent/invalid/expired host consent returns `review_exact_host_write_contract`, preserving the supported exact protected review boundary; no URL or authority is inferred. Controller capability, host grant custody (absent/invalid/expired), protected workspace, trusted executable/home integrity, helper availability and cancellation are distinct; unknown backend text is not returned. Downstream checks remain `not_checked`. The existing signed host-review record is v4-bound: `exact_v5_host_review_adapter_unavailable` explicitly reports that a protected reusable-v5 review adapter is unavailable. It is not an approval or a suggestion to edit configuration, publish v4, or let an agent approve. Existing independently authorized exact contracts remain subject to fresh worker admission. Console's v4 Run form does not collect reusable inputs; use the typed owning-service interface for v5, not a substituted v4 task.

## Protected setup through installed skills (source candidate)

Use this workflow only when installed `aegis loops --help` lists `setup-protected`. Skill installation itself grants no authority. The agent requests a product-launched independent desktop interaction; it never operates a human terminal, browser, password field or approval dialog. Do not run the hidden `doer-setup-companion` manually. Do not use `setup-approve` through an agent tool/PTY: that command is solely for a genuine operator's direct terminal invocation.

1. Read the same retained task with `aegis --config OWNER_CONFIG --target CONSOLE_URL loops draft-show DRAFT_ID`. Keep its exact `id`, `version`, `agent`, `contract`, publication key and expiry; do not recreate the task when a prerequisite blocks it.
2. For an explicitly requested remedy, create a private JSON invocation file containing only `{"id":"EXACT_RETURNED_DRAFT_ID","expected_version":EXACT_RETURNED_VERSION,"action":"successor"}`. Run `aegis --config OWNER_CONFIG --target CONSOLE_URL loops setup-protected FILE`. The typed owning service validates current principal/ownership and exact draft version, and launches its pinned hidden companion. It accepts no password, decision, review receipt, charter bytes or approval flag. `successor` is only the bounded credential-only correction described below, not an arbitrary charter editor.
3. The human enters the enrolled Aegis principal password (not the authority passphrase) in an independently displayed protected native UI, then reviews the complete exact scope in a separate protected native confirmation. Root-owned trusted helpers capture input outside agent stdin. No GUI means unavailable; never fall back to a model-operated terminal or generic typed approve. Password session, CSRF and single-use review handle remain memory-only in the companion, never CLI output or agent input.
4. On `approved`, require exact owning-service readback, not helper exit text. For successor, verify version increments once and the same Agent advances to its immediate approved Registry revision. Contract, fixed retention, original Loop ID, revision, predecessor and publication key survive. Existing exact validated publication and historical publisher provenance are reused, not rewritten. Re-read the draft before another action. `successor` grants no provisioning, host write, publication or Run.
5. Only with separate owner intent, invoke the same command with `action: "provision"` and the new exact draft version. The human reviews the complete canonical charter/plan/effects and independently approves that exact plan for deterministic apply. Require its returned verified receipt, exact plan/charter digests and verified artifacts. Then independently invoke `action: "host"` to review the exact task/workspace/paths/assertion/retry contract and signed expiring host grant. Each action requires its own fresh password/native review. Neither action runs the task.
6. Resume authoring from the returned draft: build a private Doer authoring file using `agent_id: draft.agent.id`, `loop_id`, `revision`, optional `previous_digest`, `idempotency_key: draft.publication_key`, and `doer: draft.contract`; `loops doer FILE --output NEW_FILE`, authenticated `loops validate NEW_FILE`, then explicitly authorized `loops publish NEW_FILE`. Read back the exact immutable revision via `loops show LOOP REVISION`.
7. For separately requested Run, prepare `loops queue FILE` with exact readback `agent` and `loop` revision references, a stable owner-approved `idempotency_key`, and `activate: true` only if intended. The owning service repeats all current runtime/provider/Laya/host/receipt prerequisites. Require Queue attempt, independently checked selected-file artifact, verifier receipt and successful terminal disposition; `approved`, readiness or publication is not execution.

No-display acquisition classification and the real daemon → pinned companion → no-display `setup-protected` boundary are covered offline. The latter requires a private trusted checkout and must actually pass, not skip. Genuine desktop approval and provider execution remain separate unverified boundaries; an injected launcher or manual hidden-companion invocation is not public-boundary proof.

Preserve `denied`, `cancelled`, `timeout`, `unavailable`, `outcome_unknown` and `response_unverified` distinctly. On uncertain mutation, read retained draft, exact Agent history, receipts and signed host consent through supported owning-service readback before any retry; do not rotate intent or manufacture a human decision. Provider/model authentication, independent desktop interaction and real selected-file Run remain installed/live acceptance gates, not unit-test claims.

## Guided credential-only successor review

The guided setup page now reviews a native credential-only successor for the same retained draft: only a sole enabled stanza already without capabilities, tools/toolsets, memory or persistent integrations and with exactly `provider:codex` is eligible. It removes that one credential scope and requests `controller-codex-access-token.v1`; model, provider, runtime, authentication methods/selectors, other policies and other stanzas remain unchanged. Exact compatible imported canonical successors are reused, not automatically approved. Both original and proposed canonical bytes/digests are displayed before an independent principal decision. Browser confirmation consumes a session-bound, expiring, single-use receipt; rejection applies no authority. No setup step implies provider readiness, provisioning, host-write consent, publication or Run.

The skill-native review uses typed owning-service transport: `POST /v1/loops/doer/drafts/:id/setup-review` takes strict `{"id": DRAFT_ID, "expected_version": VERSION}` and returns `proposal`, `receipt`, `expires_at`, and `confirmation_url` (relative to the configured console origin). Present both exact canonical charters and give the human this URL. The human authenticates independently, requests a fresh exact proposal review on the existing protected browser setup page, and confirms or rejects its session-bound, expiring, single-use receipt. The typed review receipt is not browser approval proof. Never have the model click or type human confirmation, carry browser credentials, or manufacture assent. `POST /v1/loops/doer/drafts/:id/setup-decision` accepts strict `{"id": DRAFT_ID, "receipt": RECEIPT, "decision": "reject"}` for no-effect rejection. `approve-successor` fails closed with HTTP 403 even with a valid review token: independently captured typed continuation proof is unavailable. Account-level SO_PEERCRED authentication does not prove a human decision. Typed review receipts bind transport identity, expire after five minutes, are single-use for rejection, and fail closed on restart; at most 1024 outstanding reviews are retained. Optional `loops setup-review FILE` and `setup-decide FILE` wrappers require `--target` and never open another store. After human browser confirmation, use typed draft readback to recover the same task and exact successor; provisioning, host consent, publication and Run remain separate. If the protected browser journey is unavailable, report that gate instead of manual binding-file commands or an approval string. Partial writes remain visible; recover exact facts without rotating intent.

## Guided authoring and readiness (source candidate)

The issue 281 worktree adds a guided Agent → Doer task/assertion → readiness → independent contextual approvals → publish → Run journey. This section describes source, not a released/installed capability. Native companion integration is still under development: final source/build, extracted-binary browser integration, visual review and an explicitly authorized live source run must pass before that journey is called complete. [The launch ledger](implementation/issue-281-launch-review.md) records the proof boundary.

- **Retain, do not execute:** the authenticated principal's server-held draft preserves the exact Agent reference, task, workspace, writable paths, selected-file assertion and publication key across review. Edits require the current draft version (CAS); stale versions conflict. Draft expiry is fixed at creation (seven days), with at most 16 retained drafts per principal; edits do not renew expiry or recycle an old intent. Saving is not charter approval, publication, host consent or Run.
- **Readiness is observation:** candidate authorability and execution prerequisites are separate. The console includes a controller provider authentication row; token/transport qualification is resolved once at that stage, before receipt and runtime checks. A missing/blocked foundation leaves later checks `not_checked`. Approved-draft page inspection remains helper-free even after exact signed continuation resolution: the helper stays `not_checked` with `helper_probe_required` until an explicit probe. A bounded Laya protocol probe does not establish task eligibility or a model grant. Publication may remain available while execution is blocked. The worker repeats fresh checks before claim/effects.
- **Review each remedy independently:** choose an existing exact charter successor, independently approve its exact Agent binding, review/approve/apply the exact provisioning plan, and separately decide the exact host-write contract. No button, model proposal, draft or readiness label implies all those decisions. Compatible continuation archives the prior draft binding and preserves the original Loop identity/revision/predecessor and publication key. Exact publication, validation and stable owner/publisher provenance must verify before reuse; the newly admitted runtime participant is separate from historical creation provenance. Frozen/admitted runtime bindings cannot be rebound. Unknown, corrupt, foreign or changed publication denies, never allocates a replacement.
- **Host consent is narrow:** the signed record binds deployment, authenticated principal/stable owner, exact enabled Agent, candidate/predecessor and full contract (task, absolute workspace, writable files, selected verifier, assertion and retry bound). Its lifetime is at most 24 hours. Signed custody and current Registry ownership are reloaded before effects; the existing explicitly configured contract allowlist remains supported and is not silently rewritten. Consent grants no credential, model, provisioning, session, Graph or native-test authority. Local Laya/Hermes custody and runtime admission remain independent prerequisites.
- **Native continuation is a separate gate:** the required adapter is a separate Aegis companion process with a native `CONFIRM` decision (not password intake), protected Unix transport and genuine Linux peer credentials. The signed continuation retains the original browser requester/session and the authenticated human executor separately, with exact draft/version, candidate/Agent/Loop/charter, publication/Run keys, deployment/configuration and expiry. Browser cookies are not relabeled as local-OS identity, and a daemon self-socket is not a fallback. The in-flight portal/companion source is not yet qualified as an installed path. Unavailable, cancellation, denial, timeout, malformed response and no-decision must not mint approval or rotate the original draft/key.
- **Publish then explicitly Run:** publication creates an inactive immutable definition, not execution. Run must pass current exact authority, receipt, runtime and contract checks; a blocker/uncertain response requires same-key observation before a distinct run. Success requires independently reloaded selected-file artifact, verifier receipt, exact binding and terminal disposition—not a Laya verdict, cursor, process exit or model claim.

### Custody protection, recovery and consent renewal

Host-write approval and worker admission reject controller state/custody, configured controller files, credential-authority database/key paths and controller/helper executable targets, including resolved path aliases. The check also applies to configured contract allowlists and repeats before effects; consent cannot override it. These checks do not claim complete same-account filesystem confinement.

Before launching another native confirmation, the continuation portal reads the stable signed intent. A matching record recovers the original authenticated executor, original authentication timestamp and expiry, and the original draft/publication/Run keys. A missing record can request review; corrupt, mismatched or expired custody denies rather than silently reopening confirmation or extending authority.

Host consent remains bounded to at most 24 hours. While an exact-scope approval is active, reuse preserves its signed bytes, approval time and expiry. After expiry, a fresh explicit authenticated host-write decision appends a signed generation linked to its predecessor; reads never renew consent. Historical generation-zero records retain their filenames and bytes. The controller fully verifies the contiguous history under its store lock, including expired facts. History is limited to 256 generations per exact deployment/owner/Agent/contract scope; exhaustion denies and never prunes history or grants a new scope. Unknown publication outcomes recover only the exact committed generation. This renewal is distinct from runtime/session authority and cannot renew an expired continuation.

### Reading a Loop's definition contract

The Console's **Definition contract** section distinguishes declared dataflow
ports from the task assertion. A selected-file v4 Loop intentionally has no
external input or output ports: its task, workspace, writable-file allowlist,
selected file, expected-text mode and attempt bound are sealed into that exact
revision and displayed separately. Reusable v5 instead shows its declared typed
inputs; concrete task values arrive at Run time, not from the definition page.

The **Evidence and outcomes** section describes declared verification requirements
and possible terminal outcomes. Neither a succeeded terminal node nor a pre-run
contract digest proves a successful execution or an artifact content digest.
Actual artifacts, verifier receipts and disposition belong to an exact execution.
Both sections remain readable without JavaScript; enhanced tabs support pointer
and keyboard selection without publishing, executing or changing the definition.

### Exact raw bytes versus legacy text

The new Console builder defaults to **exact raw UTF-8 bytes**, encoded in a v4 contract as `"exact_bytes": true` with `expected_text`. For `"hello"`, `hello` passes but `hello\n` or surrounding spaces fail. An explicit empty expected string with this flag requires an empty file. Omission/false preserves historical trimming of both expected and observed UTF-8 text; omitting `expected_text` is presence-only and cannot be combined with `exact_bytes: true`. The assertion mode is sealed in the contract/Loop digests and selected-file evidence; it does not rewrite historical definitions. CLI JSON authoring must select the flag explicitly, rather than assuming the Console default. These are selected regular-file assertions, not arbitrary binary or multi-file proof.

Offline public CLI/owning-service success regression (builds the real Aegis binary; Go and a short private socket path required):

```sh
mkdir -p .t
TMPDIR="$PWD/.t" GOMAXPROCS=2 go test -p=1 ./internal/api -run '^TestInstalledOnlineDoerExactBytesSuccess$' -count=1
GOMAXPROCS=2 go test -p=1 ./internal/command -run '^TestDoerProtectedSetupUnavailableClassification$' -count=1
```

The first test registers an Agent through the public API, authors an exact-byte `hello` v4 Loop through a real CLI process, validates/publishes/shows it through the owning service, checks prerequisite denial without work or file effects, then separately prepares synthetic approved runtime/host fixtures and resumes the unchanged queue key. Fresh public Queue readback checks artifact, verifier receipt, disposition, raw file bytes and replay without another attempt. The service is hosted by the Go fixture; Hermes, Laya and approval input are explicitly synthetic offline integration, not real-provider execution, native human consent, or installed-service qualification. The second test verifies that missing display/helper acquisition becomes `protected_setup_unavailable` before network or confirmation; it does not launch a desktop helper.

Safe local preparation checks (no operator helper, live model or service):

```sh
GOMAXPROCS=2 go test -p 1 ./internal/loop ./internal/evidence ./internal/hostapproval ./internal/continuation -run 'TestDoer|TestSelectedFile|Test.*Raw|Test.*Exact|Test.*Approval' -count=1
GOMAXPROCS=2 go test -p 1 ./internal/command -run '^TestCompanion' -count=1
```

The companion command tests use inert injected protocol helpers and private fixture sockets; they are not human desktop confirmation or integrated installed-browser proof. Neither these checks nor the fake-Hermes installed vertical satisfy the required live source gate. Same-account subprocess/path controls still do not provide complete host confinement.

## Reusable typed v5 definition

`aegis loops doer-reusable FILE [--output NEW_FILE]` accepts an existing `agent_id`, `loop_id`, positive `revision`, `idempotency_key`, optional exact `previous_digest`, and `doer_reusable: {"max_attempts": 2}` (one to three attempts). It prints a v5 publication draft, or creates a new private file without overwriting one. It does not bind a task or workspace, consult a configured authority, publish, activate, queue, or run. Publication still requires the authenticated service and its ordinary ownership/authority checks.

The immutable v5 definition declares required typed ports `task` (string), `workspace` (string), `writable_files` (array), and `verify_file` (string); `expected_text` is an optional string. `loop.NormalizeDoerBinding` strictly validates a separate set of typed port ID/type/JSON values: missing, duplicate, unknown, null, mistyped, malformed, oversized or unsafe task/path values deny. An absent `expected_text` means presence-only; an explicitly empty string means an empty-text assertion. Successful normalization produces a bounded task contract, never authority. The single-node Queue worker now resolves the exact immutable Graph snapshot's mappings to the v5 ports before claiming, requires the operator-configured digest of that resulting per-run contract, and repeats authorization before every effect. The v5 JSON artifact binds that digest to the independently checked, content-addressed selected-file bytes; the receipt and completion transaction recheck exact contract provenance. Existing v4 definitions without the additive `exact_bytes` flag retain their historical assertion semantics. The v5 route does not grant configuration, model, tools or filesystem-sandbox authority.

The Console draws each unambiguous stored transition of cyclic v4 and v5 definitions as a directed SVG edge, with visible condition/traversal labels and a separate feedback gutter. The expanded textual equivalent retains all declared edges, including invalid ones withheld from geometry. An exhaustion destination is displayed only when an explicit `exhausted` exit exists; an ordinary successful exit is not labeled exhaustion. This is definition visualization, not an execution trace. V5 activation is an ordinary digest-bound lifecycle preview/action, but activation by itself does not authorize a run. Authenticated agents can now use typed `loops queue` with normalized task/configuration inputs and an already authorized exact contract; direct snapshots require no reusable Graph publication. Retrieve the exact stored result with `queue result ITEM`. The Console inspects those records rather than requiring a human v5 launch form. See [the reusable task-to-result workflow](REUSABLE_DOER.md) for request recipes, preconditions and evidence boundaries.

`aegis loops templates` lists the included versioned Doer recipe and its non-authorizing recipe digest from the executable without opening an Aegis installation. The authenticated `/console/loops` page displays it under “Included templates” even when the published Loop count is zero. The “Draft Doer Loop” form offers eligible latest enabled registered-Agent workspaces, without requiring a runtime session; without one, drafting remains visible but publication preview is denied. It takes an exact operator-selected task, absolute workspace, distinct writable files, selected verification file, assertion, retry bound, Loop revision and publication key. Preview pins the selected exact Agent revision and shows the selected publisher, revision, predecessor, key, selected-file contract and contract digest without persisting a Loop. Explicit confirmation rechecks the latest enabled Agent revision and stable Loop ownership through workspace publication; a published revision remains inactive and provides no execution authority. This does not grant a mandate, model/provider choice, execution, or credential access. The template is never included in authoritative Loop records or counts.

The published v4 detail offers a native Run form. The server generates its request key, verifies the authenticated same-origin principal and pinned Loop digest, resolves the immutable publisher Agent, and calls the existing one-request Loop/Graph/Queue application service with explicit activation. It shows the reserved request ID and exact prerequisite action on denial, or links to independently reloaded Queue attempts, selected-file artifact, verification receipts and terminal disposition. A blocked request is not an executed task. A browser password identity cannot select a charter stanza restricted to local-OS authentication; the response names that matching-authentication successor review boundary rather than borrowing CLI identity. V5 is agent-operated through typed normalized Run inputs; it does not add a browser launch form or bypass exact per-task authorization.

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
    "exact_bytes": true,
    "max_attempts": 3
  }
}
```

Omit `expected_text` (and omit/disable `exact_bytes`) for a presence-only regular-file check. With `exact_bytes: true`, an explicit empty string requires an empty file; without it, historical outer-whitespace trimming remains in effect. The workspace must already exist and the selected file should not already satisfy the assertion. Writable paths are explicit relative paths beneath the workspace. The builder seals the task, paths, check and attempt bound in the exact Loop digest and prints the separate Doer-contract digest to stderr. Use `./scripts/build-source.sh ./aegis` from the checkout and `./aegis loops doer --help` to inspect the installed source command; do not use `go run` as a build-only substitute. A real publication requires an enabled exact Agent, fresh authority and separately reviewed activation. Authoring and publication are not runtime grants.

The controller defaults to no implementation execution. The existing configuration route requires separate operator approval for an absolute trusted `implementation.go_binary`, `implementation.authorized_contracts` containing the exact printed Doer-contract digest, and absolute `implementation.laya_python` plus existing private `implementation.laya_home` with a prepopulated offline Laya checkpoint. The source candidate also accepts independently signed exact v4 host consent in place of the configured contract entry, without removing local controller/helper or fresh runtime prerequisites. The controller does not download Laya or change the operator's Hermes profile. The participant must be registered with a tool-free, credential-free Hermes authority. A Graph pins exactly one Agent/Loop node; the Queue submission permits only one Queue attempt, while the Loop permits up to three internal Hermes proposals. The actual Hermes model/provider remain those sealed in that authority context for both implementation and diagnosis/completion; the upstream Doer skill's experimental family routing is not supported here.

For v4, `aegis loops queue FILE` with `activate: true` and direct lifecycle activation check the exact enabled Agent, exact charter and freshly selected tool-free/credential-free stanza with a usable model, verified receipt for that charter, current Hermes runtime/version, the exact authorized Doer contract (configured allowlist or independently signed v4 host consent), trusted executable paths, private Laya home, bounded workspace policy, and one typed local Laya gate response before creating a new activation, Graph, or Queue item. The preflight discards that gate's yes/no verdict; task eligibility is judged again before claim. Missing foundations return an inline reason such as `provisioning_receipt_missing`, `doer_model_required`, `runtime_unavailable_or_unsupported`, `local_laya_unavailable`, or `implementation_prerequisite_required`, with no new executable Queue preparation. A v4 response also carries a stable `request_id` and the exact `required_charter` reference. For a missing matching receipt, `required_action: plan_preview_exact_charter` points to the existing `aegis plan preview AGENT --revision REVISION --environment local` review entry; a verified receipt still requires a separately authenticated exact approval and provision apply. For `doer_model_required`, `required_action: review_charter_successor_with_usable_model` means a new independently reviewed charter and exact Agent/Loop bindings would be needed—not an automatic model switch. For an exact-charter receipt remedy, retry the identical request file and idempotency key; a charter successor changes authority and requires a newly reviewed request with a fresh key and exact Agent/Loop references. Do not repurpose an already rejected activation intent. An empty `graph` and `queue_item_id` here mean the prerequisite gate ran before publication/submission, not that an execution failed. Existing attempts and recorded lifecycle events still replay their immutable outcome; a compatible pending recovery repeats the gate. Structural publication remains available without these execution prerequisites. This is a point-in-time preflight, not session issuance, a guarantee of future model access, or permission to alter controller configuration. The worker repeats exact contract and fresh runtime/effect admission before a claim and each write.

When an authenticated v4 request has both an unusable selected model and no verified receipt, it reports `doer_model_required` first; provisioning the unchanged charter cannot cure that blocker. The v4 Loop detail exposes a native Console Run form with exact revision/digest and a server-generated request key retained across page reloads for ambiguous-response recovery. The result offers same-key readback/resume; starting a distinct run explicitly rotates the key. It calls the existing QueueLoopAs path with activation requested and resolves the published Agent server-side; prerequisite reasons and request IDs are shown before execution, while Queue/attempt/receipt/disposition facts come from Queue readback only. The browser Console's password subject is not interchangeable with a charter's `local-os` selector; that mismatch is denied, not turned into readiness. Without an independently approved native continuation, use the supported authenticated CLI for a Loop whose charter requires local-os; the source companion path still needs integrated acceptance.

For a compatible already-accepted v4 preparation, a repeat request with an explicit item ID or the same idempotency key retains that exact ID. The optional `queue_item_id` is a recovery selector, not a changed execution intent: it must equal that request key's accepted Queue item ID. A different key cannot adopt the item. A newly missing prerequisite is appended as a no-state-change preparation diagnostic and reloaded for Queue readback, without claim or attempt. Repeating the same blocker does not duplicate its diagnostic. This does not turn a fresh unready request into a parked Queue item.

The closed step sequence is eligibility (typed local Laya) → implement (bounded tool-free Hermes proposal; controller applies only allowlisted file edits) → judgment (Laya reads the report) → selected-file verification (controller reads the file independently) → either terminal assessment or bounded Hermes diagnosis and another proposal. The authenticated Queue detail shows the independently reloaded, content-free Doer cursor in recorded order so verify → diagnosis → implement is visible when it actually occurred; it never infers success from a cursor step or model verdict. A negative gate durably denies the Queue item as `doer_needs_input` before any claim/attempt. An already-satisfying target denies before claim so old bytes cannot count as new work. Both judgment and file check are observed on every pass; a model verdict never mints a receipt. The file verifier denies unsafe paths, symlinks, nonregular files, oversized output and mismatched text; content-addressed artifact, independently reloaded receipt, pinned contract and fresh authority are required for successful disposition. Failed or uncertain effects never become success.

Implementation failure diagnostics retain a closed controller code in the append-only step checkpoint and expose it as `doer_steps[].failure_code` through authenticated Queue readback. Reviewed native Hermes `error_surface` layer/code pairs are advisory diagnostics only; provider/model labels, exception text, response bytes and retryability flags are never projected. Proposal decoding, missing selected edits and apply failures have separate closed codes. Missing or unrecognized native classifications stay generic. Historical untyped failures remain `step_error`: this projection cannot recover a cause that was discarded before persistence, infer it from later session cleanup, or reopen a terminal run. The Loop revision, implementation budget, fresh admission and evidence requirements are unchanged.

Append-only digest-linked step checkpoints live in the existing fleet writer. Authenticated Queue readback exposes ordered content-free `doer_steps` metadata keyed by attempt ID; raw model text and file bytes are not copied into this response. Interrupted nonterminal cursors fail closed: an uncertain model turn or file write is never replayed automatically. The exact claimed attempt remains owned until its bounded lease expires; the existing authenticated `aegis queue expire` path then records an evidence-free expired disposition, subject to fresh authority and exact claim admission. If authority is no longer available, expiration needs separately authorized authority recovery; the model cannot force it. This is a bounded single-node Doer Loop, not a general multi-node Graph scheduler, host sandbox, distinct Sol/Luna model mandates, Jev adapter, token/cost ledger, arbitrary shell executor, automatic Queue retry, or live-model qualification. Native code/tests (if separately authorized in v3) and the local Laya subprocess remain host processes under the operator account; tool-free Hermes turns are capability-limited but not a host sandbox.

Focused synthetic proof: `GOMAXPROCS=2 go test -p 1 ./internal/loop ./internal/evidence ./internal/implementation ./internal/looprun ./internal/orchestration ./internal/api -run 'TestDoer|TestQueueDoer|TestSelectedFile|TestStepCheckpoint|TestImplementationQueueNativeCompletion/v4-' -count=1`. It exercises fake Laya/Hermes transports, pre-mutation queue denial, and controller-selected file checks; live provider/model operation and an operator's deployment are separate acceptance gates.
