# Issue 326: bounded direct-Loop correction and launch review

## Candidate and ownership boundary

Baseline: `d182410e862864dca482b7b31d3023ebc64ab5f7`, branch
`fix/326-direct-loop`. This is an uncommitted local candidate, not a release or
whole-issue acceptance. The parent owns independent review, freeze, publication,
CI and merge. Issue 326 must remain open until its remaining acceptance is met.
No engine rewrite, worker/binding/completion change, Loop/Graph workspace layout,
v5 browser launcher, new command, API route or schema operation is supplied here.

The existing direct Loop engine is reused from PR328,
`a171f33b1e0a8c52605c22a154a4f3bb8bfe9527`. Delivered fleet skill recipes are
historically anchored by PR394,
`a13ba22f3e8bbf242ccd95618189949ebcc7d2be`. These anchors establish historical
source delivery, not current-candidate runtime or installed acceptance. Parent
context identifies historical PR328 provider proof; that retained external proof
was not rerun or independently reverified in this builder stage. It must not be
promoted to a fresh current-provider/browser verdict. Repository launch reports
such as `doer-live-completion-launch-review.md` retain their original historical
limits and are not rewritten into new PASS evidence.

## Criterion and evidence-tier ledger

| Criterion | Existing or repaired source / fixture evidence | Qualification boundary |
| --- | --- | --- |
| Direct exact Loop submission without reusable Graph publication | Existing PR328 engine; no replacement engine in this candidate. Existing historical Graph replay remains separate. | Source reuse, not fresh live-provider proof. |
| Direct and Graph normalized input readback | Repaired Queue projection reads `QueueExecutionView.Inputs` independently of Graph resolution. JSON false, zero, object, array and escaped string cases are checked through the Queue renderer. | Focused synthetic renderer/model regression; fresh browser proof NOT EXERCISED. |
| Truthful input origin and absence | Source label is `normalized run snapshot`; original caller/default provenance is not retained. Missing declared Graph ports are absent/missing, not defaulted. | No new defaults, inference or schema fields. |
| Disclosure and escaping | Uses only the existing authenticated execution view, already used for Console JSON. No workspace-file/source/credential lookup; rendered values remain HTML-escaped. | Removed the initial invented port-name secrecy assertion: arbitrary strings named task/workspace do not establish a secret classification. No broader disclosure authority is claimed. |
| Explicit distinct v4 Run | Authenticated exact selection calls the identity/redirect helper; new cookie is followed by HTTP 303 to the clean exact-revision URL. | Handler fixture tests redirect, clean reload/reopen, unauthenticated denial and exact revision/digest cookie separation. Full browser history behavior NOT EXERCISED. Repeating the explicit action requests another run. |
| Unknown-response recovery | Existing original-key readback/resume remains unchanged; cookie key generation/binding and Run origin/CSRF admission are not relaxed. | Focused existing API regressions; no automatic unknown-effect replay or budget reset. |
| Exact selected-file negatives | Existing verifier unchanged; exact raw-byte matrix now explicitly includes empty output and invalid UTF-8 alongside whitespace/case mismatches. | Controlled local files only; not a runtime-produced hello acceptance case. |
| Immutable snapshot after definition change | Real repository API publishes Loop revision 2 after direct admission, closes/reopens storage, then compares original canonical snapshot bytes, exact revision/digest pins and accepted lineage. Existing no-Graph-family/replay/corruption checks remain. | Durable repository fixture, not provider execution or general crash-recovery proof. |
| Separate exact publication and Run | Existing supported v4 browser journey and typed v5 journey remain distinct; neither is replaced with an editor/launcher. | No new end-to-end provider/browser execution in this candidate. |
| Portable guidance and packaging | Narrow ordinary specs, Doer guide, README/quickstart/changelog and two source skills updated; existing skill manifest refreshed and validated/evaluated. | Structural packaging proof is not installed skill adoption or agent behavior. |
| Hard Loop/Graph acceptance | Existing contracts unchanged. Independent affected-source tests and static security review are scoped separately below. | Current candidate real-provider, genuine approvals, public built-runtime journey, browser, installed target, full suite, CI and merge NOT EXERCISED. No whole-326 completion. |

## Actual bounded checks

All Go test commands below used `GOMAXPROCS=2 GOFLAGS=-p=1 GOMEMLIMIT=512MiB`,
`-count=1` and the unchanged `-timeout=8m`. Raw logs and runtime/session material
are not retained in this document or committed to source.

