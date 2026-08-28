# Five-Minute Quickstart

## Prerequisites

- Linux
- Go 1.26.6+
- Hermes Agent `>=0.18.0,<0.19.0` on `PATH`
- A compatible `pinentry` in the operator's desktop session for protected authority prompts, or a real terminal for the no-echo fallback

Install the latest tagged Aegis source with `go install github.com/berryhill/aegis/cmd/aegis@latest`, or continue below to build a checkout. Self-update requires a published, non-draft stable GitHub release with assets, not merely a local or remote Git tag; until publication completes it correctly reports the previous published stable version. `aegis --update` is the strict root-only alias for `aegis update`; both use the same checksum-verifying service.

Maintainers can exercise the exact release packaging path without publishing anything:

```sh
python3 scripts/verify-console-vendor.py
python3 scripts/console_security_test.py
go generate ./web/console
git diff --exit-code -- web/console go.mod go.sum
./scripts/verify_installed_mvi_test.sh
python3 -m unittest scripts/verify_release_archive_test.py
./scripts/verify-installed-mvi.sh
```

The first four commands verify the pinned Datastar asset and browser-security contract, regenerate templ output, and reject generated/module drift. The final command requires the checkout to resolve to one exact clean tracked Git commit, builds all four declared Linux/macOS amd64/arm64 archives in an ignored repository-local proof workspace, embeds that revision, rejects missing, dirty, malformed, or mismatched Go VCS metadata, requires each archive to contain exactly one root `aegis` regular file with mode `0755`, verifies their checksums, extracts the native archive, and verifies its injected stable version plus exact `version --provenance` revision before running it with an isolated `HOME`. It first requires the bare non-TTY result `manager_not_initialized` with exit status 2 and no canonical `.aegis` creation, then drives the extracted native binary through the credential-independent Registry → Loop → Graph → Queue → evidence/disposition vertical. Its installed-console proof reconstructs a complete durable Agent → Loop → Graph → Queue chain, including content-addressed artifact, verification receipt, and terminal disposition records. Real headless Chrome exchanges a fresh bootstrap, follows only rendered exact-related-record links across that chain, verifies canonical Agent-revision and immutable Loop/Graph/Queue URLs across direct load, reload, Back, and Forward, and exercises server-side Loop search, lifecycle filtering, and pagination while preserving query state. It then closes the exact inspector and signs out under the production CSP. The proof also requires zero CSP violations, JavaScript errors, request failures, or unexpected HTTP 500 responses, proves the console document loads no executable client JavaScript, and verifies that the retained self-hosted Datastar asset is directly servable but absent from both unauthenticated and authenticated documents. The fleet vertical checks accepted execution, durable wrong-authority rejection, fresh admission, terminal queue readback, and exact historical definition digests. Supplying a version, empty output directory, and exact revision, for example `./scripts/verify-installed-mvi.sh 1.2.3 dist "$(git rev-parse --verify HEAD^{commit})"`, retains only the archives and `SHA256SUMS`; the isolated proof state is still removed. Never point it at a directory containing retained files. The command intentionally denies a worktree with tracked changes and verifies only the exact committed revision.

An exact pre-publication candidate proof is deliberately explicit and must run from a clean committed revision. Create a decision file outside the evidence workspace with exactly these fields (no extras):

```json
{"schema_version":1,"candidate_version":"1.2.3","source_revision":"EXACT_40_HEX_HEAD","decision":"hold","decided_by":"authenticated operator identity","rationale":"reason for release, hold, or withdrawal"}
```

Then run:

```sh
revision=$(git rev-parse HEAD)
hermes=$(command -v hermes)
./scripts/verify_release_candidate_test.sh
./scripts/verify-release-candidate.sh 1.2.3 "$revision" "$hermes" .aegis-release-candidate-1.2.3 ./decision.json
```

The decision must be exactly `release`, `hold`, or `withdraw`; Aegis does not infer it. The script requires a real non-symlink executable, an exact clean `HEAD`, a strict decision of at most 16 KiB, and an empty non-symlink workspace inside the repository. It builds the four archives once with the expected revision, requires the extracted candidate's exact provenance output to match it, extracts and SHA-256 binds that one native candidate, binds the exact Hermes executable and canonical decision, repeats all candidate checks against that binary, rehearses local replacement plus exact rollback, clears a local publication-staging copy to rehearse withdrawal, and writes `release-candidate-evidence.json`. It never modifies canonical `~/.aegis`, former `~/.argis`, an installed executable, Git refs, remotes, or GitHub. Local Git provenance does not authenticate a publisher, and the decision record is not authenticated by this script; non-dry-run `make release` remains publication authorization.

