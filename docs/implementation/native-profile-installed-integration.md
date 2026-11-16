# Installation-bound Hermes native provider integration

## Scope and current status

Aegis now uses Hermes's supported **installed dependency activation** for both
native gateway turns and the bounded native account parser. This is an additive,
content-bound qualification, not eligibility for every Hermes 0.23.0 installation.
The original Hermes 0.21.3 five-source record and the separate one-POST
`ProviderFormatProbe` record are unchanged. The latter still denies this modern
runtime; ordinary bounded same-account catalog requests are not that diagnostic.

The accepted Hermes source base is
`5a52faf1aed965b8692ec890a7d8af6e37b5e59e`, with the explicitly retained
`agent/codex_headers.py`, `agent/runtime_session_store.py` and
`hermes_cli/gateway_client.py` source overlays. The latter two retain the existing
64 MiB owner-link/event-buffer ceilings. Qualification binds their actual bytes,
not the Git decoration or a claim that the checkout is clean.

The compiled controller policy is
`internal/runtime/hermes/qualification/installed-codex-v1.json`. It contains two
exact tuples: the operator's committed PM generation and an independently
PM-created isolated reproduction with identical current source, interpreter,
input locks and resolved workspace locks. It is never loaded from user config,
a request, a native profile or an arbitrary generation allowlist.

Shared source namespace SHA-256:
`e8d0dcb2debaf7e0cba5f82029a0295d87d019f73e0aaea404d3c250d698ff61`.
Python executable SHA-256:
`3642edb46c7bfad367e99751c422ca6b57977157b78f8f77ee4daa9498c002b5`.
The verified PM Python package digest is
`7bbc7426f297f1a14c27d94514f92fab081b02896e9218c8666e6a6af497765a`.
Both use Python 3.14.9 / `cpython-314-x86_64-linux-gnu`, OpenAI 2.54.0 and
Pydantic 2.14.1. The complete executable dependency namespace, including `.pth`
and editable-finder code, is hashed rather than trusting these version labels.

| Tuple | Dependency namespace | PM workspace source namespace |
| --- | --- | --- |
| Operator-installed | `be4e0361892b9395ed6b525ccbaad1d41442bd0b7b40a244a45f6c98cff53518` | `4c0b37639ff574f5f6b98ae2dfa74e49dd7c15315a106af672830f4fc9695b27` |
| Isolated PM reproduction | `8b5e5bce1906076dc9c04edfb6f80e4f69c915cffb9af359bd0688d79604c7f6` | `a41a414aa8e9e4c9d467c03063f15c4b3faa484c15fb21da622e809010559d15` |

The dependency inventories differ only in the editable finder's exact namespace
listing and `uv_cache.json` build timestamp. Those differences are **not ignored**:
each tuple separately binds its dependency bytes and PM workspace source. The
operator generation predates the source update, so its editable fallback snapshot
also has a distinct exact identity. Only installation-path occurrences in the
known Hermes finder and launcher are normalized. Installer `RECORD` and
`direct_url.json` receipts and bytecode caches are excluded; other executable
source/dependency changes deny. This is trusted-host drift detection, not
confinement against root or a hostile same-UID writer.

## Public launch contract

Gateway command discovery uses:

```text
<hermes> --print-runtime-command --installed-dependencies --module tui_gateway.entry
```

Native parser discovery uses controller-owned code, not caller Python:

```text
<hermes> --print-runtime-command --installed-dependencies --code <controller-code> -- <installation> <mode> <selector>
```

Aegis validates and executes the complete returned JSON argv, preserving the
installation bootstrap, argument boundary and approved auth descriptor at FD3.
It never strips the bootstrap to reuse only Python, borrows a native profile,
fabricates PM facts, synchronizes a per-session package environment, enables lazy
installs or falls back from an unsupported modern tree to a legacy interpreter.

The query runs in a private credential-free home. Before native auth is opened,
a real credential-free `--code pass` activation validates the committed generation,
manifest and ABI. Hermes holds its normal source-read lock and generation GC
lease for each actual child lifetime. Source, interpreter, launcher, dependency
inputs and selected PM metadata are rechecked before execution. Native public
inspection/preview/resolution repeats exact production qualification after command
discovery and before opening the no-follow, owner-checked auth descriptor.

