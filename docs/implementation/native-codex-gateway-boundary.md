# Native Codex gateway: reproduced qualification blockers

**Current qualification:** [installation-bound module/code integration](native-profile-installed-integration.md) supersedes the earlier installed-dependency blocker and adds exact reviewed records. The investigations below remain historical evidence, not current blanket denial or live-provider acceptance.

**Historical diagnostic below.** The later isolated Hermes terminal-stream repair and actual modern Aegis AttemptTurn integration are described in [the current launch report](native-profile-modern-launch.md). Native no-effect correction is distinct from strict diagnostic tool rejection; same-account metadata is distinct from fallback. The old observations and failed diagnostics are retained, not promoted into current production qualification. The earlier owner-selection statement does not by itself prove read-only dependency activation under a disposable home.

This is a **blocked qualification prerequisite**, not implemented provider reuse.
The existing Hermes 0.21.3 transport/source records remain unchanged. No newer
runtime is admitted and no production Python hook, source-gate bypass, credential
connection, charter successor or installed/profile change was introduced.

## Reproduced boundary

The opt-in `native_codex_gateway_probe.py` starts the real installed
`tui_gateway.entry`, real `AIAgent` and installed OpenAI SDK, with only the HTTPX
transport replaced by a controlled HTTP/SSE vendor fixture. It does not replace
`AIAgent`, the credential resolver, stream assembler or gateway event producer.
A Linux bubblewrap mount/network namespace excludes native homes and PM state,
uses read-only source/dependency mounts, and creates fresh private HOME,
HERMES_HOME and TMPDIR. No host listener or external network is used. A single
synthetic `manual:aegis-controller` access-only pool row has no refresh token or
alternate account. The fixture checks model, account, bearer, originator,
`store=false`, streaming, absent tools and absent request-body token values.

Two startup boundaries are deliberately distinguished:

- The public `.hermes/bin/hermes --run-module tui_gateway.entry` in this namespace
  fails before ready: `ModuleNotFoundError: No module named 'dotenv'` from
  `hermes_cli/env_loader.py:17`. The namespace deliberately omits PM per-install
  dependency state. Installed `pm/environments.py:41–72` supports selecting the
  owning installation's dependency state for a different home, so this does **not**
  prove the installed public launcher is broken. Earlier isolated `--version`
  evidence reported no committed dependency environment; that is a different
  invocation, not a reason to borrow native HOME or forge PM stamps. Supported
  PM-selected clean-home launch remains unqualified by this diagnostic.
- Aegis's existing `gatewayPython` choice, `venv/bin/python -m tui_gateway.entry`,
  reaches ready. The full-turn diagnostic uses that same interpreter with
  `-I -B` and the test-only HTTP fixture. Its legacy dependency identity is not
  the managed public launcher's dependency identity or a newly supported install.

Observed installed source: `acdc8f1ba65c0231ae8277e846a4d55928913991`, with retained
pre-existing dirty files. Observed turn interpreter: Python 3.11.16;
OpenAI 2.24.0, HTTPX 0.28.1, HTTPCore 1.0.9, Pydantic 2.13.4. Every first inference
request snapshots imported source/dependency paths and SHA-256 values into
`CASE.identity.json`. The executable SHA-256 is
`1e761eb19d6f2594ab8dc64bd99ad4e1753589f3bf1ec199e4eef3aaa21e3930`.
These observations are not a general version-range qualification.

## Results and independently owned prerequisites

Retained evidence is under ignored
`.aegis-e2e/native-profile-codex-resume/final-suite-01/`; earlier directories are
preserved. `result.json`, sanitized protocol/log files and per-case source
identities accompany the test-side fixture. The suite exits **1**, not green.

| Native case | Observed outcome |
|---|---|
| Complete SSE | Real agent runs once and returns exact `SYNTHETIC_OK`, status `complete`. |
| Truncated after text delta, no terminal SSE | **FAIL:** returns the identical text and status `complete`. |
| Malformed JSON SSE | Returns status `error`. |
| Unsolicited function call / function call plus text | **FAIL:** gateway eventually reports status `complete`; no forbidden file effect occurs. Tool-only case attempts a third inference request, denied by the fixture's two-request limit. |
| HTTP 401 / 403 | Returns status `error`; modifies the disposable synthetic auth pool. No native source credentials exist in the namespace. |
| HTTP 429 / 500 | Returns status `error` after one inference request; disposable auth file remains unchanged. |
| Fixed destination | **FAIL:** every turn also attempts Codex model-catalog HTTP outside the fixed Responses endpoint, even with the bounded configuration. The fixture denies it. |
| Canary / cleanup | Synthetic token absent from captured protocol/logs; every diagnostic child reaped and private home removed. |

The decisive upstream source seam is
`agent/codex_runtime.py::_CodexResponseAssembler.result` (lines 1111–1136 in the
observed bytes, SHA-256
`50721ff3c0634a8825dd5396b3ed8aee4fd92b04cfc09a105a94420d61c3748f`). It synthesizes
message content from text deltas and rejects missing terminal SSE **only when
there is also no usable output**. `_consume_codex_event_stream` explicitly
allows content without a terminal frame. Native normalization and the gateway
then erase the distinction before Aegis receives `message.complete`. Aegis cannot
recover that missing evidence by checking the final text or updating hashes.

