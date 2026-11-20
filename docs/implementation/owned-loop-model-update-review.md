# Owned Loop model update: source implementation review

## Scope

Local source-only repair on `fix/owned-loop-model-update`, based on
`4d080acad71271c4068304ac189a6cf9ee0504bf`. No live service, credentials,
provider call, host approval, commit, push or merge. Parent owns independent
candidate acceptance and publication. This is engineering, not Aegis-managed work.

## Implemented and controlled-fixture proof

- File-free `loops model-update LOOP REVISION --agent ... --provider ... --model ...`
  builds the exact next revision and preserves non-Hermes nodes. Explicit approved
  models publish through the unchanged publication checks with no human chores.
- A missing model produces an immutable bounded operational proposal with full
  candidate, original Agent/charter, proposed charter, exact model and Console link.
  Nothing executable is published. Reopening Store retains identical proposal bytes.
- Existing Console password/origin/CSRF and single-use exact-review mechanisms own
  the independent decision. The new service seam rejects peer/local-OS subjects,
  stale/revoked password subjects, foreign owners, tampering, changed Agent and
  occupied conflicting Loop/charter revisions. Approval updates only the exact
  Agent charter binding; it does not publish or Run.
- `loops edit-publish ID` publishes the same retained bytes/digest through normal
  admission. Same-key replay is idempotent; old publication/provenance/lifecycle
  remains intact and no Queue item is created.
- Automatic design is bounded: one enabled stanza, one missing model, preserved
  defaults and scope, existing unambiguous provider transport or the existing
  zero-Agent-tool/credential controller Codex transport. Unsupported transports,
  local artifact changes, multiple missing models/stanzas and contribution-role
  changes deny. See [workflow/limits](../OWNED_LOOP_MODEL_UPDATE.md).

## Observed RED and repairs

- `TestOwnedLoopModelEditRetainsDeniedRevision` first reproduced the original
  `PublishLoopAs` approved-envelope denial, then failed with
  `missing product-owned retained edit continuation after model denial` (exit 1).
  The fixed test retains that original denial assertion and passes via the new path.
- `TestOwnedLoopModelEditCompetingRevisionDoesNotApprove` initially failed with
  `conflicting Loop revision still approved: <nil>`; exact occupied-revision
  validation repaired it without relaxing publication.
- The first Console continuation exposed raw canonical-JSON indentation in review
  receipt comparison (409). Complete compact JSON comparison now preserves every
  field/digest while allowing storage/HTML whitespace; the exact Console test passes.
- CLI tests exposed Unix path-length and inherited Console-origin fixture issues;
  private short fixture paths and explicit fixture environment isolate them. The
  online adapter also needed explicit HTTP 202 acceptance limited to edit submissions.

## Verification executed

Use `GOTOOLCHAIN=go1.26.11 GOMAXPROCS=2 GOFLAGS=-p=1 GOMEMLIMIT=512MiB`.
All commands below completed successfully unless otherwise marked:

- `go test ./internal/app -run '^TestOwnedLoopModel|^TestPublishAndReadinessEnforceCharterLaneEnvelope$|^TestAgentCharterSuccessorExactApprovalAndBootstrapLineage$|^TestDoerDraftCandidateStepLanesPinDigestAndReview$' -count=1 -v -timeout=60s`
- `go test ./internal/api -run '^TestOwnedLoopModelEditConsoleDecisionAndResume$|^TestDoerSetupNativeProposalExactDecision$' -count=1 -v -timeout=60s`
- `go test ./internal/app ./internal/api ./internal/command -run '^TestOwnedLoopModel' -race -count=1 -v -timeout=60s`
- `go test ./internal/command -run '^TestOwnedLoopModelEditCLI|^TestDistributedOperationMatrix|^TestOwningServiceInventoryMatchesDispatch$' -count=1 -v -timeout=60s`
- `go test ./internal/architecture ./internal/skillbundle/... -count=1 -timeout=60s`
- `go run ./internal/skillbundle/cmd validate .` and `evaluate .`: valid bundle;
  109 structural fixtures, explicitly not runtime/service execution.
- `go vet ./internal/app ./internal/api ./internal/command`; `git diff --check`.
- Native linked-worktree `scripts/build-source.sh` failed closed twice because
  Go omitted `vcs=git`, including with an explicit per-command worktree binding.
  This is a build-provenance prerequisite failure, not a passing candidate build.

## Launch-asset impact

Updated README, SECURITY, CONTRIBUTING, CHANGELOG, threat model, architecture
flow diagram, focused workflow guide, Loop skill, operation matrix and generated
skill-bundle manifest. Reviewed LICENSE and CODE_OF_CONDUCT as behaviorally
unaffected; the latter's existing missing private reporting route remains a
community-launch blocker, not something this slice invents.

Reviewed `docs/QUICKSTART.md`, `docs/DEMO_NO_KEY.md`, `scripts/demo-no-key.sh`,
`docs/RECORDING.md`, retained transcript and
`docs/contributing/ISSUE_BACKLOG.md`: unchanged no-key commands/historical scope;
they do not qualify this new workflow. No real Hermes demo or recording was run,
because live execution is outside this slice. Reviewed release script/provenance
contract: unchanged packaging; no new release archive/checksum/publication is
claimed. Remote release binaries/checksums and remote contributor issues were not
queried or modified. Source-candidate qualification is not launch clearance.

## Follow-up repair (same task worktree)

- Regressed the three independent findings before repair: published updates were
  still pending/expired or republished using current-Agent provenance; 16 expired
  edits exhausted lifetime capacity; unknown selectors returned HTTP 500.
- Read-only exact publication reconciliation now checks complete bytes, validation,
  original key/audit binding, owner and historical publisher/authority provenance.
  Console/read/update/publish return the original publication after expiry or Agent
  drift. Approved or published outcomes issue no new review; stale receipts deny.
- The existing 16-entry pending set is compacted only after terminal proposals are
  preserved create-only in individually bounded history. Fixed expiry, same-key
  conflicts, recovery from archive-before-compaction and concurrent cap remain.
- Unknown selectors return HTTP 400 `invalid_input` before mutation. Fixtures use
  **openai-codex / gpt-6.2-sol**, including the approved no-extra-review path.
- All 15 owned-model test roots passed normal and race. The combined app race run
  exhausted its unchanged 60-second package deadline; all app roots then passed in
  three bounded groups (history, denials, continuation). Original failure retained.
  Four authority/Doer and three storage regression roots passed both modes; vet,
  architecture, dispatch inventories and skill validation/evaluation passed.
- Workflow/retention guide, skill/manifest, changelog, architecture and threat model
  updated; other launch assets were inspected as unaffected. Real-provider demos,
  new recordings, remote assets/issues and final independent retest remain outside
  this source repair. No live service, commit, push, merge or deployment occurred.

## Remaining gates

No real browser/visual campaign, actual human interaction, live provider execution,
service restart, installed-target qualification or hard same-revision publish/Run
acceptance was performed. The API test uses the real enrolled-password Console
manager and handlers with fixture credentials, not a human decision. CLI transport
proof uses an isolated Unix fixture; it does not replace a complete built-service
CLI/Console integration campaign. Full hosted CI and independent security review
remain parent-owned. All prior live records and denied-candidate evidence are
untouched; unsupported automatic-design cases are not silently represented as done.
