# Hermes minimum-version launch-asset review

## Contract and evidence boundary

The accepted stable Hermes minimum is `>=0.18.0`, with no arbitrary upper cap. This removes a version-policy ceiling, not the structured gateway protocol, capability verification, clean-session, immutable approval, or fresh authority gates. Minimum-version acceptance does not guarantee compatibility of every future release.

The retained no-key recording documents bounded Hermes 0.18.2 development observations. It is not qualification of the whole accepted range or a new candidate. Synthetic identity tests are parser evidence only; no real Hermes/provider qualification was performed in this documentation work.

## Mandatory asset ledger

| Asset | Review outcome |
| --- | --- |
| `README.md` | Updated minimum and acceptance-versus-qualification boundary. |
| `LICENSE` | Apache-2.0 text present; unaffected by runtime-version policy. |
| `SECURITY.md` | Reviewed; authority/process boundaries unchanged. Existing owner-designated private reporting prerequisite remains unresolved in the document. |
| `CONTRIBUTING.md` | Updated runtime prerequisite and exact-version evidence requirement. |
| `CODE_OF_CONDUCT.md` | Reviewed, unchanged; private owner-designated conduct-reporting route remains a documented launch blocker. |
| `CHANGELOG.md` | Added unreleased minimum-version-policy entry; historical release entries preserved. |
| `docs/THREAT_MODEL.md` | Updated scope and explicit fail-closed/qualification boundary. |
| `docs/ARCHITECTURE.md` and its inline diagrams | Reviewed; process/authority topology unchanged. Historical 0.18.x toolset observations are not a new version ceiling. |
| `docs/QUICKSTART.md` | Updated runtime prerequisite; no fresh end-to-end quickstart claimed. |
| `docs/DEMO_NO_KEY.md`, `scripts/demo-no-key.sh` | Updated prerequisite prose; script delegates discovery to Aegis and has no independent upper cap. No live demo run in this lane. |
| `docs/RECORDING.md`, `docs/assets/aegis-no-key.typescript`, `.timing` | Added historical qualification boundary; retained bytes unchanged. Bounded accelerated `scriptreplay` passed. This is replay, not a new recording. |
| GitHub release binaries/checksums | Inspected `.github/workflows/release.yml` and release-candidate verification. No local `dist` directory; no binaries/checksums built or published here. Remote existence and checksum correctness remain unverified; publication is outside authorization. |
| `docs/contributing/ISSUE_BACKLOG.md` | Focused local proposals present; updated post-launch inspection proposal to exact accepted release qualification instead of a 0.18.x restriction. No remote issues created or verified. |
| `docs/launch/OPEN_SOURCE_LAUNCH_AND_GROWTH_PLAN.md` | Release checklist now separates minimum acceptance from exact qualified releases. |

## Other affected surfaces

- `specs/RUNTIME_AND_SESSIONS.md` and `docs/HERMES_SKILLS.md`: current contract clarified.
- `scripts/verify-installed-fleet-vertical.py`: generated charter uses the minimum-only constraint.
- `scripts/verify-release-candidate.sh`: replaces the 0.18.x substring gate with bounded strict identity/minimum checking through `scripts/verify-hermes-version.py`; existing timeout, output bound, exact source/candidate/decision, rollback, and publication boundaries remain.
- `scripts/verify_hermes_version_test.py`: synthetic minimum, newer minor/major, decorated identity, malformed/prerelease/ambiguous/oversized and CLI exit-status regressions; invoked from the existing release-candidate regression script.

## Verification performed

- `python3 scripts/verify_hermes_version_test.py`: PASS, four test methods including table-driven cases.
- `sh scripts/verify_release_candidate_test.sh`: PASS, named denial tests and identity tests.
- `sh -n scripts/verify-release-candidate.sh scripts/verify_release_candidate_test.sh`: PASS.
- Python AST parse of `scripts/verify-installed-fleet-vertical.py`: PASS; installed vertical not executed in this lane.
- `timeout 15 scriptreplay --timing docs/assets/aegis-no-key.timing docs/assets/aegis-no-key.typescript --divisor 1000`: PASS (output suppressed).
- `git diff --check`: PASS at review time.

## Residual integration and launch obligations

1. The execution-layer protected-file gate blocked the requested `AGENTS.md` edit. It remains unchanged with the old interval. Do not bypass the gate. Intended replacement: accept stable Hermes `>=0.18.0` without an arbitrary upper cap, explicitly retaining exact-version protocol/capability/authority verification and no blanket future-release qualification.
2. Integration updated `examples/office-charter.json`, `skills/aegis-skills.json`, operator-lifecycle-diagnostics and manager-onboarding skill instructions, and manager-onboarding fixtures; the canonical manifest refresh regenerated inventory digests. Skillbundle and architecture tests pass. Previously approved legacy charters retain their exact original interval and digest; runtime selection still enforces that interval. Only new charters default to the minimum-only constraint.
3. Dated `research/` observations and historical implementation evidence retain their original intervals intentionally; they report what was observed then, not today's normative acceptance policy.
4. Fresh real-runtime demo, exact-candidate installed verification, and any changed-output recording regeneration remain unverified in this lane. Full release-candidate verification requires clean exact source and operator-supplied decision inputs; no release/issue/production action is authorized here.
5. Existing private security/conduct contact and owner-controlled publication/remote contributor-issue obligations remain. Local assets or passing parser tests do not close those launch gates.

## Parent integration evidence

- Installed Hermes 0.21.3 discovery, gateway readiness, zero-tool inventory and interactive preflight passed in an isolated home without prompts or provider/model calls. This is a compatibility probe, not full live-provider qualification.
- Explicit empty-tool launch now selects `context_engine`, not the `no_mcp` sentinel. Design and zero-tool attempt/session paths fail closed on nonempty or malformed `tools.show` inventories. README, architecture, threat model and runtime specification were synchronized.
- The release parser now accepts the actual three-component decorated 0.21.3 identity; its regression failed before the parser correction and passes afterward.
- Uncached low-concurrency CLI, app, orchestration and API integration suites passed; the opt-in Graph browser test was not enabled. Core, adapter, skillbundle and architecture suites passed in parent verification.
- Full uncached Go suite passed with `GOMAXPROCS=2 GOMEMLIMIT=512MiB go test -p 1 -parallel 1 ./... -count=1 -timeout=8m`: 47 packages reported `ok`, no failures, exit 0. Evidence: `/home/silas/.hermes/.scratch/aegis-hermes-minimum-full-tests-background.log`. This does not enable opt-in live-provider/browser tests or establish release publication.
- No installed executable, shared Hermes runtime, production state, service, release, commit or remote branch was changed by this work.
