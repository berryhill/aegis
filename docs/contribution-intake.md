# Contribution intake (#375)

Use an authenticated manager session, not ordinary conversational instructions:

```
/contribute select <published-loop>
/contribute run https://github.com/owner/repository/issues/1 [narrower-scope]
/contribute status
/contribute narrow local-only
/contribute stop
```

The optional restriction is one of `no-merge`, `no-publish`, or `local-only`; it cannot expand the published delivery target. The server retains the exact selected active v6 Loop and a stable invocation key. A repeated identical invocation reads the original immutable contract without rereading upstream content or allocating another writer. A different invocation cannot silently replace the reserved issue writer, even after stop. No operator IDs, role bindings, internal keys, credentials or authority can be supplied in command text. Selection does not start execution.

The public GitHub reader denies redirects, pull requests, missing author provenance, non-open issues, oversized content and incomplete discussion. Source bytes are untrusted task data, never authorization. Exact numeric repository/issue identities, author and observation metadata, content digests, Registry/charter/runtime roles, bounded template and delivery restrictions are sealed in the contract.

Status reads the original contract and current append-only control target/sequence. Controls use current authenticated manager-session provenance. Every control read reconstructs the immutable sequence, validates canonical digests and previous links, contract/owner/Queue identity, narrowing and sticky stop, and compares the head with history. Missing, malformed or rolled-back heads deny. Control writes use the store lifetime transaction helper and append an authoritative audit event atomically.

## Bounded issue data and executor delivery

The source correction for the retained public #375 failure keeps the existing
32,768-byte Task/Acceptance limits. Previously `BindLoop` appended the complete
JSON-encoded issue/discussion to Task: a reader-valid public issue therefore
failed with HTTP 500 before admission. This is a representation mismatch, not
permission to enlarge Task or discard discussion.

Historical bindings whose complete inline Task is at most 32,768 UTF-8 bytes
retain exactly their original bytes/digests. Larger bindings retain the approved
Task and an exact contribution run/sealing-digest reference. The controller
resolves that reference only from the current authorized Queue's existing sealed
contract, verifies template/profile/control/role bindings, and delivers the entire
canonical contract as untrusted data alongside the Task in the actual builder
request. The tool-free runtime receives the bytes; it is not expected to fetch a
URL, local file or arbitrary blob. The sealing digest is not a blob address.

The complete JSON-encoded runtime envelope must still fit 65,536 bytes, including
escaping, contract, repository preimages and feedback. Intake checks the complete
source plus pinned source-repository preimages and the maximum ordinary closed
criterion feedback before contribution/Queue reservation. Each builder turn
rechecks the actual envelope before role/runtime dispatch; later repair/context
changes do not waive that bound. Oversized initial input returns HTTP 413 with
`contribution_input_bound`, never a partial success. Reader response/issue/discussion
bound rejections return HTTP 413 with `contribution_source_bound` and the fixed
applicable limit, without reflecting source bytes. Inputs that cannot fit the
one-shot consumer are unsupported: no truncation, summary, hidden chunking,
expanded limit or fallback executor is used.

Ingress bounds remain: title 1,024 bytes, body 65,536 bytes, at most 20 complete
comments each 32,768 bytes, each HTTP response 256 KiB, narrower scope 4,096 bytes.
These are byte limits, not Unicode rune counts. The full discussion and GitHub
numeric author provenance are retained, including latest owner instructions in
the observed snapshot; association/prose never grants authority. Same-key replay
returns the original snapshot, not a claim of a fresh upstream instruction read.

Reference-backed builder delivery retains the exact private serialized-input
blob digest/length with candidate role evidence. Durable readback verifies those
bytes, sealing reference and implementation/run binding. Public diagnostics carry
closed reasons, not source, credentials or raw model inputs.

Controlled coverage includes the 32,768/32,769 representation boundary and
65,536/65,537 runtime boundary, multibyte/escaped input, missing/foreign/altered
context, pre-admission rejection without a reservation, replay/reopen, complete
builder input and native repair/candidate evidence. The opt-in built-public test
uses the real issue #375 through authenticated Unix manager HTTP, then verifies
same-run replay, full retained content, controls and real process restart. This
is source/controlled public-intake qualification, not managed-provider success,
installed adoption, live principal recovery or whole #379 acceptance.

## Canonical acceptance

Consume the [canonical contribution happy path](../.hermes/contribution-happy-path-contract.md)
and its mandatory stage/evidence/owner matrix for #375–#379. This intake document
is adapter-specific guidance, not an alternate lifecycle. Source intake and control
fixtures do not qualify downstream builder, tester, delivery or installed behavior.

## Capability boundary

This correction implements the #375 intake seam, not completion of parent #373 or siblings #376–379. Intake alone does not execute non-local contribution delivery. `no-merge` and `no-publish` are not aliases for successful local-only execution. The selected-file executor cannot attest whole-contribution delivery; a Queue terminal state alone never means the contribution is complete. Status reports `completed: false` and does not claim a builder/tester/delivery journey ran.

## Launch-asset review

Correction assets changed: README, CHANGELOG, SECURITY, architecture data-flow
notes, verified-implementation schema guidance and this intake document. Reviewed
unaffected: LICENSE, CONTRIBUTING's canonical lifecycle, CODE_OF_CONDUCT, existing
threat-model authority/no-sandbox boundaries, architecture diagrams, five-minute
quickstart and no-key demonstration/recording (which do not invoke contribution
intake), focused contributor issues and release archive/checksum format. Existing
skills still use the same explicit commands and route to canonical contracts;
no installer/operation matrix/schema or CLI syntax change is needed. Builder2
must refresh revision-matched executable/package provenance and affected intake/
executor proof after canonical delivery. No release, checksum publication,
recording replacement, workstation update or installed adoption is performed by
this source correction. Real #379 acceptance remains separate.

## Local evidence boundary

Focused Go tests exercise application admission against the real Badger fleet repository, replay/reopen, reservations, current-session controls and sticky stop, plus canonical control corruption denial. These fixtures use controlled source data and are not live GitHub acceptance. The managergateway.Execute integration fixture additionally selects a published v6 profile, runs/replays without operator keys, checks status/narrow/stop, rejects malformed/chat inputs with zero reservations and denies a wrong session token. Its subject is authenticated through the application OS boundary; the retained manager session is a fixture, not a live Unix/HTTP login. Reopened-store corruption fixtures deny missing/rolled-back heads and missing/corrupt history. Isolated installed-candidate and real transport/login acceptance remain independently unverified.
