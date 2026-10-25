# Issue 384 — native connection trust-transition launch review

## Scope and authority

Repair only the existing kernel-authenticated, same-selection native Codex
connection review/commit path. No new authority source, command, record schema,
credential operation, foundational approval or configuration writer is introduced.
The native allowlist addition must already be independently owner-approved;
connection reconciliation does not approve it. Native host tests are not sandboxed.
Startup native-worker capability wiring remains unchanged: this repair does not
hot-reconfigure a worker or automatically restart a deployment.

During authorized development recovery, the exact native addition and supported
same-login reconciliation succeeded. After the necessary development restart,
the helper probe exposed numeric-library startup overhead: the unchanged model
answered the exact task in about 15 seconds with host-default threading, versus
about 8.9 seconds with two-thread pools under the existing ten-second bound.
The follow-up caps only the fixed offline helper's OpenMP/MKL/OpenBLAS/NumExpr
pools. A trusted subprocess regression is RED before that cap and GREEN after;
it also rejects inherited ambient pool sizes while retaining secret isolation.
No timeout, model, protocol, confidence threshold or authority lifetime is widened.
This diagnostic is not native coding acceptance; real owning-service readiness
and the retained Run's separate artifact/receipt/disposition remain required.

## Reproduction and exact rejection path

At base `52bfce39464868c102da1a51e0acd75a2218b36d`, new trusted regressions
`TestNativeTrustSuccessiveExactAdditions`,
`TestNativeTrustExistingBlockDeltaDenials` and
`TestNativeTrustCompetingExactAdditionCommits` fail before production edits.
The initial appended native block succeeds under the existing supported contract;
a subsequent exact contract line fails. A fresh connection whose original config
already contains the native block also fails on its first exact addition.

- Without a previous v2 transition, `readControllerCodexBinding` reaches its final
  config-drift comparison; `nativeTrustAddition` cannot reconstruct an existing
  block's predecessor, so even `ReadControllerCodexForReview` denies.
- With previous v2 history, `readControllerHistory` requires the older record's
  `ConfigSHA256` to equal the newest snapshot digest, and evaluates the old delta
  against that newest snapshot. It denies before the final additive-review branch.
- `PreviewControllerCodexAs` first tries normal binding read, then the explicit
  review reader for `--replace`; both fail and it returns `ErrConflict` before
  source/account parsing. The HTTP layer renders 409. Runtime resolution separately
  denies invalid binding with provider-auth unauthorized. A 409 alone was not the
  diagnosis.

## Repair and denial envelope

Reconstruct exactly one plain final SHA-256 contract line addition, with every
other byte unchanged. Preserve the existing initial appended-block transition.
Reconstruct each historical v2 transition backwards in memory, then independently
validate the original forward config/binding/history chain. Keep canonical v1/v2
records and no-replace publication; persist no private configuration backup.
Runtime/show continue to require equality with the complete latest config digest.
The app's existing one-use review retains exact new bytes and old config/history
commitments and rechecks unchanged profile, opaque selector, account and principal.

Regressions cover repeated initial/later transitions, existing-block connections,
unchanged history, malformed/duplicate YAML, duplicate/wildcard/multiple contracts,
removed trust, changed checker/model/provider/principal/runtime/credential config,
stale config/history, substituted history, changed login selection, replay and
competing commits. `TestControllerCodexOwningTransport` exercises the real daemon,
kernel Unix principal, strict API and subprocess public CLI with isolated synthetic
native credentials and the installed qualified read-only Hermes parser. It proves
no Loop mutation and no Graph/Queue creation, not a managed-provider execution.

## Launch-asset ledger

| Asset | Review/result |
| --- | --- |
| README | Updated existing transition limits; exact online command grammar unchanged |
| LICENSE | Apache-2.0 unchanged; no dependencies or licensing change |
| SECURITY / threat model | Updated exact reconstruction and immutable-history boundary; same-UID/non-sandbox limits retained |
| CONTRIBUTING | Existing focused/race/native-host rules unchanged; this change uses them |
| CODE_OF_CONDUCT | Unaffected |
| CHANGELOG | Unreleased source repair only; no release/version claim |
| Architecture diagram / docs/ARCHITECTURE.md | Topology, sole owning service, stores and authority sources unchanged; no new component |
| Five-minute README path / no-key demo | No native connection/grant needed by this path; script inspected, no live model invoked |
| Recording | No tracked recording discovered; none fabricated; existing launch gap is not repaired by this issue |
| Release binaries/checksums | Not produced/published; outside explicit local-candidate scope |
| Focused contributor issues | Existing issue 384 defines source scope; 349/359/385 retain separate acceptance; no new external issue created |
| Distributed skill | Updated credential-authority same-selection limits and refreshed canonical bundle manifest |

## Builder-stage evidence tiers and remaining gates

Builder proof is retained in ignored `.aegis-e2e/issue384-proof/`: original RED,
focused normal/race, boundary regression logs and frozen source/executable inventory.
Config and skillbundle packages, focused config/app/API journeys, architecture
checks, vet and Console security source checks pass. Structural skill evaluation
is not behavioral agent execution. Full repository `make verify`, independent
frozen-candidate testing, real-provider selected-file acceptance, exact-head CI and
source delivery remain parent-owned gates, not claimed here.

The original builder changed no release, production config, login selection,
provider/model, TTL, service or live Run. The later parent-authorized development
native-trust transition and restart are described above; they do not establish
native RED-to-GREEN acceptance. The retained issue-385 intent and its two-pass
budget remain separate and unconsumed at this recovery checkpoint. No production
rollout, release or historical-record rewrite is claimed.
