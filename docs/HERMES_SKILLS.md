# Official Hermes skill suite

The repository-root `skills/` directory is the canonical reviewed source for the official portable Aegis Hermes skills. Each installable skill is one immediate child directory containing `SKILL.md`. The bundle ships fifteen skills:

- `skills/aegis/SKILL.md` is a thin discovery and routing skill.
- `skills/aegis-charter-design/SKILL.md` is an advisory skill for principal-only disposable Hermes design and authoritative charter validation, import, listing, readback, explanation, and effective-authority inspection.
- `skills/aegis-trust-context-inspection/SKILL.md` is an advisory, read-only skill that routes authenticated-principal, exact-stanza, existing-mandate, and effective-authority inspection to typed Aegis read surfaces.
- `skills/aegis-audit-verification/SKILL.md` is an advisory verification skill for canonical audit chains, signed checkpoints, reconstructable lineage, immutable receipt references, and distinct delivery/projection states.
- `skills/aegis-approval-provisioning/SKILL.md` is an advisory skill for exact provisioning-plan review, authenticated single-use decisions, deterministic Aegis-owned apply, interrupted-intent recovery, and receipt verification.
- `skills/aegis-agent-registry/SKILL.md` is an advisory skill for exact existing-fleet registration, immutable current/history inspection, and append-only enabled/disabled/retired lifecycle governance.
- `skills/aegis-session-operations/SKILL.md` is an advisory routing skill for previewing and operating clean, mandate-bound Hermes sessions through typed Aegis lifecycle services.
- `skills/aegis-loop-authoring/SKILL.md` is an advisory skill for authoring, publishing, activating, inspecting, and retiring immutable typed Loop revisions through authenticated Aegis services. Its field recipes distinguish `agent_id` workspace selection and server-derived authority from runtime-authority inputs and readback. The installed `loops example` and `loops validate` surfaces support structural authoring, while `--config OWNER_CONFIG --target CONSOLE_URL` supports protected Unix online publication/readback without opening another store. This is not an executable code-task builder or verification-success guarantee.
- `skills/aegis-graph-authoring/SKILL.md` is an advisory skill for composing, publishing, inspecting, and submitting immutable typed Graph revisions with exact Agent/Loop bindings and durable admission readback. Its recipes distinguish authenticated `agent_id` workspace selection, server-derived provenance and `awaiting_runtime` acceptance from runtime-bound work. They preserve required `transition_id` and reject invented rejection-idempotency fields; they do not establish portable-schema or installed-agent acceptance.
- `skills/aegis-execution-queue/SKILL.md` is an advisory skill for exact Queue inspection and the shipped process, retry/reclaim, cancellation, expiry, exhaustion, and revocation lifecycle through authenticated Aegis services.
- `skills/aegis-evidence-disposition/SKILL.md` is an advisory skill for reconstructing exact Queue, Attempt, artifact, verification, and terminal-disposition lineage while preserving process/evidence separation and unsupported reviewer-reevaluation boundaries.
- `skills/aegis-credential-authority/SKILL.md` is an advisory skill for principal-only encrypted custody administration, exact broker binding, revocation, backup, and the single typed sanitized GitHub read path.
- `skills/aegis-manager-onboarding/SKILL.md` is an advisory skill for artifact-derived initialization and resumption, exact local-model configuration and certification, authenticated gateway and console handoff, manager launch, and bounded cleanup through typed Aegis commands.
- `skills/aegis-operator-lifecycle-diagnostics/SKILL.md` is an advisory, read-first skill for installation, profile, gateway, update, migration, reset, rollback-availability, and recovery diagnosis through shipped typed Aegis commands.
- `skills/aegis-deployment-projection/SKILL.md` is an advisory skill for selective signed per-deployment projection review, generation reconciliation, drift, interruption, revocation, and monotonic rollback semantics; the supported release has no typed projection mutation or readback surface, so it reports those operations unavailable rather than simulating them.

