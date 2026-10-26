# Contribution startup composition: bounded local implementation

`internal/config/contribution_delivery.go` defines an explicit typed trusted policy:
actor, separate Git author/committer, required CI/review/post-merge checks, merge
method and protection-policy identity. The optional credential reference contains
only the existing deployment/Agent/stanza/scope key. It contains no secret, binding
grant, approval store or model-selectable request fields. Nil means disabled;
malformed non-nil policy is rejected. Required lists reject duplicates.

`internal/command/contribution_delivery_setup.go` composes the publication owner
and controller on the SAME application Service after Fleet/credential setup. It
resolves a configured key using the existing credential Repository.Resolve, never
copies a grant from configuration and never reads/decrypts a value at startup.
Local-only construction requires no configured remote binding or credential store;
an empty container exists only to meet the existing controller factory contract.
The subject callback must read fresh existing request authentication; startup does
not invoke it and does not authenticate, mint authority or extend TTLs.

## Explicit integration gate

Root/config central ownership is pending peer agreement. The new helper currently
accepts its typed policy argument directly: Config has NOT been extended and the
normal root constructor does NOT yet call the helper. Helper tests are NOT normal
constructor acceptance or a trusted normal-loader roundtrip. Do not advertise
production setup fixed until the central owner integrates both and supplies actual
normal-constructor tests. A request-scoped authentication reader must be wired at
the existing authenticated entry boundary; a fixed startup subject is invalid.

## Remote is unavailable, not awaiting invented privacy approval

The existing delivery.Privacy contract requires exact public text AND every
committed path/byte to be reviewed against audience. The GitHub adapter already
checks exact projected intent, closing-keyword boundaries and bounded text, but
those checks alone are not complete privacy assurance. The helper now installs
`delivery.PublicationPrivacy`, which validates the existing sealed publication
scope and exact accepted projection, then inspects all uploaded content. Safe
permitted projections can pass this guard without new approval facts. See
[publication privacy](contribution-publication-privacy.md). Cold credential policy
and productive startup integration remain pending independently.

Canonical delivery history currently retains the policy DIGEST, not the complete
original ContributionDeliveryPolicy needed by cold reconstruction. The helper
returns typed `contribution_original_historical_policy_unavailable` rather than
substituting current/default policy. Completing restart reconciliation requires
canonical original-policy persistence/reload with exact digest verification; no
new approval authority may be created. This gap is separate from local Commit.

Remote acceptance additionally requires exact actor/credential binding identity,
qualified publication-content privacy review, original historical policy reconstruction,
actual remote exact-head checks/review/protection/merge/post-merge readback and
retained repair/readiness budgets. #379 disposition remains separately owned;
`completed` remains false and the issue stays open.

Launch impact: this focused doc records new local-only helper semantics and the
remaining gates. No installation, release, binary, public command or runtime
capability claim is changed. Broad launch-asset qualification was not performed.
