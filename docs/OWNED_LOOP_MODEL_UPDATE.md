# Owned Loop model updates (source candidate)

An owner can change the model on an existing Loop without changing its logical
identity, copying authority, replacing its default model, or losing the previous
revision. These operations publish definitions only; they never Run a Loop.

## Inline operator model addition (no Loop required)

In an existing gateway-backed `aegis manager` terminal, the operator can enter:

```text
/agents model-add silas openai-codex gpt-6.2-sol
```

Use the exact canonical Agent ID returned by Registry readback; `silas` here
is the example selection, not an inferred profile identity. This explicit input
is intercepted before model dispatch, including in the degraded deterministic
shell. Ordinary natural-language requests and model output cannot approve.

The same interaction captures the enrolled principal password using the existing
protected no-echo terminal mechanism, presents the complete exact old/new charter
with unchanged default, and asks for fresh `APPROVE` or `DENY`. The product uses
the owning Unix service native login, origin/CSRF and a purpose-separated
session-bound single-use receipt. No browser, Loop draft, JSON file, typed digest
or second manual command is required. Password/session/receipt material is not
sent to the model or printed in the review. Piped input is unavailable, not a
fallback to ambient peer approval. The authenticated owner must control the
selected enabled Agent; multiple enabled stanzas or unsupported lane design deny.

On approval the exact Registry successor is read back and printed, and the manager
continues. This adds only the requested permission; it does not change defaults,
connect credentials, provision, publish a Loop or execute a provider. Existing
capabilities and credential controls remain intact. A permission already present
returns conflict without a second successor. On unknown response inspect exact
Agent/charter history before retrying. If only charter import completed, a fresh
review reuses those exact bytes; cross-store atomicity is not claimed.

The older retained Loop-edit workflow below remains supported independently.

## File-free workflow

Use the owning service's configuration and Console origin, as with other online
Loop commands. First inspect `loops show LOOP REVISION` and identify the registered
owner. The revision argument below is the **existing** publication, not the proposed
successor. Replace uppercase arguments with the observed selections:

```sh
aegis --config OWNER_CONFIG --target CONSOLE_ORIGIN loops model-update LOOP REVISION \
  --agent OWNER_AGENT --provider PROVIDER --model MODEL
```

This builds and validates the next immutable revision from the published bytes.
It changes all explicitly declared Hermes turns; `--step ID` can be repeated to
select only particular Hermes turns. Deterministic/Laya nodes, topology, contracts,
retry bounds and evidence requirements remain unchanged. An unknown/non-Hermes
step is rejected with HTTP 400 `invalid_input`, without mutation. The previous digest and stable publication key are derived by
the product; no human types a digest or edits JSON. The general agent-authored
`loops edit FILE` adapter also accepts an exact `PublishLoopInput` proposal when
changes other than a model selection are intended; its owner and authority checks
are the same, and it is not an end-user prerequisite.

- **Model already approved:** the normal `PublishLoopAs` path publishes the exact
  successor, without an operator review. Publication still does not activate or Run.
- **One new model permission:** HTTP 202 returns `pending_edit`, including the
  exact candidate, original Agent/charter, proposed charter, one `missing_model`
  pair, fixed expiry and `confirmation_url`. No new Loop revision, charter,
  approval, mandate, provisioning or Queue work is created at submission.

Open the returned path on that owning Console origin. If not signed in, sign in
normally and reopen the same link. The human independently chooses **Review exact
model permission**, reads the retained Loop and complete old/new charters, then
**Approve this exact model permission** or **Reject this decision**. The product
carries the exact review receipt and digests; there is no hash-pasting or manual
JSON step. An agent must not operate the password or confirmation controls.

A fresh enrolled-password session, same-origin CSRF and a session-bound single-use
review receipt are required. Local-OS/Unix-peer identity cannot use this new
approval seam, even when mapped to the owner. Login or a connected provider alone
never approves a model. Rejection consumes only that review; it preserves the
inert edit and current authority. A later decision requires a fresh review.

