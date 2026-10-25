# Issue 382 — current Queue outcome source candidate

## Scope and boundaries

The Console renders a current-outcome summary before topology and refreshes it
through the authenticated exact-run poll. It separates Queue/disposition state,
closed recorded reason, verification and selected-content availability. Missing
legacy failure detail stays explicitly unavailable; `implementation_failed` is
not expanded from raw runtime errors, Requeue policy or topology. Success is a
recorded disposition, not a projection-only verified-acceptance claim. Selected
content still requires the existing exact stable-owner and intact evidence reader.
Native/Graph selected-content unsupported is not an execution failure reason.
No runtime classifier from unmerged #349/PR380 is copied into this change.

## Launch-asset impact

- Updated README, CHANGELOG and the execution-debugger guide for the outcome and
  authorization boundaries. Updated threat-model and architecture prose for the
  escaped server fragment and independent result-reader join; existing diagrams
  and authority edges remain valid.
- Reviewed LICENSE and CODE_OF_CONDUCT: unaffected by read-only presentation.
- Reviewed SECURITY and CONTRIBUTING: existing ownership, disclosure, CSP,
  authentication and verification requirements remain unchanged.
- Reviewed QUICKSTART and DEMO_NO_KEY: no command syntax, installation, authority
  or runtime requirements changed. Their no-key flow does not demonstrate this
  browser feature and was not rerun as outcome acceptance.
- Reviewed RECORDING and retained no-key capture scope: no demonstrated terminal
  output changed, no replacement recording fabricated or published. Historical
  recording is not current-outcome/browser evidence.
- Release binaries/checksums: no release or deployment is performed. The local
  dirty-source candidate and its full source/binary identities are retained under
  ignored task evidence; release archives, checksums and clean-commit acceptance
  remain the parent's later publication gates.
- Reviewed contributor ISSUE_BACKLOG: no new scheduler, general multi-node
  execution, protected evidence custody or live-provider capability is claimed;
  no remote issue created.

## Evidence and remaining gate

`.scratch/382/` retains focused Go/race, security/vendor, deterministic generation,
authenticated browser and build evidence. Browser fixtures observe real Serve and
durable interpreter history; separate terminal fixtures record failure and
file-verifier-evidence-backed success on the same page while replay is paused.
The success host bytes and admission are seeded and explicitly provider-free.
Reusable selected-result tests cover two stored results and exact owner/run/
attempt/authority/receipt/proof corruption denials. Synthetic reducer tests reject
foreign Queue, Graph-run and snapshot response HTML. Authentication loss removes
protected result/outcome/initial observation data and stops polling.

These are builder checks, not independent acceptance or a merge/deployment claim.
A fresh independent tester must verify the frozen complete source inventory and
candidate executable before actual candidate authenticated browser acceptance in
isolated state. Parent owns commit, PR, exact-head CI and merge; this builder does
not publish or change configuration, models, grants or production state.