The four-target build/checksum result is packaging evidence; the additional native fleet vertical is installed execution evidence. The post-Track-A persistence contract remains qualified only on Linux/amd64/ext4 with exact Badger `v4.9.5` session-authority, fleet-definition, and fleet-lifecycle settings plus bbolt `v1.5.0` credential-custody settings; see `specs/STORAGE.md`. The two fleet planes use one `fleet-v1` store at `state/persistence/fleet-v1`. Its adapter atomically persists authority-bound Loop publication provenance and append-only active/retired lifecycle history as well as accepted or rejected submission, dependency-gated attempt-bounded claims, expired-lease retry/reclaim with bounded backoff, queued-or-claimed cancellation, validated rebuildable projections, and terminal evidence/disposition. Shared application services expose principal-authenticated `agents`, `loops`, `graphs`, and `queue` CLI resources and protected `/v1` routes; `PUT /v1/loops/:loop/lifecycle` is the protected Loop lifecycle mutation, and the installed verifier drives the narrow initial execution path through a registered bounded Hermes adapter and protocol-faithful fake gateway. It proves the production gateway protocol path and disposable-home cleanup without provider credentials; it is not live-provider/model acceptance and does not implement automated queue lifecycle scheduling, dirty-store recovery, multi-node scheduling, Graph lifecycle application, or general production orchestration. Do not infer storage qualification for macOS, arm64, another filesystem, path, schema, writer model, or engine version from the cross-build or native proof.

## Build and configure

```sh
go build -o aegis ./cmd/aegis
cp examples/aegis.yaml .aegis.yaml
chmod 600 .aegis.yaml
uid=$(id -u)
user=$(id -un)
transport_dir=$(pwd -P)/.aegis/transport
install -d -m 700 "$transport_dir"
python3 - "$transport_dir/api.token" <<'PY'
import pathlib, secrets, sys
path = pathlib.Path(sys.argv[1])
path.write_text(secrets.token_hex(32) + "\n", encoding="ascii")
path.chmod(0o600)
PY
python3 - "$uid" "$user" "$transport_dir" <<'PY'
import pathlib, sys
path = pathlib.Path(".aegis.yaml")
text = path.read_text()
text = text.replace("REPLACE_WITH_LOCAL_UID", sys.argv[1])
text = text.replace("REPLACE_WITH_LOCAL_USERNAME", sys.argv[2])
text = text.replace("REPLACE_WITH_ABSOLUTE_TRANSPORT_DIR", sys.argv[3])
path.write_text(text)
PY
cp examples/office-charter.json .office-charter.json
sed -i "s/REPLACE_WITH_LOCAL_UID/$uid/g; s/REPLACE_WITH_LOCAL_USERNAME/$user/g" .office-charter.json
```

The copied files are local working files and should not be committed.

The copied valid configuration does not initialize operational authority. In a real terminal, run `./aegis --config .aegis.yaml init`, verify the displayed authenticated UID/username and exact `state/persistence/authority-v1` path, and type `y` or `yes` at the default-deny compatibility-reconciliation prompt. This creates one secure empty generation only when that path is exactly absent. You may exit the later credential/model onboarding stages if this quickstart is exercising only credential-independent commands. Non-interactive `init`, bare startup, and ordinary commands instead return `operational_authority_not_initialized` with exit status 2 and perform no mutation. Existing invalid or populated state is preserved for operator repair and is never replaced.

## Verify the no-key path

```sh
./aegis --config .aegis.yaml runtime
./aegis --config .aegis.yaml charter validate .office-charter.json
./aegis --config .aegis.yaml config
```

Success means Hermes is named and versioned explicitly, charter validation returns a canonical digest, and the token loaded from the owner-only generated `api.token_file` is shown as `[REDACTED]`. The reusable value is not present in YAML.

## Verify the console and daemon-ownership contract

The example configuration serves the embedded shell at `http://127.0.0.1:8443/console` and restricts plaintext use to loopback. For this disposable checkout proof, start the foreground daemon with `./aegis --config .aegis.yaml serve`. The daemon takes `<unix_socket>.lock` before stale-socket inspection/removal; a concurrent second `serve` must fail with `another Aegis control-plane daemon owns this transport` and must not disturb the first socket. While the socket is online, store-backed CLI commands deny with `control_plane_online` instead of opening authoritative stores directly.

