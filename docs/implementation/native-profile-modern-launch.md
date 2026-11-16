# Modern Hermes gateway launch — source integration, not qualification

The adapter now discovers the supported installation command with
`<installation>/.hermes/bin/hermes --print-runtime-command --module tui_gateway.entry`.
It executes the **complete returned argv**, including isolated Python, bootstrap,
and dependency activation/lease selection. It never extracts only the first Python
argument, evaluates returned shell text, or switches a modern tree to its stale
legacy venv on discovery failure. Trees without this modern source interface keep
the legacy launch path; the exact 0.21.3 transport and ProviderFormatProbe records
are unchanged. The observed `.dirty` Git version decoration is accepted as metadata,
not provider qualification.

## Launch and disposable policy

The command query uses a separate private empty home, a ten-second deadline, a
16 KiB output bound and closed errors. Ambient credentials, Python/PM overrides
and proxies are excluded. Only the reviewed four-element POSIX command shape is
accepted: an absolute interpreter matching the installation's published launcher,
`-I`, `-c`, and the exact installation-bound gateway bootstrap. Paths with shell or
Python quoting ambiguity deny. Launcher, interpreter, bootstrap, constants,
launcher module, PM environment selector and dependency input bytes/file identity
are checked again before spawn. The bounded stdout sink has no promoted
`io.ReaderFrom` bypass: direct writes, `io.Copy` and real child stdout share the
same retained-byte budget, including valid command JSON with oversized whitespace.
Initial command validation precedes provider credential resolution/materialization;
the query's empty home and attempt parent contain no provider auth at that point.
Transport qualification, fresh admission, token deadlines and final pre-spawn
freshness remain independent gates.

The freshness inventory includes the declared activation inputs `uv.lock`,
`pyproject.toml`, `setup.py`, `pm/lock.json`, plus `hermes_cli/venv_sync.py`,
`hermes_cli/runtime_state.py`, `pm/__init__.py`, `pm/environments.py`, `pm/paths.py`
and `pm/filesystem.py`. These cover the reviewed lazy-install preparation,
active-home selection, install-stamp path, PM import facade and generation-lock/
lease implementation. Missing inputs, byte drift, inode replacement and changed
path resolution deny. This is freshness detection on trusted host code,
not an atomic filesystem snapshot, dependency-generation attestation or sandbox.
The runtime command may select a dependency generation at boot; full modern
source/dependency qualification is still required before controller Codex use.

Modern AttemptTurn writes a new mode-0600 config in its disposable home, with the
resolved authority-selected provider/model unchanged. It sets max_turns=1,
api_max_retries=1, auto_recovery_cycles=0, disables memory/user-profile memory,
compression, title generation/model upgrades, catalog picker warming, updates,
plugins, MCP and external-login adoption. Existing safe mode, empty skills,
ignored ambient rules and tool validation remain. It copies no native profile.
The policy applies to modern bounded attempts, not persistent interactive sessions.

No production model capacity is invented. The ordinary native transport can make
an official same-account `/models` request for context metadata. The catalog picker
switch is not a network firewall. An unavailable tool can be rejected before
execution and then produce a bounded correction/summary response; a complete
summary is not artifact verification or evidence of an unauthorized effect.
`persisted_turn.complete` describes transcript-row coverage, not task success.
The separate exact one-Responses-request ProviderFormatProbe gate stays closed
for this modern runtime; it is not imposed on every ordinary turn.

## Actual integrated test boundary

Retained private evidence: `.aegis-e2e/native-profile-modern-launch/`.
`TestModernNativeAttemptIntegration` invokes the real adapter's AttemptTurn,
including source discovery, token materialization, private config, complete runtime
command, tools.show, session.create, prompt.submit, completion bounds and process/
home cleanup. It uses the existing **test-only** transport qualifier seam, synthetic
account/token, real gateway/AIAgent/SDK and a controlled HTTPX vendor boundary in a
networkless mount namespace. It is not a passing production qualification record.

