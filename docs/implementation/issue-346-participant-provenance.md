# Partial #346: Queue participant provenance

## Bounded source claim

Queue list and detail report every exact pinned node participant rather than
substituting the submitting Agent. Submission owner and runtime product remain
separate facts. Labels retain node ID, Agent ID, revision and digest; deterministic
node ordering and repeated identical execution-reference deduplication do not
collapse distinct nodes. Conflicting or malformed references are unavailable for
that node. Neither owner IDs nor runtime profile/target names identify executors.
There is no latest-Registry fallback.

The read-only `QueueExecutionView.node_participants` projection accompanies
`node_runtimes`, using the existing exact snapshot topology and digest-checked
historical Agent read. `node_runtimes` contains only runtime product/adapter/target,
so it cannot alone supply an Agent identity. This additive DTO map is necessary
for preclaim and direct-Loop records without Loop executions; no immutable schema
or persistence record changes. Executed Loop records retain exact participant
references. Rendering consumes these facts without Registry reads or authority.

Queue identity, navigation/action targets, evidence/outcome/disposition logic,
authentication, CSRF, budgets, TTLs, model/provider/config and runtime admission
are unchanged. #349/#382 outcome surfaces are outside this slice.

**#346 remains open.** Runtime-session capture, actual runtime-session association,
real-provider execution, installed-target acceptance and broader issue closure
are not implemented or claimed. The parent owns fresh independent frozen-candidate
acceptance and any subsequent publication. There is no commit, PR or release here.

## Regression and persistence scope

`internal/api/console_queue_participants_test.go` reproduces owner B falsely
projected as participant instead of executed C through production
`consoleQueueRecord` and the actual list/detail templ renderers. The original
failing output is retained outside source under `.aegis-e2e/346-proof/red.log`.

Focused regressions cover preclaim/executed multi-node labels, invalid/missing/
conflicting facts, escaping canaries and stable row/action/Agent-link targets.
Separate child process lifetimes close/reopen the actual Badger stores. Canonical
synthetic preclaim Graphs retain exact C/D participants, owner B separately, and
historical C r1 even after Registry C r2 changes its runtime target. No fixture
invokes a provider, opens a TCP listener, grants runtime authority or fabricates
runtime success. Fixture canonical history is synthetic test data, not execution
qualification. Child-only tests skip without their parent fixture invocation;
the parent test executes both child phases.

Normal and race commands (run from repository root):

```sh
GOMAXPROCS=2 GOFLAGS=-p=1 go test ./internal/api -run '^TestConsoleQueueParticipant' -count=1 -v
GOMAXPROCS=2 GOFLAGS=-p=1 go test -race ./internal/api -run '^TestConsoleQueueParticipant' -count=1 -v
```

Production browser acceptance is separate and not claimed. The existing
`console_queue_actions_browser_test.go` illustrates child-seed/closed-store/
candidate/reopen composition. Do not launch that older harness unmodified: its
random-port probe is not the workstation's candidate allocation policy. A fresh
tester must use the inspected `scripts/acceptance_port.py --pool` with
`/home/silas/.hermes/profiles/xander/workspace/aegis-acceptance-ports/locks`, retain
the claim for the entire candidate lifetime, and use private isolated config,
state and workspace. Never use reserved 8448/8098 or copy operator state/credentials.

## Launch-asset impact review

| Asset | Disposition |
| --- | --- |
| Root README, CHANGELOG | Updated with the partial source claim and explicit remaining #346 boundary. |
| LICENSE, CODE_OF_CONDUCT | Reviewed; legal/conduct text unaffected. |
| SECURITY, CONTRIBUTING | Reviewed; no security authority, contribution procedure or command change. |
| docs/THREAT_MODEL.md, docs/ARCHITECTURE.md | Reviewed; read projection only, no changed trust boundary, store or execution topology. |
| docs/QUICKSTART.md, scripts/demo-no-key.sh | Reviewed; existing no-key commands/CLI output unaffected; unrelated workflow not rerun as this slice's acceptance. |
| docs/RECORDING.md, docs/assets/aegis-no-key.typescript | Reviewed as historical, not candidate/session/browser proof; no regenerated recording claimed. |
| GitHub binaries/checksums | No release requested or published. Existing assets are not proof for this dirty source candidate; release verification remains separate. |
| docs/contributing/ISSUE_BACKLOG.md | Contributor backlog remains separate; no GitHub issue created or broader issue closed. |

The candidate build uses the inspected `scripts/build-source.sh OUTPUT` wrapper
(no wrapper `--help` mode) and Go build help. The parent must bind later publication
to the complete frozen inventory, not just HEAD or a tracked diff. Evidence,
build executable and caches are explicitly excluded outputs; all relevant
tracked/untracked source and generated/build inputs remain inventoried.
