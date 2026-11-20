# Clean-authority combined-recovery review

## Scope and outcome

This source repair allows the authenticated combined offline command to continue
from a normally closed authority store to a still-interrupted fleet store.
Authority-only recovery, normal Open, onboarding, service defaults and runtime
admission are unchanged. There is no new authentication path or TTL extension.

Clean authority validation runs under the exclusive maintenance lease, after
receipt resume, using closed-generation identity checks plus strict physical
inventory, engine checksums and canonical-record validation. It publishes no
markers or receipts. Completed history remains principal-bound and immutable;
pending history never becomes a clean-validation shortcut. Output distinguishes
`verified_clean` from actual `recovered` authority.

## Exercised evidence

- Reproduced the original `unexpected root entry` through the public CLI built
  from base `4d080acad71271c4068304ac189a6cf9ee0504bf` against an actual writer
  fixture with clean authority and dirty fleet.
- Candidate public CLI, real enrolled-password verification and explicit
  fixture-only pinentry: clean-authority recovery and exact completed-authority
  retry pass; all 47 populated fleet logical key/value records match after
  recovery/retry and normal reopen. Prior authority receipt and CLEAN bytes
  remain unchanged; a normal clean authority gets no fabricated recovery evidence.
- Public CLI wrong password, pinentry cancellation, ambiguous authority staging
  and malformed fleet publication history deny. Fleet remains non-openable and
  does not publish CLEAN on denial.
- Focused ordinary and race tests cover clean authority, authority-only denial,
  live writer exclusion, cancellation, unsafe/corrupt engine, malformed logical
  records, conflicting/missing markers, extra generations, receipt-less evidence,
  exact completed/pending history and a later normal close. Existing interrupted
  recovery, receipt-custody and real permissive-umask regressions pass.
- Focused command authentication/orphan regression, affected-package `go vet`,
  formatting/diff checks and retained-recording boundary/timing check pass.

The CLI fixture uses normal Registry, Loop, direct Queue snapshot/submission,
Preferences, Contribution control, Decision and Delivery writers. It does not
prove every fleet family or positive released/displaced publication custody.
No live database, user password, service lifecycle or TCP listener was used.

## Launch-asset impact

| Asset | Review outcome |
| --- | --- |
| Root README | Updated existing recovery summary; no new workflow or installation claim. |
| LICENSE | Reviewed, unaffected; no licensing/dependency change. |
| SECURITY | Updated clean-authority verification boundary; no authority relaxation. |
| CONTRIBUTING | Reviewed, unaffected; contribution and delivery rules unchanged. |
| CODE_OF_CONDUCT | Reviewed, unaffected. |
| CHANGELOG | Added unreleased bounded repair. |
| Threat model | Updated clean-marker/non-renewal boundary. |
| Architecture diagram (`docs/ARCHITECTURE.md`) | Reviewed, unaffected; same offline application, stores and lock boundaries. |
| Five-minute quickstart | Reviewed, unaffected; ordinary initialization and gateway paths unchanged. |
| No-key demo | Reviewed, unaffected; does not invoke offline recovery. Live runtime demo NOT EXERCISED. |
| Terminal recording and timing | Historical capture remains explicitly historical; local boundary/timing regression passed. No recovery recording fabricated. |
| GitHub binaries/checksums | Release workflow reviewed, unaffected. No release artifacts built/published or remote checksum qualification claimed. |
| Contributor issues | Local `docs/contributing/ISSUE_BACKLOG.md` reviewed, unaffected. No remote issues created. |
| Recovery guide and storage specification | Updated actual acceptance, result fields and opt-in public-CLI fixture invocation. |

## Remaining gates and infrastructure observations

This is builder-owned isolated source/public-CLI proof, not independent acceptance,
full CI, live-provider execution, installed recovery or release qualification.
Those gates remain NOT EXERCISED. No commit, push, deployment or service changes.

The linked-worktree build helper refused missing Go VCS metadata. Verification
therefore used a private standalone clone of the exact base with the complete
modified/new source overlaid, retaining truthful dirty-source provenance. The
initial CLI harness used a disposable HOME outside the checkout and correctly
failed development-profile ancestry checks; the corrected harness retains the
operator HOME only for executable profile resolution and supplies explicit
private config/state, a minimal environment and a fixture-only helper. Those
prerequisite failures are not product recovery failures or passing denial proof.
