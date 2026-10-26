# Hard acceptance prompt: verified producer → consumer Graph

Establish and exercise a hard end-to-end acceptance case for Aegis Graphs: a producer → consumer workflow with verified artifact handoff. This is product acceptance, not a schema demonstration or two manually coordinated Loop runs.

Target deployment:

Source candidates follow [acceptance port isolation](acceptance-port-isolation.md):
dynamically claim 8450–8469, never reserved main 8448/8098. The installed URL below
is historical target context, not a candidate default; all Graph criteria remain.
https://silas-workstation.taild7c550.ts.net:8446/console

Use the current installed workstation version first. The URL selects the owning deployment; use installed Aegis skills and supported typed owning-service interfaces. Do not use browser automation unless explicitly requested.

Load aegis first, then the relevant Graph, Registry, Loop, Queue, evidence, approval and lifecycle skills. Confirm the installed command capabilities. Keep current verified behavior, missing implementation and independent approval gates distinct.

## The hard acceptance case

Prove two distinct actions:

1. An authenticated agent creates, validates and publishes one immutable two-node Graph binding exact registered Agent and published Loop revisions.
2. The agent separately executes that exact published Graph through Aegis and independently verifies the complete outcome.

Use registered Agent Xander for both nodes if supported. Distinct node executions are required; creating another Agent is not part of this test. Do not change charters, providers, models or credentials merely to make admission pass.

## Graph contract

### Graph input

The concrete UTF-8 value hello, supplied at Run time—not embedded as a substitute for input binding.

### Node A — Producer

- Receives the Graph input through a typed input binding.
- Writes producer/result.txt containing exactly the five UTF-8 bytes hello, without a newline.
- Aegis independently verifies the output against a predeclared contract.
- Publishes an immutable, content-addressed artifact bound to this run, node and attempt.

### Edge — Verified artifact handoff

- Binds Node A’s exact verified output artifact to Node B’s typed input.
- Carries no credentials, mandates or authority.
- A filename, shared workspace visibility, mutable “latest” pointer, model narration or manually copied value is not artifact handoff.
- Node B must not execute before Node A’s required verification succeeds.

### Node B — Consumer

- Receives and reads the exact artifact admitted through the edge.
- Derives consumer/confirmation.txt from that artifact’s actual bytes.
- Writes exactly the UTF-8 bytes confirmed:hello, without a newline.
- Independent verification must establish both correct output bytes and consumption of the exact producer artifact—not merely acceptance of a hardcoded expected string.

### Graph completion

- Succeeds only after both nodes have independently verified outputs and the required evidence-backed terminal dispositions.
- The Graph result must retain reconstructable causality across input binding, node executions, attempts, artifact handoff, verification and disposition.

## Isolation and bounds

Use isolated producer and consumer workspaces with both output files initially absent. The consumer runtime must not have direct access to the producer workspace; it receives only the admitted artifact through the supported product mechanism.

Permit each node to write only its named output file. Permit at most two implementation attempts per node, enforced by Aegis rather than prompt prose. Neither the engineering builder, tester nor controller operator may manually create the expected output. Controller-applied edits produced by the admitted runtime are allowed only through Aegis’s normal bounded execution mechanism.

## Hard negative tests

In isolated test state, prove that Aegis fails closed when:

- The producer output is missing or has incorrect bytes, including an extra newline.
- Producer verification fails: the consumer performs no runtime effect.
- The handoff references an artifact from another run, node or attempt.
- Artifact content is substituted or corrupted after production.
- The consumer is given a path or mutable reference instead of the exact admitted artifact.
- Either node lacks its required authority or write authorization.
- A repeated submission of the same execution intent would otherwise create duplicate runs or effects.

Also prove that a same-bytes artifact from the wrong causal identity is rejected. Matching content alone does not establish valid handoff.

Test design must expose a consumer that ignores its input and simply writes confirmed:hello. Use a second independently specified input/output case or an equivalent isolated behavioral regression. Do not mutate a published definition or relax the original hello acceptance case to obtain that proof.

## Execution discipline

First attempt the journey through the installed product. If multi-node execution, typed run-time inputs, artifact handoff, isolation or aggregate disposition is unavailable, report the exact missing capability and the boundary reached. Do not simulate it with shell scripts, sequential manual Loop runs, direct-store writes, invented clients or a custom scheduler.

A readiness check, valid JSON, published Graph, queued item, live worker or passing unit suite is not end-to-end success.

If implementation is separately authorized, use the corrected aegis-implementation-model and aegis-development-loop procedures: establish a real execution-capable conductor and approval channel, retain existing task/history, use one source-owning builder, freeze the candidate, and independently test it. Do not redispatch an unchanged single-query approval failure or weaken approvals. Source tests and synthetic-runtime qualification must remain distinct from live-provider acceptance.

## Required final evidence

Report PASS, FAIL, BLOCKED or UNAVAILABLE for every acceptance criterion, with:

- Installed CLI and running-gateway version/source provenance.
- Exact Agent, Loop and Graph revisions and digests.
- Separate publication and execution identities.
- Exact normalized Graph input.
- Producer and consumer execution/attempt identities.
- Producer artifact identity, content digest and verification evidence.
- Exact edge binding and consumer input provenance.
- Independently observed output bytes.
- Node dispositions and authoritative Graph outcome.
- Negative-test results and evidence.
- Any unavailable inspection surface or unresolved independent gate.

The acceptance bar is a real, authority-bound, two-node Graph execution with verified dataflow. Do not call the case passed unless the entire chain is proven.
