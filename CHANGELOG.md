# Changelog

This project follows a Keep a Changelog-style structure. Development builds report version `dev`, while the release workflow injects the exact tag version.

## Unreleased

### Added

- Bound stable release binaries to an isolated `production` profile at `~/.argis` and ordinary source-built `dev` binaries to a visible, Git-ignored `development` profile at `<repository>/.aegis`. Development execution verifies that the binary resides in the real Aegis module/worktree root and fails closed if copied. Configuration, credential authority/deployment identity, audit/checkpoint data, manager certification, runtime state, and cleanup targets no longer share defaults; development rejects production paths, and reset is restricted to its own exact layout. Development reset uses the exact preview and default-deny `yes` confirmation without a passphrase prompt. Production reset authenticates the existing minimum-12-byte authority passphrase before confirmation and independently again after `yes` before deleting credential records or a local encrypted KEK.

## [0.1.26] - 2026-07-21

### Changed

- Made explicit authenticated credential count/list/create/value-read requests execute directly against typed authority operations without model negotiation or confirmation. Exact-reference retrieval checks active/revoked state, emits metadata-only audit, and renders terminal-escaped plaintext only in session-scoped presentation state. Complete inline creates no longer enter or poison the Hermes conversation. Fixed `credential names "…"` parsing so it no longer silently falls back to `new-credential`. The unambiguous imperative itself authorizes that exact parsed inline create, so model requests for more fields or confirmation—and model-selected target changes—cannot veto or redirect it. Paired shorthand such as `key: "record-name" secret: "credential-value"` deterministically treats the key field as the record reference and the secret field as the session value, preventing sensitive-tracker/reference inversion; the common `stay` typo is accepted only on that narrow paired-field create path. Successful internal startup checks are quiet by default, leaving one concise authenticated/ready transition. Cleanup now terminates dedicated managed Ollama directly, reserves unload polling for external-local mode, and reports stable failed teardown stage names instead of a generic incomplete-cleanup message. Added tracked-value response non-echo, session content/history clearing, updated conformance requirements, and canary-backed guard/proxy/session tests. This invalidates prior manager certifications and requires explicit recertification. Terminal scrollback and host/root/forensic capture remain outside the purge guarantee.

## [0.1.25] - 2026-07-21

### Fixed

- Made clear natural-language credential-save requests enter a deterministic Aegis-owned metadata proposal without requiring operators to understand `reference`, `kind`, or `disclosure`; inline quoted values are discarded before retention or model context and must be re-entered through protected no-echo intake after exact confirmation.
- Enabled terminal bracketed-paste mode in the rich manager composer, retained multiline clipboard text as one guarded submission with normalized CRLF, restored terminal paste mode on every exit, and verified that the ingress guard scans the complete multiline envelope for secrets.

## [0.1.24] - 2026-07-20

### Fixed

- Made conversational certification use a bounded three-execution loop with direct case-specific requests instead of repeating ambiguous wording until principal authority expires, accepted equivalent truthful encrypted-custody/out-of-model wording instead of requiring the exact phrase `protected intake`, raised the principal authority default to its validated 15-minute maximum and the manager turn/Ollama request defaults to five minutes so the complete local corpus can finish on supported CPU-bound deployments, and added `manager certify --continue-on-error` to execute the remaining corpus diagnostically without ever publishing a failed or partial certification.

## [0.1.23] - 2026-07-20

### Fixed

- Retried schema-valid certification replies that omit required conversational content with up to three fresh executions, while preserving immediate fail-closed behavior for invalid envelopes, forbidden claims, operational failures, cancellation, and authority expiry.

## [0.1.22] - 2026-07-19

### Fixed

- Allowed fresh release preparation to consume existing unstaged changelog entries while preserving and restoring the original changelog exactly on dry-run or pre-commit failure.
- Streamed canonical message-only Hermes responses through bounded monotonic sanitized snapshots while retaining complete final-envelope validation; proposal and non-canonical output remains buffered, invalid completed streams are visibly rejected, rich turn progress updates in place, and plain terminals no longer print repeated elapsed-time lines.
- Corrected the manager's contradictory credential-storage guidance: it now states that Aegis stores actual credential values encrypted after protected no-echo intake while the conversational model receives metadata only. The create-operation exemplar and conformance corpus now use the implementation's required `protected` disclosure value, and a new security-critical certification case rejects false metadata-only custody claims. The instruction and corpus identity change invalidates prior certifications and requires explicit recertification.
- Added the Aegis-owned model-visible credential bridge for the sole typed `github.get_repository.v1` broker action. Exact `aegis` stanzas now launch a hidden stdio MCP server from a disposable Hermes home, keep the session capability out of argv/environment/model context, disable ambient rules/plugins/skills/toolsets, and fail closed unless the live Hermes gateway reports exactly `mcp__aegis__github_get_repository`. Unknown MCP methods/tools/arguments and mismatched broker grants deny.