The unsolicited-tool observation is a native gateway result: those captured cases
contain no `tool.*` protocol events and prove neither forbidden tool effects nor
adapter rejection. Independent audit **refuted** this document's earlier claim
of an existing event denial: `AttemptTurn` verified only a pre-prompt empty-tool
snapshot, then silently filtered out `tool.start` and accepted a later complete.

The subsequent Aegis source repair detects `tool.*` events during empty-tool
`AttemptTurn` and design turns before ordinary event filtering, including before
`message.start`. Same-session and missing-session tool events deny; explicitly
foreign sessions retain unrelated-event filtering. Attempt denial returns the
closed `unexpected_tool_event` reason, no model output and no successful turn.
Independent acceptance then exposed C1: the old shutdown treated leader exit as
group quiescence and removed the home while a TERM-resistant ordinary child lived.
The Linux repair keeps the exact leader unreaped through bounded TERM/KILL and the
existing native fresh group census, then performs the sole Cmd.Wait. Readers are
cancelled/closed/joined; the home is removed only after proven cleanup (unless
design explicitly retains it). Unknown cleanup returns no output and retains the
home. Non-Linux disposable turns deny before launch. This does not cover deliberate
group/session escape. Unchanged independent normal/race regression evidence is in
`.aegis-e2e/native-profile-descendant-repair/`; prior failures remain immutable.
Authorized tool-bearing sessions and non-tool progress/
usage notifications are unchanged. This is **post-event detection**, not a
pre-effect sandbox, tool-execution prevention or native transport qualification.
It cannot reconstruct missing terminal SSE evidence. Synthetic RED/GREEN and
cleanup regressions are retained under
`.aegis-e2e/native-profile-tool-denial-repair/`; the independent original audit
and native failure evidence remain unchanged.

The extra catalog route is captured through
`agent/model_metadata_http.py:fetch` in a deadline
worker. Disabling update checks, catalog/title generation, memory, compression,
external-login adoption, plugins and MCP in the private fixture config does not
prove strict fixed-destination operation. Do not turn the fixture's network
interception into a production bypass or claim that local pool mutation changed
native credentials.

A separately owned Hermes change must provide a supported complete-stream
contract that rejects truncated content before returning success. Supported
clean-home dependency selection and strict auxiliary-route/tool/retry controls
also need proof. Only then consider an additive exact source/dependency record,
retaining the existing 0.21.3 record. Do not repair installed Hermes in this slice.

## Reproduction and coverage limits

Use the Makefile toolchain/resource settings and a task-owned TMPDIR. From the
worktree, with a new nonexistent private evidence directory:

```sh
python3 internal/runtime/hermes/testdata/native_codex_gateway_probe.py \
  /absolute/reviewed/hermes-source /absolute/private/task/new-evidence \
  --bounded-config --cases complete truncated malformed tool tool-text 401 403 429 500 \
  --require-terminal
```

The Go opt-in regression is `TestNativeCodexGatewayRequiresTerminalStream`:

```sh
AEGIS_TEST_NATIVE_CODEX_SOURCE=/absolute/reviewed/hermes-source \
AEGIS_TEST_NATIVE_CODEX_EVIDENCE=/absolute/existing/private/evidence-root \
go test ./internal/runtime/hermes \
  -run '^TestNativeCodexGatewayRequiresTerminalStream$' -count=1 -timeout=120s -v
```

It requires both a successful complete stream and rejection of truncated output;
it remains red on the observed runtime. Ordinary runs skip it unless explicitly
selected and configured. The new ordinary regression
`TestProviderTransportUnqualifiedBeforeCustodyAndLaunch` proves an unsupported
version or missing exact 0.21.3 source bytes cannot reach credential resolution or
runtime launch, and cleans the attempt home. It does not claim native inference.

Native expiry, cancellation, output/deadline enforcement, fresh-authority checks
at every real adapter effect, no-fallback/no-refresh assurance across all failure
classes, complete Aegis AttemptTurn/Queue/Loop/Graph execution, managed dependency
installation, live inference and independent candidate acceptance remain
**NOT EXERCISED** by this diagnostic. Existing synthetic authority/lifecycle tests
are separate evidence. Registration remains explicit opt-in `with-models`; parent
connection composition and exact charter successor approval remain separate work.

## Launch assets

Reviewed the existing README, LICENSE, SECURITY, CONTRIBUTING, CODE_OF_CONDUCT,
CHANGELOG, threat model, architecture/diagram, quickstart, no-key demonstration,
recording and release/contributor-issue material. The original diagnostic changes no public
interface, production runtime support, dependency or release artifact. The later
empty-tool event-detection repair updates the corresponding SECURITY, CHANGELOG,
architecture and threat-model boundaries without changing provider eligibility;
existing partial-feature statements remain applicable. The scoped
provider-reuse report links this failure evidence and removes the obsolete
Antigravity implementation commitment. No new release/checksum, recording, issue
publication or live-provider demonstration is claimed. The previously recorded
missing private conduct-reporting route and unverified release/live acceptance
remain launch gates; this diagnostic does not close them.