Run `./aegis --config .aegis.yaml console` to print the configured `/console` URL and the explicit `principal_password` authentication requirement; it does not create an authenticated browser session. Open that URL and sign in with the enrolled principal password. Loading the shell alone does not authenticate the browser, and failed verification issues no session cookie. The resulting volatile browser session expires after five minutes or when principal authentication expires, whichever comes first. Use an HTTPS `api.console.origin` and configure both API TLS files before exposing the TCP listener beyond loopback.

Bare development and production binaries use the explicitly approved gateway once initialization and operational authority are complete. The explicit lifecycle commands are:

```sh
aegis gateway preview
aegis gateway install
aegis gateway status
aegis gateway start
aegis gateway stop
aegis gateway restart
aegis gateway uninstall
aegis console
```

`gateway preview` is read-only and prints the exact unit path and digest. `install` requires a real terminal, prints the complete deterministic installation preview, and asks `Install and activate this gateway? [Y/n]`; Enter, `y`, or `yes` confirms, while every other answer, EOF, cancellation, or input failure declines without mutation. It revalidates the complete approved plan immediately before mutation, refuses foreign or byte-drifted `aegis.service`, uses only `systemctl --user`, validates that systemd loaded the approved fragment, and completes only after authenticated readiness reports a current, verifiable audit projection. Activation failures identify the publish, daemon-reload, enable/start, exact-unit, or audit-current readiness phase and retain the last readiness and rollback diagnostics. Rollback removes only newly published unit bytes and reverses only active/enabled state introduced by that install, preserving any pre-existing exact unit and gateway state. Bare onboarding uses the same bounded `[Y/n]` contract after its complete transport reconciliation preview. After its first successful activation, bare `aegis` presents the clean password-gated `/console` URL, attempts to open it directly as one platform-command argument, and then enters `AEGIS / manager — authenticated gateway client` in the same process. Browser-launch failure prints the exact manual URL and still enters the manager. No browser session is issued by either path. On every later bare invocation, Aegis first verifies the exact installed bytes, loaded fragment, loaded `ExecStart` executable and full `serve --config` argv, activity, and authenticated audit-current `/readyz` without opening writable authority persistence. If the development state root was removed and recreated while the exact gateway remained loaded, recovery validates the loaded fragment and complete `ExecStart` before mutation, restarts the gateway, revalidates both identities, and only then accepts authenticated readiness. A foreign fragment, stale executable, or stale configuration denies without a lifecycle action. An exact healthy gateway presents `console`, `terminal`, and `exit` with default `exit`; `console` opens only the clean configured URL, and `terminal` launches the same authenticated Unix-gateway client as `aegis manager`. The client receives a short-lived principal-and-stanza-bound session, repeats transport and session authorization for typed slash commands, and never opens gateway-owned persistence or falls back to direct state. Authentication expiry, revocation, or transport interruption denies and requires a fresh invocation. Start/stop/restart validate the exact current Aegis-owned unit before invoking `systemctl`; if the unit is absent, they return `gateway_not_installed`, name `aegis gateway install`, and perform no installation or gateway-state mutation. They then validate the loaded fragment and wait for observed active/inactive state; start and restart additionally wait for authenticated audit-current readiness. Neither status nor lifecycle commands enable linger; logout survival is reported only when the host already enables it. Uninstall requires confirmation, removes only the exact owned unit, and preserves configuration, state, and external credentials.

