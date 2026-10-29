# Bounded repository coding with trusted Go acceptance

This is the opt-in CODING contract of the existing Doer v4 journey. It is not a
new scheduler, arbitrary shell grant, multi-language verifier, repository indexer,
GitHub publisher, or host sandbox. Selected-file presence, trimmed-text and exact
raw-byte contracts remain available with their original meanings. A historical
`doer_verified` selected-file result is not retroactively software acceptance.

## Declare the task and acceptance before execution

Use `aegis loops doer FILE --output NEW_FILE`, then the existing authenticated
validation/publication and separately requested exact Run operations. Drafting
opens no authority store and grants no execution permission. The same `doer`
contract can be retained by `loops task-save` / `loops task-execute`; retained
version checks, publication identity and execution freeze rules still apply.

A coding `doer` object contains the existing `task`, absolute operator-approved
`workspace`, explicit relative `writable_files` and `max_attempts` (one to three),
plus `coding`. Set `verify_file` to the empty string and omit `expected_text` and
`exact_bytes`: native behavioral acceptance and selected-file assertions cannot
be mixed. Writable files are bounded production Go files, never trusted tests or
the module input. Each writable file needs its declared source preimage.

`coding` has these typed fields:

| Field | Meaning |
| --- | --- |
| `acceptance` | Bounded behavioral requirement frozen before proposals |
| `repository_context` | Exact task/source commitment and explicit file inventory below |
| `policy.kind` | Exactly `go-test.v1` |
| `policy.packages` | Explicit local Go package selectors |
| `policy.required_tests` | Exact trusted package import path and top-level test-name pairs |
| `policy.timeout_seconds` | Existing native-check limit, one to 120 seconds |

Use real existing source and trusted tests, not handwritten pseudo-source or
invented digests. First prove the functional defect is RED with the frozen tests.
Neither the model nor a passing file-presence assertion may replace that check.

### Explicit read-only context manifest

`repository_context` contains:

- `task_digest`: `sha256:` plus lowercase hex for the exact task UTF-8 bytes.
- `source_digest`: the controller-checked ordered `explicit-go-source.v1`
  commitment to the manifest file records, not a mutable branch name.
- `max_files`, `max_file_bytes`, `max_total_bytes`: finite declared limits.
- `files`: ordered records with exact relative `path`, observed `bytes`, lowercase
  SHA-256 `digest`, and `role` (`source`, `trusted_test`, or `checker_input`).

The existing hard ceiling is 128 files and 8 MiB for individual/aggregate content;
the declared limits may be smaller. The **entire actual serialized managed input
must also fit the runtime's 64 KiB ceiling**. Encoding overhead matters. No bound
is permission to truncate, omit a required test, or silently substitute content.

Go source/test paths and an explicit root `go.mod` with role `checker_input` are
supported. The module file is immutable and counts against every relevant budget;
it is never appended implicitly. Read-only files and trusted tests are outside
the writable allowlist. Writable source preimages are immutable task context;
the controller separately tracks the current bytes produced by its allowed edits.

The controller reads only the named files, verifies actual byte counts/digests,
and rejects source drift, escapes, symlinks, hardlinks, unsafe/nonregular files,
nontext content, conservative private/credential-sensitive paths and oversized
inputs. Rejection does not return rejected bytes. Context remains untrusted task
data: its text cannot authenticate a principal, select a stanza or grant tools.
This is explicit-file custody, not a general secret-discovery/DLP service.

## Native execution needs separate operator trust

Publication and selected-file host consent do not authorize native code.
Configure an explicitly trusted absolute `implementation.go_binary` and separately
allowlist the **outer Doer-contract digest** printed by `loops doer` in
`implementation.authorized_contracts`. The Loop revision digest and derived v3
implementation digest are not substitutes. The default native allowlist is empty;
models and registered-Agent workspaces cannot populate it.

Local Laya may use an explicit interpreter/private offline home pair or the
existing qualified installed-library discovery. A native-only Go/contract
configuration preserves that installed decision-backend discovery without
changing the explicit native fields or inventing an allowlist. An explicit Laya
configuration or existing decision is not replaced. Missing qualification denies;
there is no package, model, toolchain or dependency auto-installation.

Qualified controller-owned Codex turns set the native event-idle watchdog from
the already-approved remaining turn deadline instead of inheriting a shorter
implicit transport default. The value stays finite and positive; the Go context,
authority expiry, cancellation and unchanged token-refresh margin remain hard
cutoffs. Startup and public inspection/probe bounds are not extended. This does
not restore consumed passes: an exhausted acceptance case remains exhausted, and
a fresh post-fix live case needs independent owner approval.