## [0.1.21] - 2026-07-19

### Fixed

- Removed the manager instruction's canned `Acknowledged safely.` exemplar, required relevant replies for ordinary conversation, and added manager-specific conversational conformance so certified small local models cannot pass by copying a generic acknowledgement. The instruction and corpus identity change invalidates prior certifications and requires explicit recertification.

## [0.1.20] - 2026-07-19

### Added

- Added one injected pinentry-first authority-passphrase service for create/confirmation and unlock/verification, with bounded Assuan protocol parsing, direct process execution, allowlisted desktop/session environment, typed cancellation/policy/protocol failures, bounded retries, process-tree cleanup, and hermetic fake-helper coverage.

### Fixed

- Corrected authority intake behind synchronized bootstrap output: genuinely unavailable pre-interaction pinentry now falls back to terminal-backed no-echo stdin plus diagnostic output without requiring the presentation writer itself to be an `*os.File`; cancellation and post-interaction failures remain fail-closed.

## [0.1.19] - 2026-07-19

### Added

- Added the complete typed Core 15 manager base registry and real authenticated composer path: bounded exact parsing and local unknown/malformed consumption, state-aware help/completion, canonical alias/policy/audit naming, typed result/presentation events, authoritative orientation and audit commands, durable Aegis-native core scans/findings/investigations/local report revisions, authoritative timeline queries, cancellation/presentation/cleanup semantics, and an explicit unavailable watch/endpoint-adapter boundary.
- Replaced the built-in manager's line-only presentation with an Aegis-owned typed terminal controller: persistent authoritative principal/stanza/mandate/Hermes/route context, rich and accessible/plain profiles, a restorable multiline PTY composer with Ctrl+J newline, history, reverse search, bracketed paste and local help, bounded typed timeline state, focused exact approval and metadata-only protected-intake states, and real lifecycle/cleanup events wired to the production manager path.
- Added a centralized contextual terminal sanitizer for model/runtime text, external status, and security fields. It strips CSI/OSC/DCS/APC/PM/SOS and unsafe C0/C1 controls, neutralizes carriage-return and bidi/invisible rewriting, repairs malformed UTF-8, and applies bounded bytes/runes/lines/width before layout.

## [0.1.18] - 2026-07-19

### Fixed

- Repaired live Hermes 0.18.2 manager certification end to end: the disposable gateway now receives the Aegis contract through supported `session.create` seed history, resolves the authenticated local route through Hermes's OpenRouter-compatible custom-base environment, uses a real empty toolset, accepts and strips Hermes's `session_id` request extension, validates buffered streaming responses, and constrains local generation to the closed response schema. Certification isolates ordinary cases, executes a genuine repeated-turn case, and publishes only after every real Hermes → authenticated proxy → exact Ollama case passes.

## [0.1.17] - 2026-07-18

### Fixed

- Made live manager certification deterministic for small local models by fully specifying the strict response envelope and typed operation argument schemas without weakening authorization or secret-handling rules. The Hermes gateway now rejects `error` and `interrupted` completion statuses, and the authenticated OpenAI-compatible proxy accepts standards-compliant JSON media-type parameters while retaining strict body validation.

## [0.1.16] - 2026-07-18

### Fixed

- Corrected the live Hermes 0.18.x manager route to use `OPENAI_BASE_URL`/`OPENAI_API_KEY`, accepted the documented `session_id` gateway event field, and bound streamed events to the active session, fixing immediate live-certification protocol failures that fixture-only tests did not reproduce.

## [0.1.15] - 2026-07-18

### Fixed

- Bounded every live manager-certification Hermes turn by `manager.hermes.turn_timeout`, aborting the corpus and runtime transaction on timeout, cancellation, authority expiry, transport failure, invalid response, or semantic failure. Interrupted gateway sessions are poisoned so stale uncorrelated events cannot satisfy a later turn; failures name the exact case and stable reason, publish no certification, and bootstrap prints the retry command.

## [0.1.14] - 2026-07-18

### Fixed

- Replaced reset's exact phrase with a conventional default-deny `[y/N]` confirmation while retaining exact-plan preview and immediate pre-apply revalidation.
- Removed bootstrap's meaningless one-item model menu: an exact sole approved installed candidate is now selected visibly and automatically, while multiple candidates still require an explicit no-default selection.

## [0.1.13] - 2026-07-18

### Added

- Added a working bare-terminal credential-authority default: no-echo passphrase setup, Argon2id-derived wrapping, an XChaCha20-Poly1305 encrypted random KEK file, atomic database initialization, deployment-sentinel verification, process-local unlock, and deterministic recovery from an incomplete undelivered systemd-custody selection.