No suite skill authenticates, authorizes, approves, issues a mandate, provisions, activates, executes, widens authority, signs checkpoints, emits audit events, or attests completion. Installing a skill grants no Aegis authority. The charter skill cannot authorize its proposal, bypass Aegis's canonical import service, or union trust stanzas; it explicitly warns that successful `design --draft` and `design --smoke` runs both perform canonical imports. The inspection skill cannot select or union trust stanzas. The audit skill cannot repair canonical history or silently deliver/rebuild derived state. The approval/provisioning skill cannot decide, apply, recover, or widen a plan outside typed Aegis authority. The Agent Registry skill cannot infer ownership, treat a Hermes profile as canonical identity, rewrite revisions, or mutate lifecycle outside typed Aegis admission. The session-operations skill cannot authenticate the caller, select or union stanzas, issue authority itself, launch Hermes directly, or mutate lifecycle records. Its `aegis session preview` route is consequential because authoritative Aegis services issue and store a short-lived mandate; `start`, `revoke`, and `terminate` remain separate consequential Aegis lifecycle operations that require explicit authorization and authoritative readback. The Loop-authoring skill cannot derive publisher authority, publish or rewrite revisions itself, mutate lifecycle outside typed Aegis services, or turn a definition into execution authority. The Graph-authoring skill cannot derive authority, activate a Graph, rewrite exact Agent/Loop bindings, admit itself, enqueue work directly, or turn queue acceptance into runtime authorization or success. The Execution Queue skill cannot grant a claim or lease, bypass dependencies or attempt budgets, invoke a runtime itself, invent evidence, schedule unavailable automation, or decide disposition outside typed Aegis services. The evidence/disposition skill cannot verify bytes itself, fabricate receipts, treat process state as acceptance, authenticate a reviewer, overwrite a disposition, or claim that unsupported reviewer reevaluation is shipped. The credential-authority skill cannot disclose stored values, retain secret material in model context, mint broker authority, widen exact bindings, create a generic proxy, or keep credential custody alive outside the long-lived `aegis serve` owner. Fixture content and model narration are never live authenticated principal, stanza, mandate, ownership, approval, broker, runtime, audit, or completion evidence.

The session skill progressively discloses non-secret interpretation examples from `skills/aegis-session-operations/references/session-fixtures.json` and routes normative questions to `specs/RUNTIME_AND_SESSIONS.md`, `specs/IDENTITY_AND_AUTHORIZATION.md`, `specs/AUDIT.md`, and installed command help. Those references grant no identity, mandate, process, authority, or receipt evidence. A material authority or stanza change requires a new mandate, immutable authority context, disposable Hermes home, and clean process; permissions are never unioned across stanzas.

The credential-authority skill now includes a self-contained
`references/protected-intake.v1.md` workflow and synthetic versioned metadata
examples qualified against the actual protected-intake decoder after archive
installation. The delivered #245/#254 Linux gateway terminal performs review,
explicit approval, no-echo input, canonical creation and same-conversation return;
external agents must not build their own value-bearing client. This is create and
pending-operation cancel support, not an online adapter for rotate/revoke/bind or
all #248 platform operations. The archive/decoder regression and genuine
Unix-gateway/PTY-to-bbolt regressions are distinct from still-missing supported
Hermes installed-agent behavior. No fixture contains values or authentication.

The credential-authority skill progressively discloses non-secret interpretation examples from `skills/aegis-credential-authority/references/credential-fixtures.json` and routes normative questions to `docs/CREDENTIAL_BROKER.md`, credential-related specifications, and installed command help. Its fixtures contain metadata and denial state only; they never carry credential values, passphrases, key-encryption keys, capabilities, authentication headers, or live authority.

The manager-onboarding skill uses non-secret classification examples from `skills/aegis-manager-onboarding/references/onboarding-fixtures.json` and routes normative questions to `specs/AEGIS_MANAGER.md`, `specs/MANAGER_LIFECYCLE_AND_ONBOARDING.md`, and installed command help. It routes every operation to a typed Aegis command. Each operational session requires exactly one externally authenticated trust stanza: zero matches deny and multiple matches deny as ambiguous. Prompt/model content cannot select or change stanza or authority; stanza or material-authority changes require a new mandate and clean runtime session. Only Aegis emits authoritative audit events, which model narration cannot create or replace.

