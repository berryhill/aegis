# Changelog

This project follows a Keep a Changelog-style structure. Development builds report version `dev`, while the release workflow injects the exact tag version.

## Unreleased

### Added

- Added the authenticated browser command-intent foundation under `/console/api/commands/preview` and `/console/api/commands/execute`: a closed versioned catalog, strict JSON/native-form request models, short-lived exact-target previews, server-derived subject/session/authority bindings, fresh execute-time authority and target admission, and exact duplicate readback. The live dashboard catalog is intentionally empty until a page-owned application service, authority adapter, and acceptance tests are registered together; unknown commands deny. Browser identity/authority/audit/success claims are rejected, and an uncertain commit is retained as terminal so cancellation, repository/audit failure, malformed receipt, or response retry cannot invoke a mutation twice.
- Added a typed reusable browser-console interaction and visual-state foundation for native modal dialogs and responsive drawers, labelled/help/error-aware forms, error summaries, exact-reference review, display-only authority context, confirmations, operation receipts, notices, filters, pagination, loading/empty/denied/unavailable/degraded/error states, and non-semantic skeletons. The overlays use the browser's native declarative dialog commands so focus containment, background inertness, Escape dismissal, and focus restoration require no executable product JavaScript; real-Chrome coverage verifies keyboard behavior, narrow-view rendering, and color-independent state distinction while consequential actions remain native same-origin links and forms.
- Added principal-password authentication as the dashboard happy path: approved initialization collects and confirms a protected password, persists only an owner-only salted scrypt verifier, and exact-origin login creates a bounded configured-principal browser session with throttled denial. Authenticated rotation requires the current session/CSRF, fresh current password, exact confirmation, and explicit approval; exact-current atomic replacement and authoritative audit complete before all older sessions and one-time handoffs are revoked. The authenticated-host `aegis console` handoff remains alternate sign-in and is explicitly not password recovery.
- Added protected principal-only credential mutation boundaries across the shared application service, Cobra, and local Unix API. `POST /v1/credentials`, `POST /v1/credentials/:record/rotations`, `POST /v1/credentials/:record/revocations`, `POST /v1/credentials/:record/bindings`, and `POST /v1/credentials/backup` now create, rotate, revoke, bind, or create a policy-located ciphertext-only backup through the same typed authority operations as the CLI. Secret-bearing API intake is strict, capped at 1 MiB, wiped after one use, and metadata-only on output; backup accepts no caller path. Authentication remains bearer transport plus kernel `SO_PEERCRED`, principal authorization is derived outside request data, unavailable authority fails closed, and canary tests require submitted values to remain absent from API output and retained metadata/audit.
- Completed the credential custody, vault lifecycle, and exact Credentials views feature set (#131). The encrypted credential authority is now projected through typed application/API read models that surface authoritative active and revoked records, full version history (algorithm, KEK version, verification digest, creation time), binding counts, vault metadata (deployment id, store id, custody mode, KEK id and version, schema version, last-clean-shutdown, initialized-at), and review-only CLI previews for `aegis secret put` and `aegis secret backup`. The Credentials route is wired to the shared workspace shell with status (All/Active/Revoked) and exact-reference search filters, native inspector, and read-only detail panels. The new `VaultStatus` domain type, the `bbolt.Store.Status` and `bbolt.Store.BindingCount` metadata reads, the credential readiness classifier, and the dedicated `CredentialWorkspace` and `CredentialInspector` templ components keep the credential surface metadata-only and credential-independent from the rest of the MVI vertical: missing, locked, corrupt, or unavailable credentials never flip registry/loops/graphs/queue readiness. Browser state cannot authorize credential mutation; only authenticated API/CLI authority admission creates, rotates, revokes, binds, or backs up encrypted secrets.

### Changed

- Replaced the permanently appended collapsible principal-password rotation panel with an authenticated top-bar "Rotate password" action that opens the shared native modal dialog (#162). Rotation now presents its authentication-impact warning, labelled current/new/confirmation fields with the browser-enforced 12-character new-password minimum, explicit approval, and a dangerous confirmation with cancel, so focus containment, Escape dismissal, and focus restoration need no executable client JavaScript. `POST /console/password`, CSRF, exact approval, server-side current-password verification, authoritative audit, and older-session/one-time-handoff revocation are unchanged. The installed browser proof now enters every login and rotation value through focused input and denies unless the browser retained the exact value before submission.
- Bound Execution Queue lifecycle to manual authenticated controller-side admission over the existing single-node queue worker. Added `queue retry` and `queue cancel|expire|exhaust` CLI verbs plus protected `POST /v1/queue/:item/retry|cancel|expire|exhaust` HTTP routes; each call repeats fresh authority admission before resolving the immutable snapshot, Graph, and Loop revisions, requires the exact queue authority digest, applies a bounded retry backoff (≤ `MaxRetryBackoff` = 24h), enforces the pinned retry budget (`Attempts < MaxAttempts`), and atomically commits retry, transition, dependency-respecting disposition, and repository-assigned audit fact. The worker distinguishes `runtime_retry`, `lease_reclaimed`, `operator_cancelled`, `execution_expired`, `retry_exhausted`, and `authority_revoked` terminal outcomes as durable vocabulary; live (non-expired) retry, reclaim without `lease_reclaimed`, retry beyond the attempt budget, terminal replay, expired-lease reclaim while the lease is still active, and authority substitution all deny. The browser queue inspector adds explicit lifecycle view tabs (`queued`, `claimable`, `active`, `retrying`, `cancelled`, `expired`, `denied`, `failed`, `exhausted`, `succeeded`), a complete ordered timeline of every transition/retry/cancellation, dependent-loop outcomes, and read-only lifecycle control buttons whose eligibility reflects authoritative readback. Browser input continues to grant no authority; no automated retry, reclaim, expiry, or revocation scheduling and no distributed scheduler exist. The installed fleet vertical and its tests now prove the bounded retry/reclaim on the same `GraphRun`/`LoopExecution` with a new attempt number, the lease-expired reclaim denial, the cancel/expire/exhaust/revoke vocabulary including revocation as a distinct durable outcome, and the complete ordered historical readback.
- Bound Loop publication and lifecycle to one exact enabled publisher Agent revision whose charter and runtime match freshly admitted authority. Durable Loop records now seal immutable publisher Agent, authority-context, mandate, stanza, runtime, charter, and validation provenance; protected HTTP lifecycle mutation appends conflict-detecting active/retired history without rewriting revisions; exact reads and the read-only console expose validation, executable structure, retry/evidence contracts, publication provenance, lifecycle history, and readiness. Substitution, stale chain heads, unauthorized subjects, non-ready authority, and retired reactivation deny.
- Routed exact Hermes-registered queue participants through the existing bounded Hermes gateway-turn adapter under immutable participant/runtime/mandate/authority bindings, fresh admission, hard input/output/duration limits, and a disposable runtime home. The release-shaped installed fleet proof now invokes a protocol-faithful fake Hermes gateway, verifies invocation and cleanup, and remains credential-independent without claiming live provider/model acceptance.
- Replaced the generic Execution Queue record drawer with the authenticated OpenDesign-aligned execution workspace: authoritative active/failed/lifecycle bands, exact `#/queue/:execution-id` routes, runtime and queue identity, Graph/Loop/attempt causality, timeline, artifact, verifier receipts, and terminal disposition. The browser surface preserves denied, failed, unavailable, and incomplete states and never promotes process output or passing receipts to success.
- Made successful queue completion evidence-gated at the durable repository boundary: Loop revisions pin exact verifier, policy-version, media-type, and expected-digest claims before runtime output exists; completion independently reloads each content-addressed receipt and binds its exact attempt, artifact, action, run, owner, authority, claim, verifier, policy, expected digest, and observed artifact digest before permitting success.
- Completed the exact Graph composition, publication, typed-run submission, and inspection slice. The authenticated console now provides same-origin CSRF-protected native forms without hand-authored transport JSON. The composer resolves only server-listed immutable Agent and Loop revisions and reloads their exact port contracts; the run form reloads the reviewed exact active Graph and canonicalizes typed inputs. Both derive authority server-side from an authority-bound runtime session, repeat fresh admission, use conflict-detecting idempotency, and fail closed on reference, lifecycle, compatibility, topology, mapping, policy, predecessor, digest, typed-input, authority, or replay violations. Accepted runs create one immutable snapshot and Queue item; invalid submissions become one durable rejection without Queue admission. Exact idempotent retries return the original immutable identities while changed replays conflict. Authenticated inspection continues to expose typed contracts, exact participant/Loop/policy bindings, node-edge topology, lifecycle and submission history, immutable snapshots, authority/mandate/runtime context, normalized inputs, queue/run causality, and rejection reasons without implying general multi-node scheduling.
- Completed the public Agent Registry administration and operator-read slice: authenticated CLI and HTTP operations expose exact immutable revision history and append-only enabled/disabled/retired lifecycle revisions; stale revisions, unauthorized subjects, retired reactivation, and path-binding mismatches deny. The read-only console adds bounded native search and lifecycle filters, stable Agent cards, responsive typed provenance/ownership/runtime/charter/revision/capability/policy detail, and explicit lifecycle readiness without treating browser presentation, declared policy, or absent provisioning evidence as authority.
- Replaced the generic query-parameter console with the shared authenticated Aegis workspace shell and canonical Agent Registry, Graphs, Loops, Execution Queue, and metadata-only Credentials destinations. Typed application/API models now preserve source-qualified authoritative collection states and stable contextual action-readiness reason codes without letting browser state select identity, stanza, mandate, runtime authority, or success.
- Made interactive onboarding a lower-resistance guided journey: bootstrap identifies a concise `basic` presentation as the default, shows one recommendation and consequence at a time, and accepts `details` or `advanced` before consequential approvals to reveal exact Aegis evidence. Operators can return to `basic` without changing progress. Bare `aegis` and explicit `aegis init` share deterministic initialization and operational-authority transitions; explicit initialization then continues the five-stage artifact-derived manager-onboarding journey. The same presentation modes, approvals, revalidation, and fail-closed decline/resume behavior apply within each authorized scope, and terminal events carry color-independent `STATE`, `ACTIVE`, `QUEUED`, `ERROR`, or `ACTION` roles without replacing explicit authoritative/runtime origins.

### Fixed

- Fixed the ready Agent Registry primary action so it opens the dedicated authenticated `/console/agents/charter-import` review page instead of a fragment-only URL. The page provides a native link back to the Agent Registry and exact `aegis charter validate <charter-file.json>` and `aegis charter import <charter-file.json>` CLI guidance. Denied or unavailable actions remain disabled with their server-owned reason, and the browser remains review-only with no charter mutation or authority input.
- Tightened console bootstrap exchange to exactly 64 lowercase hexadecimal characters. Malformed values return `bootstrap_invalid_format`; syntactically valid unknown, consumed, replayed, bootstrap-expired, or subject-expired values share `bootstrap_consumed_or_expired`. Native form failures re-render typed HTML recovery guidance with `aegis console` and configured lifetimes without reflecting input, while JSON clients retain stable codes and cross-origin/host denial remains non-consuming metadata-only `403`.
- Fixed happy-path bootstrap after the development state root is removed and recreated while the exact user gateway remains loaded. Recovery now validates the loaded fragment and complete `ExecStart` before lifecycle mutation, restarts the exact gateway so it binds the recreated state, and revalidates both loaded identities before authenticated audit-current readiness. A foreign fragment, stale executable, or stale configuration denies without restart.
- Fixed automatic browser onboarding reporting success before the configured console had actually accepted the session cookie. The loopback handoff now stays alive for a correlation-bound round trip: only an authenticated configured-console response may invoke the exact same-host loopback confirmation callback, which returns the browser to a clean `/console` URL before `browser_session_established` becomes true. Missing, invalid, replayed, cross-host, non-loopback, or unauthenticated confirmation remains fail-closed and never carries identity or authority.
- Fixed fresh bare interactive onboarding so it stops after authenticated principal configuration and verified operational-authority reconciliation, then proceeds to gateway/console onboarding without creating credential-authority, model-binding, or manager-certification artifacts. Explicit `aegis init` remains the separately authorized resumable manager-onboarding path, and explicit certification remains fail-closed.
- Restored `aegis manager` and the healthy-gateway launcher's `terminal` action as authenticated Unix-gateway clients. Short-lived in-memory sessions bind one principal and `secrets-manager` stanza; typed slash commands execute through the gateway's sole application service, while missing, expired, revoked, or interrupted authentication fails closed without offline authority inspection or direct writable-state access.
- Fixed a second bare invocation against an already healthy exact gateway so startup classifies gateway and closed-authority state before bootstrap or writable-store inspection. Admission now requires exact installed bytes, loaded fragment, loaded `ExecStart` executable and complete `serve --config` argv, activity, and authenticated audit-current readiness; healthy ownership offers explicit console/terminal/exit routing with default exit, while stale or unhealthy loaded units, dirty orphaned authority, corrupt closed authority, and terminal authentication failure deny without creating a second writer.
- Fixed `aegis reset` failing after an affirmative confirmation when persistence close legitimately replaced `DIRTY` with `CLEAN`, or when Badger replaced or added a recognized database file inside an already previewed Aegis-owned persistence directory. Apply now regenerates the closed artifact inventory and deletes against fresh filesystem identities, while unknown filenames, scope changes, and unrelated identity drift still deny before mutation.
- Clarified browser-console onboarding as signing the already initialized Aegis principal into this browser: `aegis console` authenticates the host user and creates only a temporary handoff, while browser-controlled input cannot create or change principal identity, tenancy, stanza, mandate, or authority.
- Repaired browser-console authentication and navigation under the enforced CSP by replacing Datastar-dependent interactions with native same-origin forms and links for one-use bootstrap exchange, sign-out, domain navigation, record inspection, and inspector close. Form decoding is bounded and exact-field, while `default-src 'none'`, self-only script/connect/form policy, exact Origin/Host checks, session cookies, and CSRF denial remain fail-closed.
- Fixed `aegis reset` so the closed artifact inventory recognizes the legitimate Badger `state/persistence/fleet-v1` store, including its exact writer lock, lifecycle markers, temporary marker files, and known Badger files. Unknown persistence schemas, filenames, layouts, links, and non-Badger content still deny before deletion.
- Made bare development and production `aegis` enforce the same authenticated gateway install/readiness/browser-console path instead of allowing a checkout-built binary to bypass supervision and enter the manager directly. Renamed the canonical lifecycle surface to `aegis gateway preview|install|status|start|stop|restart|uninstall`; `aegis service` remains an exact compatibility alias, while explicit `aegis manager` remains the direct authenticated terminal-manager surface.
- Made bare production `aegis` treat ready operational authority—not optional manager-model certification—as the admission boundary for supervised gateway startup. Missing or invalid certification no longer diverts production startup back into manager bootstrap before gateway reconciliation, authenticated readiness, and the browser-console handoff.
- Fixed bare interactive browser authentication by replacing the unauthenticated direct `/console` launch with a plaintext-loopback-only, correlation-bound automatic handoff. The temporary endpoint lasts at most 15 seconds, admits one atomic claimant, keeps the single-use bootstrap out of the URL, exchanges it server-side, delivers the validated `HttpOnly` cookie to the configured host, and requires an authenticated same-host confirmation round trip before returning to a clean `/console` URL; unavailable or failed automatic handoff preserves the manual URL/bootstrap fallback.
- Made `aegis gateway start`, `stop`, and `restart` validate the exact current Aegis-owned user unit before invoking `systemctl`; the compatibility `aegis service` alias uses the same checks. An absent unit now returns the stable `gateway_not_installed` error with the exact `aegis gateway install` remediation and performs no installation or gateway-state mutation.
- Made `aegis gateway` and compatibility-alias `aegis service` lifecycle completion truthful and structured. Install/start/restart validate systemd's loaded fragment and require authenticated audit-current readiness; start/restart and stop wait for observed active/inactive state; only pending nonzero verifiable audit work is advanced through the protected bounded delivery endpoint; and every successful lifecycle action emits an exact unit-bound typed result instead of returning blank output.
- Reconciled each streamed manager response preview with the complete validated response before rendering the authoritative guarded-turn completion event, so long, multiline, Unicode, missing-final-snapshot, and divergent-preview responses cannot leave the terminal transcript clipped or incomplete.
- Made release construction and candidate verification fail closed on unavailable Git worktree metadata, malformed or mismatched expected revisions, tracked source changes, missing or dirty Go VCS build metadata, and candidate provenance mismatch. Release archives now embed the exact clean source commit, and `aegis version --provenance` exposes the version/revision pair for local installed-artifact verification.
- Repaired manager certification interoperability with supported Hermes tool-free requests and the strict manager response contract. The process-bound inference proxy now strips only the semantically empty `tools: []` plus `tool_choice: "none"` decoration before forwarding to Ollama, continues to reject every executable tool request, and narrows each certification turn to its exact expected response kind and operation-specific proposal argument schema. Certification also verifies the exact arguments of security-critical revocation proposals, and the conformance version invalidates older certifications.
- Repaired installed Hermes 0.18.x manager startup by selecting its built-in empty `context_engine` TUI toolset instead of passing the MCP-only `no_mcp` sentinel as a TUI toolset. This prevents unknown-toolset fallback from widening a certification request while preserving fail-closed rejection of executable tool definitions, choices, and calls.
- Made development bare interactive startup and explicit manager startup usable after certification is missing or fails: once principal and operational authority are ready, the manager enters an authenticated degraded shell, identifies the certification failure, prints the exact configured-candidate recertification command, retains deterministic local controls, and makes no cloud or alternate-model attempt. Explicit `aegis init` still resumes the guided certification stage, and `aegis manager certify` still fails non-zero without publishing partial certification.
- Fixed bare production `aegis reset` so artifact-derived discovery selects the sole former or XDG legacy layout instead of incorrectly planning against the canonical default. Explicit configuration remains non-authoritative for reset, while canonical-plus-legacy and multiple-legacy ambiguity still fail closed.
- Fixed `aegis reset` inventory for generation-managed Badger authority stores by recognizing exact numeric `.mem`, `.sst`, and `.vlog` artifacts only beneath valid lowercase 32-hex generation directories. Nonnumeric names, malformed generations, wrong-depth entries, directories, links, unsafe metadata, and unknown files remain fail-closed.
- Made `aegis reset` stop and purge the exact Aegis-owned `systemd --user` gateway before deleting configuration or state. Production reset now always performs two passphrase-file authority authentications around confirmation; foreign, drifted, loaded-without-file, unverifiable, or unsuccessfully purged gateway state preserves reset data and denies, while an absent unit is accepted only when systemd reports it not found and inactive.

### Security

- Raised the release and development toolchain floor to Go 1.26.6, which contains the standard-library fixes required by the repository vulnerability scan.

## [0.2.5] - 2026-08-12

### Changed

- Replaced the production transport-reconciliation and user-service-installation copy/paste phrase gates with separate conventional `[Y/n]` confirmations after their complete previews. Enter, `y`, and `yes` confirm; all other input, EOF, cancellation, and input failure decline without mutation, while exact principal, configuration, plan, token-path, unit-byte, and digest revalidation remains authoritative.

### Fixed

- Made user-service activation audit-current, compensating, and diagnosable. Apply now revalidates the complete approved plan immediately before mutation, records pre-existing active/enabled state, executes explicit publication, daemon-reload, enable/start, and authenticated audit-current readiness phases, preserves pre-existing exact unit/service state during rollback, removes only newly introduced state, and reports phase, readiness, systemctl stderr, and joined rollback failures. Authenticated `/readyz` now observes transport identity and audit delivery without appending an authentication event that makes its own projection stale.
- Corrected the stable production root to `~/.aegis`. The former `~/.argis` root is now an explicit migration source only; fresh startup never initializes beside a meaningful former/XDG installation, canonical-plus-legacy or multiple-legacy layouts fail closed, and Linux migration/reset operate on the exact artifact-derived source.

## [0.2.4] - 2026-08-11

### Added

- Added explicit local control-plane ownership reconciliation: production onboarding can generate an owner-only token file and absolute Unix transport after exact principal/configuration approval, then preview and install one digest-bound `systemd --user` Aegis service after a separate approval. Added `aegis service preview|install|status|start|stop|restart|uninstall` and `aegis console` for a short-lived single-use browser bootstrap over authenticated Unix transport. The lifecycle refuses foreign units, never enables linger or installs a root service, and makes no same-account isolation claim.

### Changed

- Made an online daemon the sole cooperating-process owner of authoritative stores: `serve` acquires an owner-only singleton lock before Unix-socket mutation, concurrent startup denies, and store-backed CLI paths have no direct-store fallback while the socket is online. Launch examples and installed-console verification now use generated protected token-file custody rather than an inline reusable token and exercise singleton denial plus daemon-owned console onboarding.

## [0.2.3] - 2026-08-11

### Changed

- Replaced the browser console's hand-authored HTML and JavaScript rendering path with typed Go view models and generated templ components, using a pinned self-hosted Datastar client for progressive enhancement. Added digest/provenance and static browser-security gates, generation/module-drift checks, focused typed rendering coverage, and an extracted-native-binary proof that verifies the embedded asset plus authenticated authoritative-empty fleet surface without a CDN, Node runtime, reusable bearer, or browser storage.

### Fixed

- Repaired valid-configuration startup when the generation-managed operational authority is exactly absent. Interactive bare startup and `aegis init` now freshly authenticate the configured host principal, show the exact one-generation compatibility action, and require explicit default-deny confirmation before creating one secure empty generation. Ordinary and non-interactive paths return `operational_authority_not_initialized` with exit status 2 and no mutation; invalid, unsafe, ambiguous, dirty, corrupt, markerless, wrong-owner, or populated legacy state is preserved and denied; concurrent confirmed reconciliation converges only on the same empty generation.

## [0.2.2] - 2026-08-10

### Added

- Added strict canonical queue retry, cancellation, dependency, and rebuildable projection records plus atomic Badger lifecycle transactions. Claims require available backoff, remaining attempt capacity, and exact succeeded dependencies; expired leases can be reclaimed into bounded retries and new numbered claims; queued or claimed work can be cancelled; completion requires the matching unexpired active claim. Every accepted claim, retry, cancellation, or terminal mutation updates canonical facts, active-claim binding, projection, and repository-authored audit together. Automated lifecycle scheduling, dirty-store recovery, and multi-node execution remain launch work.
- Added principal-authenticated public fleet-control resources backed by one shared application boundary and the durable Badger `fleet-v1` repository: `agents register|list|show`, `loops list|publish|show`, `graphs list|publish|show|submit`, `queue list|show|process`, `session authority`, and protected `/v1/agents`, `/v1/loops`, `/v1/graphs`, `/v1/queue`, `/v1/fleet/readiness`, and session-authority routes. Collection reads include exact validation and execution-causality records; strict transport inputs and distinct denied, unavailable, conflict, not-found, corrupt, and repair-required outcomes fail closed.
- Extended `scripts/verify-installed-mvi.sh` beyond packaging and first-run denial to invoke the extracted native release-shaped binary for the credential-independent Registry → Loop → Graph → Queue → evidence-gated disposition vertical. The proof checks accepted execution, durable wrong-authority rejection, fresh admission at consequential boundaries, content-addressed evidence, terminal queue readback, exact historical definition digests, and no production-state or credential use. It does not publish a release, execute every cross-compiled target, or prove real Hermes/general scheduler behavior.
- Added a narrow controller-side Execution Queue worker for an exact single-node Graph and single executable Loop action. It repeats fresh authority admission before claim, runtime effect, each evidence verification, and successful disposition; includes a deterministic credential-independent no-key adapter; persists output by content digest; requires claim-specific passing receipts; and atomically commits immutable evidence metadata, terminal disposition, queue transition, and audit. Admission denial and runtime/evidence failures remain distinct durable outcomes. Live read-only console inspection is wired; real Hermes worker integration, automated reclaim/retry/cancellation scheduling, multi-node scheduling, and browser execution controls remain launch work.
- Added a narrow fleet orchestration service that binds registration, Loop/Graph publication, and Graph submission to an authenticated subject and exact freshly admitted authority context; requires exact enabled participant and immutable definition bindings; durably rejects invalid submission admission; and reports mechanically distinct action-specific readiness for all consequential fleet actions. Public CLI/API adapters now call this service; lifecycle activation and general worker lifecycle remain launch work.
- Added bounded canonical `internal/queue` submission, rejection, queue-item, dependency, claim/lease, retry, cancellation, projection, and append-only transition records plus `internal/execution` Graph-run, Loop-execution, attempt, and terminal-state records. The Badger `fleet-v1` adapter atomically persists either an accepted snapshot/submission/queue-item/Graph-run/queued-transition bundle or a durable rejection, provides dependency-gated single-winner claim transactions across bounded attempts, and atomically persists retry/reclaim, cancellation, or terminal evidence/disposition state with its projection and audit. Strict codecs, exact causal-reference checks, conflict denial, rollback tests, concurrent-claim tests, close/reopen readback, public adapters, and installed proof cover this narrow substrate. These records still grant no authority and do not implement a general worker scheduler.
- Added the bounded `internal/registry` Agent Registry domain slice with a strict deterministic current-fleet fixture adapter, immutable source-identity registration, canonical content-digested Agent revisions, create-only sequential revision publication, exact idempotent replay, conflict denial, and fail-closed exact enabled-revision resolution. Principal-authenticated public registration and reads use the durable repository; source and records grant no authority.
- Added the bounded, standard-library-only Loop definition domain with immutable typed revisions; deterministic canonical encoding and SHA-256 digests; strict decoding; validation for ports, steps, mappings, transitions, exclusive gates, joins, bounded retries, terminals, cycles, and evidence claims; and publication-time records binding the exact validator and validation digest. Public CLI/API publication and exact reads use the shared authenticated service; lifecycle selection and general execution remain absent.
- Added the bounded, standard-library-only Graph definition domain with immutable content-digested revisions; exact Agent, Loop, policy, and predecessor bindings; typed acyclic dependencies and mappings; strict deterministic codecs; immutable validation results; create-only contiguous publication checks; separate activation/retirement lifecycle; and content-digested run snapshots with canonical typed inputs and exact resolved definition references. Public CLI/API publication, submission, and exact reads use the shared authenticated service; lifecycle application and general execution remain absent.
- Added a narrow Badger `fleet-v1` repository for atomic durable Registry registration/revisions, Loop and Graph revisions plus validation facts, Graph run snapshots, queue lifecycle and terminal evidence/disposition records, and repository-authored hash-linked audit facts. Exact publication and snapshot retries bind canonical request and audit semantics; changed same-key requests deny, failed transactions leave neither mutation nor audit, and clean close/reopen preserves verified facts. Shared public services consume this persistence boundary, but it is not itself authority or a complete general Queue/execution product.
- Added one standard-library-only shared reference layer for immutable identity/digest and identity/revision/digest bindings. Its deterministic versioned JSON codecs validate complete bindings and reject malformed identifiers or digests, unsupported schemas, zero revisions, unknown fields, duplicate object keys, invalid UTF-8, and trailing values; architecture tests enforce canonical ownership and inward-only dependency direction. References confer no authority; public fleet services and the narrow Queue worker consume them, while general Execution Queue lifecycle remains unimplemented.
- At its 0.2.2 introduction, the embedded accessible `/console` shell provided live read-only Agent Registry, Loop, Graph, and Execution Queue collections, inspectors for exact revisions/digests, validation facts, and execution causality, and explicit loading, authoritative-empty, unavailable, and error states. Browser access was derived only from an already authenticated principal through a random single-use bootstrap and a short-lived in-memory `HttpOnly` cookie session; exact-origin, CSRF, restrictive browser headers, bounded pagination, loopback-only plaintext, and reusable-bearer exclusion failed closed. That release's shell was a presentation adapter and granted no identity, authority, runtime mandate, or mutation capability; the Unreleased section records the later bounded Graph publication and typed-run mutation surface.

### Changed

- Re-baselined the normative MVI around the bounded Agent Registry, Loop, Graph, and Execution Queue domains while preserving exact stanza/mandate authority, immutable revision references, fresh effect-boundary admission, and the prohibition on a universal mutable aggregate. Defined action-specific readiness, stable fleet-control resource roots, and separate evidence/disposition ownership. Qualified the fleet-definition and fleet-lifecycle storage contract on Badger `v4.9.5` for Linux/amd64/ext4 with one shared `fleet-v1` store, exact path and permission rules, an exclusive writer lock, a 256 MiB reserve, and fail-closed migration, backup, dirty-recovery, and readiness policies. Immutable definitions, accepted/rejected submission, dependency-gated attempt-bounded claims, durable retry/reclaim and cancellation primitives, terminal evidence/disposition, public thin-vertical services, and installed proof are implemented; migration/backup/recovery tooling, automated lifecycle scheduling, multi-node scheduling, and real Hermes execution remain incomplete. Credential custody and the typed GitHub broker remain supported infrastructure but are not a global fleet prerequisite.

### Fixed

- Kept command-lifetime application services open and reusable across repeated fresh principal admissions while rebuilding them only when the validated configuration changes, so first-run credential-authority setup can advance into an already-installed Hermes/Ollama model stage without reopening and locking the same Badger authority repository.
- Made the release state-machine regression suite hermetic on machines without global Git identity by supplying a synthetic `.invalid` identity only to its mocked annotated-tag operation; production release commits and signed tags still require the operator's configured identity and signing authority.
- Hardened installed release acceptance so every generated archive must contain exactly one root `aegis` regular file with mode `0755`; traversal, absolute/nested paths, links, extra members, unsafe modes, and release-output paths traversing symlinked parents fail closed before extraction or target mutation.
- Recognize the decorated version identity emitted by supported Hermes Agent 0.18.2 Git installations while preserving fail-closed rejection of malformed, ambiguous, and unsupported version output.

## [0.2.1] - 2026-08-07

### Added

- Added a repository-owned integrated authority denial matrix covering crash/restart, corruption/substitution, race, bounded key-codec fuzz, and three generated secret canary campaigns. The matrix runs in CI and is exposed through `make authority-denial-matrix`; proof state stays in a mode-`0700` repository-local disposable directory and is removed on exit (#29).
- Added a separately gated release-candidate acceptance verifier (`scripts/verify-release-candidate.sh` plus its hermetic regression test) that requires an exact stable version, source revision, real Hermes executable, empty repository-local workspace, and strict operator-supplied decision record; builds the release archives once; binds the extracted native candidate, Hermes executable, decision, and source by SHA-256; rehearses exact local rollback and withdrawal without touching production or publication state; and emits a bounded evidence manifest. Named denial tests reject malformed, oversized, substituted, symlinked, and revision-mismatched inputs before a build (#30).
- Added an explanatory launch-asset pass for the 0.2.1 cycle that documents the new verifier, refreshes the quickstart, recording, demo, and contributor pointers, and pins the Makefile release default to the in-flight version so a bare `make release` prepares the right tag (#30).

### Changed

- Made Badger the exclusive operational authority repository: removed the generic filesystem authority repository and routed production authority/session families through generation-managed Badger. Narrow authority and command repository contracts are now injected into session, broker, command, API, and slash consumers; executable demos, tests, architecture, security, and launch documentation are updated to match the exclusive boundary (#23).
- Enforced fresh three-plane admission for broker authority resolution: the broker now uses one replay-verified canonical authority snapshot immediately before credential resolution, binds that snapshot exactly to the session, mandate, authority-context identity, digest, and evaluation instant, and independently fails closed on action capability, declared and runtime-verified Aegis tool authority, and credential scope. The dispatcher provides a prerequisite commit-then-quarantine handshake for Aegis tool invocation and the exact context digest becomes the Aegis tool ingress key (#25).

### Fixed

- Tolerated legitimate Assuan `S ` status records in the pinentry GETPIN response loop so `aegis init` succeeds against pinentry backends (e.g. `pinentry-curses` when its `isatty` probe fails, `pinentry-gnome3` falling back from DBus) that emit non-fatal status before the final `ERR` line. The `S ` prefix is informational in the Assuan protocol and is now ignored instead of being classified as a generic protocol failure (#32, closes #31).

## [0.2.0] - 2026-08-07

### Added

- Added one repository-owned installed-MVI verifier used by CI and release publication. It builds release-shaped Linux/macOS archives for amd64/arm64, generates and verifies `SHA256SUMS`, checks the extracted native binary's injected stable version, and proves bare non-TTY first run reports `manager_not_initialized` without creating production state. Focused denial tests reject invalid versions, non-empty output, and symlink output before mutation.
- Added a separately gated release-candidate acceptance verifier. It requires an exact stable version, source revision, real Hermes executable, empty repository-local workspace, and strict operator-supplied decision record; builds the release archives once; binds the extracted native candidate, Hermes executable, decision, and source by SHA-256; rehearses exact local rollback and withdrawal without touching production or publication state; and emits a bounded evidence manifest. Named denial tests reject malformed, oversized, substituted, symlinked, and revision-mismatched inputs before a build.
- Added immutable per-session authority contexts bound exactly to canonical subjects, mandates, charter revisions, runtime descriptors, and effective authority.
- Added append-only authority-revocation facts, bounded dispatch/turn lifecycle records, fresh exact-context runtime admission, content-addressed runtime artifacts, and claim-specific verification receipts that cannot grant authority or declare domain success.
- Added a focused Hermes runtime-adapter operation for exactly one bounded turn under an immutable launch contract and fresh authoritative admission decision. The operation remains internal; it adds no provisioning or activation surface.
- Added an opt-in Linux PTY operator-acceptance POC for one current `aegis manager` journey: ordinary conversation, protected creation of credential `test`, authoritative count, a pronoun-only conversational reference to the just-created credential, clean exit, audit verification, bounded JSONL evidence, and generated-canary non-leak checks. Hermetic CI tests only the recorder and forced-leak denial; live Hermes/Ollama/model execution remains explicitly manual.
- Added narrow state-store primitives for atomic create-only JSON records and exact-byte content-addressed blobs. Records reject replacement, traversal, and symlinked paths; blobs use canonical `sha256:` references, verify existing and reread content before acceptance, and fail closed on malformed references or detected corruption.
- Added a typed authority repository with create-only mandates and per-session contexts, strict schema/kind-discriminated canonical codecs and qualified SHA-256 identities, append-only per-context activation/revocation/expiry facts, fail-closed complete-chain replay, and deterministic non-authoritative transition roots.
- Added generation-managed Badger session-authority persistence under `state/persistence/authority-v1`, with staged no-replace publication, embedded identity verification, digest-bound `ACTIVE` selection, and explicit `CLEAN`/`DIRTY` open-close lifecycle markers. One versioned binary key registry now covers store metadata and every authority key family, and the generation store implements the typed authority repository with create-only mandate/context transactions, atomic session uniqueness, and atomic append-plus-root transition updates.
- Added context-cancellable, cross-process maintenance coordination for the Badger authority plus bounded deterministic export, integrity-checked fresh-generation import, exact verified activation and rollback, and fail-closed retention-based garbage collection. Maintenance requires one closed clean generation, never replaces a backup destination, never reuses imported generation/store identity, and never guesses a rollback target.
- Added a durable metadata-only audit-delivery outbox and local canonical-order projection with bounded principal-only CLI/API delivery, aggregate status, projection verification, readiness gating, interruption-safe reconciliation, and an explicit derived-state rebuild that never mutates the canonical audit chain or checkpoints.
- Added executable production-package import boundaries plus focused subprocess-crash, corruption, concurrent-authority, fuzz, race, and secret-isolation campaigns for the generation-managed Badger authority. Restart after interrupted commits remains fail-closed, malformed or substituted identities deny, and generated secret canaries are required to remain absent from persisted state and diagnostics.
- Added the normative post-Track-A storage contract and executable qualification matrix: Badger `v4.9.5` session authority and bbolt `v1.5.0` credential custody are qualified only on the exact Linux/amd64/ext4 single-process combinations. Architecture tests pin module versions, restrict engine imports and canonical schema ownership, classify every production package family, and reject unknown or drifted persistence combinations.
- Added strict canonical authority command, fact, non-authoritative receipt, rebuildable projection, and portable replay records. The closed activate/revoke/expire command vocabulary carries authenticated actor and exact chain preconditions; deterministic replay consumes verified commands and controller-authored facts, rejects malformed or substituted history, and never derives authority from receipts or projections.
- Added one atomic Badger authority-command transaction that persists the canonical command and fact together with a durable receipt, current projection, and derived outbox entry. Exact and concurrent retries return the replay-verified receipt without duplicate work, conflicting command-ID digests deny, failed transactions leave no partial state, and consistent projection/admission reads fail closed on substituted derived records.
- Added an immutable replay-verified committed-authority position binding authority context, sequence, fact digest, and projection digest. Canonical authority audit delivery now emits strict-sequence, create-only, metadata-only evidence for that exact position, and the grant-producing readiness read denies until evidence reaches the current committed boundary in the same Badger snapshot.

### Changed

- Hardened Badger authority lifecycle and maintenance mutation on qualified Linux with no-follow descriptor-relative marker publication/removal, generation moves, failed-staging cleanup, and device/inode-revalidated garbage collection. Initialization, export, and import now preserve a fixed 256 MiB disk reserve beyond bounded candidate output. Activation and rollback return typed outcomes that distinguish pre-selection failure from durable selection with incomplete retirement or later sync work; logical recovery imports only canonical records into a fresh inactive generation and transactionally rebuilds derived authority projections, while native recovery requires an exact retained generation.
- Closed the authenticated design-to-provisioning chain: design now uses `no_mcp`, bounds requirements/proposals and charter import to 1 MiB, and rejects multiple proposal envelopes; plan preview requires fresh configured-principal authentication and the exact local environment; approvals bind an immutable plan ID plus the current charter, complete plan digest, runtime, and environment at request, decision, and apply; and interrupted-provisioning recovery removes owned artifacts only when that consumed authority binding remains intact.
- Replaced the experimental cross-domain plumbing aggregate and universal validator with bounded canonical `core`, `execution`, and `evidence` responsibilities. Existing subject, decision, effective-authority, mandate, session, provisioning-artifact, and receipt types remain canonical instead of being duplicated.
- Removed the experimental `aegis plumbing` command, plumbing/GraphRun API routes, POC orchestration service, and production imports of `internal/plumbing` and `internal/poc`; these were not stable compatibility surfaces.
- Made the typed layout rederive every state-rooted default after a state-directory override, and added one fail-closed clean-install classifier for legacy mandates, authority contexts, authority revocations, and sessions. Initialization permits only absent or securely proven empty legacy trees and revalidates immediately before apply.
- Unified pre-configuration lifecycle dispatch and ordinary service construction on one complete profile-owned layout, so production and development consistently select initialization, exact legacy migration, reset, repair denial, or normal startup without losing checkpoint, authority, credential, certification, model, or runtime path custody. Fresh initialization now creates the mode-`0700` state root, publishes and verifies the empty authority generation before configuration, revalidates operator identity immediately before the atomic mode-`0600` configuration link, durably syncs its directory, and resumes only from a valid empty interrupted generation. Development binaries refuse production migration explicitly.
- Separated canonical facts, rebuildable projections, operational metadata, blobs, runtime state, and credential custody in the normative architecture. Derived state, lifecycle markers, model narration, and cross-store partial results cannot grant or reconstruct authority; release cross-build support is no longer presented as persistence qualification.

### Fixed

- Kept broker availability stanza-local: a configured GitHub broker no longer forces unrelated non-broker stanzas to request broker authority or receive capability material. Added an integrated two-subject proof for exact one-stanza selection, separate clean Hermes processes/homes, the principal stanza's exact live one-tool registry, one typed GitHub repository operation, and fail-closed team escalation/capability reuse.
- Preserved atomic no-replace Badger generation publication in cross-platform release builds by using `renameat2(RENAME_NOREPLACE)` on Linux and `renameatx_np(RENAME_EXCL)` on macOS instead of compiling the Linux-only primitive into every target.
- Removed the authority-bearing proxy token from the manager Hermes environment. Hermes now receives only a fixed non-secret parser-compatibility API-key string and starts behind an inherited one-shot pipe gate; Aegis acquires Linux pidfd custody, binds the exact process durably to the loopback proxy, and only then releases Hermes to execute. Denied or failed release never executes Hermes and cleans its disposable state. Custody also binds the host boot identity so it fails closed after reboot, while each accepted proxy connection must still belong to the custodied process. Manager Hermes launches now request the real empty `no_mcp` toolset rather than the capability-bearing `context_engine` toolset.
- Repaired the executable no-key demonstration for the enforced development/production profile split. It now builds a normally classified checkout binary in the repository root, uses an ignored disposable repository-local workspace, reports the `dev` profile, preserves an honest no-provider boundary, cleans up its artifacts, and has a CI regression test.
- Treat conversational answers such as `named NAME` at the dedicated credential-name prompt as the requested name instead of persisting a `named-` prefix, route filtered metadata requests such as `show me all doppler secrets` to authoritative `secret.search` rather than silently returning the unfiltered list, and render manager credential counts, lists, searches, details, history, mutations, and terminal-only values as readable labeled views instead of dense raw JSON.

## [0.1.36] - 2026-07-22

### Fixed

- Route `new cred named NAME` directly to deterministic protected intake, interpret that same full phrase correctly when entered at the local name prompt, and detect Doppler-prefixed tokens before an ordinary composer submission can reach Hermes.

## [0.1.35] - 2026-07-22

### Fixed

- Stabilized Unix peer-authentication coverage under CI load by using the shared bounded Unix HTTP client instead of an arbitrary one-second request deadline during real Hermes runtime discovery.

## [0.1.34] - 2026-07-22

### Fixed

- Recognize explicit reference-leading credential retrieval requests such as `I need to see the bdp-dev cred value` and execute them directly through Aegis authority instead of routing them to Hermes.
- Route terse authenticated requests such as `new cred` directly into local reference and protected-value intake, defaulting disclosure to protected instead of letting the model repeatedly negotiate metadata it does not authorize. Human credential names entered at the reference prompt are normalized to a visible hyphenated reference.
- Skip a redundant external Ollama unload mutation when the exact runner is already absent, avoiding a scheduler request that can exhaust cleanup after a crashed or cancelled turn while retaining exact running-model verification.

## [0.1.33] - 2026-07-22

### Fixed

- Added no-echo bracketed multiline paste to protected credential intake, consuming the complete framed value and confirmation before returning to the composer. Confirmation mismatch now flushes queued protected input, and credential-bearing composer submissions fail before Hermes even when trusted-local plaintext routing is otherwise active.

## [0.1.32] - 2026-07-22

### Fixed

- Prompt for an exact credential reference before protected value intake when a natural create request omits the name, instead of silently persisting the placeholder `new-credential`.
- Synchronized remaining multi-command PTY tests with composer restoration so commands cannot lose their submit key during prompt handoff under race-test load.

## [0.1.31] - 2026-07-22

### Fixed

- Recognized natural `with a secret of ...` create phrasing as inline credential value syntax, keeping the value on the deterministic encrypted-authority path instead of unexpectedly prompting for protected re-entry.
- Synchronized the random-canary PTY test with the restored composer before submitting `/status`, eliminating the CI timing race where the command could be typed during prompt restoration and never submitted.

## [0.1.30] - 2026-07-22

### Fixed

- Kept natural inline credential creation on the deterministic Aegis authority path when the operator omits the space in forms such as `secretnamed`, preventing the value-bearing request from falling through to Hermes.

## [0.1.29] - 2026-07-21

### Fixed

- Applied exact modes in the group-writable development-reset fixture independently of process umask, eliminating the GitHub runner umask `0022` failure in normal and race tests.

## [0.1.28] - 2026-07-21

### Fixed

- Made the group-writable development-reset fixture create nested directories in deterministic parent-first order, eliminating the clean-CI and race-test failure caused by randomized Go map iteration.
- Classified Ctrl-D/composer EOF as normal `terminal_eof`, including an EOF/cancellation race, instead of `runtime_failed`. External-local teardown now invalidates the proxy and performs exact-model unload verification before waiting for disposable Hermes cleanup, so earlier stages do not consume the shared bounded cleanup interval; genuine verification failure remains fatal.
- Removed redundant approval friction from deterministic first-time manager credential creation. Clear authenticated create imperatives now store an inline value directly or proceed to protected no-echo intake; create remains atomic insert-only, while replacement/rotation, revocation, and binding retain approval. The parser now recognizes natural `make a new cred` and unquoted `value is VALUE` phrasing without retaining the value in presentation history. Explicit credential-value syntax that misses deterministic create grammar now fails closed before Hermes dispatch.
- Classified a binary located in the verified source checkout as development even when a clean tagged checkout causes `go build` to embed a stable module version. Development reset may traverse ordinary group-writable workspace parents while retaining strict checks on the `.aegis` root and all reset artifacts; production receives no such exception.

## [0.1.27] - 2026-07-21

### Added

- Bound stable release binaries to an isolated `production` profile at `~/.argis` and ordinary source-built `dev` binaries to a visible, Git-ignored `development` profile at `<repository>/.aegis`. Development execution verifies that the binary resides in the real Aegis module/worktree root, overrides any clean tag version embedded by a local `go build`, and fails closed if copied to an unrecognized worktree. Configuration, credential authority/deployment identity, audit/checkpoint data, manager certification, runtime state, and cleanup targets no longer share defaults; development rejects production paths, and reset is restricted to its own exact layout. Development reset uses the exact preview and default-deny `yes` confirmation without a passphrase prompt. Production reset authenticates the existing minimum-12-byte authority passphrase before confirmation and independently again after `yes` before deleting credential records or a local encrypted KEK.

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
