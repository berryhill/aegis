# Issue 245 — launch-asset review, loop attempt 1

Task `t_386c7cc`, session `sess_81456c8f774c`, step `review_launch_assets`, dispatch `dr_525bf3cc`, completion attempt `ta_t_386c7cc_dr_525bf3cc_14`.

Review activity completed. Candidate outcome: **FAILED_RETRYABLE**. Publication is not allowed. This is not final verification, publication, CI, merge, or task completion.

## Candidate and evidence custody

Consumed local candidate `34373584168e41dbcb6fe346efa4e57dad9bfc40`, branch `task-t_386c7cc-245`, in the registered task worktree. HEAD contains the issue-245 implementation relative to base `0d6663f3380e3170bc0aea7fc3b37257e7d94290`. Initially there were four untracked verification logs but no tracked modifications. Those logs were preserved by moving them into ignored `.scratch/issue-245-launch/`, not deleted or staged. The source was then clean, including untracked status, before packaging.

The unchanged installed verifier passed for that exact clean candidate before this review's documentation corrections. All four binary archives and the skill bundle passed checksums, native extraction, clean embedded VCS revision, fail-closed first run, credential-independent fleet execution, and authenticated real-Chrome console verification. This proves the verifier's bounded fleet/browser contract, not the new conversational credential-create journey or a live provider. Version `0.0.0` is local proof packaging, not a published release.

Extracted Linux/amd64 binary SHA-256: `d3fbb0310f85bf25f0530618fba12c8c93d3e76d3e41d492e5794b7f085e00a3`. Independent `go version -m` readback reported the exact candidate revision and `vcs.modified=false`. Archive/checksum inventory is retained in `.scratch/issue-245-launch/dist/SHA256SUMS`.

The documentation corrections below are uncommitted successor changes. No current dirty checkout is claimed to have passed clean-source packaging. Preserve the candidate and these corrections; prepare a successor through the normal implementation/test loop and repeat exact-revision acceptance. No provenance check was weakened.

Historical findings in `docs/implementation/issue-234-launch-review.md` were inspected and remain unchanged and attributed to their original task/candidates. No canonical migration-audit identifier or prior issue-245 loop packet was supplied; this attempt-1 record does not fabricate one. The completion output carries current findings to the sole loop gate.

## Asset review and disposition

| Asset | Evidence and disposition |
| --- | --- |
| README.md | Corrected stale unconditional gateway-create unavailability. Distinguished gateway metadata approval and exact identifier entry from in-process immediate creation/name normalization. Existing new Terminal Credentials link retained. |
| LICENSE | Reviewed Apache 2.0 text. Unaffected by this change; no licensing edit. |
| SECURITY.md | Corrected gateway guidance-only description, scoped slash-confirmation claim to Agent Registry, documented the separate approved intake boundary and limitations. Private-reporting owner decision remains unchanged. |
| CONTRIBUTING.md | Corrected gateway test obligations: ordinary turns remain value-free, unsupported clients deny, capable terminal handoff and separate approved intake are tested. |
| CODE_OF_CONDUCT.md | Reviewed; technically unaffected. Explicit missing owner-designated private reporting route remains a final community-launch gate. |
| CHANGELOG.md | Existing implementation entry reviewed; added launch-documentation reconciliation entry. |
| docs/THREAT_MODEL.md | Added exact operation/session lifecycle, approval/replay/uncertain-write threats, separate raw-byte boundary, client-header non-authentication and no-echo/unsynchronized-input limits. |
| docs/ARCHITECTURE.md | Corrected prose and diagram to include capable-terminal handoff, session-bound approved operation and separate credential write outside Hermes; unsupported guidance retained. |
| docs/QUICKSTART.md | Added both issue phrases and actual metadata/review/yes/value/confirmation sequence; scoped legacy immediate-create behavior to in-process manager. |
| docs/TERMINAL_CREDENTIALS.md | Reviewed against terminal, service and API source; existing new guide remains unchanged. |
| docs/DEMO_NO_KEY.md; scripts/demo-no-key.sh | Reviewed; excludes manager/credential proof and needs no issue-specific command change. Real invocation failed before charter/design at unsupported Hermes identity; unresolved reproducibility finding, not passed demo. |
| docs/RECORDING.md; docs/assets/aegis-no-key.typescript and .timing | Reviewed and replayed successfully. Explicit historical development evidence, unaffected CLI sequence; not current demo or credential-create proof. No fabricated/regenerated transcript. |
| .github/workflows/release.yml; scripts/verify-installed-mvi.sh; local archives and checksums | Reviewed unchanged packaging/publication boundary. Exact pre-documentation candidate passed locally, with all five checksum entries independently rechecked. No GitHub release inspected as current-candidate evidence or created. |
| docs/contributing/ISSUE_BACKLOG.md | Updated proposal 15 to distinguish unsupported-client denial from capable-terminal protected handoff. All proposals remain repository-local; no external issue creation. |