The shell provides accessible live surfaces for Agent Registry, Graphs, Loops, Execution Queue, and metadata-only Credentials. Agent Registry exposes bounded registration review/execute and exact lifecycle forms, Loops exposes reviewed publication/lifecycle command intents, Graphs exposes bounded compose/publish and typed-run forms, and Credentials provides authoritative search/status filters, counts, stable pagination, exact deep links, and reviewed create/rotate/revoke/exact-bind/policy-backup operations. Credential confirmation requires the authenticated principal, same-origin CSRF, a one-use session receipt, and fresh target revalidation; cancellation consumes and wipes that receipt. Results never render secret values, and initialize, unlock, reveal, restore, KEK administration, and arbitrary-path backup remain terminal operations. On an eligible queue record, review each displayed consequence before using Process, Reclaim expired lease, Cancel, Expire, Mark retry exhausted, or Record authority revocation. The native form is bound to the short-lived authenticated session and CSRF value; the server reloads the exact item, creates identifiers server-side, and repeats fresh authority admission. Disabled controls explain why the current authoritative state is ineligible, live retry remains denied, and a stale submission fails closed rather than forcing a transition. Typed Go view models are rendered through generated templ components; native same-origin forms and links handle password login, sign-out, domain navigation, record inspection, and inspector close without executable client JavaScript, a CDN, Node runtime, inline executable content, or browser-side HTML construction. The retained pinned self-hosted Datastar asset remains embedded and provenance-checked for release compatibility, but the console document does not load it. Every collection state preserves its authoritative source and stable reason code and distinguishes ready, empty, denied, unavailable, degraded/repair-required, loading, and error outcomes; a failed read never asserts a zero count. Contextual action readiness is diagnostic and fail-closed: browser input cannot select identity, stanza, mandate, runtime authority, or success, and consequential actions remain disabled without exact server-owned authority context. The Credentials surface exposes binding metadata only and never resolves or renders secret values. Selecting a record opens an inspector with exact revisions and digests. Loop records include executable steps, transitions, ports, bounded retries, evidence contracts, immutable validation, exact publisher Agent/authority provenance, and append-only lifecycle history/readiness; Graph records include immutable validation facts; Queue records include the item, current rebuildable projection, parent Graph run, child Loop executions, and attempts so execution causality remains reviewable. Agent registration is available through the public CLI/API and the bounded authenticated Registry workflow. Loop publication/lifecycle and Graph publication/submission are available through both the public CLI/API and their bounded authenticated browser command forms; queue processing and reclaim/terminal operations are exposed through the authenticated queue inspector. Durable rejection remains authoritative server-side state rather than browser narration. The installed-MVI verifier exercises those CLI resources and the authenticated console from the extracted native archive. Durable retry/reclaim/cancellation primitives are exposed through the bounded queue controls, but automated queue lifecycle scheduling, Graph lifecycle application, live-provider acceptance, and multi-node scheduling remain absent. Run `python3 scripts/verify-console-vendor.py`, `python3 scripts/console_security_test.py`, `go generate ./web/console`, and `git diff --exit-code -- web/console go.mod go.sum` for the source/supply-chain contract; network and denial behavior is covered by `go test ./internal/api ./internal/console -count=1`.

The Agent Registry destination additionally provides bounded native search and enabled/disabled/retired lifecycle filtering, stable Agent-ID selection, typed cards, and detail sections for fleet/source provenance, owner/accountability, runtime and charter bindings, immutable revision history, declared capabilities/policies, lifecycle, and action-specific execution readiness. When registration readiness is `ready`, its native primary link opens `/console/agents/charter-import`, where the operator supplies an exact pre-imported canonical charter revision, deterministic current-fleet JSON, and exact fleet/source identity. Review is non-mutating and shows the server-derived Agent revision and digest; execute consumes only a single-use receipt, reparses the bounded artifacts, and registers through the shared service. The browser does not import a charter, provision an agent, select authority, or assert success. Agent inspectors expose only legal append-only lifecycle transitions against the exact current revision and digest, and retirement requires explicit confirmation. The Registry does not fabricate effective stanza authority or provisioning receipts. The installed fleet verifier exercises registration, exact revision history, lifecycle transitions, and historical readback.

After any workflow that emits canonical audit events, verify and advance the local derived delivery projection with:

```sh
./aegis --config .aegis.yaml audit verify
./aegis --config .aegis.yaml audit delivery-status
./aegis --config .aegis.yaml audit deliver --limit 100
./aegis --config .aegis.yaml audit verify-delivery
```

Delivery is principal-only, bounded to 1–1000 canonical-order events, and safe to resume after interruption. `audit rebuild-projection` is recovery for derived outbox/projection corruption or loss; it first verifies the canonical chain and never rewrites canonical events or signed checkpoints. A serving control plane reports `/readyz` unavailable until this projection is current and verifiable.

After the exact gateway is healthy, launch the terminal client directly with `aegis manager`, or run bare `aegis` and choose `terminal`. A fresh successful bare activation enters that client automatically after presenting the console. The client obtains a short-lived authenticated local session over the protected Unix socket, accepts the canonical slash-command vocabulary plus `/agents readiness|list|show|prepare|register`, and closes that session on exit. `/agents prepare` validates bounded regular-file charter/current-fleet inputs and returns a non-authorizing proposal; `/agents register` requires the exact prepared revision digest and verifies authoritative post-write readback. It does not open authority/application stores or fall back to offline mode; authentication expiry, revocation, or transport failure requires a fresh `aegis manager` invocation.

