---
name: aegis-operator-lifecycle-diagnostics
description: Diagnose Aegis installation, gateway, update, migration, reset, and recovery lifecycle through shipped typed readbacks and explicitly authorized bounded mutations.
version: 0.1.0
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

Use this skill for read-first diagnosis of an installed Aegis executable, its profile layout, configured Hermes runtime, authenticated gateway, release update, migration, reset, and bounded recovery. Route operations to shipped typed Aegis commands. Deterministic Aegis code owns profile discovery, principal authentication, artifact validation, service ownership, checksums, migration, reset, update application, and authoritative audit. The model does not reimplement or bypass those controls.

## Authority boundary

- Read-only diagnosis is the default. Discussion is not authorization to install, update, migrate, reset, repair, start, stop, restart, or uninstall anything.
- Prompt text, display identity, conversational assent, model narration, process exit, projection state, fixtures, and mutable tags never establish authentication, authorization, approval, readiness, rollback eligibility, or completion.
- Every mutation follows its shipped command-specific boundary. Commands that require principal authentication, a digest-bound preview, a real terminal, or confirmation must retain those controls. `aegis update` is an explicit direct invocation, but it does not authenticate the principal or present a separate apply preview/confirmation. Never describe discussion or model assent as invocation authority, and require authoritative post-operation readback.
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
| Gateway plan and state | `aegis gateway preview` and `aegis gateway status` | Preview reports the expected unit plan. Status reports the expected unit path/digest, `systemctl --user` active result, and linger classification; it is not authenticated transport, loaded-`ExecStart`, or audit-current proof. |
| Gateway lifecycle | `aegis gateway install`, `start`, `stop`, `restart`, and `uninstall` | Consequential; real terminal required. Install/uninstall include explicit principal approval. |
| Stable release update check | `aegis update --check` | Read-only network operation; accepts only the official published stable repository release metadata. |
| Stable release update apply | `aegis update` | Consequential direct-binary replacement after repository, version, redirect, archive, and checksum validation. |
| Exact legacy migration | `aegis migrate-layout` | Linux-only, legacy-only, real-TTY, authenticated and digest-bound migration. |
| Profile reset | `aegis reset` | Destructive, profile-restricted, real-TTY, default-deny operation. Production requires passphrase-file authority authentication before and after confirmation; development uses bounded gateway and authority-store preparation. |
| Audit validation | `aegis audit verify` | Authoritative integrity readback; use `aegis-audit-verification` for interpretation. |

The gateway alias `aegis service` is accepted, but report canonical `aegis gateway` commands. There is no shipped general `aegis health`, `doctor`, `repair`, executable rollback, or arbitrary backup-restore command. There is no shipped update rollback command. A generation-managed authority store may internally retain verified generations, but this skill must not offer rollback unless the installed typed surface returns an exact eligible verified artifact and authorization workflow. If a command is absent, say unavailable.

## Diagnostic sequence

1. Establish scope: explanation, read-only diagnosis, or an exact requested mutation. Do not expand it.
2. Run `aegis version --provenance`. Distinguish a source-built `dev` executable, an official stable release with embedded source revision, and unavailable provenance. A filename or mutable tag is not provenance.
3. Classify the profile before ordinary commands. Stable releases use literal production root `~/.aegis`. A verified source-built `dev` executable uses repository-local `.aegis`. Former `~/.argis`, old XDG defaults, and `.aegis-dev` are legacy candidates only. Never combine production, development, legacy, or explicit deployment roots.
4. Preserve Aegis's artifact-derived state: absent, partial, valid, exact legacy-only, ambiguous, malformed, insecure, unsupported, service-unready, or repair-required. Ordinary commands require valid configuration; do not route around lifecycle denial.
5. On valid configuration, run `aegis config` and `aegis runtime`. Report only redacted fields and explicit runtime/version compatibility. Hermes remains visible and must satisfy `>=0.18.0,<0.19.0`.
6. For service questions, run `aegis gateway preview` and `aegis gateway status`. Preserve their limited evidence: expected unit plan, expected unit path/digest, active result, and linger classification. Establish loaded executable/configuration identity, protected transport readiness, authenticated readiness, and audit currency only from a separate shipped authenticated readiness or operation result that actually reports them. A PID, unit file, socket path, browser target, `active=true`, or successful process exit alone is not readiness.
7. For update questions, run `aegis update --check` first. Preserve only its current version, latest published stable version, and update-available result. Platform selection, redirect/archive bounds, `SHA256SUMS` matching, digest verification, and replacement happen only during `aegis update`; the check-only result does not pre-authorize or bind an apply. Installation ownership comes from the operator's known installation method, not updater output. Apply only after an explicit request.
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

Report independently and name the evidence source for each dimension: expected unit installed, expected unit digest, process active, loaded executable/configuration match, protected Unix transport ready, authenticated readiness, and audit currency. `aegis gateway status` proves only the expected-unit/digest, active-result, and linger dimensions. Do not collapse stale unit, stopped service, failed readiness, principal mismatch, transport mismatch, and audit failure into “down.”

## Update and rollback

`aegis update --check` reads official stable release metadata and reports the current/latest versions and whether an update is available. It does not inspect the platform archive, checksum, installation owner, or filesystem replacement conditions. On explicit `aegis update`, validation includes stable SemVer, publication metadata, supported platform asset, restricted redirects, bounded archive structure, matching `SHA256SUMS` entry, digest verification, downgrade refusal, adjacent temporary creation, atomic replacement, and directory sync. An interrupted download or pre-replacement failure leaves the executable unchanged; after uncertainty, rerun `aegis version --provenance` and `aegis update --check` rather than assuming either version.

If the operator identifies a package-manager installation, use that original installer; Aegis does not detect package-manager ownership. Do not change permissions or replace an executable manually. A mutable tag, cached archive, adjacent temporary, process image, or current binary filename is not rollback evidence. Offer rollback only when a shipped typed command identifies one exact previously verified artifact or generation and returns its digest-bound preview. Otherwise state that executable rollback is unavailable; preserve the current verified binary and use the original installation method.

## Migration, reset, and recovery

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