## Commands and real results

Go was supplied through command-local `PATH=/home/silas/go/bin:$PATH`; installed toolchain reported Go 1.26.6. No runtime/model/provider configuration was changed.

- `go mod download`: passed.
- `python3 scripts/build_source_test.py`: passed, one nested-worktree provenance regression.
- `python3 scripts/verify-console-vendor.py`: passed pinned asset digest.
- `python3 scripts/console_security_test.py`: passed current static harness.
- `go generate ./web/console` and `git diff --exit-code -- web/console go.mod go.sum`: passed, no generated/module drift.
- `./scripts/verify_installed_mvi_test.sh`: passed denial tests.
- `python3 -m unittest scripts/verify_release_archive_test.py scripts/verify_installed_fleet_vertical_test.py scripts/operator_acceptance_poc_test.py`: ten tests passed.
- `./scripts/verify_release_candidate_test.sh`: passed named denial tests, not an operator-approved candidate transaction.
- `go test ./...`: passed; cache-eligible, with command and CLI suites rerun.
- `go test -race ./...`: passed; package timing output retained.
- `go vet ./...`: completed without diagnostics; independently repeated successfully after documentation edits.
- `test -z "$(gofmt -l cmd internal web)"`: passed without rewriting source.
- `go run golang.org/x/vuln/cmd/govulncheck@v1.6.0 ./...`: exit 0; selected Go 1.26.8 automatically. No called-symbol vulnerabilities; reported two imported-package and four required-module vulnerabilities not apparently called. This is not a claim of zero dependency vulnerabilities or a scan under Go 1.26.6.
- `./scripts/verify-installed-mvi.sh 0.0.0 .scratch/issue-245-launch/dist 34373584168e41dbcb6fe346efa4e57dad9bfc40`: exit 0. Build-source helper, archive validation, 101 skill evaluations, all checksums, extracted-binary fleet and real-Chrome authenticated console passed. Zero CSP violations, JavaScript errors, request failures or unexpected HTTP 500 responses reported.
- Independent `sha256sum -c SHA256SUMS`, native archive validation/extraction and `go version -m`: passed.
- `env -i HOME="$HOME" PATH="/home/silas/go/bin:/home/silas/.hermes/hermes-agent/venv/bin:/usr/local/bin:/usr/bin:/bin" ./scripts/demo-no-key.sh`: exit 1, `aegis: unsupported Hermes identity output`. No provider material supplied, profile/runtime replaced or failure hidden.
- `scriptreplay --timing docs/assets/aegis-no-key.timing --divisor 1000 docs/assets/aegis-no-key.typescript`: exit 0. Transcript SHA-256 `90e34999cdc7e8bb35f6d3532e961bd5f0e9409a29cdbd86ffa1d20aa8d1544c`; timing SHA-256 `68285362b0c89625f151ddc7f3a691c9f392d6ee8c4857227f75b8fa3b77d390`.
- Focused protected gateway/terminal tests: passed for managergateway and command. Initial API regex selected no tests; corrected `go test ./internal/api -run TestManagerIntake -count=1` passed both matching tests.
- `python3 -m unittest discover -s scripts -p verify_installed_credentials_test.py`: five tests passed; harness guards, not the full Credentials browser journey.
- `python3 -m unittest discover -s scripts -p console_security_contract_test.py`: FAILED import of removed `bounded_navigation_source`. An initial package-style invocation first failed module resolution; correct discovery exposed the substantive missing-symbol defect.
- `git diff --check` and `git diff --exit-code -- cmd internal scripts web go.mod go.sum`: passed after documentation edits. Production code, tests and release guards were not modified by this verify step.

