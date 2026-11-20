# Interrupted operational authority recovery

This is a source-only implementation, not evidence that an installed or production store has been recovered.

## Supported procedure

Stop the exact owning gateway using its supported command. Never reset/init,
manually edit markers, select a guessed generation, or modify engine files.
Run as the configured OS principal:

```sh
aegis --config /absolute/path/to/aegis.yaml gateway stop
aegis --config /absolute/path/to/aegis.yaml authority recover-interrupted
```

Recovery requires the existing enrolled principal password through the protected
secret provider, not the credential-vault passphrase, command arguments, environment
variables, or confirmation flags. Missing enrollment, cancelled authentication,
wrong principal, or a present protected Unix socket refuses by default. The command opens no
full service, credential vault, runtime or listener. It performs bounded offline
physical recovery of authority and an existing fleet store. Authentication
freshness is bounded by configured TTL, capped at two minutes, and checked before
publication boundaries. A retry requires fresh authentication.

Only after zero exit status and authority JSON `recovered: true` or
`verified_clean: true` (the command also checks the fleet before returning zero):

```sh
aegis --config /absolute/path/to/aegis.yaml gateway start
aegis --config /absolute/path/to/aegis.yaml gateway status
```

Fresh authenticated product readback and gateway/fleet readiness are separate
acceptance gates. Physical fleet recovery neither renews authority nor retries
execution or repairs logical inconsistencies.

## Explicit orphan transport review (Linux source candidate)

When the exact configured transport is an orphan, the explicit
`authority recover-interrupted --quarantine-orphan-transport` adapter first
reviews its identity without mutation. It is not permission to stop a service,
remove a socket manually, reset state, or bypass protected principal input.
A complete kernel UNIX-socket VFS device/inode inventory must prove the socket
unbound; connection refusal alone is insufficient. A bound non-listening socket
also refuses. Missing kernel proof fails closed.

After fresh enrolled-principal authentication, the adapter revalidates the exact
configuration, principal, verifier and reviewed socket identity. It holds the
same lock used by API startup through no-replace quarantine and authority/fleet
recovery. The original inode remains at the reported `quarantined_transport`;
no automatic deletion occurs. Configuration/verifier/socket drift, unsafe path
custody, lock contention, cancellation or expired authentication refuses. The
configured authentication budget (capped at two minutes) covers the whole
operation, not a fresh budget for each store. Recovery failure after quarantine
can leave transport evidence; that path is custody evidence, not success.

This adapter is source qualification only until independently reviewed and
incorporated into a verified candidate. It does not establish live recovery,
provider admission, renewed authority, or whole-contribution acceptance.

## Already-clean authority in combined recovery

A normal authority close can precede an interrupted fleet close. After the same
fresh OS-principal/password authentication, combined recovery verifies that
authority under its exclusive maintenance lease and then recovers the fleet
under the existing single deadline. It requires exact ACTIVE/CLEAN equality,
absent DIRTY, private directories, one selected store, empty staging/retired,
strict engine-file custody, engine identity/checksums and every canonical
authority record. A marker alone is not integrity proof.

Pending recovery receipts still take their original exact resume path, including
a crash after CLEAN publication. Exact completed recovery returns its original
result. After a subsequent normal close changes engine bytes, completed history
must still belong to the authenticated principal and its retained original copies
must verify unchanged. Receipt-less evidence, damaged history and ambiguous
layouts deny; nothing is deleted or rewritten to obtain a clean result.

For a normally closed authority, `verified_clean: true` means validation only:
`recovered` remains false, no new authority evidence or receipt is created, and
ACTIVE/CLEAN and engine bytes stay unchanged. A retained prior evidence path is
custody history, not a claim that this close was physically recovered. The
authority-only application API (including its upgrade caller) still rejects an
ordinary clean close; normal Open and fresh runtime admission are unchanged.
A successful authority result printed alongside a nonzero command exit does not
mean fleet recovery succeeded.

## Exact admissible state and engine proof

A new recovery requires private owned lifecycle directories, absent CLEAN, exact
valid ACTIVE/DIRTY equality, exactly the selected store, empty staging/retired,
and no unexpected root entries. An exclusive maintenance lease excludes supported
writers. Direct Open does not bypass the dirty boundary.

