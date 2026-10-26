# Hard acceptance prompt: GitHub Trigger → verified Graph execution

Establish and exercise a hard end-to-end acceptance case for Aegis Triggers: one real GitHub issue-label occurrence invokes the exact published producer → consumer Graph, without manual orchestration after the event.

This is a proposed acceptance contract, not a claim of installed capability. Saving this document does not authorize creating repositories/issues/labels/webhooks, changing credentials, enabling subscriptions, executing work, implementing code or deploying services. Obtain the relevant operator scope and preserve independent authority gates.

## Target and discovery

Target deployment: https://silas-workstation.taild7c550.ts.net:8446/console

Use the current installed workstation version and supported typed owning-service interfaces. Load aegis and relevant installed product skills; confirm actual command help and current source before selecting schemas or operations. Do not invent commands, endpoints, receipts, node types or scheduler behavior.

Source candidates follow [acceptance port isolation](acceptance-port-isolation.md):
dynamically claim 8450–8469, never reserved main 8448/8098. Named installed URLs
remain separate targets, not candidate defaults. No Trigger, Graph, real-event or
provider criterion changes with this location policy.

Related contracts:

- [Graph hard acceptance](graph-happy-path-hard-acceptance.md)
- [Loop hard acceptance](loop-happy-path-hard-acceptance.md)
- [Repository context](README.md)

Design context: https://github.com/berryhill/aegis/issues/201

The GitHub case exercises the provider-specific webhook increment. It does not replace the earlier authenticated manual/API Trigger increment or prove those prerequisites already exist.

## Preconditions: prove the execution substrate separately

Start with the producer → consumer Graph from the Graph acceptance contract. Prove a separate manual run first. Producer receives a typed text input, produces an independently verified artifact, and consumer receives that exact artifact and produces independently verified confirmation bytes. A broken Graph is not automatically a Trigger failure.

Use separate approved workspaces/run identities for the manual prerequisite and event-driven test. Both event-run output files must initially be absent; preserve prior results rather than deleting them to manufacture the precondition.

For this event-driven case, bind the expected output to the selected GitHub occurrence rather than replacing the original hello Graph test. Use a separately published compatible definition or reviewed supported per-run binding. Never mutate the original published acceptance definition or weaken its assertions.

## Primitive and creation contract

A Trigger has an exact Connector/source binding and an inspectable visual flow of invocation logic. An exact Trigger revision appears as one entry node in a Graph alongside Loop nodes. Connector authentication proves source origin, not execution authority.

The proposed low-friction authoring journey is: open the existing Graph → Add Trigger → select the approved GitHub Connector and source → define match/mapping → validate/publish → review/enable the exact activation binding. These are desired product interactions, not claims of existing UI labels or adapters.

Ask only for unresolved task decisions. Aegis resolves exact revisions, internal identifiers, idempotency identities and execution preparation. Do not make the operator carry digest JSON, receipt IDs, binding files or worker commands. Preserve the same draft/intent across prerequisite setup or interruption. Missing independent approvals remain explicit and human-controlled; no duplicate task consent.

## Fixed test configuration

Before firing the event, select and record:

- An explicitly operator-approved GitHub test repository and existing or separately authorized test issue.
- An approved GitHub Connector revision scoped to that source; no credential values in definitions, prompts or evidence.
- Event: a label is added to an issue.
- Match: exact repository, exact selected issue, exact label `aegis-trigger-test`.
- Typed mapping: derive Graph text input from the authenticated event's canonical repository name and issue number.
- Exact Trigger, Graph, Agent and Loop revisions and the separately admitted activation binding.
- Exact workspace/output paths, independent verification policy and bounded implementation attempts inherited from the reviewed execution contract.

Do not use `intake` or `implement`; they have existing operational meaning. Check for other automation on the test repository before any authorized source mutation. The test label must initially be absent; do not remove an existing label or alter another workflow without scope authorization.

Define the expected UTF-8 bytes after repository/issue selection and before event delivery:

- Producer output: `github:<repository>#<issue-number>`
- Consumer output: `confirmed:github:<repository>#<issue-number>`

Substitute the actual canonical repository and issue number; angle-bracket placeholders are not executable input. Neither output has a newline. Pin assertions before the runtime effect; output observed afterward cannot define its own expected value. A hardcoded hello result or a consumer ignoring its admitted artifact cannot pass.

## Publication and activation evidence

Prove separately:

1. Publish and read back the immutable Trigger with exact Connector/source binding, typed invocation logic, ports, constraints, validation and provenance.
2. Inspect its real visual logic flow; published rendering matches canonical semantics, not browser-local mock state.
3. Publish/read back a Graph pinning that exact Trigger entry node and exact downstream Loop references with validated typed connections. Opening the node resolves the exact Trigger revision.
4. Independently review and enable the exact activation/subscription binding using supported authority controls. Publishing a definition alone must not start a listener or grant execution.

Avoid circular definition digests: the Graph pins the Trigger; the separately specified activation binding identifies the exact Graph revision and entry node. Its schema and service behavior must be verified or designed under separately authorized implementation, not fabricated here. Visual authoring cannot remove mandatory source verification, deduplication or authority admission.

## Fire one real GitHub event

With the binding enabled and event-run outputs absent, the authorized tester adds `aegis-trigger-test` to the selected issue once.

