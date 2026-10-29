# Exact-run execution debugger

Open an authenticated Queue detail. Direct Loop runs display the exact pinned
Loop control flow, not a wrapper Graph. Graph runs display immutable topology;
select a node and **Open exact Loop execution** or choose its scope to inspect
that node's pinned Loop. Definition, initial execution HTML and polled Loop
scopes share the canonical Go canvas geometry: cells, 212×102 nodes, directed
paths, feedback return gutter, arrowheads and condition/traversal-bound labels.
Observations and historical replay decorate that pinned layout without moving
it; Graph overview keeps its separate dependency layout. The textual equivalent
tracks the selected Graph or Loop scope.
Revision/digest, Graph run, snapshot, child execution,
Queue attempt, local retry and implementation pass remain distinct identities.

The controller commits content-free step starts/finishes, taken transitions,
verification outcomes and interpreter terminal observations with digest-linked
checkpoints. Reads reconstruct these facts from durable custody. Polling is
same-origin, authenticated, no-store and exact-run cursor-bound. It never mutates
execution or grants authority. The debugger observation model excludes task text,
model output, file bytes and raw cursor payloads. Its Console response additionally
contains an escaped server-rendered current-outcome fragment: selected-file task
and stored content enter only through the existing exact owner/evidence result
reader, never from interpreter observations. Missing legacy observations mean unknown; no synthetic
start/finish timestamps are inferred.

**Pause following**, the replay slider and **Current** change presentation only.
Zoom and node selection survive observations. Disconnection marks observations
stale rather than inventing progress; reconnect re-reads the exact run and checks
existing checkpoint identities. Authentication loss clears protected topology.
Keyboard node selection and reduced-motion/narrow-screen presentation are
supported. A browser reload reconstructs history from the service.

Interpreter terminal is not Queue success. Only the authoritative disposition
supplies terminal Queue state and artifact/receipt references. Graph dependencies
remain definition context, not observed artifact handoffs. The bounded worker
still does not schedule general multi-node Graphs (tracked separately in #317).

## Current execution outcome

Above the topology, the current-outcome summary separates actual Queue state,
authoritative disposition and its closed public reason, verification status and
selected-result availability. Queued and claimed/running observations are not a
terminal result; a projection alone cannot establish success. A recorded success
is displayed as disposition success. Direct native coding acceptance additionally
requires independently reloaded native custody, exact artifact/receipt bindings,
and intact content-addressed evidence; missing, corrupt or foreign evidence denies
the read rather than establishing acceptance. This historical check does not
rerun host tests or claim today's workspace still matches the recorded result.
Only the existing selected-file evidence reader can label selected content verified.
Native and Graph result types remain unsupported for selected-file content; this
is an availability limitation, not an execution cause or a verification failure.

The summary follows current authenticated observations even while history replay
is paused, preserving the selected node, zoom, tabs and history. Transport loss
labels the last observation stale. Authentication loss removes outcome, result,
initial observation data and topology and stops protected polling.

For legacy `implementation_failed` records without a finer authoritative cause,
the summary explicitly says that detail was not recorded or is unavailable. It
shows only reviewed recorded native failure codes when present. It does not
infer trusted-check failure from digest presence, reconstruct raw stderr, private checker/model text, or infer a cause from
Requeue eligibility or topology. Historical execution is never rerun for diagnosis.

## Controlled regression evidence

With Python Playwright and its Chromium installed in an isolated environment:

```sh
AEGIS_DEBUGGER_BROWSER_TEST=1 AEGIS_GRAPH_BROWSER_PYTHON=/path/to/python \
  go test ./internal/api -run TestExecutionDebugger -count=1 -v
```

The fixture authenticates against a real isolated `Serve`, pauses a production
Loop interpreter after its durable step start, and releases it only after the
browser observes that start. It tests failed then passing verification across
two implementation passes in one Queue attempt, exact revision pinning after a
newer publication, history, pause/current/replay, reconnect/reload, keyboard,
zoom/selection preservation, 390px layout, CSP and auth loss. Admission/runtime
execution is controlled and provider-free; this is not real-provider or installed
workstation acceptance. Separate same-page terminal fixtures atomically record
failure or success after the running interpreter is observed. The success fixture
uses real file-verifier evidence with seeded host bytes, not a provider-produced
result. The reusable result fixture separately checks real stored exact bytes,
owner denial and corrupt evidence; neither fixture qualifies live inference.