### Fixed

- Replaced generated copy/paste confirmation sentences throughout bootstrap and layout migration with conventional `[Y/n]` confirmation; Enter now accepts displayed safe defaults while digest and artifact drift checks remain authoritative.

## [0.1.12] - 2026-07-18

### Added

- Unified bare local defaults under literal `~/.argis`, with one typed home/layout resolver, secure mode validation, read-only canonical/legacy discovery, fail-closed coexistence, and Linux `aegis migrate-layout` using exact confirmation, digest binding, verified copy/publication, and descriptor-anchored source cleanup.

### Fixed

- Kept a confirmed systemd credential-custody selection as a resumable onboarding prerequisite instead of misclassifying the intentionally absent external credential/database as repair-required. After systemd delivers the KEK, `aegis init` now separately previews and confirms creation of the deployment-bound authority database without copying or modifying the delivered credential.
- Restored the executable no-key demonstration by adding the required bounded manager cleanup timeout to `examples/aegis.yaml`, with a regression test that loads the launch configuration through the strict decoder.
- Corrected legacy reset beneath mode-`0775` external XDG parents without weakening artifact checks: Aegis now uses device/inode-verified descriptor-relative deletion, never chmods the external parent, and can retain an empty legacy child while default discovery returns `uninitialized`.

## [0.1.11] - 2026-07-18

### Added

- Added `aegis reset`, a pre-service-construction, host-authenticated, exact-plan-bound first-run replay command with deterministic preview, real-TTY exact-phrase confirmation, strict path/inode/ownership inventory, configuration-last deletion, credential/audit destruction disclosure, preservation of external runtime/model/profile/systemd assets, and hermetic reset-to-onboarding coverage.

## [0.1.10] - 2026-07-18

### Fixed

- Accepted the documented Ollama 0.32 model-inventory metadata during strict installed-candidate discovery, while retaining rejection of unknown response fields.

## [0.1.9] - 2026-07-18

### Added

- Added principal-authenticated, no-default manager-model candidate listing, managed/external-local route preview, installed-only loopback Ollama discovery, exact digest-bound configuration preview/confirmation, atomic secure publication, and configuration/artifact/certification drift status without model download, copy, certification, or activation.

### Fixed

- Made manager terminal intake cancellation-aware, including Linux no-echo intake and confirmation restoration, and unified operator exit, plain aliases, EOF, expiry, runtime failure, and first-signal cancellation under bounded idempotent cleanup with default second-signal termination.
- Added explicit lifecycle/readiness state, exact degraded reason reporting, truthful command availability, and hermetic PTY/fake Hermes/Ollama verification for cancellation, signal, cleanup, and onboarding behavior.

## [0.1.8] - 2026-07-18

### Fixed

- Allowed the bounded HTTPS redirect from GitHub release URLs to GitHub's release-asset host while continuing to reject API, multi-hop, non-HTTPS, credential-bearing, and untrusted-host redirects.

## [0.1.7] - 2026-07-18

### Fixed

- Added `aegis version` as a configuration-free equivalent of `aegis --version`.

## [0.1.6] - 2026-07-18

### Fixed

- Made release publication safely resumable after an interrupted atomic push by verifying the immutable local signed tag, exact release commit/changelog, local and remote ref identities, and tagged source before publishing only the missing refs; ambiguous states fail closed and dry-run remains non-mutating.
- Strengthened hermetic updater discovery coverage and validation for stable publication metadata, official repository identity, redirects, downgrade attempts, checksums, and malformed archives while retaining published-release-only selection and atomic replacement.
- Disabled terminal echo before rendering protected-intake prompts, closing the prompt-to-password-read race that could echo immediately supplied secret bytes, and verified exact echo-state restoration.

## [0.1.4] - 2026-07-18

### Added

- Connected the built-in manager through exact certification, managed/external-local Ollama lifecycle, an expiring replay-resistant loopback proxy, disposable safe-mode Hermes gateway sessions, shared credential operations, protected no-echo mutations, metadata-only history/results/receipts, and rollback-safe cleanup, with hermetic fake-process and random-canary coverage.

### Fixed

- Added strict root-only `aegis --update` dispatch through the same injected checksum-safe update service as `aegis update`, with ambiguous action combinations denied.
- Added pre-configuration root dispatch and deterministic first-run initialization: host-native UID/user verification, exact path/content preview, explicit confirmation, atomic mode-`0600` configuration publication, recognized interrupted-write recovery, fail-closed malformed/insecure/ambiguous handling, and actionable non-TTY uninitialized output.

## [0.1.3] - 2026-07-17

### Fixed

- Release preparation now verifies signed-tag and pinentry availability in its disposable clone before creating the real release commit, so signing failure leaves the source repository unchanged.

## [0.1.2] - 2026-07-17

