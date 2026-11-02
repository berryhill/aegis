# Agent Registry detail

The authenticated Console retains the Registry roster and selects one exact
Agent revision inline below it. `#/agents/:id` is navigation only; the server
resolves `record_key` and an optional exact `revision` after authentication.
Native query-bearing links remain usable without scripts. Back restores bounded
collection filters, viewport, selected card and focus when browser storage is
available. None of that presentation state enters authority evaluation.

## Evidence hierarchy

The detail presents header/actions, readiness, exact charter/runtime constraint,
provisioning receipts, charter and Registry revision history, effective authority,
all trust-stanza declarations, and exact-bound session/execution evidence.
Historical views retain their lifecycle label but never offer current lifecycle
mutations. Current actions still repeat exact-current revision/digest admission.

Effective authority comes only from the shared application's evaluation of the
authenticated operator, an empty requested-stanza constraint, and the fixed local
environment. Missing or ambiguous matches deny. No permissions are combined,
and declarations, prompt text, URL fields and browser storage cannot confer them.
The current canonical charter schema accepts `local-os` authentication only.
The Console's password subject is not a local-OS subject: ordinary browser login
must not borrow that stanza. Consequently a positive local-OS service evaluation
is not evidence of a granted password-browser authority context. Overlapping
accepted charter selectors are rejected before storage, not persisted to make a
visually convenient ambiguous-match fixture.

Readiness observations are separate from effective authority and runtime
admission. The service checks the exact charter/runtime binding and current
runtime version. Provisioning integrity requires the exact verified receipt,
canonical plan, consumed matching approval, the one supported deterministic
provisioning effect and current bounded artifact bytes beneath the Aegis-owned
provisioned root. A receipt alone is insufficient. Missing, unverified,
incompatible and unavailable evidence stays distinct. Successful observations
are point-in-time facts, not a mandate or permission to execute; a denied operator
remains blocked even when runtime/provisioning observations succeed. Reads create
no approval, mandate, runtime session, Queue item or provisioning artifact.

Recorded sessions join only the exact charter digest/revision/runtime/target.
Their records do not encode Registry revision ownership or establish fresh
admission. There is no supported session-detail browser route; this is explicitly
unavailable rather than replaced with a fabricated link. Exact Queue links require
the selected immutable Agent participant reference and matching accepted snapshot
and Queue item. An absent link never implies activity.

## Focused qualification

Run ordinary regression checks with repository resource limits:

    GOMAXPROCS=2 GOFLAGS=-p=1 GOMEMLIMIT=512MiB go test ./web/console ./internal/api -count=1
    node scripts/agent_navigation_test.cjs

The opt-in `TestAgentConsoleFrozenBrowser` fixture seeds and closes isolated
synthetic stores before starting the real candidate's `serve`. It uses password
authentication in real Chrome, native controls, scoped evidence writes and no
provider execution. Build the development executable at the real module root
using `./scripts/build-source.sh ./aegis`; it must not be moved into a directory
without its module root. Set `AEGIS_AGENT_CANDIDATE` to that executable and
`AEGIS_AGENT_ROOT` to a new repository-local ignored evidence directory, then run
the exact browser test through `scripts/acceptance_port.py` with the operator's
existing shared lifetime-lock pool. Follow `.hermes/acceptance-port-isolation.md`;
never use 8448/8098 or retained gateway state.

The fixture distinguishes actual browser denial from positive local-OS service
observation. Neither the unavailable browser-positive/ambiguous fixture nor the
unavailable session-detail route is counted as PASS. Desktop/narrow DOM and
screenshots must be independently compared with the accepted artifact. A passing
fixture, source merge or CI result is not a release or workstation deployment.

## Launch-asset impact

Affected: this focused behavior/qualification document, README discovery,
CHANGELOG and the security-policy pointer to this boundary. The existing
architecture diagram and threat-model separation remain unchanged: this is a
read-only application projection, not a new authority or runtime edge. LICENSE,
CODE_OF_CONDUCT, CONTRIBUTING, quickstart, no-key demonstration, terminal recording
and contributor backlog are unaffected by these Agent-only observations and
navigation corrections. They do not claim to demonstrate this browser detail.
Release packaging, binaries/checksums and release workflows are unchanged; no
release, workstation installation, standing gateway restart or new contributor
issue is part of this change. Historical recordings remain historical evidence,
not fresh Agent-detail acceptance.