Evidence logs: `.scratch/issue-245-launch/{checks,installed,demo,replay,vuln,focused,independent}.log`, plus preserved earlier task logs and retained archives. Independent read-only same-provider/model Hermes reviewer reached its eight-turn bound and exited 1 with a static summary; it is advisory evidence, not a passed independent acceptance run. Parent readback confirmed the relevant API/service/terminal code and documentation findings before corrections.

Not every documented workflow was executed: commands requiring owner-authored release decisions, live model certification, production update/reset, gateway unit installation/activation, provisioning, or retained-state cleanup were not applied to the operator environment. Placeholder-ID workflows require their prerequisite authenticated artifacts. No external release or issue, approved decision file, live conversation, fresh protected terminal recording, or systemd acceptance was fabricated. Full release-readiness remains outstanding; its source transformation cannot accept the current uncommitted documentation successor.

## Finding-by-finding custody for the sole loop gate

1. **L245-DOC-1 — repaired in working source, successor acceptance pending.** Unconditional gateway unavailability and immediate-create wording conflicted with `internal/command/manager_credential_intake.go:161-218` and `internal/managergateway/credential_intake.go:93-175`. README, quickstart, architecture, security, threat model and contributor obligations now distinguish handoff from mutation, explicit approval from the natural-language request, and gateway from in-process behavior. Parent diff/readback verified bounded documentation-only changes. No claim that these edits are part of the already packaged revision.
2. **L245-HARNESS-1 — unresolved, repairable launch/security-test defect.** `scripts/console_security_contract_test.py:4` imports `bounded_navigation_source`, absent from current `scripts/console_security_test.py` (only `require` and `main`). Discovery cannot run the adversarial source tests. These scripts are unchanged by issue 245; the last change to the producer is commit `5d1b0ff2fa37e8aaccbd70c48dadbb499afae200`. Repair the test/producer contract against current bounded navigation and retain hostile-source rejection; do not delete assertions or relax CSP merely to pass.
3. **L245-DEMO-1 — unresolved reproducibility gap.** Real no-key invocation failed supported-runtime discovery. Historical replay succeeds but does not supersede that failure. Locate a verified supported Hermes 0.18.x executable through approved custody and rerun the isolated no-key workflow, or escalate the precise prerequisite. Never replace the normal runtime, fake its identity or weaken discovery. Regenerate/review the recording only if demonstrated output changes.
4. **L245-ACCEPTANCE-1 — unresolved exact issue acceptance evidence.** The exact candidate's installed fleet/browser proof does not enter this new credential dialog. Source PTY/gateway/custody tests and documentation inspection do not establish the requested installed conversational terminal journey. Obtain exact successor installed-terminal proof for both phrases, metadata approval, typing/multiline confirmation, same-session return, canonical single-record readback and fail-closed denials using generated material; distinguish live-model acceptance from fixture evidence. Preserve prior security-review output and all unresolved issue-specific findings at the gate.
5. **Owner gates — separate, unchanged.** SECURITY.md requires confirmed private vulnerability reporting/contact; CODE_OF_CONDUCT.md requires an owner-designated private conduct route before community launch. Matt owns these decisions. They are final launch/publication policy gates, not reasons to prohibit local source repair or fabricate a code-loop retry. No contact or clearance is invented.

## Attempt 2 implementation addendum (not release acceptance)

Consumed packet `tflp_83a5ce560bb143e0`, hash `f043e15a801ddd2b71e6890bd6ed5a35cd8af1291cdd9b69eb9d2236d7db689a`. Direct readback matched HEAD `34373584168e41dbcb6fe346efa4e57dad9bfc40`, the task branch and all carried documentation corrections. Historical attempt-1 results below and above remain unchanged.

Domain/control contract: repair the inherited test producer/consumer mismatch without granting browser authority or weakening CSP, authentication, protected intake or provenance. Acceptance for this bounded implementation step is reproduced RED followed by passing adversarial source checks, current Agent navigation checks and focused credential tests. Non-goals are runtime replacement, provider/model changes, installation/activation, publication and a claim of completed issue acceptance.

