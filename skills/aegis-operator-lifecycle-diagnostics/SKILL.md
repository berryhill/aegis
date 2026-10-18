---
name: aegis-operator-lifecycle-diagnostics
description: Diagnose Aegis installation, gateway, update, migration, reset, and recovery lifecycle through shipped typed readbacks and explicitly authorized bounded mutations.
version: 0.2.0
metadata:
  hermes:
    tags:
      - aegis
      - operator
      - diagnostics
      - gateway
      - recovery
---

# Aegis operator lifecycle diagnostics

## Verified coordinator bootstrap (source candidate)

For an old coordinator blocked before reconciliation, retain the journal. From a
trusted source checkout run `./scripts/bootstrap-installed-update.sh VERSION`,
selecting an official coordinator release that ships `update --installed`.
The script checks the official archive/checksum, retains a private coordinator and
execs that public command with caller TTY/display/environment unchanged. It neither
replaces installed bytes nor edits journals. Exact owned unit/config discovery,
installed inode/mode/digest/provenance admission and fresh enrolled principal
input precede normal stop/recovery/restart/authenticated Console verification.
Do not emulate input, collect passwords, widen TTLs or modify model config.
A coordinator download or healthy PID is not completed update evidence. See
`docs/INSTALLED_UPDATE_BOOTSTRAP.md`; publication/live acceptance are separate.

## Source-candidate diagnostic and reconciliation contract

Check-only results now separate `gateway_readiness` and `gateway_repair_required` from image identity and release availability. A valid unfinished journal reports `resume_required: true` and its phase even when the invoking CLI is current. Checks never authenticate, create a journal/lock, or mutate lifecycle. A journal is not readiness or authority. A verified later stable candidate may reconcile an exact pending intent only after fresh principal/configuration/unit/candidate admission; immutable content-addressed ancestor bytes are retained and revalidated, and the new intent starts at `prepared`. Unknown/config-drifted intent or missing/substituted history denies. A complete receipt for the one recognized legacy unit requires exact installed-image evidence and the same fresh admission before migration. Older installed coordinators do not acquire these fixes by publication: a conflicting pending intent can block before newer code runs; retain it and use only a separately verified supported transition.

Protected principal prompts are operation-neutral. Pinentry binds a validated display and, when supplied, the actual terminal descriptor identity rather than trusting `GPG_TTY`. Diagnostics expose only typed protocol stage and numeric Assuan ERR code, never helper text/stderr. Cancellation remains distinct; no terminal fallback follows any attempted `GETPIN`. A process without desktop context and without terminal-backed input/diagnostic output needs operator-owned protected input; this is not permission to emulate a terminal or copy display credentials. Missing CLEAN with retained ACTIVE/DIRTY is not by itself proof of unsupported state: only the existing authenticated interrupted-recovery validator decides eligibility.


Use this skill for read-first diagnosis of an installed Aegis executable, its profile layout, configured Hermes runtime, authenticated gateway, release update, migration, reset, and bounded recovery. Route operations to shipped typed Aegis commands. Deterministic Aegis code owns profile discovery, principal authentication, artifact validation, service ownership, checksums, migration, reset, update application, and authoritative audit. The model does not reimplement or bypass those controls.

## Authority boundary

