# Issue 281 launch-asset impact review

## Verdict and provenance

Documentation is aligned to the **unreleased source candidate**, not an installed release or completed guided journey. Reviewed worktree: `/home/silas/.hermes/.scratch/aegis-281-agent-loop-journey`, based on `785801ee89e96b56bbf7fdc7d84e59ebb1eda21e`, with concurrent implementation edits. No commit, GitHub write, publication, operator configuration/model change, live service, real confirmation helper or live-provider run was performed by this review. A passing source build does not freeze concurrent source or fulfill the live-source gate.

[Doer Loop](../DOER_LOOP.md#guided-authoring-and-readiness-source-candidate) is the focused contract. Draft authoring, execution readiness, independent contextual charter/Agent successor approval, exact provisioning approval/apply, host consent, publication and Run are distinct boundaries. Published predecessors and old execution intents remain immutable. The new Console assertion default is raw UTF-8 bytes (`exact_bytes`); omitted/false preserves legacy trimmed assertions. No credential/model grant or complete host confinement is claimed.

## Source evidence inspected

| Boundary | Repository evidence | Evidence limit |
|---|---|---|
| Principal-owned retained draft, stable key, bounded expiry/capacity and CAS | `internal/app/doer_draft.go`, `internal/api/console_doer_draft.go`, Doer preview/publication handlers in `internal/api/server.go` | Draft is proposal data, not authority; source tests are not installed browser proof. |
| Authorability versus execution prerequisites | `internal/app/doer_readiness.go`, `internal/orchestration/doer_readiness.go`, candidate-readiness API tests | `not_checked` is not ready; helper protocol availability is not eligibility; freshness is repeated at effects. |
| Immutable successor/new bindings and exact provision reviews | `internal/app/doer_draft_successor.go`, `internal/api/doer_setup.go`, setup/successor tests | Prior draft binding is archived; successor creates a new Loop identity, revision-one binding and publication key. Independent authenticated confirmation remains required. |
| Independent exact signed host contract | `internal/app/doer_host_approval.go`, `internal/hostapproval/model.go`, `internal/store/doer_host_approval.go`, `internal/orchestration/doer_runtime.go` | At most 24h; exact deployment/owner/Agent/contract custody and Registry readback before effects. Existing configured allowlist remains supported. Consent does not grant model, credentials, session, provisioning or native-test authority. |
| Additive raw-byte policy and independently checked output | `internal/loop/doer.go`, `internal/evidence/file.go`, `internal/orchestration/doer_preclaim.go`, `internal/orchestration/doer_runtime.go`, raw-policy tests | `raw-utf8-bytes-equals` compares bytes without trimming; legacy `trimmed-utf8-equals` still trims both sides. Evidence/disposition, not narration, decides success. |
| Original requester versus authenticated human executor | `internal/app/doer_continuation.go`, `internal/store/doer_continuation.go`, `internal/continuation/`, custody tests | Signed exact intent retains browser identity/session and original Unix peer separately; no cookie relabel or authentication/self-socket fallback. |
| Native companion/portal | `internal/api/doer_companion_launch.go`, `internal/api/doer_continuation_portal.go`, `internal/api/peercred_linux.go`, `internal/command/doer_companion.go`, `internal/command/pinentry_confirm.go`, associated templ source | Reviewed again after local tests/build. Source now contains separate-process launch, inherited transport descriptor, PID-bound protected Unix review and native `CONFIRM` client. Integration is still concurrent/in-flight: final source freeze/review, new integration tests, extracted-candidate browser and genuine human acceptance are pending. Do not call the path installed or fully supported from helper fixtures alone. |

## Required asset disposition ledger

“Changed” describes this documentation lane only. “Unaffected” means inspected with no issue-281 edit warranted, not global release acceptance. Pending items are launch gates, not fabricated artifacts.

| Asset | Disposition | Reviewed evidence / remaining action |
|---|---|---|
| `README.md` | Changed | Concise link to focused source contract and pending proof; existing no-key path retained. |
| `LICENSE` | Unaffected | Apache-2.0 text and Aegis contributor notice inspected; no licensing change introduced. |
| `SECURITY.md` | Changed + pending owner gate | Adds independent approvals, exact host consent and native identity boundary. Existing private reporting policy still needs owner confirmation that GitHub private reporting is enabled or an actual private contact is published; remote availability was not checked. |
| `CONTRIBUTING.md` | Changed | Adds isolated regression scope and source-versus-live acceptance boundary; retains full verification prerequisites. |
| `CODE_OF_CONDUCT.md` | Unaffected + pending owner gate | Conduct/privacy rules inspected; explicitly missing owner-designated private reporting route remains a community-launch blocker. |
| `CHANGELOG.md` | Changed | Records source work under Unreleased, without inventing a version/date/release or completed companion acceptance. |
| Threat model (`docs/THREAT_MODEL.md`) | Changed | Adds stale-CAS, immutable-binding, exact-contract, peer-substitution and raw-byte threats; no complete same-account confinement claim. |
| Architecture diagram (`docs/ARCHITECTURE.md`) | Changed + pending visual review | Adds approval-separated Mermaid flow with companion edges marked pending; textual contract matches diagram. No rendered screenshot/visual acceptance is claimed. |
| Five-minute quickstart (`docs/QUICKSTART.md`) | Changed + pending rerun | Routes guided journey to focused source guide, preserves no-key path. Build/templates/help exercised; real design/init/service/model steps deliberately not executed. |
| No-key demonstration (`docs/DEMO_NO_KEY.md`, `scripts/demo-no-key.sh`) | Guide changed; script unaffected; current run pending | Script inspected and shell syntax checked. It invokes real disposable Hermes design, so it was not run in this lane. Existing provider-failure boundary does not prove guided Doer/live task success. |
| Short terminal recording (`docs/RECORDING.md`, `docs/assets/aegis-no-key.typescript`, `docs/assets/aegis-no-key.timing`) | Guide changed; captures unaffected/historical; refresh pending | Both files inspected and replay command exited 0. Capture identifies Hermes 0.18.2 and stops at unavailable inference provider; not current guided journey or live-model completion. New exact-candidate capture, sanitization and owner publication review remain required; no files fabricated. |
| GitHub release binaries | Pending; workflow unaffected | `.github/workflows/release.yml`, `scripts/release.sh`, `scripts/verify-installed-mvi.sh`, `scripts/verify-release-readiness.sh`, `scripts/verify-release-candidate.sh` inspected. No issue-281 `dist` binaries present; dirty concurrent source is not exact clean-candidate acceptance. Existing clean-source gates and explicit owner publication remain required. No GitHub release inspected or modified. |
| Release checksums | Pending; generator unaffected | No local `SHA256SUMS` artifact exists for this candidate. Installed verifier generates and checks archive checksums; no digest is invented. Actual candidate archives and matching checksums must be produced/verified after source acceptance. |
| Focused early-contributor material (`docs/contributing/ISSUE_BACKLOG.md`) | Changed | Adds bounded local follow-on regression/acceptance material, not fabricated remote issue links. Existing proposals inspected; remote issue creation requires owner authorization. |
| Current issue templates (`.github/ISSUE_TEMPLATE/task.yml`, `.github/ISSUE_TEMPLATE/config.yml`) | Unaffected | Exact scope/security/acceptance/verification fields and private-report guidance still apply. Remote private-report endpoint availability remains an owner gate. |
| `docs/DOER_LOOP.md` | Changed | Focused guided contract, retained CAS/key, independent decisions, raw/legacy semantics, host alternative, native pending boundary and safe checks. JSON example explicitly selects `exact_bytes`. |
| `docs/implementation/issue-281-launch-review.md` | Created | This ledger retains reviewed assets, actual local evidence and remaining target-specific gates. |

## Security recovery follow-up

The source candidate now includes stable signed continuation recovery before native review and immutable host-consent renewal generations. Recovery preserves the original executor authentication time/expiry and intent keys. Renewal appends only after a fresh explicit decision following expiry; active consent reuses its original time/expiry. All retained generations are reverified and the 256-generation bound fails closed without pruning. Controller state/custody, configuration, credential-authority paths and helper executable targets are excluded from approved workspaces even with configured allowlisted contracts.

Parent checks after these patches: focused recovery/renewal/continuation/host-approval suites passed in Store, hostapproval, app and API packages; the filtered architecture invocation had no matching tests. Repository-type protected-path and controller-target denial tests passed. A full serial source run found stale skill-bundle digest and operation-matrix coverage gaps; repair and a fresh complete run remain required. No installed or live execution acceptance is claimed.

## Commands actually executed

All commands below ran from the reviewed worktree. Fixtures used their isolated state/transports; no operator service, real model or desktop prompt was started.

```sh
GOMAXPROCS=2 go test -p 1 ./internal/loop ./internal/evidence ./internal/hostapproval ./internal/continuation -run 'TestDoer|TestSelectedFile|Test.*Raw|Test.*Exact|Test.*Approval' -count=1
GOMAXPROCS=2 go test -p 1 ./internal/loop ./internal/evidence ./internal/implementation ./internal/looprun ./internal/orchestration ./internal/api -run 'TestDoer|TestQueueDoer|TestSelectedFile|TestStepCheckpoint|TestImplementationQueueNativeCompletion/v4-' -count=1
GOMAXPROCS=2 go test -p 1 ./internal/command -run '^TestCompanion' -count=1
GOMAXPROCS=2 go test -p 1 ./internal/app ./internal/store -run 'TestDoerDraft|TestDoerHost|TestDoerContinuation' -count=1
sh -n scripts/demo-no-key.sh scripts/build-source.sh scripts/release.sh scripts/verify-installed-mvi.sh scripts/verify-release-candidate.sh scripts/verify-release-readiness.sh
./scripts/build-source.sh ./aegis
./aegis version
./aegis loops templates
./aegis loops doer --help
./aegis loops doer-reusable --help
./aegis loops implementation --help
scriptreplay --timing docs/assets/aegis-no-key.timing docs/assets/aegis-no-key.typescript >/dev/null
git diff --check
```

Results: all listed test invocations passed; `internal/continuation` has no package tests, and the filtered `internal/looprun` invocation had no matching tests. Companion checks inject inert helpers; no genuine human decision was made. Shell syntax checks passed. Source build passed, version was `dev`, configuration-free templates listed `aegis.doer.selected-file` v1, and all three help commands described draft-only behavior. The temporary `./aegis` binary was removed after inspection (none existed before this review). Replay and whitespace checks exited 0. A Python check of all 12 owned Markdown documents found no missing local link targets or new guided-contract fragment errors; the Doer JSON example parsed successfully. Git status/revision and repository-local inventory/readback were also inspected; no installed/protected source parity is inferred.

## Remaining gates and accurate preparation

1. **Final source:** freeze concurrent native portal/companion changes, re-review exact final paths and run the full source verification gate, including generated templ parity and newly added portal/launcher regressions. Earlier filtered tests/build do not prove later edits. No full final suite is claimed here.
2. **Browser/integration:** exercise the exact built candidate's retained draft, stale CAS/key recovery, readiness ordering, each independent approval, immutable successor/new binding, publication and same-key Run. Prove separate companion process identity and original Unix human versus browser requester with deny/cancel/unavailable/timeout/no-decision/substitution cases. Do not substitute a cookie relabel, self-connection or fake confirmation for the native acceptance target.
3. **Visual/accessibility:** render the architecture and inspect keyboard/focus, exact-scope review, cancellation, retained-field recovery, mobile layouts and evidence navigation on the exact candidate. No screenshots or visual verdict exist from this lane.
4. **Live source/operator:** explicitly authorized operator journey with actual supported Hermes/local Laya/configured model, independently approved exact authorities and host contract, and independent selected-file bytes/artifact/receipt/disposition readback. Fixture evidence is not fulfillment. Do not auto-install models, change profiles/configuration or launch services to satisfy this gate.
5. **Installed/release:** after accepted committed source, run existing clean-source release-readiness/installed-MVI and release-candidate workflows with explicit version/revision, supported runtime, private empty repository-local evidence workspace and genuine operator-authored decision. These workflows may build/launch isolated candidates and are outside this docs-only execution. Verify actual archives, VCS provenance and matching `SHA256SUMS`; publication requires separate owner authorization. No binaries/checksums/releases are fabricated.
6. **Recording/community:** regenerate the exact accepted candidate's sanitized journey recording if it is to advertise this feature, replay/review before owner-approved publication; resolve the private security and conduct reporting routes. Keep contributor proposals local until remote writes are explicitly authorized.
