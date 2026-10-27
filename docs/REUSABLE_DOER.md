# Reusable Doer: Agent task → verified run result

The primary interface is the authenticated typed CLI/owning service, not a human
launch form. Publish one `doer-reusable` v5 revision, retain its exact Agent and
Loop references, and call `loops queue FILE` for each distinct explicit task.
No reusable one-node Graph publication or new Loop revision is needed.

## Source-candidate exact protected setup

With the matching candidate service, a requested v5 Run blocked on exact
provisioning or host consent returns `setup_review_task`, retaining the original
normalized `host_run`, Agent/Loop refs, task contract and stable request key.
Identical replay returns the same retained task; observation alone creates none.
This is non-authorizing review data, not executable Queue work or a new consent.
Use the existing `loops setup-protected` action with the returned ID/version and
`action: "provision"` or `action: "host"` as required. The human independently
authenticates and reviews the complete exact scope; the agent never supplies a
password or decision. No desktop means unavailable, not an approval fallback.
Read back the same retained task, then resume the original `loops queue` payload
and key. Do not substitute a v4 publication, binding files or a separate worker.

A differently recorded owner must have both exact provisioning and independently
reviewed signed host scope. The host fact binds its recorded owner, controller
principal/approver, deployment, Agent, immutable v5 candidate, contract and complete
normalized Run-input digest. Neither Run consent nor the workspace alone can
mint or refresh this different-owner authority. Definition mutation remains
stable-owner-gated; this is not independently authenticated profile delegation.
Older services without this returned review contract remain unavailable at the
actual named adapter; new advisory skills cannot supply backend capability.

## Fixed configuration recipe

Use the existing `QueueLoopInput.inputs` array of `{port_id,type,value}` entries.
The adapter/request recipe binds an already approved absolute workspace,
`writable_files`, and `verify_file`; `max_attempts` is fixed by the published
reusable definition. The named task and a separately declared optional
`expected_text` vary per intent. These are recipe defaults, not a new persistent
template/defaults domain or authority. See
[`examples/reusable-doer-queue.json`](../examples/reusable-doer-queue.json).

Canonical v5 still has five typed input ports: required `task`, `workspace`,
`writable_files`, `verify_file`, and optional `expected_text`. It has zero
Graph-dataflow output ports. The named result is an execution read view, not a
new definition port. V5 compares trimmed UTF-8 text; it has no `exact_bytes`
input. Use the existing v4 `exact_bytes: true` contract when raw-byte equality
is required. Never infer a verifier assertion from task prose.

```sh
aegis --config OWNER_CONFIG --target CONSOLE_URL loops queue TASK_REQUEST.json
aegis --config OWNER_CONFIG --target CONSOLE_URL queue show EXACT_QUEUE_ITEM
aegis --config OWNER_CONFIG --target CONSOLE_URL queue result EXACT_QUEUE_ITEM
```

The request must pin read-back revision/digests, a stable intent key and explicit
activation when needed. `POST /v1/loops/queue` uses the same service. Each bound
contract digest needs independent existing exact host authorization before Run;
a published template, normalized inputs or task text cannot grant it. All
runtime, model/provider, Laya, workspace and effect-time authority checks remain.
Missing prerequisites are blockers, not task completion. Same-intent replay
returns the existing work; changed payload conflicts. Distinct tasks need distinct
keys and exact preauthorized contracts. For an exactly owned successful direct
v5 predecessor, Run validates its saved preparation fence, runtime binding,
charter, terminal attempt/disposition and absence of other active work sharing
that authority, then retires the completed session through the existing service
lifecycle and prepares fresh authority. No manual termination handoff is needed.
`prior_run_terminal_authority_retirement_required` remains a fail-closed blocker
for missing, foreign, drifted, busy or nonterminal predecessors, not permission
to bypass a fence. Failed v5 work cannot start a fresh budget under a new key;
typed budget-linked v5 reexecution is not supported. v4 reexecution is unchanged.

For a second explicit replacement task in the same approved workspace, keep the
first output intact. Its existing bytes must not satisfy the new predeclared
assertion. Do not delete/move/erase output to defeat the unchanged preclaim
already-satisfying guard. If new exact scope lacks authorization, stop at that
independent gate; never broaden the configured allowlist from an agent request.

## Exact result readback and human inspection

`queue result ITEM` and `GET /v1/queue/:item/result` authenticate metadata access
and additionally require the pinned Agent's stable owner or the source-candidate
exact reviewed controller context. Workspace metadata sharing is not arbitrary
content-read authority. The controller path reconstructs original workspace,
runtime and signed host/evidence bindings at the recorded completion time under
fresh reader authentication. Later ordinary expiry/revocation or Agent lifecycle
changes do not rewrite intact historical results or renew execution authority.
The reader reconstructs the exact bound contract and rechecks successful
Queue disposition, run/Loop/Agent/attempt/claim/authority, passed receipt,
content-addressed proof and selected-file blob hashes. It never reads the current
workspace. A v5 proof JSON object is not selected-file content.

The response names `task`, `status`, `verification`, `source`, exact Queue/run
references and, only on verified success, `result` with artifact/receipt,
contract digest, file ref, encoding, media type, size and content. UTF-8 content
is text; other bytes use base64. Existing 1 MiB selected-file limits apply.
Failed/pending work has no verified result. Corrupt/substituted evidence denies.
Elapsed time is enqueue-to-disposition (includes waiting), with explicit timestamp
provenance; usage is `unavailable`, not zero tokens/cost or guessed telemetry.

The authenticated selected Queue detail shows task, safe text content, exact
references, verification/status, elapsed source and unavailable usage through
that same application reader. Only the selected record is joined, never a
collection-wide blob read. There is no generic path/blob route, browser credential
return, human v5 launch form or mandatory human Run step.

## Isolated reproducible evidence

```sh
GOMAXPROCS=2 GOFLAGS=-p=1 GOMEMLIMIT=2GiB go test ./internal/api -run '^(TestInstalledOnlineReusableDoerDirectAdmission|TestInstalledOnlineDoerExactBytesSuccess)$' -count=1
AEGIS_QUEUE_RESULT_BROWSER_TEST=1 GOMAXPROCS=2 GOFLAGS=-p=1 GOMEMLIMIT=2GiB go test ./internal/api -run '^TestInstalledOnlineReusableDoerDirectAdmission$' -count=1 -v
```

The first fixture uses a real built CLI, owning Unix service, durable worker and
independent verifier. Both exact task contracts are preauthorized before the
first run, with the same Agent/Loop/workspace/allowlist. A synthetic Hermes gateway
selects proposed hello/beta edits from the explicit task case, not the assertion.
The second task replaces hello with beta through normal admitted effects. Both
intents replay without another attempt and conflict on changed payload. Reading
first-task result after replacement still returns original stored hello bytes.
The optional Chrome proof uses the existing stdlib CDP driver, real password
authentication, production routes/CSP and DOM; it inspects both generated records
read-only. No Playwright installation is needed. This is synthetic runtime and
approval fixture evidence, not live-provider success or human approval proof.
