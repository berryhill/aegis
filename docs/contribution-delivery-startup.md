# Contribution startup composition: bounded local implementation

Publication lifetime cleanup and terminal/restart custody handoff use the same
owning service; see [#423 custody and recovery](contribution-publication-custody.md).
No additional setup, credential, grant or service is introduced.

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

## Implemented startup source and request provenance

The agreed central seams now exist in source: Config has an optional strictly
validated `contribution_delivery` policy, and the normal root service factory calls
the helper after Fleet/credential composition. The installed subject reader is
`Service.CurrentContributionDeliverySubject`, not a fixed startup principal.
Public-loader regressions cover an absent policy, a valid roundtrip, unknown
fields and invalid policy. These source changes are not installed-target proof.

The request reader revalidates password subjects against the owning live Console.
It does not treat a manager session inserted by `Open` as authentication.

Run and retained Resume now keep one registered service-owned invocation through
native candidate execution, delivery and canonical disposition. A dropped HTTP
response does not cancel that admitted invocation or require another Continue.
The standalone candidate execution API retains its candidate-only boundary;
manager Resume uses the joined journey instead of releasing ownership between
native continuation and delivery. Explicit Stop still cancels and joins the same
registered owner; transport detachment does not refresh the original subject,
mandate, role, deadline or cumulative budget. Per-effect admission remains live.
Terminal navigation retrieves existing verified results without dispatching work.

The protected Unix middleware now retains the existing SO_PEERCRED authentication
in a private service-bound request context. Delivery entry points validate this
proof against the original manager subject without renewing its timestamps or
hard expiry. A supplied Subject alone, another Service, cancellation, changed
identity/claims, or expired authority denies. Password subjects retain their
method and require the owning live Console registry, including revocation.

The actual production-factory/Serve HTTP fixture passed normal-mode controlled
native execution, trusted RED/GREEN, local Reserve/Git.Apply and retained replay
readback (`.acceptance/production-native.log`). The earlier password/native stanza
mismatch is retained in `production-red.log`; no relabelling or charter widening
was applied. Focused request-authentication race regressions also pass. The full
production HTTP race fixture was interrupted at the builder's 150-second command
budget, with no verdict; this is uncovered race acceptance, not a product PASS.
Controlled protocol fixtures are not live-provider or protected-remote proof.

## Remote is unavailable, not awaiting invented privacy approval

The existing delivery.Privacy contract requires exact public text AND every
committed path/byte to be reviewed against audience. The GitHub adapter already
checks exact projected intent, closing-keyword boundaries and bounded text, but
those checks alone are not complete privacy assurance. The helper now installs
`delivery.PublicationPrivacy`, which validates the existing sealed publication
scope and exact accepted projection, then inspects all uploaded content. Safe
permitted projections can pass this guard without new approval facts. See
[publication privacy](contribution-publication-privacy.md). Productive startup
acceptance and real remote qualification remain pending independently.

Original policy retention and cold custody are implemented separately from this
startup path. The existing content-addressed blob owner retains the original
immutable policy before effects; reconstruction verifies its exact historical
digest rather than substituting current/default policy. Nonsecret credential pins
and original commit metadata bind reconstruction to the original intent. Cold
credential use still requires fresh same-owner admission, the exact current
approved binding/version, and a synchronous historical permit. Adapter mode and
HTTP-sink checks prevent that read permit from authorizing Apply or mutation.
Missing legacy metadata or unavailable original policy fails closed.

The committed `8f1088b3dd43c1d2dd6de4f8da7ebd3e728a648f` candidate independently
passed synthetic managed-local and encrypted cold normal/race fixture checks.
Those bounded results do not qualify subsequent startup/request-subject edits,
real provider execution, protected GitHub delivery or installed behavior.

Remote acceptance additionally requires exact actor/credential binding identity,
qualified publication-content privacy review, original historical policy reconstruction,
actual remote exact-head checks/review/protection/merge/post-merge readback and
retained repair/readiness budgets. #379 disposition remains separately owned;
`completed` remains false and the issue stays open.

Launch impact: this document records current source composition and bounded
controlled qualification, not completed delivery. The launch-asset review distinguishes
affected documentation, unaffected assets and unexercised behavioral/artifact
checks. No release, installation, live service or complete runtime capability is
claimed. See the current qualification section in the
[launch review](implementation/issue-378-launch-review.md).