- RED, before production corrections: `go test ./internal/api -run
  '^TestDirectLoopJourney'`: exit 1. Direct inputs were empty; Graph values were
  empty and incorrectly default-labeled; new-run action had no canonical redirect.
- GREEN ordinary: `go test ./internal/api ./web/console -run
  'Test(DirectLoopJourney|ConsoleLoopRun|ConsoleQueue|QueueWorkspace)'`: exit 0,
  API 68.735s and web/console 0.012s.
- GREEN race: `go test -race ./internal/api ./web/console -run
  'Test(DirectLoopJourney|ConsoleLoopRun|QueueWorkspace)'`: exit 0,
  API 4.439s and web/console 1.040s. This is a narrower selection than ordinary.
- GREEN ordinary evidence/storage: `go test ./internal/evidence
  ./internal/persistence/fleet/badger -run
  'TestSelectedFileExactRawBytesRejectsWhitespace|TestDirectSnapshotAtomicDurableWithoutGraph'`:
  exit 0, evidence 0.005s and fleet/badger 0.372s.
- GREEN race, same evidence/storage selection: exit 0, evidence 1.023s and
  fleet/badger 1.519s. No production verifier change was needed.
- `AEGIS_REFRESH_SKILL_MANIFEST=1 go test ./internal/skillbundle -run
  '^TestRefreshManifest$'`: exit 0, 0.090s. Existing manifest generator only.
- `go test ./internal/skillbundle/...`: exit 0 (7.724s and command package 0.173s).
- `go run ./internal/skillbundle/cmd validate .`: exit 0; valid bundle, 15 skills,
  bundle version 0.2.18. `evaluate .`: exit 0; 103 structurally valid fixtures,
  behavioral/service/runtime execution explicitly `not_run`.

### Independent scoped evidence and assertion follow-up

Independent source report:
`/home/javi/.hermes/cache/scratch/326-independent-source/report.md`.
For the pre-follow-up frozen candidate, ordinary and race modes each passed
19 top-level tests and 41 subtests across five packages, with zero fail/skip
events. Its supplied freezer compared complete manifests before and after:
1811 source entries and 27028 build-input entries. These counts describe that
frozen inventory, not full-suite coverage or the later assertion-repair candidate.
Exact commands, selected names, parsed events and identity comparisons are in
that evidence directory (`selection.json`, `execution.json`, `before.json`,
`after.json`, `identity-comparison.json`). Both test commands exited 0.

Independent static security review:
`/home/javi/.hermes/cache/scratch/326-independent-security/review.json`.
It reports scoped PASS with no introduced blocking security or logic defect,
full before/after identity equality, and the same 1811/27028 inventory counts.
It launched no tests, build or runtime. This is static review of the then-frozen
candidate, not browser/provider/installed acceptance or independent retest of
the subsequent test/doc edits.

That source review demonstrated three concrete assertion gaps, now repaired:
the V5 negative matcher must detect the actual query-bearing v4 Run action;
route denials require exact 401/403/400 responses instead of merely non-200;
and initial run-cookie indexing requires a successful-response/cookie precondition.
A positive control using the old `/run"` matcher failed with
`V5 negative matcher misses the rendered V4 Run action` (exit 1) before the
matcher was corrected. The same matcher now checks the rendered v4 positive
control and v5 absence. No v5 launch surface or production code was changed.

Follow-up focused ordinary and race commands used the resource settings above:
`go test [ordinary, then -race] ./internal/api -run
'^(TestDirectLoopJourney.*|TestConsoleLoopRun.*|TestConsoleDoerRunAffordanceOnlyOnV4)$'
-count=1 -timeout=8m`. Both exited 0 (ordinary 1.429s, race 4.466s).
Gofmt and `git diff --check` passed. These builder results are not a new
independent frozen-candidate acceptance packet.

### Executable provenance correction

The independently frozen executable reports `aegis version dev` (exit 0), while
`version --provenance` exits 1 with `source provenance unavailable: binary has no
embedded source revision`. Go build metadata and frozen inventory binding do not
repair that missing embedded revision. Any earlier builder report that treated a
zero-exit build invocation as verified canonical provenance is superseded by this
observed failure. Plain `go build` is compilation evidence only; it is not the
prescribed provenance-bearing `scripts/build-source.sh` delivery gate. The frozen
binary does not establish that gate, regardless of a prior reported command exit.

No rebuild was performed for this assertion/documentation follow-up. The parent
will commit the completed source and invoke the supported
`./scripts/build-source.sh ./aegis`, then verify actual embedded provenance and
refreeze source, docs, generated inputs and executable together. Earlier scoped
results remain historical evidence for their exact candidate; documentation and
bundle validation cannot supply missing runtime or provenance acceptance.

