# Native controller Codex connection candidate — launch review

## Delivered contract

The owning daemon now exposes typed `controller-codex candidates PROFILE_HOME`,
`preview PROFILE_HOME [--credential ID]`,
`connect REVIEW_ID` and `show`, using the established `--config OWNER_CONFIG
--target CONSOLE_URL` Unix transport. Account identity is resolved by the exact
qualified native Hermes APIs, never supplied by a caller. Fresh kernel peer
principal authentication authorizes this operational connection; no new human
approval ceremony or foundational authority grant is invented. Review identity,
expiry, account revalidation and exact reviewed configuration checks are mandatory.

The daemon never writes the owning config. It publishes a private mode-0600
`OWNER_CONFIG.controller-codex.json` operational sidecar under an owner-only
parent, binding exact config SHA-256/source/optional credential ID/account/principal without credentials.
Descriptor-anchored atomic no-replace publication, fsync and typed readback deny
existing-record conflicts. Every show/runtime resolution revalidates the sidecar,
principal and config digest; drift denies without fallback. Legacy explicit YAML
bindings work without a conflicting sidecar. Startup Config stays immutable.
Lost responses require `show`, not replay; a drifted inert record is retained for
reconciliation, never automatically overwritten or rolled back.

Independent testing reproduced silent loss with a delayed final config rename:
a same-UID operator comment written after the final check disappeared while connect
returned success. This was not CAS. The fix removes config mutation entirely;
no advisory lock or claim of perfect same-UID CAS is made. Concurrent config edits
remain untouched even when they arrive during sidecar publication; readback denies
observed drift and future runtime resolution always rechecks it.

Native parsing remains read-only and offline: no refresh, imports, automatic
account/pool selection, root fallback, source writes, environment credentials or
copied login. An explicitly reviewed canonical shared-root source is supported,
never as fallback. Metadata inspection returns at most 64 candidates with opaque
IDs (domain-separated digest of bounded explicit native entry IDs), account
fingerprints, distinct eligible grant count and closed eligibility reasons. It
returns no raw IDs, labels, native exceptions, source JSON, token fingerprints or
credentials and invents no native current selection. Inspection is non-mutating.
Explicit `--credential` selects only that pinned native ID; missing/duplicate/
replaced IDs and account drift deny through preview/connect/runtime. Same-entry
same-account valid native refresh outside Aegis is allowed. The existing review
still expires, is single-use and binds current subject/source/ID/account/config.
The approved profile-local pool is decoded before singleton evidence; no singleton
is required. One unique local native OAuth grant and exact aliases are eligible;
distinct grants deny ambiguity without an explicit exact-entry selector. Borrowed/global/external
custody references return a closed source_approval_required diagnostic, without
reading their sources. Missing/suppressed/unusable/non-OAuth sources deny. Hermes 0.21.3 and the exact original five fingerprints are unchanged.
No Agent tool/scope or charter/Loop/Run/host-write authority is added.

## Verification

`TestControllerCodexOwningTransport` exercises a real daemon, actual Unix peer
identity, real CLI, installed qualified Hermes parser and synthetic-only stores.
It proves preview, stale/unknown/caller-authored/foreign-source/changed-account
and changed-config denials, connection, exact readback, same-daemon resolver
adoption, preservation of an exact published Loop and absence of Graph/Queue.
The same owning transport fixture exercises candidates, two distinct accounts
without a default, exact CLI preview/connect/show, persisted ID readback, replaced
ID denial during review and resolution, account drift, and same-entry refresh.
It may use `AEGIS_TEST_CONTROLLER_CODEX_BINARY` for the exact frozen executable;
otherwise it builds its isolated CLI. Native selection regressions additionally
cover duplicate/missing/invalid IDs, unsupported type, dead/unknown status, expiry,
borrowed/external/suppressed sources, 65-entry denial, token/label/raw-ID canaries,
source-byte preservation and distinct grants for the same account.
Config regressions assert exact owning-byte preservation, publication-boundary
operator-edit preservation/denial, no-replace conflicts, digest drift and private
record/parent/symlink/hardlink/unknown/competing-binding denials. Native regressions cover expiry, mismatch,
malformation, missing local store/pool, pool-only success/aliases, stale singleton,
account pin/expiry, distinct usable identities, mixed/dead/borrowed/external pools.
Pool-only RED failed with doer_provider_auth_absent before the fix. The owning
transport regression now uses pool-only native OAuth data through API/CLI preview,
connect/readback, and same-daemon resolution; preview is metadata-only.
Application regressions deny foreign/expired/stale/future/agent/unknown subjects.
Initial RED exposed staging extension and YAML indentation defects; both fixed.

These fixtures do not prove a live provider login, human foundational approval,
or execution of the owner's retained Run. No live credentials/config/service or
published records were read or changed. Parent owns live connection and same-Run
retry after independent testing. No successor Loop, output artifact, publication,
commit, flow record or Git configuration was created or changed.

## Launch assets

Updated DOER_LOOP documentation, credential/router skills and operation matrix.
README and SECURITY receive a narrow source-candidate pointer/boundary note.
Reviewed LICENSE, CONTRIBUTING, CODE_OF_CONDUCT, threat model, architecture,
quickstart, no-key demonstration/recording and CHANGELOG as otherwise unaffected:
no-key path, authority boundaries and release status are unchanged. No release
binary/checksum or contributor issue publication is authorized or claimed.
The `.candidate/native-codex-selection` directory holds focused race-test output,
source inventory (tracked/untracked/modes/deletions, excluding `.candidate`) and
executable identity for independent testing. Earlier candidate proofs remain
preserved in their original directories. No further source writes after freeze.
