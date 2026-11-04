# Issue 233 — Graph detail launch impact

## Source delta and claim boundary

Graph-only geometry/fallback checkpoint plus read-only authentication observation.
The observer uses the **existing** authenticated `GET /console/graphs` render,
with same-origin credentials, `cache: no-store`, redirect refusal and inert HTML
parsing of the existing `#session-form` / `#logout` markers. It does not choose a
principal, grant authority, renew a deadline, or insert fetched content. The only
new HTTP symbol is **static asset** `GET /console/assets/graph-observation.js`,
served through the existing asset headers. No new session/read API is introduced.
It is deliberately outside presentation-only `NavigationJS`; the structural
operation inventory and generated bundle manifest include the asset.

A visible page checks every 15 seconds (five-second read timeout), immediately
on focus/visibility return. Unknown transport/read state clears content with an
unavailable explanation, not a fabricated revocation statement. Pagehide removes
protected body content; a cached restoration reloads the authenticated server
render. An in-flight observation predating focus/visibility change cannot unhide
stale content. This is not instantaneous revocation push or recall of previously
disclosed data. Without JavaScript, a new authenticated navigation is necessary.

Exact revision URL semantics remain `record_key=ID:revision#/graphs/ID:revision`.
No bare-ID alias, client-chosen latest, new metadata/title/description, editor,
authority selector or runtime scheduler is added. Graph-input mappings remain
labelled Graph mappings; dependency port mappings remain contextual incoming and
outgoing edges, not invented Graph inputs.

## Affected assets updated

- `README.md`: bounded exact Graph workspace and source/acceptance distinction.
- `CHANGELOG.md`: unreleased behavior only, no release/tag mutation.
- `SECURITY.md`, `docs/THREAT_MODEL.md`: read observation, truthful unknown state,
  expiry/revocation observation delay, BFCache clearing and no-script limits.
- `docs/ARCHITECTURE.md`: exact server projection and separate read observer;
  dependency geometry is not execution or artifact-handoff proof.

## Reviewed, unaffected by this scoped change

- `LICENSE`, `CODE_OF_CONDUCT.md`: no license/community-policy change.
- `CONTRIBUTING.md`: existing isolation, immutable pins, resource/authentication
  gates and independent acceptance still apply; no new public command syntax.
- `docs/QUICKSTART.md`, `docs/DEMO_NO_KEY.md`: no new CLI/configuration or no-key
  provider/runtime path. The no-key demonstration does not exercise this UI.
- `docs/RECORDING.md`: retained terminal recording remains explicitly historical;
  it does not prove this Graph browser candidate. No recording was synthesized.
- Architecture diagram: no identity/authority/transport topology expansion;
  Graph UI observation is documented adjacent to existing diagram boundaries.
- Release binaries/checksums and contributor issues: no publication authorized,
  no binary/checksum/issue created or modified. No claim of current release or
  remote issue-cohort acceptance follows from this source review.

## Verification and remaining launch gates

Pinned templ generation and focused Graph web Go tests passed. Node Graph
navigation, numerical workspace geometry, and new auth-observation regressions
passed. Bundle manifest refresh/validation passed. Python fixture syntax checks
passed. These are source-tier results, not browser acceptance.

`internal/api/graph_console_frozen_browser_test.go` and
`scripts/graph_console_frozen_browser_test.py` prepare a private child-seeded
state, close both stores before starting actual frozen CLI Serve, and use native
password login in real headless Chrome. Fixtures include branch/fan-in/out,
12-rank and 128-chain/disconnected definitions, exact history/current records,
missing references, and canonical refusal of malformed/duplicate/over-bound
publication. Browser assertions cover four widths, all edge endpoints, full Fit,
keyboard pan/selection, exact Agent/Loop destinations, raw durable pins,
no-mutation interaction, no-script fallback, open-page cookie loss and strict CSP.
The fixture has **not been compiled or executed** in this continuation because
root capacity is below the mandatory 8 GiB pre-heavy reserve. It is not frozen
or independently accepted. Touch/actual mouse-drag, real expiry/server-side
revocation, missing-record/back reconstruction, and negative form-tamper/CSRF
journeys still require completion/verification; do not infer their PASS from
unit coverage or cookie removal. Pixel/token comparison remains parent and
independent tester work against the supplied accepted artifact.

No local demo, recording, release build, installed gateway, provider or runtime
execution campaign was launched. Capacity, actual candidate browser execution,
independent acceptance and parent-owned delivery remain explicit gates.