The runtime snapshot is based on the isolated repaired Hermes source at
`acdc8f1ba65c0231ae8277e846a4d55928913991`, with explicit read-only headers overlay.
Repaired `agent/codex_runtime.py` SHA-256:
`4656c98f67d03dc1f83040b882bd2e067769831b8836b6efd8d72b6074a759fa`.
Headers overlay SHA-256:
`96d6bb690871ec3875299536cf3bb3f9580ad358d29ce92cde55637deab5efa1`.
The supported PM-built developer environment uses Python 3.14.9, OpenAI 2.54.0,
HTTPX 0.28.1, HTTPCore 1.0.9, Pydantic 2.14.1 and nemo-relay 0.11.0. The snapshot's
developer launcher is produced by Hermes's real `mint_launcher` source API, not a
handwritten fake launcher or fabricated installation stamp. Test entry instrumentation
installs only the vendor observer/transport then executes the original entry body;
AIAgent and its stream assembler remain real. Instrumentation and source/dependency
identities are retained explicitly. Synthetic catalog capacity is fixture data,
never a production override.

The integration exercises complete SSE, delta/item EOF, malformed/failed/cancelled/
unknown terminals, tool-only/tool-plus-text correction, 401/403/429/500, output bounds,
cancellation after the actual Responses request, authority expiry and token-margin
expiry. It verifies exact account/model, effective one-iteration/zero-tool state,
no forbidden file attempt, no socket network attempt and removed process/home.
Tool cases are no-unauthorized-effect tests, not strict rejection tests. Turn text
is not Loop/Graph/Queue task success.

## Remaining gates and correction to earlier dependency interpretation

A read-only public **installed command metadata query** succeeds and returns the
complete four-element argv with store Python 3.14.9. That is not proof that a fresh
disposable home activates its dependencies. In the inspected Hermes bytes,
`pm.environments.owning_home_root` identifies an installation owner, while
`activate_dependencies` reads the active dependency home's state.
`venv_sync.prepare_launch` skips preparation when HERMES_DISABLE_LAZY_INSTALLS=1;
its borrowed-launch path otherwise synchronizes a separate environment. Owner
identification alone therefore does not establish read-only generation borrowing.
Independent read-only probes with the actual owner generation visible still exit
before the stdlib probe body: `no dependency environment is committed for this install`.
The active disposable home has no committed selection. Borrowed preparation would
sync a per-home environment, and generation leasing creates/prunes/unlinks `.leases`
files. Neither operation is authorized by this source-only repair. A supported
installation-bound dependency-selection/lifetime-custody contract remains an owner
prerequisite, separate from disposable session state and these source freshness checks.
No native HOME, profile, fabricated PM facts, lazy-install enablement or source
refresh was substituted to bypass this unresolved installed-launch boundary.

The successful developer-environment path proves working adapter integration,
not installed PM generation selection. Production qualification remains blocked
on supported clean-home installed dependency activation/lease provenance, complete
candidate-bound independent qualification and live-provider entitlement where
required. No version/hash-only promotion, broadened qualification range, source
refresh, fallback account/provider, installed edits, commits, publication or
activation is included. Loop/Graph/Queue real-provider acceptance remains separate.

## Launch-asset impact review

Updated README, CHANGELOG, SECURITY, CONTRIBUTING, architecture, threat model and
quickstart pointers, plus the historical gateway report's current-status pointer.
Reviewed LICENSE and CODE_OF_CONDUCT (unchanged), existing architecture diagram
(control-plane boundaries unchanged), no-key demo/script, historical terminal
recording and its source, release/checksum scripts/workflow and local contributor
backlog. No new release/archive/checksum, remote issue or recording is claimed.
The no-key demo's real installed launch cannot be promoted from this developer
fixture; fresh exact-candidate no-key/recording and installed/release acceptance
remain unexercised gates. Existing conduct contact/release limitations remain.
Source skills retain unchanged authority/qualification guidance; no skill-bundle
bytes or shared/profile skills are changed by this slice.
