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

## Canonical acceptance

Consume the [canonical contribution happy path](../.hermes/contribution-happy-path-contract.md)
and its mandatory stage/evidence/owner matrix for #375–#379. This intake document
is adapter-specific guidance, not an alternate lifecycle. Source intake and control
fixtures do not qualify downstream builder, tester, delivery or installed behavior.

## Capability boundary

This change implements #375 intake, not the parent #373 or siblings #376–379. Non-local contribution delivery execution is unavailable. `no-merge` and `no-publish` are not aliases for successful local-only execution. The selected-file executor cannot attest whole-contribution delivery; a Queue terminal state alone never means the contribution is complete. Status reports `completed: false` and does not claim a builder/tester/delivery journey ran.

## Launch-asset review

Affected: README, CHANGELOG, session-operations and loop-authoring discovery, and this limitation/security-boundary document. Existing no-key demonstration and five-minute quickstart remain independent of contribution intake. LICENSE, SECURITY, CONTRIBUTING, CODE_OF_CONDUCT, existing threat model and architecture diagram retain their existing authority boundaries; this change creates no new credential or approval authority. No recording, release binary/checksum publication or contributor issue is created by this source-only change. Release/installed/live-provider acceptance remains separate and unverified.

## Local evidence boundary

Focused Go tests exercise application admission against the real Badger fleet repository, replay/reopen, reservations, current-session controls and sticky stop, plus canonical control corruption denial. These fixtures use controlled source data and are not live GitHub acceptance. The managergateway.Execute integration fixture additionally selects a published v6 profile, runs/replays without operator keys, checks status/narrow/stop, rejects malformed/chat inputs with zero reservations and denies a wrong session token. Its subject is authenticated through the application OS boundary; the retained manager session is a fixture, not a live Unix/HTTP login. Reopened-store corruption fixtures deny missing/rolled-back heads and missing/corrupt history. Isolated installed-candidate and real transport/login acceptance remain independently unverified.