The persistence parent must itself be a real, private, current-owner directory;
a symlink or writable parent is refused before evidence creation. Normal
inspection and opening remain fenced until the recovery receipt has its durable
completed outcome, even if CLEAN is already published. Resume the exact
authenticated recovery rather than opening another writer.

Before engine inspection, preserve ACTIVE, DIRTY and every selected engine file
in a private `recovery-evidence/interrupted-*` directory with `SHA256.json` recording
original hashes, sizes, modes and modification times. Copies are bounded to 4096
files and 16 GiB logical bytes, cancellable and sparse-aware; symlinks, hard links,
foreign ownership, world-write and nested engine entries refuse.

On Linux, original engine evidence alone may retain mode `0664` or `0660` on
Badger `LOCK`, `DISCARD`, numeric `.vlog` and numeric `.mem` files created under
a permissive umask. This is not a chmod repair or a general writable-file bypass.
Descriptor-relative no-follow traversal pins every ancestor and the source file;
the store must be current-UID-owned `0700`, every ancestor root/current-UID-owned,
with no unprotected group/other-writable ancestry (root-owned sticky directories
are permitted). A current-UID private ancestor protects writable descendants.
Any access/default POSIX ACL, unsupported ACL query, special mode, unknown engine
name, unsafe link count, identity drift or content/metadata mutation denies.
Only the original is admitted this way: snapshot/work copies are created `0600`,
and candidate validation retains its strict checks. Receipt resume reidentifies
the exact original/candidate byte pair on either side of exchange and separately
rechecks the candidate strictly before promotion. No markers, receipt permissions,
normal live opens, authentication bounds or writer exclusion are relaxed.

This custody proof trusts root and the current UID, as the existing local storage
model does. It cannot prove historical confidentiality or rule out prior exposure
or hostile same-UID/root processes. Suspected earlier compromise remains outside
supported recovery; current private mode is not evidence that compromise never
occurred. Originals remain unchanged and retained after exchange.

Recovery supports the pinned unencrypted Badger v4.9.5 WAL format. Independent
framing, CRC and complete-transaction proof must succeed. Only fully scanned zero
padding is removed, and only from a separate working copy. Read-only identity,
checksums and every canonical family/reference/replay are verified before writable
replay of that copy; the exact canonical key/value/version digest must match after
replay. Unknown keys, orphan records, torn/checksum-invalid logs, inconsistent
receipts/projections/outbox/audit and ambiguous layouts refuse. Revocation, expiry
and audit-delivery lag remain canonical, not converted into readiness.

## Publication, receipts and retries

Linux publication atomically exchanges selected/work directories; ACTIVE,
generation, activation and logical authority remain unchanged. Darwin builds but
recovery fails closed before mutation: no non-atomic replacement fallback exists.

The durable prepared receipt binds principal, operation ID, complete generation,
evidence and candidate digests. The selected/work pair must match exactly in one
of two orientations: original/candidate permits exchange; candidate/original
permits finishing. Matching markers alone grant nothing. After exchange, both
parent directories are synced, CLEAN is published, DIRTY removed, the root synced
and closed state read back, then the completed receipt is durably published.
`recovered: true` is never returned before receipt completion.

Receipts require canonical JSON, exact operation/principal/identity/digests,
owner-only regular single-link no-follow files, a 16 KiB bound and private parent
directories. `verified.json` and `completed.json` inside each operation directory
are immutable history; `recovery-evidence/receipt.json` identifies the latest
operation. Evidence/store is never exchanged or overwritten. Exact completed
retries return readback success. Subsequent genuine store opens may change engine
bytes: completed history does not block a new interrupted recovery, and the new
operation cannot overwrite prior history.

Both CLEAN/DIRTY can be resumed only with exact receipt and directory-pair proof.
Changed evidence, candidate substitution, unsafe custody or ambiguous orientation
refuses. On error the command returns nonzero and `recovered: false`; started
snapshots remain, including partial evidence. Preserve them privately. Missing
`SHA256.json` means snapshot creation did not finish. No automatic cleanup occurs.
Hashes are integrity bookkeeping, not signatures against a malicious OS owner.

## Verification boundary

