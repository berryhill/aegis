# Private original-engine recovery: local review

## Scope and security decision

This is a local source candidate, not installed/live recovery. No production state,
ports, credentials, service, commit, push or deployment is part of this change.
The owning initialization/command authentication and fixed expiry, authority
maintenance lease and fleet writer lock are unchanged.

Badger v4.9.5 creates LOCK and mmap-backed logs under process umask; a real `0002`
child leaves `0664` LOCK, `.mem`, `.vlog`, and (on first fleet creation) DISCARD.
Only these named original engine forms receive group-write admission. Other
unsafe modes/names still deny. No original is chmodded or replayed in place.

The Linux source reader pins no-follow directory descriptors from `/`, verifies
root/current-UID ownership and absence of access/default POSIX ACLs, denies
unprotected writable ancestry, and requires a current-UID `0700` leaf. It opens
single-link regular engine files descriptor-relatively, checks ACLs and exact
allowed modes, then verifies named/open identity, ownership, size, mode, mtime,
ctime and retained ancestry after byte-authenticated copy. Reads may update atime.
Directory inventories additionally reject entry/identity/change-time drift.

Root and same-UID processes remain trusted, not sandboxed. This cannot establish
historical confidentiality or prove that no earlier exposure occurred. Suspected
historical compromise is not qualified by this procedure. The existing qualified
OS/filesystem/storage envelope is not broadened. Non-Linux admission denies.

The receipt pair is classified by exact original/candidate bytes, then the
candidate is separately checked through strict inventory. This also handles equal
byte digests without promoting a permissive original. Controls, strict copies,
normal opens and canonical/WAL validation are not relaxed. Completed fleet history
can admit a later interrupted original without discarding the earlier evidence.

## Local evidence and independent verification

Builder checks use `GOTOOLCHAIN=go1.26.11 GOMAXPROCS=2 GOFLAGS=-p=1
GOMEMLIMIT=512MiB` and disposable test state. Reproduce:

```sh
go test ./internal/persistence/recoveryphysical ./internal/persistence/authority/badger ./internal/persistence/fleet/badger -run 'Test.*(Recovery|RecoverInterrupted|OriginalEngine)' -v -count=1 -timeout=120s
go test -race ./internal/persistence/recoveryphysical ./internal/persistence/authority/badger ./internal/persistence/fleet/badger -run 'Test(OriginalEngine|RecoveryPair|InterruptedRecoveryPrivateUmask|FleetRecoveryPrivateUmask)' -v -count=1 -timeout=120s
go test ./internal/initialize ./internal/command -run 'Test.*(AuthorityRecovery|RecoveryPassword|RecoveryPrincipal)' -v -count=1 -timeout=120s
go test ./internal/architecture -count=1 -timeout=90s
go vet ./internal/persistence/recoveryphysical ./internal/persistence/authority/badger ./internal/persistence/fleet/badger
```

Real umask child tests retain inode/mode/byte equality of originals, populated
canonical authority bytes and fresh live-open denial, plus authority fault/resume
before receipt, before exchange, after exchange and after CLEAN. Fleet covers
DISCARD, a test-only post-exchange receipt interruption and a later new crash.
Negative fixtures exercise actual Linux ACL metadata, symlinks/hardlinks, public
leaf/world-write/executable modes, unknown/control names, permissive candidates,
and controlled source mutation during copying. Existing tests retain invalid WAL,
receipt, cancellation and wrong-principal denial. Mutation callbacks are bounded
fixture injection, not a hostile-process/same-UID confinement claim.

Original baseline recovery implementations, overlaid without modifying the source
worktree, fail the same new real-umask tests at `unsafe interrupted authority
evidence source` and `unsafe recovery file`. The first combined baseline selector
selected only fleet; a separate exact authority selector establishes authority RED.
Retained logs distinguish that selection error from successful coverage.

The linked-worktree build helper denied missing Go VCS metadata, including with
an explicit per-command worktree binding. No guard was bypassed. Use a standalone
private clone of the exact base with the complete byte/mode-identical dirty source
overlay for candidate build/independent acceptance. Build/provenance, source
manifest and full patch belong in the task's private evidence packet, not a false
clean-source claim. No publication or release artifact is implied.

## Launch-asset impact ledger

- **Changed:** CHANGELOG, SECURITY, threat model, normative STORAGE contract and
  AUTHORITY_RECOVERY procedure: narrow original-only admission, retained strict
  candidate/control gates, trust limits and actual local fixture scope.
- **Reviewed unchanged:** root README's existing recovery link and fail-closed
  claims; LICENSE; CONTRIBUTING's isolation/maintenance requirements;
  CODE_OF_CONDUCT; architecture Mermaid diagram/topology; QUICKSTART; DEMO_NO_KEY;
  RECORDING and retained timing/typescript; contributor ISSUE_BACKLOG; release
  build/checksum scripts. No CLI flags, demo flow, runtime topology, licensing,
  conduct policy, packaging or contributor issue lifecycle changes.
- **Not requalified:** historical recording, real no-key/provider demonstration,
  remote release binaries/checksums and installed/live acceptance. No remote
  issues/releases were created or read as proof. Fresh candidate build/checksum is
  development evidence only.
- **Existing launch blockers remain:** owner-designated conduct-reporting route,
  historical rather than exact-candidate recording, and broad legacy backlog text
  still listing dirty-store recovery among follow-on work. This narrow repair is
  not a launch-readiness audit or permission to rewrite unrelated history.

Independent tester review of the frozen source/binary and authenticated full
combined command journey remains required. Persistence fixtures and the unchanged
command authentication negatives are not installed recovery, fleet execution,
provider qualification or a demonstration that the reported live store is safe.