The operator-lifecycle-diagnostics skill uses non-secret interpretation examples from `skills/aegis-operator-lifecycle-diagnostics/references/lifecycle-fixtures.json` and routes normative questions to `docs/PATH_LAYOUT.md`, the relevant lifecycle implementation, and installed command help. Its fixtures do not prove live host state, identity, authority, release provenance, ownership, service health, rollback eligibility, recovery, or completion. The skill authenticates and authorizes nothing: diagnosis is read-only by default, and each mutation retains the authentication, terminal, preview, confirmation, and post-operation controls its shipped command actually implements. It explicitly records that direct self-update has no separate principal-authentication or apply-confirmation boundary, that `update --check` does not inspect archives or checksums, and that package-manager ownership is operator-supplied context. It does not provide a general doctor, repair, executable rollback, or arbitrary backup-restore surface; absent operations are reported as unavailable rather than simulated. It does not claim host sandboxing, complete zero trust, general least privilege, service readiness from process activity, or recovery from process exit or model narration.

The Graph skill now bundles `references/submission-contract.v1.md` and a complete
synthetic `references/workspace-submit.v1.json` request. With an executable whose
help advertises the flag, `aegis graphs submit --check FILE` strictly decodes
that application request and checks required envelope fields without opening
stores. It emits `evidence_class=request_shape_validation`,
`authority_admission=not_run`, and `submitted=false`; it does not resolve Graphs,
validate actual Graph input types, grant authority, or prove behavior. The
synthetic references must never be submitted unchanged. An older executable
without `--check` is missing this capability even if the advisory skill installs;
full capability-qualified compatibility remains outstanding.

The strict `skills/aegis-skills.json` manifest binds the bundle and each skill to exact content digests, compatibility ranges, operation ownership, dependencies, authority class, required operations and toolsets, sensitivity, network, filesystem, and file inventory declarations. `skills/evaluations.json` carries the non-secret structural evaluation cases. Neither file grants runtime authority.

## Bundled operation ownership and transport inventory

The routing skill includes `references/operation-matrix.v1.json` for all fifteen
primary skills. `TestDistributedOperationMatrix` compares unique ownership with
the actual constructor-built Cobra tree and current public Echo registrations,
including console, protected intake, health and static routes. Source and test
references are navigation, not claims that the referenced tests passed. The
archive's exact source revision binds the inventory; the inventory is not a
portable schema or runtime-capability attestation.

Explicit gaps distinguish missing adapters from unavailable operations: no HTTP
runtime binding, no transparent daemon-online forwarding of direct-service CLI
calls, no general conversational platform adapter supplied by credential intake,
no Graph lifecycle mutation, reviewer reevaluation or deployment projection.
CLI and API presence must not invite temporary authenticated clients, credential
extraction or a second writer. Unknown mutation outcomes require authoritative
readback with the same intent identity before replay.

## Validate and evaluate source

Registry interpretation fixtures explicitly declare `explanatory_projection_not_wire_response`. Their registration projections preserve the CLI/HTTP `{agent, created}` envelope and identical Agent content on replay, but reduced fields, synthetic digests, and error/history summaries are not complete wire contracts. Projection regression tests and the existing real-store Registry API test are distinct evidence; neither is an installed-agent behavioral evaluation.

Operation availability is distinct from skill installation: the shipped advisory deployment-projection skill owns an `unavailable` reconciliation operation. Evaluation rejects any `route` expectation whose operation is not explicitly `shipped`. A normal request for an unavailable operation expects denial, not a simulated operation; prerequisite reads cannot stand in for projection execution.

`evaluate` reports `status: "valid"`, `evidence_class: "structural_fixture_validation"`, and the `cases` / `structurally_valid` counts. Behavioral, service, and runtime execution are explicitly `not_run`. The old unqualified `passed` count is removed: consumers must use `structurally_valid`, not interpret fixture validation as successful operation. This command validates declared expectations; it does not run an agent, submit requests, exercise a service, or observe a runtime. Packaging, request-schema tests, real-store integration, runtime protocol, observed agent behavior, installed inventory, and public publication require separate evidence.

