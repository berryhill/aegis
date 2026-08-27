# Official Hermes skill suite

The repository-root `skills/` directory is the canonical reviewed source for the official portable Aegis Hermes skills. Each installable skill is one immediate child directory containing `SKILL.md`. The bundle ships two skills:

- `skills/aegis/SKILL.md` is a thin discovery and routing skill.
- `skills/aegis-trust-context-inspection/SKILL.md` is an advisory, read-only skill that routes authenticated-principal, exact-stanza, existing-mandate, and effective-authority inspection to typed Aegis read surfaces.

Neither skill authenticates, authorizes, approves, issues a mandate, provisions, activates, executes, widens authority, or attests completion. Installing either skill grants no Aegis authority. The inspection skill cannot select or union trust stanzas and does not turn fixture content or model narration into live authority.

The strict `skills/aegis-skills.json` manifest binds the bundle and each skill to exact content digests, compatibility ranges, operation ownership, dependencies, authority class, required operations and toolsets, sensitivity, network, filesystem, and file inventory declarations. `skills/evaluations.json` carries the non-secret structural evaluation cases. Neither file grants runtime authority.

## Validate and evaluate source

Run `make skillbundle-verify`. The target executes the Go validator and evaluator under `internal/skillbundle/`. Validation denies unknown JSON or YAML fields, trailing documents, undeclared files, symlinks, non-regular files, executable skill content, path traversal, digest or size drift, duplicate or orphan operation ownership, dependency errors, remote active content, inline network or shell behavior, secret-shaped literals, and positive prompt-authority claims.

A release build runs `go run ./internal/skillbundle/cmd build . DIST VERSION SOURCE_REVISION`, where `VERSION` is exact stable SemVer and `SOURCE_REVISION` is one exact lowercase 40-hex Git commit. The builder first validates and evaluates the repository source, injects that exact release version and source revision into the archive manifest, fixes archive ordering, ownership, permissions, and timestamps, and writes `aegis-skills_vVERSION.tar.gz`. `go run ./internal/skillbundle/cmd verify ARCHIVE SOURCE_REVISION` extracts into a disposable sibling directory, rejects unsafe archive members, repeats full manifest/content validation, and denies unless the embedded immutable revision exactly matches the caller's expected release revision. `SHA256SUMS` covers the skill archive alongside platform binaries.

## Hermes tap and direct install

Hermes Agent `>=0.18.0,<0.19.0` discovers immediate skill directories beneath a configured tap's `skills/` path. Against the published `berryhill/aegis` repository:

1. `hermes skills tap add berryhill/aegis` records the tap in the selected `HERMES_HOME`.
2. `hermes skills search aegis` shows the repository path and provenance.
3. `hermes skills inspect berryhill/aegis/skills/aegis` previews the routing skill without installation.
4. `hermes skills inspect berryhill/aegis/skills/aegis-trust-context-inspection` previews the read-only trust-context inspection skill without installation.
5. `hermes skills install berryhill/aegis/skills/aegis --yes` installs the direct repository routing skill.
6. `hermes skills install berryhill/aegis/skills/aegis-trust-context-inspection --yes` installs the direct repository inspection skill.
7. `hermes skills list`, `hermes skills audit aegis`, and `hermes skills audit aegis-trust-context-inspection` provide installed readback and Hermes's independent security scans.

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