After this source action, prohibit manual Aegis Run, Queue process commands, event injection, manual artifact copying or expected-output creation. Supported controller-applied runtime edits are allowed only through the admitted bounded execution path. The authenticated GitHub occurrence must drive the entire journey automatically.

Required chain:

```text
Real GitHub label-added delivery
→ source verification and durable occurrence intake
→ exact Trigger evaluation and typed mapping
→ current Aegis authority admission through exact activation binding
→ one accepted exact Graph-run lineage
→ producer Loop execution and independent verification
→ exact verified artifact handoff
→ consumer Loop execution and independent verification
→ evidence-backed terminal Graph outcome
```

Durable intake/acknowledgment, admission and execution are distinct. A `2xx`/`202`, webhook receipt, matching event, Queue item or live worker is not end-to-end success. Event payloads may not select another Graph, principal, stanza, runtime, credentials or authority.

## Positive acceptance

- [ ] Installed CLI/running-gateway provenance and separately observed readiness recorded.
- [ ] Manual prerequisite Graph run passes with independent output/handoff verification.
- [ ] Real Trigger-flow authoring/detail and Graph entry-node composition work through supported product surfaces; definitions persist and reopen exactly.
- [ ] Exact publication and independent activation evidence exists before the source event.
- [ ] Source delivery is authenticated and durably recorded before acknowledgment according to the reviewed Connector protocol.
- [ ] One occurrence evaluates the exact Trigger; matching path, mapped values and invocation decision are inspectable.
- [ ] Exactly one accepted invocation/Graph-run lineage results; existing approved scope is carried through without operator orchestration.
- [ ] Real bounded Loop executions produce the exact event-derived bytes within their authorized write and attempt bounds.
- [ ] Consumer uses the exact admitted producer artifact, not shared path visibility, copied text or a mutable latest reference.
- [ ] Independent artifacts/receipts and authoritative terminal dispositions prove both nodes and the Graph outcome.
- [ ] Product history reconstructs source delivery → occurrence → evaluation → invocation/admission → exact Graph/Loop executions → artifact handoff → evidence/disposition.

## Hard negative tests

Run controlled faults only in isolated test state. Do not corrupt live Connector configuration, production records or retained outputs to manufacture a test.

- Nonmatching label, issue or repository: no Graph execution; inspectable non-match reason.
- Exact duplicate/redelivery and concurrent delivery of the same canonical event: original decision/run lineage, no duplicate runtime effects or reset budgets.
- Same event identity with changed bytes: conflict, not a new accepted occurrence disguised as replay.
- Invalid signature/source authentication, wrong source/tenant binding or malformed/oversized input: no unauthorized invocation/effect; preserve transport rejection versus durable observation/admission semantics.
- Payload attempts to replace Graph, Agent or authority: cannot override immutable bindings or controller admission.
- Disabled Trigger, stale activation binding or revoked execution authority: no unauthorized run; report the actual durable outcome rather than inventing state names.
- Interruption after durable receipt and at admission boundaries: supported recovery finishes the same occurrence without loss or duplicate accepted Graph lineage. Separate this from unsupported exactly-once external-effect claims.
- Producer verification failure: consumer performs no runtime effect and Graph cannot succeed.
- Cross-run/node/attempt artifact or receipt substitution, even with matching bytes: no accepted handoff or false successful disposition.
- Consumer ignores input and hardcodes expected confirmation: an independently specified second event-derived case or equivalent behavioral regression detects this without weakening the original contract.

Isolated/synthetic negative tests supplement, not replace, the real GitHub positive run. Redelivery of one delivery and a genuinely new label transition are different events; do not claim unlimited suppression of new legitimate occurrences.

## Failure and implementation discipline

If Connector, visual Trigger flow, exact Graph Trigger-node binding, activation, automatic processing or downstream execution is unavailable, report the precise missing capability and furthest proven boundary. Do not simulate the feature with polling scripts, standalone Doer, direct-store writes, invented clients, custom schedulers or sequential manually launched Loops.

Report PASS, FAIL, BLOCKED or UNAVAILABLE per criterion; label unreached downstream stages NOT EXERCISED. Keep historical claims, source/unit proof, synthetic qualification and live installed acceptance distinct.

If implementation is separately authorized, follow the repository delivery contract and installed engineering skills: one source writer, real execution-capable conductor, frozen candidate, independent acceptance, exact-head CI and verified merge. Release/update/live activation remain separately scoped outcomes. Preserve all independent approvals and do not redispatch unchanged single-query approval failures.

## Required final evidence

Return exact identities/readbacks for:

- Installed and running versions/source revisions and readiness.
- Test repository, issue, label action and source occurrence/delivery references.
- Connector, Trigger, Graph, Agent and Loop revisions/digests, publication and activation evidence.
- Trigger evaluation path and exact normalized mapped inputs.
- Invocation/admission decision, submission, Queue and Graph-run identities.
- Both Loop executions/attempts and implementation-budget accounting.
- Exact output bytes, immutable producer artifact, edge binding and consumer input provenance.
- Independent verification receipts/policies and node/Graph dispositions.
- Negative campaigns, evidence tier, command/result references and any unexercised or unavailable dimension.

The decisive pass condition: one real GitHub label action causes the exact admitted Graph to execute and reach an independently verified outcome, with no operator orchestration between event delivery and completion. A webhook acknowledgment or accepted submission alone is not a pass.