All ordinary principal, exact Agent/charter, provider/login, tool-free runtime,
session, host and effect-admission gates still apply. Models/providers/login
selection are not changed by coding authoring or readiness. Independent setup
requirements are reported before executable admission, not hidden in a queued
success. Readiness is observation, not a grant.

For an exact owner-approved native-only configuration addition that invalidates a
config-bound native Codex connection, the existing explicit `controller-codex
preview ... --replace` / `connect REVIEW_ID` path supports a narrow same-selection
review. Removing the one appended native-trust mapping, or the one new final
plain exact `sha256:` contract line in an existing block allowlist, must reconstruct
the previous exact configuration commitment. The checker and all older contracts
remain byte-for-byte unchanged. Each prior connection transition is independently
revalidated against its exact historical configuration without rewriting history
or persisting private configuration backups. Other drift, multiple new contracts,
removed/broadened trust, ambiguity, source/selector/account changes or stale history deny. Preview
has no runtime effect; connect rechecks the one-use review and retains append-only
history. Runtime/show still require the latest full configuration commitment.
This is not general configuration repair or atomic CAS against hostile same-UID
configuration writers; YAML and credential stores are never rewritten by it.

**Native tests and proposed production Go code run as the controller OS user.**
They are trusted host code, not a filesystem or network sandbox. Use reviewed
repositories/tests and an appropriately isolated host account. Tool-free model
turns do not make subsequent native code harmless.

## Supported managed journey

1. Resolve the current exact registered Agent through authenticated readback.
2. Build the bounded coding draft locally and inspect its exact contract digest.
3. Use `loops readiness FILE` for the exact candidate. Native authority absence is
   `doer_native_contract_unauthorized`; configured missing/untrusted checkers have
   distinct closed reasons. Raw backend text is never a public diagnostic.
4. With explicit publication intent, validate and publish through the configured
   owning service, then `loops show LOOP REVISION` and match validation, immutable
   digest, publisher and publication provenance. Publication remains inactive.
5. In a separate Run action, send exact Agent/Loop references and the stable
   execution key to `loops queue FILE` (explicit activation when intended).
   Aegis handles admitted preparation and the bounded worker. Do not carry manual
   session/binding files or run a separate worker command.
6. Reload the exact Queue and native coding history, artifact, verifier receipt
   and disposition. Independently inspect the allowed resulting production file
   and verify unchanged trusted inputs. Process exit and model narration are not
   software acceptance.

Use the documented `--config OWNER_CONFIG --target CONSOLE_URL` owning-service
transport while the gateway owns state. No credential extraction, second writer,
direct-store fallback or ad-hoc authenticated operational client is supported.

The native checker materializes **only declared, controller-verified current
inputs** into a private temporary checker workspace. It preserves their relative
paths and root module identity; unlisted source/tests cannot enter the compiler
input. Local module replacements that could redirect resolution into another
host tree deny. It reuses the existing native process timeout, bounded output,
required-test evidence and scrubbed environment (`GOPROXY=off`, `GOSUMDB=off`,
`GOTOOLCHAIN=local`, `GOWORK=off`, `CGO_ENABLED=0`). Missing offline dependencies
fail; this explicit-context path does not discover/materialize a vendor tree or
provide general dependency provisioning. Legacy nil-context native policy retains
its existing qualified behavior; see [verified implementation](VERIFIED_IMPLEMENTATION.md).

A failed check supplies normalized bounded statuses and immutable required-test
identities to corrective proposals/diagnosis, never arbitrary raw test Output,
stderr, remote errors or a shell capability. A wrong-package or presence-only
stub cannot produce the required passing native test evidence.

## Evidence and continuation

Authenticated Queue/debugger readback exposes content-free `coding_history`
separately from legacy selected-file `doer_steps`. It binds the exact Queue,
Attempt, Loop execution and contract to:

- the original task/context commitment and reloaded source/test custody;
- every reserved implementation pass, including proposals that failed;
- current candidate/check/context-state digests;
- exact managed input digest and byte count, prepared versus delivered state,
  and actual returned turn/session references when available;
- predecessor history, consumed-before/current/cumulative consumption and remaining
  budget; and an explicit trusted native acceptance gate.

The exact serialized source/test input is retained in private content-addressed
custody **before** the managed adapter call. Failed custody prevents that effect.
Delivery is recorded only after the actual successful adapter return, including
when later patch decoding fails. Prepared input is not proof of delivery, and a
missing session reference is not invented. Final acceptance independently reloads
input/context/source/check evidence. Public history contains no raw model prompt,
source text, arbitrary host output or provider credentials, and grants no generic
blob/path reader. `queue result` remains the separately scoped selected-file
result interface; native coding uses its dedicated evidence/history and the
explicit approved production-file inspection.

