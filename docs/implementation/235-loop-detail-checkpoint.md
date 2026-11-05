# #235 bounded source checkpoint

## Implemented

- Separate authored `LoopRevision.Name` → `LoopDetailModel.HumanTitle`; stable RecordModel.Label/Key and native Run form bindings retained. Missing human name is explicit. A viewed historical revision no longer claims to be latest; publication head remains unavailable pending read enrichment.
- Full Tarjan SCC partition, deterministic component/member order, all component exits and per-transition bounds. Global iteration/exhaustion are explicitly not declared: opaque labels and max edge traversals are not promoted into domain facts.
- Self transitions retain directed return geometry. All stored steps retain the textual equivalent even when the canvas exceeds display bounds. Contextual incoming/outgoing transitions include mappings.
- Required-input and evidence header counts, Required inputs tab label, optional node instruction/tool rendering, cycle-band scope correction, and truthful supported direct Run / exact Graph-node explanatory copy.
- Loop-only same-origin hash reconciliation preserves collection filters and clears stale revision queries when changing exact read selectors.

## Evidence

- New residual tests RED against baseline: incomplete SCC returned self-only map[3:true]; overflow textual representation erased all 257 steps.
- `go generate ./web/console`: PASS after correction of an intermediate templ syntax error.
- `GOMAXPROCS=2 GOFLAGS=-p=1 GOMEMLIMIT=512MiB go test ./web/console -count=1`: PASS.
- Same resource settings `go test -race ./web/console -run '^TestLoop' -count=1`: PASS.
- Focused API `go test ./internal/api -run '^TestConsoleLoop(HumanTitle|ContractProjection)' -count=1`: PASS; same selection with `-race`: PASS.
- `node scripts/loop_detail_navigation_test.cjs`: PASS actual shipped-script selector/filter/stale revision/malformed-input checks.
- Root `./scripts/build-source.sh ./aegis` with exact task `core.worktree` override: PASS; `./aegis version`: dev.
- Real durable Console seed `TestLoopDetailFrozenSeed`: PASS under lifetime shared port claim 8450, isolated `.scratch/235-seed-check`; child exits before any Serve startup. Synthetic v4 definition publishes its actual authored name through typed service; no Run/provider execution.
- Actual candidate browser harness added but NOT RUN: system python3 lacks Playwright. No browser/visual/auth-loss/no-script acceptance claim.
- `git diff --check`: PASS before checkpoint ledger.

## Remaining / not accepted

This is NOT a complete frozen accepted candidate. Parent should continue sole-writer custody after this stopped checkpoint.

- Execute new actual candidate harness in a qualified Python/Chromium environment under lifetime shared port helper, new private root and root module binary. Expand it for history/missing records, disjoint/nested/self fixtures, no-script/CSP/auth-loss/keyboard/touch and maximum layout Fit acceptance.
- Whole internal/api package is deliberately NOT RUN under parent direction to avoid worker waiting on aggregate. Parent owns required full package checks.
- Supported executable/implementation bindings have not been traced/projected; absent generic step fields remain Not declared. Required inputs panel still retains the preexisting full definition/sealed-v4 presentation and needs input-focused reconciliation.
- Latest publication enrichment remains explicitly unavailable, not falsely inferred. Existing 0.3 Fit floor and maximum-layout behavior are unchanged; coordinate canonical shared behavior with #233 owner.
- No design DOM/browser visual comparison was performed: accepted offline artifact digest verified as `634e23fafafb5cd032a86ac80dbc3809d8e5bd9939cda80c16584beb4d4d2a9b`, relevant title/tabs/cycle CSS text inspected only.
- Legacy execution-topology compatibility fields remain zero-valued rather than inferring iteration or exhaustion from opaque conditions; independently verify affected Queue display expectations.

## Scope / launch review

No Graph-owned sources, embed.go, runtime/admission/services, Run form fields, credentials, authorities, grants, provider configuration or peer source were changed. No commit, publication, queue record or service deployment. Only synthetic isolated fixture configuration is written.

Root README, LICENSE, SECURITY, CONTRIBUTING, CODE_OF_CONDUCT, CHANGELOG and threat-model assets were inspected for scope; presentation-only changes do not alter their authority/runtime contracts. Changelog receives a narrow source checkpoint note. Architecture, quickstart, no-key demo, recording, release binaries/checksums and contributor-issue publication remain unmodified: no new command, install/release, runtime authority, or provider capability is claimed. Complete launch-asset assurance is parent-owned alongside full acceptance, not asserted by file existence or this ledger.