- Read-only diagnosis is the default. Discussion is not authorization to install, update, migrate, reset, repair, start, stop, restart, or uninstall anything.
- Prompt text, display identity, conversational assent, model narration, process exit, projection state, fixtures, and mutable tags never establish authentication, authorization, approval, readiness, rollback eligibility, or completion.
- Every mutation follows its shipped command-specific boundary. Commands that require principal authentication, a digest-bound preview, a real terminal, or confirmation must retain those controls. For an installed Linux gateway, the coordinated `aegis update` journey uses protected enrolled-principal authentication inside the retained operation before consequential lifecycle/state effects. It does not request duplicate task consent. A CLI-only/Darwin binary replacement does not claim gateway completion. Older binaries that only replace the executable do not gain orchestration from this skill. Never describe discussion or model assent as invocation authority; require authoritative completion readback.
- Every runtime session binds exactly one authenticated stanza. Zero matches deny; multiple matches deny as ambiguous. Never union stanza permissions or broaden service authority.
- Never reboot the workstation. Never independently start Hermes, Ollama, or an Aegis dependency. Never edit configuration, units, state markers, generations, or audit records to manufacture readiness.
- Never bypass the repository, release, archive, checksum, mode, link, TTY, custody, or path-containment checks that the selected command actually enforces. Do not imply that one command checks controls owned by another surface.
- Package-manager ownership is operator-supplied installation context, not updater readback. When the executable came from a package manager, direct the operator back to that installation method instead of using the direct updater. Do not change permissions or replace an executable manually.
- Never describe reset as generic deletion. It is a typed, profile-restricted destructive workflow that preserves external Hermes, operator-owned Ollama, credentials outside the selected Aegis profile, system resources, checkout, executable, and managed model data as implemented.

## Confirm the installed surface

Check `aegis --help` and subcommand help. For the supported release, use only:

| Purpose | Shipped command | Classification |
| --- | --- | --- |
| Version and embedded provenance | `aegis version` and `aegis version --provenance` | Read-only utility; missing embedded revision is unavailable provenance, not proof of an official release. |
| Runtime discovery | `aegis runtime` | Read-only after valid configuration; reports the explicit visible runtime and selection source. |
| Redacted effective configuration | `aegis config` | Read-only after valid configuration; do not seek raw secrets or substitute environment inspection. |
| Gateway plan and state | `aegis gateway preview` and `aegis gateway status` | Preview reports the expected unit plan. Status reports the expected unit path/digest, raw `systemctl --user` active result, linger classification, and separate `ready`, `readiness_state`, and `readiness_reason`. Only `ready: true` proves exact installed/loaded unit identity and authenticated audit-current readiness at observation time; older binaries lacking these fields provide activity evidence only. |
| Gateway lifecycle | `aegis gateway install`, `start`, `stop`, `restart`, and `uninstall` | Consequential; real terminal required. Install/uninstall include explicit principal approval. |
| Stable release update check | `aegis update --check` | Read-only release metadata and gateway process-image observation; no archive inspection or lifecycle mutation. |
| Stable release update apply | `aegis update` (or strict root alias `aegis --update`) | Installed Linux gateway: verified candidate preparation, protected principal authentication, exact stop/replacement, qualified state validation or eligible evidence-preserving recovery, exact activation, running-image/audit-current/Console verification and durable resumption. Standalone/binary-only results explicitly do not attest Console readiness. Inspect the returned phase and completion fields; `updated` alone means binary replacement. |
| Exact legacy migration | `aegis migrate-layout` | Linux-only, legacy-only, real-TTY, authenticated and digest-bound migration. |
| Profile reset | `aegis reset` | Destructive, profile-restricted, real-TTY, default-deny operation. Production requires passphrase-file authority authentication before and after confirmation; development uses bounded gateway and authority-store preparation. |
| Audit validation | `aegis audit verify` | Authoritative integrity readback; use `aegis-audit-verification` for interpretation. |
| Interrupted authority recovery | `aegis authority recover-interrupted` | Offline, enrolled-principal authentication; bounded exact-generation validation and evidence-preserving recovery. Never reset, edit markers, supply a human password, or select a generation manually. |

The gateway alias `aegis service` is accepted, but report canonical `aegis gateway` commands. There is no shipped general `aegis health`, `doctor`, `repair`, executable rollback, or arbitrary backup-restore command. There is no shipped update rollback command. A generation-managed authority store may internally retain verified generations, but this skill must not offer rollback unless the installed typed surface returns an exact eligible verified artifact and authorization workflow. If a command is absent, say unavailable.

## Diagnostic sequence