The opt-in `TestCombinedRecoveryPublicCLI` fixture uses a built executable,
normal principal enrollment and an explicit fixture-only pinentry helper. An
actual writer closes authority normally, populates Registry, Loop, direct Queue
snapshot/submission, Preferences and Contribution control/Decision/Delivery
histories, then exits without closing fleet. The public command and retry must
preserve every logical fleet key/value, permit normal reopen and leave authority
markers and prior completed receipts unchanged. Negatives cover wrong password,
cancellation, ambiguous authority and malformed fleet publication history. This
is isolated public-CLI storage proof, not a real-provider run, positive released/
displaced-root qualification, independent review or installed/live acceptance.

Run against a source-built candidate in its real repository directory:

```sh
AEGIS_COMBINED_RECOVERY_BINARY="$PWD/aegis" \
  go test ./internal/persistence/fleet/badger -run '^TestCombinedRecoveryPublicCLI$' -count=1 -v
```

Without the environment variable this acceptance test is explicitly skipped.
The optional `AEGIS_COMBINED_RECOVERY_BASELINE` points to the prior source-built
executable and requires the original clean-authority failure before the candidate
recovers that same disposable state. No gateway or listener is started.

The bounded Queue presentation source repair recognizes legacy shared physical
families `0x70`–`0x73`: Archive history/head and presentation replay/Preferences
coexist with Contribution contracts/request/issue/control records. Recovery
requires exactly one canonical interpretation with exact key, history, head and
replay bindings. Archive additionally reuses immutable ownership and terminal
Queue/disposition checks. Preferences use the existing `0x74` head. Missing,
orphaned, inconsistent or ambiguous records deny; no records are dropped or
renumbered. Control prefix scans exclude only corroborated Preferences records.

Graph node/checkpoint families `0x67`/`0x68` now reuse the normal reopen validator.
The `0x79` Decision/Delivery collision requires exactly one canonical historical
interpretation; Delivery and Publication `0x7a` reuse normal history, slot,
root, mirror and release/handoff checks. Prefix scans exclude only Decisions
corroborated by canonical contracts, roles and continuity reservations. Recovery
does not demand live effect authority or renew historical decisions.

Isolated normal-writer histories, malformed records and abrupt-exit combined
authority/fleet recovery are exercised, with exact logical byte preservation and
unchanged prior authority receipt. This is not complete fleet qualification:
public authenticated CLI independent acceptance, positive released/displaced-root
recovery qualification and installed/live target acceptance remain separate gates.
Do not infer that an operator retry is safe from these focused tests.

Fleet Loop availability uses distinct persisted encodings: the `0x27` event
family contains canonical sealed event/audit envelopes, while the `0x28` head
family contains a raw event digest keyed by logical Loop ID. Recovery uses the
normal availability-history validator to bind each head to the final event and
validate sequence, predecessor, immutable publication and stable ownership. It
also validates event-key identity and checks histories reached from events, so a
missing head cannot hide an orphan. This is exact schema validation, not a JSON
fallback, skipped index or logical repair.

Representative synthetic fixtures write Registry, Loop publication/lifecycle/
availability, Graph, Queue claim/attempt and evidence/disposition records through
normal fleet writers, then abruptly exit without closing Badger. Recovery must
preserve every logical key/value byte and permit normal historical readback.
Tests cover malformed heads/events and retry after the authority stage has
already completed, including a subsequent fleet refusal. These are isolated
storage proofs, not authenticated CLI, real-provider or installed-target proof.

Local regressions cover populated abrupt process exit, committed revocation/audit
and exact canonical preservation; malformed/incomplete/checksum WAL refusal;
receipt custody/substitution/cancellation denial; actual retries after injected
receipt, exchange, directory-sync, CLEAN, DIRTY-removal and completion boundaries;
completed idempotency and a later independent recovery with unchanged history.
Isolated child processes also create actual Badger files under umask `0002` and
exit without close: authority recovery preserves populated canonical bytes and
original inode/mode/bytes through pre/post-exchange retries; fleet recovery covers
the additional `DISCARD` file and a controlled post-exchange receipt-resume fixture.
Real ACL, links, unsafe modes, snapshot mutation and permissive-candidate negatives
remain separate from these successful recovery cases.
Fault hooks model process interruption at completed boundaries, not hardware
power-loss or filesystem durability guarantees. Installed/live acceptance and
independent release review remain separate. Normal launch, demo, contributor and
release workflows are not recovery authorization.