## Launch-asset impact ledger

| Required asset | Review disposition |
| --- | --- |
| Root README | Changed narrowly: normalized input display and canonical v4 distinct Run, with this acceptance boundary linked. No fully accepted 326 claim. |
| LICENSE | Reviewed; Apache licensing unchanged, no new dependency or licensing boundary. |
| SECURITY.md | Reviewed as unaffected by implementation scope: authenticated existing input disclosure, cookie binding, origin/CSRF and authority boundaries remain; no new secret access or sandbox claim. |
| CONTRIBUTING.md | Reviewed as unaffected: same private fixtures, one source owner, independent acceptance and fixed resource budgets; no delivery bypass. |
| CODE_OF_CONDUCT.md | Reviewed as unaffected; its existing missing owner-designated private conduct-reporting route remains a community-launch blocker. No contact invented. |
| CHANGELOG.md | Additive Unreleased source-correction entry; does not close 326 or claim a released fix. |
| Threat model (`docs/THREAT_MODEL.md`) | Reviewed as unaffected by these presentation/identity-navigation repairs; runtime authority, credential custody and host-not-sandbox limits unchanged. |
| Architecture diagram (`docs/ARCHITECTURE.md`) | Reviewed existing Fleet/Queue/authority diagram and direct-Run prose. No new component, store or execution boundary; historical Graph composition is not an exclusive execution requirement. Diagram unchanged in this bounded repair. |
| Five-minute quickstart (`docs/QUICKSTART.md`) | Corrected obsolete statement that new v4 Run submits a single-node Graph. No-key proof remains separate. No new command or execution example added. |
| No-key demo (`scripts/demo-no-key.sh`) | Reviewed as unaffected; it discovers runtime and exercises the design boundary, not these Console journeys. NOT RUN: this stage forbids runtime/config/authority setup. No successful provider result manufactured. |
| Short terminal recording (`docs/RECORDING.md`, retained no-key timing/typescript) | Historical record remains historical and unchanged; not re-recorded, replay-certified or advertised as current Console proof. Fresh candidate browser/visual recording remains pending. |
| GitHub release binaries/checksums (`docs/RELEASE_PIPELINE.md`) | Release-deferred. Local build is not an official archive, checksum verification or published artifact. No remote release inspected or changed; exact-head CI, release packaging/checksums and owner-authorized publication remain separate. |
| Focused contributor issues (`docs/contributing/ISSUE_BACKLOG.md`) | Reviewed local proposals as unaffected. No remote issues created, modified or closed; no new issue required merely to record this repair. |
| Source skills and derived manifest | Two guidance additions use existing protected edit path successfully; no installation/profile mutation. Existing manifest generator refreshed `skills/aegis-skills.json`; validator/evaluator passed with structural-only tier. No operation matrix or `docs/HERMES_SKILLS.md` change: no route/command/schema added. |
| Inherited Queue introduction (`web/console/queue_workspace.templ:175`) | Independent security review identifies stale source/default-binding copy despite truthful normalized-snapshot rows. Peer-owned template is unchanged pending explicit owner agreement; no generated template refresh or ownership bypass. This remains a documented copy gap, not evidence of inferred provenance in the repaired projection. |
| Ordinary normative specs | MVP, identity/authorization and storage distinguish direct exact Loop work from intentional Graph composition over shared immutable snapshot/Queue substrate. Preserve Graph-specific participant/authority checks and historical acceptance sequence. |
| Protected AGENTS.md amendment | PENDING, unchanged. Earlier protected write approval timed out without consent. No retry or alternate-tool write. Proposed wording would identify numbered MVI Graph sequence as historical, and include direct exact Loop workspace submission without weakening runtime/Graph authority. Parent must obtain separate supported approval; this ledger is not that amendment or permission. |

## Remaining gates

Protected AGENTS wording approval, peer-owned Queue introduction agreement,
verified provenance-bearing build/refreeze and broader independent acceptance
remain outstanding. Independent affected-source and static security review above
are SCOPED only and predate this assertion follow-up. The complete real hard
Loop/Graph journey, fresh
current-provider execution, genuine human decisions where required, real-browser
history/visual checks, installed skill adoption, installed workstation acceptance,
full suite, exact-head CI and merge are not proven here. No service, runtime,
provider, credential, configuration, approval or grant setup was performed to
make this documentation appear green. No release or workstation update is part
of the authorized scope.