CODING success uses `aegis.implementation.verifier`, claim
`verified-implementation`, policy `aegis.loop.verified-implementation.v1`, and an
`implementation_verified` disposition. The JSON artifact/receipt digest is not the
raw production-file SHA-256. A legacy selected-file verifier receipt is not this
native software-correctness claim.

One Queue attempt can contain multiple implementation passes. Known complete-pass
failure, or a validated known failed managed reservation with prepared input and
durable proof that no implement/apply stage began, may use the existing
authenticated append-only requeue/reexecution path
under only the remaining original budget. It retains the same published Loop,
original task/context and exact predecessor lineage; a successor Queue record is
not a reset or a rewrite of its terminal predecessor. A correction uses the
controller's recorded current writable preimage while keeping the original
read-only context. Fresh authority still applies. Success, exhaustion, unknown or
partial effects, missing/corrupt evidence, source drift, swapped references and
conflicting successors deny. Do not republish, rotate uncertain intent or create
a successor merely to evade failure. An authenticated exact stable-key Requeue
reconciles its already admitted immutable successor before fresh-continuation
preimage checks, including after successful effects changed the source. This
read-only reconciliation neither resumes runtime work nor reserves new authority.
Changed intent conflicts and a different key cannot acquire another successor.
A pre-apply continuation must reverify the
recorded preimage, retain its consumed pass and prepared/not-delivered distinction,
and cannot infer a stop acknowledgment or a historical failure cause. A successfully
returned managed turn rejected by strict proposal decoding additionally requires
its existing closed `proposal_rejected` stage, matching failure classification,
validated prepared/delivered input custody and no implement/check/apply evidence.
Authenticated `coding_history.stages` exposes only stage name, pass and digest;
it never exposes payloads. Delivery alone or a missing old classification does
not make an earlier failure recoverable. Missing
witnesses, unknown/interrupted outcomes and partial applies still deny. Distinct
tasks can retire only exactly owned, idle, terminal zero-authority coding runtimes;
they do not reset the failed task's budget. First reconcile authoritative history.

## Historical native recovery assessment (#410 source candidate)

Authenticated `queue show ITEM` and `GET /v1/queue/:item` expose
`coding_history[attempt].recovery` with content-free `effect`, `decision`,
`reason` and `next_action`. `no_apply_proven` requires validated durable
prepared-input custody and, after delivery, the matching closed
`proposal_rejected` witness. `apply_observed` means a verified completed
candidate/check boundary, not software acceptance or permission to replay.
`unknown` never means no effects, even if workspace bytes are unchanged.
Invalid or substituted custody fails closed with
`native_recovery_evidence_invalid`; it is not an empty successful history.

The legacy `gate` / `input_prepared` / `input_delivered` history without a
rejection or completed candidate witness returns `policy_decision_required`,
`native_recovery_effect_evidence_missing`, and
`preserve_history_no_supported_reconciliation`. This is an actionable stop:
retain the terminal Queue and evidence, report the missing historical fact,
and request policy-owner review. **No supported reconciliation/approval action
currently supplies that fact.** Review does not authorize a replay; do not
edit custody, infer a witness from narration/absence/unchanged bytes, reopen a
terminal Queue, republish, rotate keys or reset the budget.

Only existing proven histories can request the already-supported authenticated
linked Requeue under existing policy, exact lineage and fresh checks. Its
assessment is not authority: source/context, lifecycle, identity, idempotency,
successor uniqueness and cumulative budget are rechecked before any effect.
Exhausted, cancelled, expired and revoked histories remain unavailable. A
transport loss preserves the same intent; reconcile exact owning-service
readback before retrying. No new recovery command or human approval grant is
introduced. Confirm installed help: source additions are not installed adoption.

## Qualification tiers

Source tests include controlled proposals/providers and actual native Go checking,
input-byte capture, failure/requeue/correction and denial cases. These are
synthetic-provider evidence, not real-provider execution. Real managed-provider
existing-repository acceptance must separately show frozen RED tests, actual
bounded source/test delivery, a runtime-produced production edit, GREEN trusted
acceptance, exact receipts/disposition and unchanged tests. Reusable selected-file
hello/world remains a separate non-regression journey; when its policy trims text,
inspect exact raw bytes independently rather than claiming raw-byte enforcement.

Packaged/installed proof, production qualification, release publication and fleet
onboarding are separate operations. A merged source change or development run
alone does not establish those tiers.
