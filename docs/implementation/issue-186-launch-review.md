# Issue 186 Queue skill launch review

## Scope and evidence boundaries

Queue advisory guidance/evaluations and tests only, based on `1606a30a3f1ba96ed1cb582a84a9500976a668c0`.
No backend, runtime admission, authority policy, grant, scheduler, CI budget or
security timeout changes. Existing typed services remain the sole effect owners.
The [portable evaluation ledger](../../skills/aegis-execution-queue/references/evaluation-ledger.v1.md)
maps source fixtures to their actual boundaries; it is not another evaluator.

- Structural: canonical manifest validation and `evaluate` check declarations;
  behavioral/service/runtime execution remains `not_run` in their output.
- Executable source: strict command decoding, real validator secret-shaped-content
  denial, exact foreign-Loop evidence rejection with atomic unchanged history,
  and compiled CLI through Serve/Unix authentication/isolated Badger. These are
  not observed agent adherence or real-provider acceptance.
- Portable package: opt-in `TestPackagedQueueSkillInstall` runs the supplied
  delivered CLI install/inventory outside the checkout and checks exact files.
  Missing archive identity is SKIPPED, not PASS. Parent owns frozen-archive
  execution and fresh supported-Hermes `skills_list`/`skill_view` readback.
- Full main CI, independent frozen-candidate review, release publication and
  installed/live deployment remain separate gates; none is inferred here.

The source fixture reads exact `direct_loop`/source/input identity, workspace
cancellation, unsupported online expire/exhaust, strict bundled bind/reclaim
requests, remaining attempt budget and backoff. Ordinary retry cannot substitute
for an authoritative stop acknowledgment, including after lease expiry.
Exhaustion remains state `failed` and reason `retry_exhausted`. The one-node
retry contract and exact raw-v2/managed-v7 `max_attempts: 1` two-node boundary
remain distinct. No runtime policy was changed to satisfy a test.

## Affected / unaffected launch assets

| Asset | Review / disposition |
| --- | --- |
| `CHANGELOG.md` | Updated Unreleased with Queue guidance and separate evidence tiers. |
| `docs/HERMES_SKILLS.md` | Updated only Queue guidance/evaluation/package tier mapping. |
| Queue skill, evaluation fixtures and canonical manifest | Updated v0.3.0 text, portable ledger, declared file inventory and canonical digests. |
| Root `README.md` | Reviewed unaffected: existing direct-source readback and bounded-chain source boundaries remain implemented; no command or product behavior changes. Existing broad lifecycle vocabulary is not a new exhausted state claim. |
| `LICENSE` | Reviewed unaffected: no licensing change. |
| `SECURITY.md`, `docs/THREAT_MODEL.md` | Reviewed unaffected: no authentication, grant, runtime/evidence admission, disclosure or host-isolation change. Advisory installation grants no authority. |
| `CONTRIBUTING.md`, `CODE_OF_CONDUCT.md` | Reviewed unaffected: same delivery/testing and community rules. Existing owner-designated private conduct-reporting route remains a community-launch blocker; not repaired or waived by this slice. |
| `docs/ARCHITECTURE.md` and architecture diagram | Reviewed unaffected: existing CLI/API -> application -> isolated persistence path; no new service, store, scheduler or runtime adapter. |
| `docs/QUICKSTART.md` | Reviewed unaffected: five-minute no-key path does not install this skill or execute Queue lifecycle. |
| `docs/DEMO_NO_KEY.md`, `scripts/demo-no-key.sh` | Reviewed unaffected: this slice does not change demonstrated commands; no real Hermes/provider demo was launched. |
| `docs/RECORDING.md`, retained no-key transcript/timing | Reviewed unaffected historical evidence, not a Queue skill/model demonstration; no recording synthesized or refreshed. |
| GitHub release binaries/checksums, release workflow | No publication authorized. Dirty source binary is local test evidence only; parent must rebuild/freeze after commit, then independently verify archive/checksums. Existing release machinery unchanged. |
| `docs/contributing/ISSUE_BACKLOG.md`, issue templates | Reviewed unaffected: no new contributor workflow or external issue writes. Existing focused proposals remain local proposals. |

## Builder verification and repairs

Retained predecessor RED evidence covers missing exact guidance/evaluation cases
and missing acceptance-port helper. The first manifest refresh failed on the
stale Queue version; the declared v0.3.0 and new ledger inventory were supplied,
then the canonical refresh passed. No parallel manifest generator was introduced.

The actual compiled-CLI lifecycle fixture initially caught two test-construction
errors: `loop` was incorrectly expected instead of canonical `direct_loop`, and
the ordinary retry inherited the reclaim-only reason. The fixture now uses
`graph.SourceDirectLoop` and `ReasonRuntimeRetry`; production policy is unchanged.
The same binary then passed `TestInstalledOnlineLifecycle` in 54.943s under the
shared foreground port helper on candidate port 8450. No extra candidate listener
or reserved 8448/8098 service was used. Failure logs remain in builder scratch.

Ordinary focused skillbundle, command wire, acceptance-address and foreign-loop
tests passed. Race skillbundle (89.915s), command wire (1.056s) and foreign-loop
(1.788s) passed with `GOMAXPROCS=2 GOFLAGS=-p=1 GOMEMLIMIT=512MiB`, `-count=1`
and the unchanged eight-minute test cap. Final race lifecycle also passed
(62.097s) using that same binary under the shared helper on port 8450; the
acceptance-address race test passed (1.054s). Final standalone validation passed
with fifteen skills; evaluation reported 109 structurally valid cases and
behavioral/service/runtime execution `not_run`. `git diff --check` passed.
These are focused builder checks, not full-suite or main CI PASS.

Local controlled build: `scripts/build-source.sh ./aegis`, Go 1.26.10,
`vcs.revision=1606a30a3f1ba96ed1cb582a84a9500976a668c0`, `vcs.modified=true`.
SHA-256: `38452fe8edbd149edf5bc105914595c9709b822e52ee6e1b74c41e9bd5358db2`.
This truthful dirty build is not a releaseSourceRevision/provenance acceptance
claim or a frozen final archive. No commits, pushes or deployment performed.
