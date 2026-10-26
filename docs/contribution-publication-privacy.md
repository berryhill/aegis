# Closed publication-content privacy guard

`delivery.PublicationPrivacy.Review` is installed by the owning command setup
helper. Local Commit does not consult it. Remote readiness is still pending.

The guard verifies the existing sealed contribution contract, exact digest/run,
repository name/ID and canonical GitHub issue provenance using contribution.Marshal.
The sealed base commit equals Plan.BaseHead, not Plan.Base (the branch name).
The sealed effect and current narrower delivery mode must permit the exact
operation; no-merge permits publication and PRs but not merge, while no-publish
and local-only prohibit remote publication. There is no new audience flag,
per-effect consent, model waiver or exception list. Scope validation never grants
effect or credential rights. Fresh source-owner instruction review, authenticated
acceptance, CheckEffectAs and exact credential custody remain independent ceilings.

ValidateProjectedIntent, validateGraph, treeEntries and authoredObject bind the
exact projected delta, commit, object graph and metadata. Every uploaded blob,
including unchanged base blobs, is inspected; history outside that graph is not
scanned. The accepted Source still owns frozen inventory, staged allowlist and
independent acceptance validation. This guard reads no private runtime state,
ambient secrets, filesystem or environment.

Known private path components (.env and variants, .git, .ssh, .aws, credentials,
.aegis, .hermes, .gnupg, secrets and private key file suffixes) are denied. Ordinary
source names such as session.go, approval.go and secret-scanner code are not
private merely because they contain domain words. Bare private_key identifiers
are not key material. Recognized PEM private-key headers, full-length credential
prefixes and credential assignments are denied without reporting their values.
This bounded detector cannot prove the absence of unknown secrets.

Bounds remain 2,048 objects, 16 MiB graph, 512 KiB per object, depth 32, bounded
metadata, two encoding layers (hex/base64/URL), shared work budget and cancellation.
Invalid UTF-8/binary, unsupported graph types/modes, unsafe paths, projection drift
and existing closing-keyword policy remain denied. No untrusted allowlist exists.

Tests exercise the real Review and closed loopback GitHub adapter, real custody
Use callbacks, all exact Git object upload APIs and feature-ref publication.
Every publication-negative fixture first proves successful publication using the
same valid sealed baseline, then verifies no further custody or HTTP access.
Synthetic intake/authority/credentials are test fixtures, not live qualification.

Cold original-policy reconstruction and productive credential wiring remain
separate. Core #377 readiness/startup integration, #379 joined disposition,
independent frozen-candidate acceptance, live provider and actual protected-target
GitHub delivery are not established by these tests. No root/config/core or #379
changes, real GitHub calls, credential provisioning or deployment are included.

Launch impact: focused privacy/security and startup documentation updated. No CLI,
license, release artifact, demo, quickstart or installation behavior is changed;
full launch qualification remains pending.