1. Establish scope: explanation, read-only diagnosis, or an exact requested mutation. Do not expand it.
2. Run `aegis version --provenance`. Distinguish a source-built `dev` executable, an official stable release with embedded source revision, and unavailable provenance. A filename or mutable tag is not provenance.
3. Classify the profile before ordinary commands. Stable releases use literal production root `~/.aegis`. A verified source-built `dev` executable uses repository-local `.aegis`. Former `~/.argis`, old XDG defaults, and `.aegis-dev` are legacy candidates only. Never combine production, development, legacy, or explicit deployment roots.
4. Preserve Aegis's artifact-derived state: absent, partial, valid, exact legacy-only, ambiguous, malformed, insecure, unsupported, service-unready, or repair-required. Ordinary commands require valid configuration; do not route around lifecycle denial.
5. On valid configuration, run `aegis config` and `aegis runtime`. Report only redacted fields and explicit runtime/version compatibility. Hermes remains visible and must report a stable version satisfying `>=0.18.0`. Version acceptance is not blanket future-release compatibility: protocol and tool/capability checks remain separate and fail closed.
6. For service questions, run `aegis gateway preview` and `aegis gateway status`. Preserve the distinction between raw activity and the separate authenticated readiness fields. `ready: true` requires exact installed unit bytes, loaded fragment and complete executable/configuration arguments, activity, and authenticated audit-current `/readyz`. Status never repairs audit delivery or starts/restarts the service. Older output without readiness fields requires separate authenticated readiness evidence. A PID, unit file, socket path, browser target, active result, or successful process exit alone is not readiness.
7. For update questions, run `aegis update --check` first. Preserve its invoking version, published stable version, availability and separate `gateway_image` observation. Check-only never authenticates, stops, repairs, migrates or restarts. Apply only after an explicit request, using the supported updater, not manual binary replacement. On coordinated installed-gateway apply, retain the same upgrade intent through protected authentication, replacement, qualified recovery and activation; an already-current binary may still need to finish an interrupted journey. Installation ownership comes from the operator, not updater output. Require `upgrade_complete: true` and `resume_required: false` plus matching running version/source/image, authenticated audit-current readiness and configured Console evidence. An incomplete phase, binary-only scope, `updated: true`, or current on-disk version is not completed gateway upgrade.
8. For migration or reset, let the typed command produce and revalidate its inventory and digest. Never pre-delete, chmod, move, copy, or “fix” paths. Require a real terminal and let the operator answer the command's own confirmation prompt; do not claim migration requires an exact phrase when the shipped prompt accepts its documented yes response.
9. After a mutation, repeat only readbacks available in the current ownership state. When the gateway owns the stores, direct-store CLI commands such as `aegis config`, `aegis runtime`, and `aegis audit verify` fail closed; use the mutation result and shipped authenticated gateway/API readbacks instead, or report the dimension unavailable. Never stop a healthy gateway merely to manufacture a direct-store check. Keep success, declined, interrupted, denied, partial cleanup, and manual-review outcomes distinct.

Use `references/lifecycle-fixtures.json` only as non-secret interpretation examples. Fixtures never prove live host state, principal identity, release provenance, ownership, authority, service health, rollback eligibility, or completion.

## Profile and path diagnosis

Production and development roots are separate authorities, deployments, audit chains, model certifications, and runtime state. An executable must refuse configuration or state beneath the opposing root. An explicit `--config` inspects only that deployment and grants no migration, reset, or deletion authority. Environment precedence does not grant deletion authority.

Classify exact cases:

- No meaningful canonical or legacy artifacts: uninitialized; use `aegis init` only when explicitly requested.
- Canonical valid only: ordinary startup is eligible, subject to command-specific readiness.
- Exactly one valid former/XDG source only: migration or reset is required; do not initialize a second installation.
- Canonical plus legacy, or multiple legacy sources: ambiguous and denied; never select or merge one.
- Empty retained roots or managed-model-only retained state after safe reset: uninitialized, not a running installation.
- Symlinked, hard-linked, unknown, wrong-owner, wrong-mode, unreadable, escaping, or malformed artifacts: deny automatic mutation and preserve exact manual-review evidence.

