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
wrong principal, or a present protected Unix socket refuses. The command opens no
full service, fleet store, credential vault, runtime or listener. Authentication
freshness is bounded by configured TTL, capped at two minutes, and checked before
publication boundaries. A retry requires fresh authentication.

Only after zero exit status and JSON `recovered: true`:

```sh
aegis --config /absolute/path/to/aegis.yaml gateway start
aegis --config /absolute/path/to/aegis.yaml gateway status
```

Fresh authenticated product readback and gateway/fleet readiness are separate
acceptance gates. Recovery neither renews authority nor repairs fleet-v1.

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
foreign ownership, writable-by-others files and nested engine entries refuse.

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

Local regressions cover populated abrupt process exit, committed revocation/audit
and exact canonical preservation; malformed/incomplete/checksum WAL refusal;
receipt custody/substitution/cancellation denial; actual retries after injected
receipt, exchange, directory-sync, CLEAN, DIRTY-removal and completion boundaries;
completed idempotency and a later independent recovery with unchanged history.
Fault hooks model process interruption at completed boundaries, not hardware
power-loss or filesystem durability guarantees. Installed/live acceptance and
independent release review remain separate. Normal launch, demo, contributor and
release workflows are not recovery authorization.