After approval, the requesting owner uses the returned ID:

```sh
aegis --config OWNER_CONFIG --target CONSOLE_ORIGIN loops edit-show EDIT_ID
aegis --config OWNER_CONFIG --target CONSOLE_ORIGIN loops edit-publish EDIT_ID
```

This publishes the **same retained candidate bytes and digest** through ordinary
publication checks. The independent decision itself never publishes it. Repeating
publication reconciles the same stable idempotency key. `model-update`,
`edit-show`, `edit-publish` and the Console recognize an already published exact
update after edit expiry or later Agent revisions. Readback re-joins the original
request key, complete revision/validation, publisher, owner and immutable audit/
authority provenance; it neither republishes with the current Agent nor approves
again. Equal candidate digests under a different key are not a replay. The Console
shows the original publication instead of another permission-review control.
Old Loop revisions,
provenance, lifecycle, execution and evidence records are not rewritten. Activation,
provisioning, host-write consent, provider connection/qualification and execution
remain separately checked operations.

## Deliberately bounded model design

This slice prepares a model-only successor for one sole enabled stanza and one
exact missing provider/model pair (shared across any number of selected turns).
It never unions stanzas or removes tools/credentials to obtain eligibility.

For an existing provider it preserves that provider's unambiguous declared
transport policy. For a new Codex/openai-codex provider it proposes the existing
`controller-codex-access-token.v1` transport; this requires the normal zero-Agent-
tool/credential policy and does **not** establish a connected or qualified runtime.
The existing default and every other scope/selector remain unchanged. A matching
already-imported charter may be reused, but import is never approval.

Ambiguous/multiple enabled stanzas, multiple missing models, unresolved model-only
selections, new unsupported provider transports, contribution-role authority and
new local-model artifact digests remain unsupported by this bounded automatic
designer and deny; no fallback model or guessed local artifact is substituted.
An already-approved selection still uses the ordinary publication envelope.
This is general to registered workspace owners, not just the imported default
Agent; the older `agents providers` migration/promotion workflow remains unchanged.

## Retention and recovery

At most 16 active pending reviews per principal are held in the existing private
operational Store. The seven-day permission window stays fixed at creation; it is
never renewed. Approved, published, expired and stale proposals do not permanently
consume pending capacity. Before releasing a slot, the exact immutable proposal
is preserved create-only in an individually bounded history record. Interrupted
archival can leave identical copies, never a deleted proposal or an unbounded
principal history blob. Existing bounded sets remain readable and are compacted
on the next missing-model submission.

`edit-show` projects `pending`, `approved`, `published`, `expired` or `stale` from
authoritative publication/Agent state and the fixed expiry, not stored permission
flags. Historical reads remain available, but unfinished expired/stale edits cannot
publish or approve. Approved outcomes need owner publication, not another approval.
Review receipts remain memory-only under existing Console policy; restart requires
a fresh review only while still pending. Candidate/key changes conflict. A disabled
or changed Agent, occupied conflicting Loop revision, occupied conflicting charter,
foreign principal, stale password or revoked session cannot approve the proposal.

Charter import and Registry successor publication are not one cross-store
transaction. An exact imported charter or an exact immediate approved successor
can be independently re-reviewed after an interrupted response. Conflicting state
denies; fresh owner publication admission is still required. The proposal itself
is never used as authority. Local stores/processes are not a host sandbox.

## Evidence boundary

Focused app fixtures reproduce the original denied publication, then prove
retention, independent approval and same-revision continuation. API tests exercise
the real Console password manager, CSRF and single-use receipts in-process; CLI
fixtures exercise typed owning-Unix transport. These are controlled source tests,
not an actual human decision, browser acceptance, live-provider execution,
installed-target qualification or deployment. Parent-owned independent acceptance
and the separate exact publish/Run hard gate remain unexercised by this source slice.
