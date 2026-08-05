# Full MVI Gap Ledger

## Baseline and custody

This ledger freezes the repository state after Track A and before post-Track-A product closure.

- Final Track-A source: `d9705af5ceecc937c3e4db149072a123f437c9f5`
- Track-A pull-request chain present in that source: PRs `#9` through `#18`
- Current implementation task: `t_a27c96b`
- Current flow: `aegis_implementation_loop`, session `sess_c15dbaf1c2ed`
- Installed-evidence mechanism: `scripts/verify-installed-mvi.sh`, invoked by CI and release workflows; current execution evidence must be recorded by the verification step and is not implied by this document.

Status terms are exact: **fixed** is present in the source baseline; **changed** means the prior finding was replaced by a narrower contract; **open** blocks the claimed finish line; **deferred** is outside this MVI and must not be marketed as implemented.

## Track-A findings

| Finding | Status | Current evidence | Remaining boundary |
|---|---|---|---|
| Persistence contract described only bbolt even after session authority moved to Badger | changed | `specs/STORAGE.md` and `internal/persistence/qualification/contract.go` define separate Badger authority and bbolt credential planes | New engines/platforms remain unqualified |
| Authority records could be duplicated or mutated through a broad aggregate | fixed | `internal/core/authority_repository.go`, canonical record codecs, append/replay transition facts, and removal of experimental plumbing surfaces | No distributed authority or consensus claim |
| Authority store activation and crash evidence were underspecified | fixed | generation lifecycle, synced markers, strict `ACTIVE`/`CLEAN`/`DIRTY` handling, maintenance tests, PRs `#11`, `#15`, `#16` | Qualification is bounded to the matrix in `specs/STORAGE.md` |
| Authority writes and indexes could diverge | fixed | atomic Badger repository transactions and full transition replay/root verification, PR `#12` | Cross-engine atomicity is not claimed |
| Audit delivery could be inferred from an in-memory projection | fixed | durable metadata-only outbox, exact-prefix verification, readiness denial, PR `#14` | Not an external transparency service |
| Manager/model process identity could be substituted or reused | fixed | pidfd-bound inference proxy and lifecycle custody, PRs `#13`, `#16` | Host sandboxing and separate OS-account custody remain out of scope |
| Architecture tests covered selected inward imports but not dependency versions, engine ownership, schema ownership, or package-family additions | changed | executable checks in `internal/architecture/boundaries_test.go` | The classified-family registry requires review when a new top-level family is added |
| Release success could be claimed from source tests alone | fixed | repository-owned installed verifier added in PR `#18` | A release artifact still requires exact-head CI and checksum evidence; this ledger fabricates neither |
| Canonical facts, projections, blobs, operational metadata, runtime state, and credential custody were not normatively separated | changed | `specs/STORAGE.md` state-class contract | Ordinary filesystem records are not promoted to a general transactional engine |
| Model or Hermes narration could be mistaken for authorization/evidence | fixed | authority, admission, approval, provisioning, audit, and verification remain Aegis-authored; specs and denial tests enforce this | Independent attestation is deferred |

## Open product-closure findings

| Finding | Status | Risk / required evidence |
|---|---|---|
| Full release qualification on Linux/arm64 or macOS persistence | open | Release archives exist, but storage qualification is Linux/amd64/ext4 only. Do not equate cross-build with storage qualification. |
| Complete backup/restore across canonical filesystem state, Badger, bbolt, audit checkpoints, and host key custody | open | Per-engine backup features do not prove a coherent full-system restore. |
| Separate-process or separate-account audit authority | deferred | Current audit authority is same-process/same-account local storage. |
| Host filesystem/network confinement for Hermes | deferred | Disposable homes and safe mode are process/runtime-state isolation, not a sandbox. |
| Generic credential retrieval or arbitrary authenticated proxying | deferred | The MVI exposes only the typed, allowlisted GitHub metadata operation. |
| Multi-principal, federation, mandate delegation, and cross-stanza information flow | deferred | These require new authority protocols and cannot be inferred from current records. |
| Installed proof for this exact task head | open | Step 10 must run the focused/broad suite and installed verifier from the exact final head before merge. |

## Change rule

No row becomes fixed from prose, a green unit test, a generated artifact, or model narration alone. Closure requires code/system evidence, exact-head verification, and an updated row with the governing source, test, PR, and installed result. A changed claim must update this ledger, the normative specification, executable qualification/architecture checks, and affected launch assets in the same change.