The bounded parser has a ten-second child budget, bounded private output,
closed diagnostics, inherited-FD custody and joined process-group cleanup. A
nonexistent HOME is supported for that code child. Query and session state remain
private; owner-nested disposable session homes are supported without moving live
state or relaxing the owner/native-profile rejection rules. Cancellation releases
kernel lease custody even when a killed child leaves an unlocked lease filename
for normal PM pruning.

Native entries are decoded by the actual Hermes `_load_auth_store` and
`_provider_state_in` APIs. Aegis returns only closed metadata and opaque IDs,
selects the exact approved local OAuth pool entry/account and never refreshes or
falls back to singleton/global/external credentials. An owner refresh of the
same selected entry/account may be used after fresh expiry/source validation.

## Exercised evidence

Private evidence is under `.aegis-e2e/native-profile-installed-integration/`.
`installed-equivalent/` is the final isolated PM-created owner, not copied native
state. Its PM build uses the operator generation's exact extras and a copied
**dependency package source snapshot**, not native profile configuration/history.
No synthetic stamps, activation manifests or copied native auth are used.

- Read-only validation of the actual operator-installed tuple passes the compiled
  production gate. Unknown versions and the mismatching legacy record deny.
  This test performs no subprocess, native auth/config/history read or lease write.
- The isolated unchanged source passes the production gate with **no test
  qualifier**. Public native candidate inspection, account preview and resolution
  use the real parser and a synthetic FD file through supported code commands.
- Valid/exact selection, ambiguity, expiry, borrowed/external/unknown status,
  missing selected entry, account drift and same-entry owner refresh are checked;
  native auth bytes remain unchanged. The complete public module command and
  a real code child demonstrate actual SDK imports and a held PM lease without sync.
- Missing/malformed activation metadata and changed dependency bytes deny.
  Activation failure precedes auth custody. Cancellation kills the actual bounded
  child and releases its real PM lease. Negative dependency tests replace an inode
  in the isolated fixture, never edit a potentially shared package-cache hardlink.
- Actual `AttemptTurn` runs through native gateway, AIAgent and SDK with a synthetic
  HTTPX vendor boundary in a networkless mount namespace. Complete/default-model,
  exact per-node model, truncated terminal, provider 500, cancellation and
  unavailable-tool correction cases pass. Immutable authority, exact account/model,
  zero native tool effects, actual generation lease, no sync and process/home
  cleanup are asserted. The observer changes one entry module only; its separate
  exact test-source record is **not** compiled policy. Uninstrumented production
  qualification is proven separately above.

These tests do not claim live-account entitlement, real inference, installed Aegis
activation, verified task artifacts or Loop/Graph/Queue success. In particular,
`persisted_turn.complete` remains transcript coverage, never the success oracle.
The parent/operator owns publication, authenticated account decisions and the
requested deployment/acceptance on 8448. No installed runtime, profile or service
was changed by this Aegis integration worker.

## Launch impact and remaining gates

README, security, threat-model/architecture, quickstart, contribution guidance,
Doer-loop guidance and current-status pointers are updated. Historical launch
investigations are retained with a superseding pointer rather than rewritten as
successful installed evidence. LICENSE, CODE_OF_CONDUCT, the architecture diagram,
no-key demonstration, recording sources, release/checksum assets and contributor
backlog are unaffected in scope; no new recording, release or issue is claimed.
Fresh installed/no-key/release acceptance remains the parent delivery gate.
The parent must add the corresponding CHANGELOG entry and synchronize the
source skill's older 0.21.3-only wording; this worker does not overlap those files.

Normal/race targeted runtime tests and vet pass on Go 1.26.11. A direct command
build passes. The existing `scripts/build-source.sh` rejects this linked-worktree
build because Go omits VCS settings (`vcs expected git, got missing`); this is
reported, not bypassed or supplied with fabricated metadata. Its raw check binary
is not a deployable artifact. Parent publication/build verification must resolve
that packaging/provenance gate before installation.