### Fixed

- Release packaging now verifies `SHA256SUMS` from the archive directory, preventing valid archives from being reported missing before publication.

## [0.1.1] - 2026-07-17

### Added

- Strict built-in manager configuration, immutable local route/model identity contracts, deterministic policy and response envelopes, closed typed proposal codecs, metadata-only receipts, stable manager reason codes, and an official/traceable candidate registry with no uncertified default.
- Bare interactive `aegis`, explicit `aegis manager`, and `aegis init` dispatch with terminal ownership, fixed `secrets-manager` context visibility, deterministic slash controls, fail-closed credential-paste scanning, and honest no-model fallback.
- Bounded credential metadata list/search, a session-authenticated exact-model loopback inference proxy with request/response scanning, a strict local Ollama fixture adapter, and a reusable multi-turn Hermes gateway protocol client with malformed/oversized/timeout fixture tests.

### Known limitations

- No real Ollama artifact was downloaded or certified, so no manager model is selected. Managed Ollama process supervision, complete protected-intake UI operations, persistent certification/receipts, and the final end-to-end Hermes-to-proxy route remain incomplete and are not claimed.

### Fixed

- Release-tag CI now compares the built CLI and adapter versions directly instead of comparing a tagged child binary with the `dev` test-process version.
- Self-update now distinguishes a missing published GitHub release from a generic HTTP failure and explains the required fail-closed remediation; installation and release documentation records the current failed `v0.1.0` deployment instead of implying that release assets exist.

## [0.1.0] - 2026-07-17

### Added

- Go/Cobra CLI and Echo v5 control-plane API over an explicit Hermes Agent adapter.
- Strict canonical charters, one-to-many trust stanzas, deterministic selection, mandates, exact single-use approvals, deterministic Aegis-owned provisioning, session lifecycle control, and hash-linked audit checkpoints.
- Disposable Hermes design and operational homes, toolset launch-argument verification, typed provider credential resolution, Unix peer-credential API identity, optional TCP TLS, pre/post-authentication rate limiting, and stable route telemetry abstraction.
- Hermetic CLI and complete Unix-socket API workflow tests, in-flight graceful-shutdown coverage, short sanitized no-key terminal recording, and bounded fuzz campaigns.
- Explicit review fields for all approval-relevant scope, complete stored-plan digest verification, injectable audit authority, and interrupted-provisioning recovery.
- Stable Semantic Versioning release enforcement, module-version detection for `go install`, and a checksum-verifying atomic `aegis update` command for supported release platforms.
- Deterministic `make release` preparation from a dirty checkout via isolated committed-source verification, signed-tag publication, and capability-restricted advisory Hermes review; invoking the target is the explicit operator authorization.
- Deployment-bound embedded bbolt credential authority with per-version envelope encryption, versioned external KEK custody, strict codecs and startup checks, no-echo principal intake, exact credential bindings, rotation, logical revocation, metadata-only inspection/audit, and consistent ciphertext backups.
- Linux pathname-socket credential broker with pre-body `SO_PEERCRED`, digest-only session capabilities, bounded deadline/request-ID replay state, exact mandate/runtime/binding reauthorization, immediate lifecycle revocation, and one bounded `github.get_repository.v1` action that applies authentication internally and returns sanitized metadata.

### Security

- Release and development builds now require Go 1.26.5 or newer, avoiding reachable standard-library vulnerabilities present in the initial Go 1.26 patch releases.
- Ambient provider credentials are excluded from Hermes launches.
- Unknown provisioning effects, wildcard authority, ambiguous stanza matches, any mutated stored plan field, replayed approvals, unsupported Hermes versions, interrupted publication, and bearer-only principal claims fail closed.
- Credential ciphertext/context mutation, wrong KEKs, unsafe authority/key-file ownership or modes, duplicate exact bindings, wrong destinations, and revoked records/versions fail closed.
- Trust stanzas now require complete policy blocks plus issuer/environment-bound identity selectors; stored canonical policy and mandate authority are rechecked, effective inspection is authenticated, narrowing requests have safe reason codes, and CLI/API denials preserve the same shared decision.

### Known limitations

- Hermes-home isolation is not host sandboxing.
- Hermes 0.18.x has no stable post-launch individual-tool enumeration used by Aegis.
- Audit append/checkpoint authority needs a separately protected deployment boundary for stronger tamper resistance.
- TCP TLS has no certificate-to-subject mapper; principal API operations require Unix peer credentials.
- The broker is not yet exposed as a verified model-visible Hermes tool because Hermes 0.18.x safe-mode bridge registration remains unresolved. Production service/runtime user provisioning, systemd unit/TPM recovery, selective fleet projections, network confinement, and Infisical migration remain external work. Operational Hermes provider credentials remain environment-backed.
