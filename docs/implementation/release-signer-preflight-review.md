# Fresh release signer-policy preflight

## Reproduced defect and repair

Fresh preparation signed a disposable tag but did not verify that its signing
key matched the configured trusted-principal policy. A cryptographically valid
signature from another configured key could therefore create and push a fresh
release tag, although recovery correctly denied that same signature later.

The disposable preflight tag now passes `git verify-tag` before any real release
commit is created. The final freshly signed tag is independently verified again
immediately before atomic push. Preflight failure restores the prior changelog
and changes no real refs. A later final-signature failure preserves the local
commit/tag for inspection without pushing; no existing tag is moved, replaced,
deleted or re-signed. Existing signer enrollment and allowed-signers policy are
not broadened. Signed-tag recovery remains unchanged.

## Evidence

The new bad-policy regression failed against the old code because release
unexpectedly succeeded. With the repair, the complete release-recovery fixture
suite passes, including exact source/ref snapshots for preflight failure and
no remote push after final signer drift. Twenty related Python integration,
evidence, publication and pipeline tests also pass. Fixtures use synthetic
signatures and disposable local bare remotes, not production signing keys.

The v0.2.27 release run was cancelled before publication; its immutable tag is
preserved. This correction does not authorize re-signing or replacing that tag.
The owner separately selected v0.2.28 with the existing trusted signer for the
actual publication/timing acceptance. That remains pending until verified live.

## Launch impact

Updated CHANGELOG.md, CONTRIBUTING.md, SECURITY.md and docs/RELEASE_PIPELINE.md.
Reviewed the inherited fast-release launch matrix: root README, LICENSE,
CODE_OF_CONDUCT, architecture diagram, threat-model source/runtime boundaries,
five-minute quickstart, no-key demo, recording/docs/assets and contributor issue
backlog are unaffected by this verification-only strengthening. No runtime,
model, gateway, charter or credential interface changes are included. Binaries
and checksums remain release output requiring actual workflow/readback evidence;
no release success, all-platform native execution or live Doer success is claimed
from these fixture tests. Exact-head CI and real publication timing remain gates.