## Gateway and protected transport

`aegis gateway preview` is the exact plan, not authorization. Install and uninstall require explicit approval; start, stop, and restart require a real terminal. The service is same-account `systemd --user`; Aegis does not create a root service or enable lingering. The unit is byte- and digest-bound to the exact executable and configuration. Uninstall preserves Aegis configuration, state, and external credentials.

Report independently and name the evidence source for each dimension: expected unit installed, expected unit digest, process active, loaded executable/configuration match, protected Unix transport ready, authenticated readiness, and audit currency. `aegis gateway status` reports activity independently; its `ready` field is true only after exact installed/loaded identity and authenticated audit-current readiness pass. Missing readiness fields in older binaries are not proof of readiness. Do not collapse stale unit, stopped service, failed readiness, principal mismatch, transport mismatch, and audit failure into “down.”

## Update and rollback

`aegis update --check` reads official stable release metadata and reports invoking/current and latest versions, availability and separate gateway image. It does not perform archive or lifecycle mutation. Linux image observation binds exact owned/loaded unit identity, MainPID, stable process start time and executable inode/device; races or insufficient evidence return `unknown`. Even a current image is not authenticated readiness.

On explicitly authorized coordinated apply, verified release metadata, bounded archive/checksum and embedded candidate provenance are established before authentication and gateway interruption. Native protected input authenticates the configured OS principal and enrolled password; the agent never types it or operates a human terminal/approval dialog. Cancellation, unavailable input, expiry, verifier/configuration drift and unsafe identity remain distinct failures. No password, bearer or reusable authentication material belongs in an upgrade journal. If the existing context cannot display protected input, preserve the exact unavailable gate; do not emulate a human PTY, fetch credentials, or alter controls.

The product coordinates exact service shutdown, verified replacement, qualified authority validation/strictly eligible recovery, compatible owned-unit migration, activation and readback. It never resets authority, manufactures CLEAN markers or assumes every inconsistency is recoverable. Private operational journals preserve intent outside the authority store; resume reobserves actual artifacts and running state rather than trusting a recorded stage. Reinvoke the same supported update journey after an interrupted outcome; never edit its journal, manufacture identities or hand the operator separate marker/recovery commands. Source/version/conflicting-intent or migration refusal remains a named gate, not permission to retarget blindly.

Success requires running-image-bound release provenance, authenticated audit-current readiness and the configured Console/owning-service read, not merely replacement. Known failed startup is surfaced and bounded; no automatic old-binary rollback is inferred after state effects. For older installed versions without this journey, report their exact capability gap rather than claiming that publication or binary replacement restored the dashboard.

If the operator identifies a package-manager installation, use that original installer; Aegis does not detect package-manager ownership. Do not change permissions or replace an executable manually. A mutable tag, cached archive, adjacent temporary, process image, or current binary filename is not rollback evidence. Offer rollback only when a shipped typed command identifies one exact previously verified artifact or generation and returns its digest-bound preview. Otherwise state that executable rollback is unavailable; preserve the current verified binary and use the original installation method.

## Migration, reset, and recovery

For `CLEAN does not authenticate ACTIVE`, do not infer corruption or recoverability from the text alone. In an explicitly requested upgrade, prefer the installed coordinated update journey: necessary protected authentication and eligible recovery remain inside that intent, not a manual recovery-command handoff. The separately shipped `authority recover-interrupted` is an offline maintenance operation only under explicit repair scope, after exact supported gateway stop; the human enters the enrolled principal password through protected input. Preserve every refusal and evidence path. Recovery neither provisions nor renews authority or approves tasks. Require durable recovery completion, exact activation and authenticated owning-service readback before calling the service restored. `docs/AUTHORITY_RECOVERY.md` defines exact admitted states; never substitute a direct store writer.