Run `make skillbundle-verify`. The target executes the Go validator and evaluator under `internal/skillbundle/`. Validation denies unknown JSON or YAML fields, trailing documents, undeclared files, symlinks, non-regular files, executable skill content, path traversal, digest or size drift, duplicate or orphan operation ownership, dependency errors, remote active content, inline network or shell behavior, secret-shaped literals, and positive prompt-authority claims.

A release build runs `go run ./internal/skillbundle/cmd build . DIST VERSION SOURCE_REVISION`, where `VERSION` is exact stable SemVer and `SOURCE_REVISION` is one exact lowercase 40-hex Git commit. The builder first validates and evaluates the repository source, injects that exact release version and source revision into the archive manifest, fixes archive ordering, ownership, permissions, and timestamps, and writes `aegis-skills_vVERSION.tar.gz`. `go run ./internal/skillbundle/cmd verify ARCHIVE SOURCE_REVISION` extracts into a disposable sibling directory, rejects unsafe archive members, repeats full manifest/content validation, and denies unless the embedded immutable revision exactly matches the caller's expected release revision. `SHA256SUMS` covers the skill archive alongside platform binaries.

## Hermes tap and direct install

Aegis accepts stable Hermes Agent `>=0.18.0` without an arbitrary upper cap. The inspected Hermes skill-discovery contract uses immediate skill directories beneath a configured tap's `skills/` path; a later accepted version still needs exact-version skill/protocol qualification, not an assumption of compatibility. Against the published `berryhill/aegis` repository:

1. `hermes skills tap add berryhill/aegis` records the tap in the selected `HERMES_HOME`.
2. `hermes skills search aegis` shows the repository path and provenance.
3. `hermes skills inspect berryhill/aegis/skills/aegis` previews the routing skill without installation.
4. `hermes skills inspect berryhill/aegis/skills/aegis-charter-design` previews the charter design skill without installation.
5. `hermes skills inspect berryhill/aegis/skills/aegis-trust-context-inspection` previews the read-only trust-context inspection skill.
6. `hermes skills inspect berryhill/aegis/skills/aegis-audit-verification` previews the read-only audit verification skill.
7. `hermes skills inspect berryhill/aegis/skills/aegis-approval-provisioning` previews the approval and provisioning skill without installation.
8. `hermes skills inspect berryhill/aegis/skills/aegis-agent-registry` previews the Agent Registry skill without installation.
9. `hermes skills inspect berryhill/aegis/skills/aegis-session-operations` previews the session-operations skill without installation.
10. `hermes skills inspect berryhill/aegis/skills/aegis-loop-authoring` previews the Loop-authoring skill without installation.
11. `hermes skills inspect berryhill/aegis/skills/aegis-graph-authoring` previews the Graph-authoring skill without installation.
12. `hermes skills inspect berryhill/aegis/skills/aegis-execution-queue` previews the Execution Queue skill without installation.
13. `hermes skills inspect berryhill/aegis/skills/aegis-evidence-disposition` previews the evidence and disposition skill without installation.
14. `hermes skills inspect berryhill/aegis/skills/aegis-credential-authority` previews the credential-authority skill without installation.
15. `hermes skills inspect berryhill/aegis/skills/aegis-manager-onboarding` previews the manager-onboarding skill without installation.
16. `hermes skills inspect berryhill/aegis/skills/aegis-operator-lifecycle-diagnostics` previews the operator lifecycle diagnostics skill without installation.
17. `hermes skills inspect berryhill/aegis/skills/aegis-deployment-projection` previews the deployment projection skill without installation.
18. `hermes skills install berryhill/aegis/skills/aegis --yes` installs the direct repository routing skill.
19. `hermes skills install berryhill/aegis/skills/aegis-charter-design --yes` installs the direct repository charter skill.
20. `hermes skills install berryhill/aegis/skills/aegis-trust-context-inspection --yes` installs the direct repository inspection skill.
21. `hermes skills install berryhill/aegis/skills/aegis-audit-verification --yes` installs the direct repository audit skill.
22. `hermes skills install berryhill/aegis/skills/aegis-approval-provisioning --yes` installs the direct repository approval and provisioning skill.
23. `hermes skills install berryhill/aegis/skills/aegis-agent-registry --yes` installs the direct repository Agent Registry skill.
24. `hermes skills install berryhill/aegis/skills/aegis-session-operations --yes` installs the direct repository session-operations skill.
25. `hermes skills install berryhill/aegis/skills/aegis-loop-authoring --yes` installs the direct repository Loop-authoring skill.
26. `hermes skills install berryhill/aegis/skills/aegis-graph-authoring --yes` installs the direct repository Graph-authoring skill.
27. `hermes skills install berryhill/aegis/skills/aegis-execution-queue --yes` installs the direct repository Execution Queue skill.
28. `hermes skills install berryhill/aegis/skills/aegis-evidence-disposition --yes` installs the direct repository evidence and disposition skill.
29. `hermes skills install berryhill/aegis/skills/aegis-credential-authority --yes` installs the direct repository credential-authority skill.
30. `hermes skills install berryhill/aegis/skills/aegis-manager-onboarding --yes` installs the direct repository manager-onboarding skill.
31. `hermes skills install berryhill/aegis/skills/aegis-operator-lifecycle-diagnostics --yes` installs the direct repository operator lifecycle diagnostics skill.
32. `hermes skills install berryhill/aegis/skills/aegis-deployment-projection --yes` installs the direct repository deployment projection skill.
33. `hermes skills list` and `hermes skills audit SLUG` provide installed readback and Hermes's independent security scans.