Alternatively, a genuinely new installation can run a bare executable in a terminal or invoke `aegis init`. Both commands use the same deterministic initialization and operational-authority approvals, but bare startup stops onboarding once those controls are verified: it does not create credential-authority, model-binding, or certification artifacts and instead continues through the approved gateway/readiness path, presents the password-gated browser console, and enters the authenticated gateway manager. Explicit `aegis init` continues the resumable manager-onboarding journey. That journey identifies `PRESENTATION / basic (default)` and shows one recommendation and plain-language consequence per decision. Enter `details` or `advanced` before a presentation-owned approval to reveal exact authoritative paths, endpoints, custody, digest, certification, and gateway evidence; enter `basic` to return to concise guidance. Both depths invoke the same deterministic transitions and approval rules, so switching depth neither authorizes nor resets anything. Its opening text explains the exact-local manager purpose and safe exit/resume contract. Manager-onboarding progress is derived from artifacts and shown as five stages with completed, current, and remaining counts, so rerunning `aegis init` resumes at the first incomplete stage without replaying verified checks. Certification discloses its potentially substantial CPU/GPU/RAM/time use before approval; declining it or gateway activation preserves safe artifact-derived resume. After principal and operational authority are ready, explicit `aegis init` still resumes unfinished credential/model/certification work. Bare development and production `aegis` complete the approved gateway/readiness/console-then-manager path described above, while explicit `aegis manager` enters an authenticated degraded manager shell when certification is absent or invalid. That shell disables conversational inference, states the certification failure, prints the exact configured-candidate `aegis manager certify CANDIDATE_ID` command, keeps deterministic local controls available, and attempts no cloud or alternate model. An installed tagged release `aegis` uses the production defaults `~/.aegis/aegis.yaml` and `~/.aegis/state`. A repository-built development `./aegis` reports `dev` and uses ignored repository-local defaults `.aegis/aegis.yaml` and `.aegis/state`. It must remain in the real Aegis module/worktree root, rejects production paths, and cannot migrate the production profile. Review each displayed plan and press Enter to accept its `[Y/n]` default. First-run initialization creates the state root mode `0700`, publishes and verifies an empty generation-managed Badger authority at `state/persistence/authority-v1`, then revalidates the operator identity and atomically links the mode-`0600` configuration. An interruption before the configuration link is resumable only from that valid empty generation. Initialization refuses any legacy `mandates`, `authority-contexts`, or `authority-revocations` tree that is populated or cannot be securely proven empty; execution-session records are a separate non-authoritative operational family. After plan authorization, the credential-authority prompt recommends passphrase-encrypted local custody; enter accepts it, while `advanced` reveals systemd service-credential custody and the explicitly weaker development host-file option. The recommended path asks for and confirms an authority passphrase in two fresh pinentry windows. It prefers an explicit absolute `--pinentry-executable`, otherwise conventional `pinentry`, and uses terminal-backed no-echo input only if pinentry is unavailable before interaction. It generates a random KEK, persists only its Argon2id plus XChaCha20-Poly1305 encrypted envelope, creates and verifies the credential authority database, and continues to runtime/model/certification checks. It never sends the passphrase or KEK to Hermes, Ollama, or a model. Pinentry cancellation does not fall back; headless services should use systemd credential custody.

Verify the initialized non-interactive manager boundary without starting Hermes or Ollama:

```sh
printf 'not chat' | ./aegis --config .aegis.yaml
# exits with manager_requires_tty and names deterministic subcommands
```

Without configuration, the same non-TTY invocation emits structured `manager_not_initialized` output naming `aegis init` and exits 2 without prompting. With a valid configuration but no operational-authority generation, it instead emits `operational_authority_not_initialized`, names the same interactive next command, exits 2, and leaves the authority path absent.

## Reset and replay onboarding

For development/testing, a principal can return a default local installation to first-run state:

```sh
aegis reset
# review every delete/preserve path and the irreversible credential/audit warning
# for a release binary, authenticate the passphrase-file authority before confirmation
# answer yes at the default-deny prompt
# for a release binary, authenticate the authority independently a second time
# reset stops and removes the exact Aegis-owned user gateway before deleting state
aegis
```

