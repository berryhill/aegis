# In-place upgrade verification

The supported user action remains `aegis update` (or the strict root alias
`aegis --update`). `--check` is read-only. An installed Linux gateway upgrade
is not complete merely because the executable was replaced.

## Implemented contract

1. Prepare the official stable archive/checksum, validate its executable/version/
   source claims, and seal target/candidate path/inode/mode/digest before downtime.
2. Revalidate exact installed/loaded service identity and authenticate the configured
   OS principal plus enrolled password through existing protected input. Input
   cancellation/unavailability is not consent. Recheck sealed bytes after input,
   before any unit migration or stop, and again before replacement.
3. Migrate only exact recognized historical owned-unit bytes. Bind original and
   desired digests before atomic publication; reload replay after interruption is
   safe. Rate-limit restarts and reserve separate bounded HTTP-drain/store-flush
   time. Foreign units remain denied.
4. Stop only the exact owned service, install verified bytes, inspect qualified
   closed authority and invoke existing authenticated evidence-preserving recovery
   only through its strict admission. On Linux, freshly re-admit the exact stopped
   installed/loaded plan and MainPID=0, then hold the API startup lifecycle lock
   through authority validation. A private operator-owned orphan socket is renamed
   without replacement to `<socket>.upgrade-orphan-<device>-<inode>` beside the
   original; evidence is never deleted. Complete all-states kernel VFS diagnostics
   must prove it unbound (including relative/alias non-listening binds). Unsafe
   locks, changed identities, unavailable diagnostics and foreign services deny.
   An absent socket still requires the same lock and stopped-service admission.
   If a never-started gateway has no transport directory, prepare only its
   configured missing directories mode 0700 beneath an owned non-writable
   anchor after fresh admission; unsafe anchors and cancelled requests deny.
   This is current-network-namespace maintenance, not same-UID/root confinement
   or reset authorization. Mismatched CLEAN/ambiguous state refuses.
   Cached authentication cannot extend recovery past its original expiry.
5. Activate, verify the current process inode against the installed release digest,
   require authenticated audit-current readiness, verify authenticated Console
   rendering/data/assets and an owning-service Agent read, and revoke the probe's
   own Console session. Recheck audit readiness after probe audit events.
6. Persist completion only after those checks. Output distinguishes `updated`
   from `upgrade_complete`, `resume_required`, running version/revision, current
   audit and Console evidence. CLI-only/Darwin results are explicitly binary-only.

The private canonical no-follow/single-link journal is operational evidence outside
`authority-v1`, never authentication. Resume reobserves real state and preserves
exact intent/recovery evidence; it does not trust a stage or assume old-binary
rollback is safe after state effects. Unknown-schema migration is not fabricated.

Candidate identity and admission share one extractor over the same sealed byte
buffer. Explicit linker claims must be complete and unambiguous. Go release
builds with `-trimpath` may omit `-ldflags` from build metadata; only in that
absence, an exact release-tagged main module plus a clean Git revision (40
lowercase hexadecimal characters) supplies version/source claims. Explicit bad
linker claims never fall back. These remain checksum-bound claims, not signatures.

## Evidence layers

Source regressions cover ordinary and already-stopped gateways, actual binary
replacement, eligible interrupted authority, invalid CLEAN/socket/open-writer
refusal, protected-input cancellation/unavailability, verifier/configuration drift,
retained authentication expiry, journal tamper/link/identity conflict, failed
migration/startup and same-intent resume, stale image, non-Aegis HTTP 200/fallback,
broken authenticated rendering/data/assets, and probe revocation on malformed
login bodies. Full-command tests launch real isolated Aegis gateway processes;
only service-manager metadata is synthetic. They exercise production Unix/API,
audit, HTTP authentication/rendering and Linux executable identity.

Doer coding execution and its partial outcomes are retained privately. Model/file
verdicts are not acceptance. Parent review reproduced/fixed candidate substitution
and authentication-window defects with RED/GREEN tests and independently reran
packages, races, architecture/skill checks, build/vet and the bounded repository
stage. Hosted exact-head/full installed packaging remains a separate gate.

## Launch impact

Updated README, quickstart, architecture/diagram, security policy, threat model,
changelog, operator lifecycle skill and canonical skill manifest. LICENSE,
CODE_OF_CONDUCT, no-key demonstration, historical recording/guide and contributor
backlog were inspected as unaffected by this upgrade-only feature. Public release
archives/checksums require actual publication verification. No new remote issues,
model/provider changes, authority reset or unrelated infrastructure are included.

## Live acceptance boundary

The owner separately authorized release, supported workstation update and exact
recovery/activation. Actual installed/running provenance, authenticated readiness
and configured Console/browser readback are still required before claiming live
completion. A headless agent connection is not a human terminal or protected GUI;
missing protected input must remain an exact independent gate, never a fabricated
password/confirmation or permission bypass. No fixture result proves live recovery.