Tap registration is discovery only; it does not install or enable a skill. Direct installation mutates the selected Hermes profile, so it requires an explicit operator choice. Installation makes advisory instructions available to Hermes; it does not authenticate a principal, select a stanza, issue a mandate, or grant Aegis authority. Discussion or design work is not installation authorization.

## Disposable-profile proof

Never test installation against a normal profile. Use a durable repository-local proof directory, for example `.aegis-skill-proof`, and remove it after review:

1. Create `.aegis-skill-proof/home` with mode `0700`.
2. Set `HERMES_HOME` to the absolute path of that directory for every `hermes skills ...` command above.
3. Require the discovered slug set to match the manifest exactly and read the installed hub lock to confirm the GitHub identifier and recorded content hash.
4. Verify that the ordinary profile was not touched and that no Aegis-managed disposable runtime home received the skill.

These live GitHub commands prove published-repository discovery and are intentionally separate from hermetic unit tests. They cannot prove an unmerged revision is already available from the public tap.

## Queue historical readback

The real-store `TestDQHandoff` regression exercises workspace acceptance,
controller runtime binding through the application service (no HTTP binding
route exists), first claim, live-lease denial, expiry-backed HTTP reclaim and
authoritative failure from an incompatible runtime adapter. Claim eligibility
reloads the exact immutable binding; completion binds audit to its runtime
mandate while retaining original workspace submission provenance. Replay keeps
the original timestamp and digest throughout. This is service/persistence
evidence with a synthetic session process, not actual-agent or supported-Hermes
execution success.

Authenticated Queue `list` and exact `show` include valid workspace submissions in
`awaiting_runtime` without requiring a terminal disposition. They do not claim a
runtime has been bound, claimed, or executed. Every Graph node's exact participant
revision is reloaded and digest-checked. The response adds `node_runtimes`, keyed
by node ID; legacy `runtime` is populated for single-node Graphs only (an empty
binding for multi-node Graphs). Multi-node history visibility is not multi-node
processing support. Corrupt participant evidence still fails closed with
`repair_required`. The isolated HTTP regression is service evidence, not an
installed skill or actual-agent behavioral qualification.

## Managed exact-artifact inventory (explicit opt-in)

A separate source-built `aegis-skillbundle` tool now implements `install`,
`inventory`, `update`, and `rollback`. It is not the `aegis update` executable
updater and is not yet a separately published release binary. Build it once:

```sh
go build -o .aegis-skill-proof/aegis-skillbundle ./internal/skillbundle/cmd
```

The compiled tool needs no checkout at operation time. Supply a downloaded or
locally prepared archive, the independently selected exact `sha256:HEX` archive
digest, its exact 40-hex source revision, and an explicit absolute destination:

```text
aegis-skillbundle install ARCHIVE SHA256_DIGEST SOURCE_REVISION ABSOLUTE_HOME
aegis-skillbundle inventory ABSOLUTE_HOME
aegis-skillbundle update ARCHIVE SHA256_DIGEST SOURCE_REVISION ABSOLUTE_HOME
aegis-skillbundle rollback RETAINED_ARCHIVE SHA256_DIGEST SOURCE_REVISION ABSOLUTE_HOME
```

The destination must already be a canonical, non-symlink directory that is not
group/world writable. Use a new mode-0700 repository-local proof home for tests.
The tool never reads a default destination from `HOME` or `HERMES_HOME`. It refuses
to adopt or overwrite an existing unmanaged `skills` directory, including an
empty one. Do not use it to migrate an ordinary populated profile implicitly.
Installation is an explicit operator filesystem action, not Aegis principal
approval, session provisioning, runtime activation, or an authority grant.

On Linux/macOS the tool verifies frozen archive bytes, stages and syncs the
complete manifest/dependency inventory under `.aegis-skill-bundles/ARCHIVE_HEX`,
and atomically replaces the destination's `skills` symlink. Other platforms
report `installation_platform_unavailable`. Qualification of macOS durability
and supported Hermes versions remains separate from Linux tests and cross-builds.
The `.hub` operational-metadata link has one fixed managed target outside the
immutable generation and survives updates and rollback; its contents are not
read as source, provenance, approval, or behavioral evidence. All other installed
paths must match the archive exactly. Source/archive validation still forbids
symlinks; these two installed-layout links are tool-owned, not archive members.

Each transaction holds a nonblocking process lock released by process exit.
Interruption before pointer publication preserves the old inventory; after
publication, read `inventory` before deciding whether to repeat the same exact
artifact. A post-publication sync/readback error is an uncertain result, not
proof of no mutation. Retained verified generations allow explicit rollback;
no `latest` lookup or version guessing occurs. `update` and `rollback` use the
same exact-artifact transaction; neither silently selects a version. Unreferenced
staging/generation directories from interruption are not activated or garbage
collected automatically. Never remove the lock inode to bypass a live transaction.

`inventory` verifies the retained archive checksum/revision and every distributed
byte, including manifest and evaluations, and emits
`evidence_class=installed_inventory_verification`, exact version/digests/slugs,
and `authority_granted=false`. Local edits, extra files, unsafe metadata links,
and missing content deny inventory/update/rollback and are preserved for review.
Hermes hub update/install commands must not independently mutate this managed
inventory. Keep the destination quiescent during changes: pointer atomicity is
not a snapshot across multiple file reads by an already-running agent, and the
tool does not attest runtime inactivity or reload a running session.

This tooling supplies distribution mechanics, not capability qualification,
actual-agent behavioral acceptance, a public tap receipt, or release publication.

## Updates, local changes, and rollback

Hermes 0.18.x `skills check` compares upstream content with the recorded installation hash. It does not independently make the Aegis manifest authoritative and must not be treated as approval or immutable provenance.

Before updating an official skill, compare every installed file with the exact file inventory and digest in the currently installed verified bundle. If any file differs, stop and preserve the local copy for operator review; do not run `hermes skills update` or reinstall over it. The official update path may proceed only from an exact clean installed digest to a newly downloaded archive whose archive checksum, embedded source revision, manifest, and content digests all verify. After installation, repeat exact installed inventory verification.

Rollback names a previously retained `aegis-skills_vVERSION.tar.gz` and its exact SHA-256 digest. Verify the archive and embedded source revision before restoring it. Never select rollback content through `latest`, a branch, or another mutable tag. A rollback also refuses locally modified installed files until the operator explicitly preserves or removes those changes.

The repository currently supplies validation, evaluation, deterministic packaging, archive verification, explicit managed-inventory transactions, release checksums, and Hermes tap/direct-install instructions. It does not silently enable official skills in runtime sessions, mutate normal profiles during tests, or turn a skill manifest, tap, archive tag, or model statement into Aegis authority.