If exactly one former `~/.argis` or XDG installation is detected, bare startup reports `legacy-layout-detected` rather than creating `~/.aegis`. In a real terminal, run `aegis migrate-layout`, review its digest-bound copy/preservation plan, and press Enter at `Apply this digest-bound migration plan? [Y/n]`; or run `aegis reset` to remove only the recognized legacy installation. Canonical plus legacy artifacts are ambiguous and are never merged or selected automatically. Linux migration copies, fsyncs, verifies, publishes, then cleans the source; reset can retain an empty legacy child beneath an unsafe external XDG parent without chmodding that parent. See [PATH_LAYOUT.md](PATH_LAYOUT.md).

Reset is intentionally fixed to the selected profile rather than accepting a custom `--config`. Repository paths remain denied except for the exact ignored `.aegis` subtree authorized by a development binary; reset also rejects the home directory itself, filesystem roots, paths outside the authenticated operator home, unsafe parents, symlinks, hard-linked files, unknown state content, and path/inode changes outside the narrow persistence rule below. A recognized Badger file may change while confirmation is pending only inside an Aegis-owned Badger directory already included in the preview. A recognized lifecycle marker may transition only directly inside an already previewed `persistence/authority-v1` or `persistence/fleet-v1` directory. Apply regenerates the closed recognized inventory and deletes using those fresh identities; unknown names, changed directory scope, and unrelated changes still deny before deletion. Reset never accepts a force flag; non-TTY, Enter, explicit `no`, EOF, cancellation, or input other than `y`/`yes` performs no writes. After confirmation (and the second production authentication), it validates and disables/stops/removes only the exact current Aegis-owned `systemd --user` unit, reloads the user manager, verifies removal, and only then applies the reset plan. If the unit file is absent, reset proceeds only when systemd reports the exact unit not found and inactive. Foreign or drifted unit bytes, loaded/active state without the expected file, inspection failure, or purge failure leaves configuration and reset state intact.

The reset scope is the exact Aegis-owned user gateway unit, resolved configuration, recognized Aegis state-store objects (charters, plans, approvals, receipts, provisioning artifacts, audit/checkpoints), recognized Badger authority generation trees and lifecycle markers, the exact Badger `state/persistence/fleet-v1` store and its known writer-lock/lifecycle/database artifacts, manager certifications and disposable homes, and recognized interrupted initialization/configuration/authority temporaries. Unknown persistence schemas, filenames, layouts, links, and non-Badger content deny before deletion. Legacy authority JSON paths remain recognizable only for collision detection and safe cleanup; they are not merged into the Badger store. A credential database and passphrase-encrypted or development host-file KEK are deleted only when configured below the Aegis state root and independently recognizable as Aegis artifacts. External authority/KEK paths and systemd credential custody are reported and preserved. Aegis also preserves its executable and source checkout, Hermes and normal Hermes profiles, Ollama and an operator-managed daemon, external credentials/systems, external Ollama model stores, and downloaded model data. This includes the Aegis managed model store if present.

Reset irreversibly destroys encrypted credentials and audit history in scope without separate backups. It is intended for development/testing unless the operator deliberately accepts that loss. On success it prints `state: uninitialized`, `gateway_purged`, and `next_command: aegis`; `config.Inspect` is absent, so the next bare interactive invocation re-enters onboarding. Gateway purge and reset deletion are intentionally ordered but not atomic: a crash after successful purge and before deletion can leave the gateway absent while state remains, which is fail-closed and recoverable. Bare non-TTY invocation instead remains fail-closed with `manager_not_initialized`.