- L245-HARNESS-1: reproduced the exact missing-helper ImportError. Restored `bounded_navigation_source` in the production source harness, pinning only the exact current Graph, Agent and credential adapter blocks with fixed SHA-256 values; reject missing/duplicate/changed adapters and any fragment/redirect primitive outside them. Retained all original adversarial assertions, added Agent/Graph mutations and extra/missing/duplicate adapter cases, and invoked the helper from the main harness. These pins are source-review guards, not proof of runtime authorization or general JavaScript safety.
- Running the associated dynamic Agent test uncovered another inherited fixture mismatch: `window` was undefined. Corrected its VM browser fixture to expose `window` as the global and the actual URL pathname. No production navigation code changed and no adversarial expectation was removed.
- GREEN: `python3 -m unittest discover -s scripts -p console_security_contract_test.py` (four tests); `python3 scripts/console_security_test.py`; `node scripts/agent_navigation_test.cjs`; `git diff --check`; command-local Go PATH with `go test ./internal/managergateway ./internal/command ./internal/api -run 'Test.*(Intake|Credential)' -count=1` (all three packages passed).
- L245-DOC-1: preserved all prior corrections; clean successor commit/packaging remains pending, not covered by the historical binary.
- L245-DEMO-1 and L245-ACCEPTANCE-1: still open verification obligations. This source-only implementation step did not qualify a supported Hermes executable or execute the installed conversational journey. No fixture pass is substituted for either obligation.
- OWNER-LAUNCH-GATES: unchanged, owner decisions remain required. No external writes, merge, runtime changes or fabricated clearance.

Launch impact of this addendum is test/evidence-only; prior asset corrections and the full required downstream launch review remain pending. Confirmed judgment: a passing primary harness did not establish that its adversarial consumer or browser-shaped VM fixture ran. The narrow remediation preserves checks and fixes their execution contract. Missing evidence remains explicitly attached to the same task rather than being erased by a local GREEN result. Publication remains prohibited until the remaining reviews and exact-successor acceptance pass.

## Attempt 3 installed-terminal implementation addendum

Consumed identity-bound packet `tflp_e477382a14494aa1`, hash `085c8328f8c78ef6f2fc7c0c5da4b53a81174442d2baf66e0e53e6790e45052c`. Authenticated canonical FlowStore/TaskQueue readback now succeeds through the profile-scoped constructors: task `t_386c7cc`, session `sess_81456c8f774c`, cursor 4 (implementation), loop attempt 3 and packet identity match. Complete prior step outputs were read, not the truncated conductor excerpts. This recovered an additional carried browser failure (`L245-BROWSER-2` / `L245-INSTALLED-BROWSER-2`) omitted from the previous gate packet; it remains a required exact-candidate installed-browser check.

Domain/control contract: close the missing executable-terminal test boundary without altering production authorization, custody, runtime discovery or model configuration. Acceptance is a digest/revision-bound extracted CLI process, real Unix authentication/API/custody, exact metadata review, no write before approval, protected typing and multiline confirmation, continued same-process conversation, canonical single-record/value reopen, zero-write cancellation and plaintext-canary absence. Service-manager observations are explicitly fixtures, not host deployment evidence. No production Go code changed.

Changes:

- `manager_gateway_intake_linux_test.go` retains its in-process coverage and adds `TestInstalledGatewayIntakePTY`, which actually starts the supplied executable instead of calling the terminal loop in-process. Both tests share seven scenarios. Metadata review and pre-approval zero-record assertions are stronger; reopened custody verifies exact decrypted bytes in memory; canary scans include all disposable regular files, including the bbolt database outside the state directory, and the random first line of multiline input.
- `manager_installed_intake_linux_test.go` requires independent binary SHA-256 and exact clean embedded Git revision, writes only generated protected fixture material, uses a minimal child environment, validates isolated unit bytes against production Preview/Installed, and accepts only three exact read-only systemctl observation queries. It never invokes real systemd or modifies an operator unit.
- CONTRIBUTING documents the required opt-in invocation and evidence limits; CHANGELOG records test coverage, not a new release verdict.

Fresh evidence from this step:

- Independently read binary SHA-256 `ca076ad762d16b81fb3a10b66e5eda0889b570a747f7eb482fb33a2484fbeef9` and Go provenance: revision `cd75e148158313f4230003a383dc5e22505c1ce5`, `vcs.modified=false`, Go 1.26.6. Binary: `.scratch/issue-245-launch-attempt2/extracted/aegis`.
- With `GOMAXPROCS=2 GOFLAGS=-p=1 GOMEMLIMIT=2GiB`, ran `go test ./internal/command ./internal/managergateway ./internal/api -run 'Test(InstalledGatewayIntakePTY|GatewayIntakePTY|ProtectedIntake|ManagerIntake)' -count=1 -parallel=2 -v`, supplying that binary's absolute path, checksum and revision through the three documented non-secret acceptance variables. PASS: seven installed scenarios, seven in-process scenarios, exact-approval/single-consumption test, eleven gateway no-write negative scenarios, strict metadata and browser-capability denial tests. This was focused verification, not the full suite owned by final_verification.
- The installed scenarios exercised both issue phrases through the extracted production CLI, metadata review/yes, protected typing and multiline confirmation, `/status` after return, terminal restoration, exactly one persistent record on success and zero on cancellation. Reopened encrypted custody returned the exact confirmed bytes without printing them. No PTY transcript or value was retained.
- Independent read-only same-provider/model reviewer session `20260913_155006_487905` confirmed the fixture boundary and identified the old whole-value/state-only canary and metadata-only readback gaps; these are repaired and passed above. Its snapshot caught the test while the external process branch was not yet implemented. Subsequent direct source readback plus passing external-binary tests supersede that intermediate observation; the reviewer did not verify the final patch.
- The operator recovery output and `.scratch/recovery-245-supported-runtime/{RECOVERY.md,demo.log}` match the same candidate and establish the real supported-Hermes 0.18.2 no-key demo passed. No runtime/provider/model changes were made by this worker. This is reused evidence, not a fresh demo rerun.
- A fresh inert Chrome probe using the unchanged installed verifier's launch flags reached DevTools readiness in 0.15 seconds and was terminated/reaped. This rules out a currently absent Chrome prerequisite; it does NOT resolve the historical full installed-browser failure. Retained diagnostic reference: `.scratch/issue-245-chrome-probe/tmpsjd7wruf/chrome.stderr`.

Done/remaining:

- DONE: canonical evidence read access, supported-runtime recovery evidence, executable-terminal harness implementation and focused success/denial proof against the named existing binary.
- REMAINING: normal candidate preparation and exact-successor tests/packaging; complete installed-browser verification, security/launch/final reviews, exact-head CI and merge. No new production defect was established by the focused work. The old browser failure must not disappear from the later gate merely because it was omitted from the earlier packet.
- Evidence boundary: the extracted CLI is exact `cd75e148...`; api.Serve and injected custody are built from that production source with this uncommitted test-only successor. The session truthfully operates in deterministic degraded mode. This is not live-model conversation, real-systemd operation, packaged-server startup, production custody bootstrap or Matt's laptop deployment acceptance. Never report those as proven.

Launch impact review: inspected README, LICENSE, SECURITY, CODE_OF_CONDUCT, threat model, architecture diagram, quickstart, no-key demo documentation/source reference, historical recording guidance, release workflow/provenance/checksum evidence and contributor backlog. Only CONTRIBUTING, CHANGELOG and this evidence register need edits for this test-only slice. Existing user commands and security/architecture claims are unchanged. No new recording, release or contributor issue was fabricated or published. Community reporting-route owner decisions remain separate and unresolved. The required downstream full launch/executable-workflow review remains intact.

Judgment update: authentic source PTY tests were insufficient evidence of installed command routing; an actual digest-bound CLI child closed that gap without granting a fixture authority. Synthetic service-manager observations and injected server custody must stay visible in the verdict. Recovering full canonical outputs also revealed a dropped failure, demonstrating why truncated step summaries cannot define acceptance. Publication remains gated on downstream verification; this addendum is not task completion.

## Historical attempt-1 judgment (superseded only by explicit addenda above)

Confirmed: clean exact-head fleet/browser packaging can pass while adversarial Python source-contract tests are broken and current no-key demonstration is not reproducible. A successful historical replay is not current runtime evidence. Effective remediation was narrow documentation reconciliation with explicit candidate binding; production repair belongs to the implementation step. Uncertain/missing: approved supported-runtime discovery, issue-specific installed conversational acceptance, and owner reporting decisions. Publication remains denied; carry all unresolved findings to `merge_on_success_or_loop` rather than retrying this completed review step.
