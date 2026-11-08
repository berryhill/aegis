# Console logical Loop discovery

Published and Archived each list **one entry per stable logical Loop ID**, not
one entry per published revision. Publishing to an existing ID creates a new
immutable revision under that same entry. The authored name is display text;
identically named Loops with different stable IDs remain separate.

The entry shows the latest published revision independently from logical lifecycle
and the exact active revision. A newer publication does not imply activation.
Revision history under the entry and on exact detail links to each immutable
revision's diagram, validation and publication provenance. Exact bookmarks remain
readable even when archived or outside collection filters. Collection counts,
search, lifecycle filters and pagination operate on logical Loops; exact detail
selection does not add a historical revision to the collection or change counts.

Archive/Restore appears only on the logical collection entry. It changes discovery
for **all current and future revisions**, preserves exact definitions and append-only
lifecycle/history, and **does not stop execution**. It does not activate, retire,
republish, delete or reset runtime budgets. Mutations retain owning-Agent admission,
server-derived identity, fresh Console authentication, same-origin CSRF and exact
publication-head/availability-state/sequence/digest CAS. A stale page must reload;
repeating an already applied action is denied, not a second operation.

The raw `/v1/loops/inventory` remains revision-oriented. Graph and Queue history
continues to resolve the exact revisions/digests it originally pinned.

## Focused verification

```sh
GOMAXPROCS=2 GOFLAGS=-p=1 go test ./internal/api ./web/console \
  -run 'TestConsoleLoop|TestConsoleLogicalLoop|TestLoopAvailabilityHTTP|TestLogicalLoop' -count=1
```

Opt-in actual-browser regression reuses the child seed and frozen-candidate
harness, rather than adding another costly default integration root. Build the
candidate using `scripts/build-source.sh ./aegis` first. The private root must not
already exist; use a new path under ignored `.scratch/`. With Playwright and Chrome
installed, run under the operator's existing lifetime claim pool:

```sh
AEGIS_LOGICAL_LOOP_BROWSER=1 \
AEGIS_LOOP_DETAIL_CANDIDATE="$PWD/aegis" \
AEGIS_LOOP_DETAIL_ROOT="$PWD/.scratch/logical-loop-browser" \
python3 scripts/acceptance_port.py \
  --pool /home/silas/.hermes/profiles/xander/workspace/aegis-acceptance-ports/locks \
  -- env GOMAXPROCS=2 GOFLAGS=-p=1 go test ./internal/api \
  -run '^TestLoopDetailFrozenBrowser$' -count=1 -v
```

Set `AEGIS_LOOP_DETAIL_BROWSER_PYTHON` when Playwright is in a dedicated virtualenv.
This creates only isolated synthetic definitions: no Loop execution, main-state
mutation, live-provider qualification, installed acceptance or deployment claim.