Run `./aegis --config .aegis.yaml manager` in a real terminal to enter the inference-capable manager once every derived readiness check passes, or its authenticated degraded shell when authority is ready but certification is absent or invalid. Ordinary canonical message responses stream as sanitized `Hermes model / untrusted` previews; before the authoritative guarded-turn completion is displayed, the terminal reconciles them with the complete validated response so accepted long or multiline content is not clipped by a missing final preview. Proposals remain buffered until complete validation. Rich terminals update one progress line in place, and accessible/plain terminals print at most one progress line per turn. The registry has no default model. When discovery finds exactly one approved installed candidate, bootstrap visibly selects it without presenting a meaningless one-item menu; multiple installed candidates still require an explicit selection with no default. `exit` or `use an already-present candidate` performs no network mutation. Choosing `pull` requires a closed-registry candidate and a complete network/disk/route preview; `[Y/n]` confirms the selected action while artifact and configuration digests—not copied prose—bind what Aegis applies. Progress remains visible, interruption is resumable, and Aegis rediscovery binds the resulting exact digest before separate certification. Until an exact local artifact has passed the conformance suite, the wizard reports the failed prerequisite and exact next action; no cloud fallback is attempted. In manager mode the exact Core 15 base names are `/help /status /context /authority /limits /scan /watch /findings /investigate /timeline /report /audit /cancel /clear /exit`; `/quit` aliases `/exit`, and compatibility `/complete` delegates to the same registry. `/watch` and host-expanding scan profiles report unavailable because this checkout installs no source manager or endpoint adapter. See [BASE_SLASH_COMMANDS.md](BASE_SLASH_COMMANDS.md). Enter submits, `Ctrl+J` inserts a universal newline, Up/Down recall history, `Ctrl+R` searches recent history, `?` on empty input shows help, and bracketed paste remains one bounded guarded submission. Use `AEGIS_ACCESSIBLE=1 ./aegis --config .aegis.yaml` for the no-animation, line-oriented accessible/plain renderer; `TERM=dumb` also selects it. Ctrl-C, SIGTERM, EOF, or session expiry enter the same bounded cleanup path and restore terminal raw/echo/canonical state.

Do not edit manager model fields merely to suppress that denial. For an already-installed official Ollama candidate, use the deterministic no-download path:

```sh
./aegis --config .aegis.yaml manager model candidates
./aegis --config .aegis.yaml manager model route --mode external-local --endpoint http://127.0.0.1:11434
./aegis --config .aegis.yaml manager model discover --endpoint http://127.0.0.1:11434
./aegis --config .aegis.yaml manager model configure CANDIDATE_ID --endpoint http://127.0.0.1:11434
./aegis --config .aegis.yaml manager certify CANDIDATE_ID
./aegis --config .aegis.yaml manager model status
```

Discovery uses local Ollama metadata only. The generated configuration uses a 15-minute principal authority lifetime and five-minute manager turn/Ollama request deadlines so the complete CPU-bound certification corpus can finish. Configure previews the exact digest-bound route and requires literal `yes`; decline and interruption perform no write. On Linux, certification starts a disposable child behind an inherited pipe gate with Hermes 0.18.x's built-in empty `context_engine` TUI toolset and only a fixed non-secret parser-compatibility API-key string. The `no_mcp` value remains an MCP-selection sentinel and is not used as `HERMES_TUI_TOOLSETS`. Aegis acquires pidfd custody plus the current host boot identity, binds that exact process durably to the proxy, and only then releases Hermes to execute. Denial, custody failure, malformed or missing release input, release-pipe failure, process exit, or reboot fails closed; launch failures kill the blocked process and clean its disposable state. The proxy then admits only TCP connections owned by the custodied process. Certification loads and tests the artifact through that process-bound loopback proxy, including an ordinary manager-specific conversational response that cannot pass as a generic acknowledgement. Hermes's empty `tools: []` plus `tool_choice: "none"` compatibility decoration is stripped before the tool-free Ollama request; non-empty tools and executable tool choices remain denied. The strict provider response schema carries exact operation-specific proposal arguments, and security-critical corpus cases verify those exact arguments. A schema-valid reply whose sole failure is omitted required conversational content enters a bounded three-execution loop using direct, case-specific wording rather than repeating an ambiguous request until authority expires; equivalent truthful descriptions of encrypted Aegis custody and the out-of-model boundary pass without one magic phrase. Other failures abort on the first result by default. Add `--continue-on-error` to run the remaining corpus for diagnostics; cancellation and authority expiry still stop immediately, every case must still pass, and no failed or partial certification is saved. Every execution is bounded by `manager.hermes.turn_timeout`, while principal authority expiry bounds the complete run. A timeout, cancellation, expiry, protocol/transport failure, invalid envelope, other failed case, or exhausted conversational check reports the exact case and stable metadata-safe reason and performs cleanup; instruction or corpus changes invalidate an earlier certification. Model installation/downloading remains an operator action outside this quickstart.

In the rich manager composer, terminal bracketed paste keeps multiline clipboard text in one guarded submission. Press Enter once after the paste summary; Aegis scans the complete normalized multiline input before sending any allowed text to Hermes.

