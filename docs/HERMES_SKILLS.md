# Official Hermes skill suite

The repository-root `skills/` directory is the canonical reviewed source for the official portable Aegis Hermes skills. Each installable skill is one immediate child directory containing `SKILL.md`. The bundle ships six skills:

- `skills/aegis/SKILL.md` is a thin discovery and routing skill.
- `skills/aegis-charter-design/SKILL.md` is an advisory skill for principal-only disposable Hermes design and authoritative charter validation, import, listing, readback, explanation, and effective-authority inspection.
- `skills/aegis-trust-context-inspection/SKILL.md` is an advisory, read-only skill that routes authenticated-principal, exact-stanza, existing-mandate, and effective-authority inspection to typed Aegis read surfaces.
- `skills/aegis-audit-verification/SKILL.md` is an advisory verification skill for canonical audit chains, signed checkpoints, reconstructable lineage, immutable receipt references, and distinct delivery/projection states.
- `skills/aegis-approval-provisioning/SKILL.md` is an advisory skill for exact provisioning-plan review, authenticated single-use decisions, deterministic Aegis-owned apply, interrupted-intent recovery, and receipt verification.
- `skills/aegis-agent-registry/SKILL.md` is an advisory skill for exact existing-fleet registration, immutable current/history inspection, and append-only enabled/disabled/retired lifecycle governance.

No suite skill authenticates, authorizes, approves, issues a mandate, provisions, activates, executes, widens authority, signs checkpoints, emits audit events, or attests completion. Installing a skill grants no Aegis authority. The charter skill cannot authorize its proposal, bypass Aegis's canonical import service, or union trust stanzas; it explicitly warns that successful `design --draft` and `design --smoke` runs both perform canonical imports. The inspection skill cannot select or union trust stanzas. The audit skill cannot repair canonical history or silently deliver/rebuild derived state. The approval/provisioning skill cannot decide, apply, recover, or widen a plan outside typed Aegis authority. The Agent Registry skill cannot infer ownership, treat a Hermes profile as canonical identity, rewrite revisions, or mutate lifecycle outside typed Aegis admission. Fixture content and model narration are never live authority or evidence.

The strict `skills/aegis-skills.json` manifest binds the bundle and each skill to exact content digests, compatibility ranges, operation ownership, dependencies, authority class, required operations and toolsets, sensitivity, network, filesystem, and file inventory declarations. `skills/evaluations.json` carries the non-secret structural evaluation cases. Neither file grants runtime authority.

## Validate and evaluate source

Run `make skillbundle-verify`. The target executes the Go validator and evaluator under `internal/skillbundle/`. Validation denies unknown JSON or YAML fields, trailing documents, undeclared files, symlinks, non-regular files, executable skill content, path traversal, digest or size drift, duplicate or orphan operation ownership, dependency errors, remote active content, inline network or shell behavior, secret-shaped literals, and positive prompt-authority claims.

A release build runs `go run ./internal/skillbundle/cmd build . DIST VERSION SOURCE_REVISION`, where `VERSION` is exact stable SemVer and `SOURCE_REVISION` is one exact lowercase 40-hex Git commit. The builder first validates and evaluates the repository source, injects that exact release version and source revision into the archive manifest, fixes archive ordering, ownership, permissions, and timestamps, and writes `aegis-skills_vVERSION.tar.gz`. `go run ./internal/skillbundle/cmd verify ARCHIVE SOURCE_REVISION` extracts into a disposable sibling directory, rejects unsafe archive members, repeats full manifest/content validation, and denies unless the embedded immutable revision exactly matches the caller's expected release revision. `SHA256SUMS` covers the skill archive alongside platform binaries.

## Hermes tap and direct install

Hermes Agent `>=0.18.0,<0.19.0` discovers immediate skill directories beneath a configured tap's `skills/` path. Against the published `berryhill/aegis` repository:

1. `hermes skills tap add berryhill/aegis` records the tap in the selected `HERMES_HOME`.
2. `hermes skills search aegis` shows the repository path and provenance.
3. `hermes skills inspect berryhill/aegis/skills/aegis` previews the routing skill without installation.
4. `hermes skills inspect berryhill/aegis/skills/aegis-charter-design` previews the charter design skill without installation.
5. `hermes skills inspect berryhill/aegis/skills/aegis-trust-context-inspection` previews the read-only trust-context inspection skill.
6. `hermes skills inspect berryhill/aegis/skills/aegis-audit-verification` previews the read-only audit verification skill.
7. `hermes skills inspect berryhill/aegis/skills/aegis-approval-provisioning` previews the approval and provisioning skill without installation.
8. `hermes skills inspect berryhill/aegis/skills/aegis-agent-registry` previews the Agent Registry skill without installation.
9. `hermes skills install berryhill/aegis/skills/aegis --yes` installs the direct repository routing skill.
10. `hermes skills install berryhill/aegis/skills/aegis-charter-design --yes` installs the direct repository charter skill.
11. `hermes skills install berryhill/aegis/skills/aegis-trust-context-inspection --yes` installs the direct repository inspection skill.
12. `hermes skills install berryhill/aegis/skills/aegis-audit-verification --yes` installs the direct repository audit skill.
13. `hermes skills install berryhill/aegis/skills/aegis-approval-provisioning --yes` installs the direct repository approval and provisioning skill.
14. `hermes skills install berryhill/aegis/skills/aegis-agent-registry --yes` installs the direct repository Agent Registry skill.
15. `hermes skills list` and `hermes skills audit SLUG` provide installed readback and Hermes's independent security scans.

Tap registration is discovery only; it does not install or enable a skill. Direct installation mutates the selected Hermes profile, so it requires an explicit operator choice. Installation makes advisory instructions available to Hermes; it does not authenticate a principal, select a stanza, issue a mandate, or grant Aegis authority. Discussion or design work is not installation authorization.

## Disposable-profile proof

Never test installation against a normal profile. Use a durable repository-local proof directory, for example `.aegis-skill-proof`, and remove it after review:

1. Create `.aegis-skill-proof/home` with mode `0700`.
2. Set `HERMES_HOME` to the absolute path of that directory for every `hermes skills ...` command above.
3. Require the discovered slug set to match the manifest exactly and read the installed hub lock to confirm the GitHub identifier and recorded content hash.
4. Verify that the ordinary profile was not touched and that no Aegis-managed disposable runtime home received the skill.

These live GitHub commands prove published-repository discovery and are intentionally separate from hermetic unit tests. They cannot prove an unmerged revision is already available from the public tap.

## Updates, local changes, and rollback

Hermes 0.18.x `skills check` compares upstream content with the recorded installation hash. It does not independently make the Aegis manifest authoritative and must not be treated as approval or immutable provenance.

Before updating an official skill, compare every installed file with the exact file inventory and digest in the currently installed verified bundle. If any file differs, stop and preserve the local copy for operator review; do not run `hermes skills update` or reinstall over it. The official update path may proceed only from an exact clean installed digest to a newly downloaded archive whose archive checksum, embedded source revision, manifest, and content digests all verify. After installation, repeat exact installed inventory verification.

Rollback names a previously retained `aegis-skills_vVERSION.tar.gz` and its exact SHA-256 digest. Verify the archive and embedded source revision before restoring it. Never select rollback content through `latest`, a branch, or another mutable tag. A rollback also refuses locally modified installed files until the operator explicitly preserves or removes those changes.

The repository currently supplies validation, evaluation, deterministic packaging, archive verification, release checksums, and Hermes tap/direct-install instructions. It does not silently enable official skills in runtime sessions, mutate normal profiles during tests, or turn a skill manifest, tap, archive tag, or model statement into Aegis authority.