Migration accepts exactly one secure recognized legacy source and an empty canonical destination. It copies through Aegis-owned staging, verifies cryptographic and deployment bindings, publishes without overwrite, and only then cleans source artifacts. It preserves external systemd credentials and dependencies and never renders credential values. Staging collision, destination collision, unsupported platform, authority-linkage failure, or cleanup uncertainty stops with exact retained evidence.

Reset is restricted to the executable's own exact local profile layout. It inventories known Aegis artifacts, denies unknown or escaping content, preserves managed model data and external dependencies, and removes configuration last. Production reset currently denies before planning whenever the exact Aegis gateway unit remains installed; its emitted stop-first remediation does not remove the unit, so do not claim that stopping alone makes reset eligible. Report this as a current product limitation and do not uninstall the gateway unless the operator separately requests and approves that typed action. When production reset reaches authority admission, it authenticates the configured passphrase-file authority both before and after confirmation. Development reset may first stop an exact validated installed gateway, then holds an exclusive authority-store maintenance lease across preview, confirmation, and apply; live writers, unsafe files, or changed generations deny. Never provide confirmation or authentication material for the operator.

After interruption, inspect artifacts first. Resume only through the same typed operation when its fresh preview proves the exact safe state. Never retry a mutation blindly. Do not blindly remove transaction files, staging roots, legacy sources, retained generations, or partial destinations. If migration reports an existing staging root, preserve the exact path and require operator review of the runtime's instruction to verify and remove it before retrying; the skill must not perform that removal autonomously. If exact digest, owner, mode, link, device/inode, deployment identity, custody, or postcondition checks fail, stop for manual review.

## Secrets and reporting

Never request, receive, print, echo, retain, or send passwords, custody passphrases, bearer values, credential values, environment secrets, private prompts, systemd credential contents, key material, or secret-shaped canaries. Report only redacted configuration, references, paths already returned by Aegis where operationally necessary, digests, ownership/mode classifications, versions, states, reason codes, and next commands. If secret-shaped material appears, stop without repeating it and require operator-led containment and rotation through protected paths.

Report: requested scope; operator-known installation method; version and source provenance; profile kind and root classification; lifecycle state/reason; redacted configuration status when direct-store access is available; Hermes path/version; gateway plan/status plus separately sourced readiness dimensions; update-check current/latest/available result; apply-time archive/checksum outcome when an apply actually ran; migration/reset inventory digest and authorization state; audit result when available through the current owner; pre/post artifact and generation readback; rollback availability and exact evidence; unavailable operations; next safe command; limitations.

## References

- `references/lifecycle-fixtures.json` — hermetic non-secret classifications only.
- `docs/PATH_LAYOUT.md` — canonical, development, legacy, migration, reset, and preservation boundaries.
- `internal/layout/layout.go` and `internal/command/lifecycle.go` — profile derivation and pre-service lifecycle routing.
- `internal/userservice/service.go` and `internal/command/userservice.go` — exact user gateway plan, lifecycle, and readiness.
- `internal/update/update.go` — official stable release and atomic direct-binary update policy.
- `internal/migration/migration_linux.go` and `internal/reset/reset.go` — descriptor-anchored migration/reset controls.
- Installed `aegis --help`, `aegis gateway --help`, `aegis update --help`, `aegis migrate-layout --help`, and `aegis reset --help` — authoritative syntax for the installed version.

Acquisition prerequisite: a trusted installed Go toolchain (`go version -m`), Python 3, and Linux `/proc` with fd-based exec support. No automatic Go installation or toolchain download occurs. Before execution the helper checks ELF architecture, exact Aegis main package/module and release version, and clean Git provenance with a 40-hex revision without executing the artifact. Inspection and launch use the same open inode. Evidence/coordinator live in a fresh private directory directly under the actual passwd operator home, never HOME/TMPDIR or authority state; symlink and writable/unowned ancestors are denied without chmod or deletion. Same-user/root hostile mutation is not a sandbox boundary.