To create a credential, type `new cred named demo`; Aegis immediately enters protected no-echo value intake without a model turn. Terse `new cred` instead asks locally for the name; enter `demo`, `named demo`, or the full `new cred named demo` phrase. Aegis strips prompt and command words, visibly normalizes human names, defaults disclosure to protected, and does not ask the model to choose a kind or protection level. Complete inline forms and paired shorthand also execute directly. That low-ambiguity imperative authorizes the exact parsed insert without another confirmation dialogue or model round trip, so it cannot poison or be vetoed by the Hermes conversation. On close, Aegis invalidates the route, removes Hermes state, verifies the external Ollama runner and avoids a redundant unload request when it is already absent, or terminates the dedicated managed daemon; it clears retained presentation/history and reports a stable failed teardown stage rather than claiming completion if cleanup fails.

If a natural create request omits the credential name, Aegis prompts locally for a name and shows the normalized exact reference before starting protected value intake. It never silently stores a generic placeholder reference.

At `Secret value:`, bracketed paste accepts a complete multiline credential document as one no-echo value. Paste the block, press Enter, then paste and submit the same block at confirmation. A mismatch flushes any queued protected lines before the composer returns. Do not paste credentials at the ordinary composer: Aegis blocks detected credential-bearing submissions locally before Hermes and asks you to restart protected intake.

Authenticated read-only questions execute immediately without model negotiation. `how many secrets do we have?` returns a labeled total/active/revoked summary from the authority; `list my credentials` returns a readable numbered metadata list, while `show me all doppler secrets` performs a metadata search using `doppler` and clearly identifies an empty result. Detail, history, creation, rotation, revocation, binding, and explicit terminal-only value results use the same human-readable manager presentation rather than raw JSON. None of these requests asks for confirmation or reaches the model.

After this local manager is fully ready, an operator can run the explicitly gated [human-to-manager acceptance POC](OPERATOR_ACCEPTANCE_POC.md) against a disposable installation. It creates a generated canary credential named `test`, asks for the authoritative count, then says `Show me the one I just created.` without repeating the name. It records only sanitized non-secret JSONL evidence, checks operation effects/cleanup/audit, and is intentionally excluded from ordinary hermetic CI.

An explicit request such as `what is the value for credential: "demo"` or `I need to see the demo cred value` decrypts and renders that exact reference directly in the authenticated terminal session without reaching the model. The value is terminal-escaped, audit remains metadata-only, retained TUI state is purged on close, and terminal scrollback is outside Aegis cleanup. Missing, ambiguous, or revoked references fail closed.

## Provider boundary

```sh
./aegis --config .aegis.yaml design --smoke
```

Without `credentials.design_provider` and its source credential, this must not be presented as a successful model turn. The command may reach Hermes and report its authentic provider-configuration failure. It uses disposable state and does not modify the normal Hermes profile.

Clean up this repository-local example with `rm -f aegis .aegis.yaml .office-charter.json && rm -rf .aegis`. `aegis reset` deliberately rejects repository paths and is not a replacement for that explicit example cleanup.
## Manual Execution Queue lifecycle

The queue lifecycle surface is controller-authenticated and JSON-file driven. Use `aegis queue show ITEM` to read the immutable item, projection, GraphRun, LoopExecution, attempts, claims, transitions, retries, cancellations, evidence, and disposition. The mutation commands are:

```sh
aegis queue retry retry.json
aegis queue cancel terminal.json
aegis queue expire terminal.json
aegis queue exhaust terminal.json
aegis queue revoke terminal.json
```

`retry.json` supplies the exact queue `authority`, `queue_item_id`, fresh `retry_id` and `transition_id`, nanosecond `backoff`, whether this is an expired-lease `reclaimed` operation, and the matching stable `reason_code` (`lease_reclaimed` for reclaim). Reclaim is denied before lease expiry; lease duration is bounded to 1 second through 1 hour, backoff to 24 hours, and the pinned attempt budget cannot be raised. `terminal.json` supplies the exact authority, queue item, fresh cancellation/transition IDs, and the command's stable reason (`operator_cancelled`, `execution_expired`, `retry_exhausted`, or `authority_revoked`). Every operation repeats fresh authority admission and denies substitutions or terminal replay.

The `/console/queue#/queue` page provides lifecycle filters, provenance, dependency state, a complete ordered timeline, and eligibility/denial explanations. Its closed process/reclaim/terminal forms submit authenticated controller requests, but browser fields do not supply identity, authority, reasons, or operation IDs. Session, origin, CSRF, exact-item reload, fresh authority admission, and legal-transition checks remain server-side, and stale or ineligible submissions deny. This MVI does not run an automated retry/expiry scheduler or distributed queue coordinator.
