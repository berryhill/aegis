# #252 bounded skill-suite source and evidence mapping

Revalidated against source baseline `2c0bf9e8cdb1b57da09cc24d7da92f350056c0ac`.
This is a source/contract mapping, not a new all-domain behavioral score or a
claim that the complete issue's installed-agent acceptance has run. Historical
findings must not override subsequently delivered adapters or repeat completed
work. The enclosing revision's tests and matching archive bind these references.

## Historical finding disposition

| Finding | Current source / reused delivery | Bounded correction or evidence distinction |
| --- | --- | --- |
| F1 structural passes | `internal/skillbundle/evaluate.go`, `evaluate_evidence_test.go` | Output explicitly reports `structural_fixture_validation`; behavioral/service/runtime execution remain `not_run`. Preserve this, not a fictional behavioral pass count. |
| F2 portable recipes | Registry registration input, Graph submission, Queue bind/reclaim and protected credential intake are already bundled | Add Loop hello-builder input/reference, matching complete Graph publication/submission inputs and complete public request-check response. `TestPortableBundledAuthoringContracts` consumes an installed verified archive, public builder/strict decoders and actual Graph input normalization without operational source/spec/doc dependencies. |
| F3 workspace authority | Loop/Graph skills and `internal/orchestration/workspace_self_service_test.go` | Preserve authenticated `agent_id` selection, server-derived latest-enabled/stable-owner authority, participant restriction and credential independence; no fabricated authority or mandatory runtime/provisioning prerequisite. |
| F4 binding/reclaim | #186 PR446, Queue v0.3.0 and portable evaluation ledger | Already documents binding, ordinary-retry unavailability, exact expired-lease reclaim and unsupported online expire/exhaust. Do not reimplement lifecycle or weaken guards. |
| F5 daemon ownership | `internal/command/fleet_online.go`, `owning_service_inventory_test.go` | Existing typed Graph online adapter is delivered. Correct the stale bundled submission reference that incorrectly called it unavailable. Preserve explicit instance selectors, no second writer/token extraction/generic client. |
| F6 response envelope | `registry_fixtures_test.go`, Registry explanatory projections | Existing `{agent, created}` envelope correction retained. New Graph check response is compared as a complete literal object with actual command output; publication/accepted response descriptions stay explicitly projections. |
| F7 pending public readback | `internal/api/workspace_skill_contract_test.go` | Real-store public adapter regression already covers single/multi-node pending show/discovery, replay/conflict and participant corruption. Correct portable guidance to `preparation-pending` and `queue preparations`; legacy `awaiting-runtime` is historical readback, not a new runtime diagnosis. No new execution campaign. |
| F8 install/update/rollback | PR438 / `internal/command/skills.go`, existing `internal/skillbundle/install*.go` | Delivered packaged CLI already supports exact-artifact install/inventory/update/rollback, local-edit refusal, interrupted publication and locking. Reuse these mechanics/tests and prior extracted-CLI/Hermes discovery evidence; no installer redesign or normal-profile change. Public release is a separate owner action. |
| F9 ownership/availability | Bundled `aegis/references/operation-matrix.v1.json`, constructor/route inventory tests and manifest | Existing full public inventory assigns primary skills and explicit missing/unavailable classifications. Projection stays advisory/unavailable. Version eligibility is not qualification of every operation/version; exact installed help/capabilities remain required. |

## Suite ownership and qualification

The bundled operation matrix remains the complete source-bound CLI/HTTP/Console
ownership inventory for all fifteen primary skills: router, trust inspection,
charters, audit, approvals/provisioning, sessions, Registry, Loops, Graphs, Queue,
evidence/disposition, credentials, onboarding, lifecycle and projection. Its
constructor/registration inventory checks are structural coverage, not behavioral
execution. No new public surface is introduced by this correction, so no new
operation owner or adapter classification is appropriate.

Preserve #188 PR450 credential-authority v0.2.2, #186 PR446 Queue v0.3.0,
closed #348 workflow alignment and PR438 packaged installer. Their recorded
independent acceptance is scoped historical evidence, not a fresh run of those
journeys on every later candidate. No credential/provider/runtime/installer
implementation changes occur here. Runtime protocol, real-store service,
packaged discovery, actual model adherence, and public release/install evidence
must remain separate; skips never count as acceptance.

The new test checks matching publication ports and typed submission values but
uses synthetic reference digests. It cannot establish reference existence,
activation, authority admission, Queue binding, execution or success. Existing
Graph preflight tests retain missing `transition_id`, invented-field, mixed
workspace/runtime selector and bounded-attempt denials. Unknown mutation results
require the same intent and authoritative readback, never a replacement identity.

## Independent evidence boundary

Fresh no-edit verification must bind the complete dirty source inventory and
built executable/archive, rerun affected portable-contract/structural/adapter
checks and confirm all new files are distributed. Reuse unchanged Queue,
credential and installer acceptance only with the stated scope/provenance;
optional acceptance requiring unsupported local prerequisites must be reported
as skipped/uncovered, not repaired by a new campaign. Actual external-agent
adherence across every supported domain, broad version qualification, live
provider execution and public publication/installation are not asserted by this
source correction. These unexercised qualification tiers are not claimed as PASS
or imposed as additional gates on the owner's current implementation-through-merge scope.
Parent owns review, exact-head CI, PR/merge and issue disposition. Other owners
retain v0.3.0 release and #235; this delta does not perform either.

## Launch-asset impact

Changed: Loop/Graph advisory skills/references and canonical file inventory,
focused schema/text regressions, `docs/HERMES_SKILLS.md`, this mapping and the
Unreleased changelog. No new shipped command, wire schema, authority boundary,
configuration, runtime, dependency, installation transaction or release format
changes.

Independent extracted-CLI testing found the existing Graph `submit --check`
was blocked by manager initialization before decoding. The root now exempts
only the true local shape-check path, like existing local Loop builders.
The full-root portable contract regression uses an absent config; normal submit
and `--check=false` still deny before initialization. No operational authentication
or service admission is bypassed. The first independent FAIL and builder timeout
are retained; focused retest is required for the repaired candidate.

Reviewed as unaffected for this documentation/fixture delta: root README
(already routes managed skills inventory), LICENSE, SECURITY, CONTRIBUTING,
CODE_OF_CONDUCT, threat model, architecture including its diagram, five-minute
quickstart, no-key demo and historical terminal recording. The demo/recording
explicitly excludes this workspace-authoring qualification; no live demo or
recording is synthesized or rerun. Release binary/checksum generation remains
unchanged and no public release assets are created. Existing early-contributor
inventory is unchanged; a read-only `good first issue` query returned no open
items, not permission to create or relabel issues. No unrelated launch rewrite,
new queue, delegation, credential intake, model/config/grant, service or profile
installation is included.
