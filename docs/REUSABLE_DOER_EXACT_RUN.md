# Exact owner-principal Run parity for reusable Doer

A published reusable v5 Loop remains an inert definition. Registration grants workspace ownership and authoring capabilities, not arbitrary runtime or credential authority.

For an explicitly requested Run, the owning service may prepare the same exact bounded host-write scope as the existing v4 owner-principal Run policy. This applies only to a freshly authenticated owner principal, the exact enabled Agent and approved charter, and a zero-ambient-authority ephemeral Hermes stanza. No model, provider, credential, or controller configuration is changed.

The service resolves normalized per-run inputs into the concrete task contract before scope preparation. The signed record binds the actual immutable v5 revision, its fixed attempt budget, concrete task/workspace/allowlist/assertion contract digest, exact Agent, deployment, owner, and stable Run intent. Expiry remains bounded by principal freshness and session expiry. Linked reexecution retains the remaining implementation budget. Workers repeat normal admission and exact signed-scope checks around effects.

The optional signed `doer_reusable` field distinguishes the actual v5 source. Omitting it preserves historical v4 canonical bytes and signatures. A template, publication, lifecycle activation, or observational readiness request cannot create host-write authority. Foreign owners, privileged or persistent stanzas, stale authentication, substituted candidates/contracts, and mismatched Run intents remain denied. Other workflows continue to require their independently authorized exact scope.

## Required regression coverage

`TestInstalledOnlineReusableDoerDirectAdmission` must execute the production public CLI through the owning Unix service, separately publish then run the same immutable v5 revision, and verify runtime-produced selected-file output, stored content and terminal evidence without a fixture `AuthorizedContracts` allowlist. It also checks that pure readiness did not mint a grant. Its existing required normal and race CI package shards run this test.

Keep `TestInstalledOnlineDoerPrincipalSingleRunWithoutProvisioning`, `TestDoerEphemeralPreparationPolicy`, and the real signature, substitution, freshness and historical-v4 compatibility tests in `internal/hostapproval/reusable_test.go` alongside it. Synthetic runtime fixtures prove controller integration, not live provider acceptance.

## Live acceptance remains distinct

A passed regression suite is not a working-provider claim. Inspect the exact live Queue item, implementation observations, artifact/verification receipts and final disposition, and read output bytes independently. Preserve failed history and remaining budget; never replace a published Loop or reset attempt bounds to disguise a failure.
