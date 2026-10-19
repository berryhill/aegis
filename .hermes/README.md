# Aegis project acceptance context

`AGENTS.md` at the repository root is the canonical automatically discovered project-instruction entry point. This `.hermes/` directory contains explicitly referenced acceptance documents; its Markdown files are not automatically loaded as rules. Do not add a competing `.hermes.md` or `HERMES.md` merely to supplement `AGENTS.md`: the installed Hermes loader gives that context type priority.

## Acceptance index

Read [Implementation and delivery contract](implementation-delivery-contract.md) for every implementation, repair, publication or deployment request. It defines working end-to-end behavior, default PR/CI/merge ownership, separately authorized release/activation, genuine gates and completion evidence. Explicit document-only or local-only scope remains binding.

| Document | Read when | Required outcome |
| --- | --- | --- |
| [Loop happy path](loop-happy-path-hard-acceptance.md) | Authoring, admission, runtime execution, retries, verification or disposition for a Doer Loop changes | Separately publish and execute the same exact immutable Loop; runtime-produced exact bytes, bounded attempts and independently verified disposition |
| [Graph happy path](graph-happy-path-hard-acceptance.md) | Graph authoring, typed inputs, multi-node execution, artifact handoff, dependency gating or aggregate disposition changes | Separately publish and execute the same exact immutable two-node Graph with verified producer-to-consumer dataflow |
| [GitHub Trigger happy path](trigger-happy-path-hard-acceptance.md) | Connector intake, visual Trigger logic, Graph Trigger-entry composition, activation bindings or event-driven invocation changes | One real GitHub label event automatically drives the exact admitted Graph to independently verified completion |

Read the relevant full document before defining acceptance or modifying affected behavior. Changes spanning multiple contracts require all applicable documents. The Trigger case builds on the Loop and Graph cases; it does not replace their independent proof. These are hard acceptance targets, not claims of installed capability or historical passing tests. Do not run live operations solely because an acceptance document describes them.

## Working and evidence rules

- Preserve the requested scope and actual independent approval gates. These documents grant no execution, credential, provisioning, publication, release or deployment authority.
- Follow repository worktree and ownership rules. Preserve dirty work, use one source-owning writer per candidate, and independently test the frozen candidate. For authorized engineering work, follow the installed `aegis-implementation-model` and `aegis-development-loop`; a template or rendered instruction is not an executor.
- Never weaken authority, output assertions, evidence bindings or retry limits to manufacture a passing case. Fix demonstrated defects; do not substitute a different Agent, immutable definition or execution intent to hide a failure.
- Report each criterion as PASS, FAIL, BLOCKED or UNAVAILABLE; mark downstream stages NOT EXERCISED when they were not reached. Distinguish an actual tested failure from missing capability, missing proof or an independent approval gate.
- Keep unit/regression proof, synthetic-runtime qualification, real-provider acceptance, source delivery and installed-workstation proof separate. Record exact source/runtime identities and readback for the tier actually exercised.
- Instructions are advisory engineering context. Runtime admission, credential custody, independently captured approvals, CI and protected-base controls enforce authority; Markdown does not replace them.

## Repository hygiene

Keep reviewed, reusable instructions and acceptance contracts versionable. Do not commit credentials, approval/session material, provider authentication, runtime homes, generated logs, dumps or machine-local execution state here. Do not blanket-ignore `.hermes/`, because that would hide its acceptance contracts. Retain operational evidence only at an explicitly authorized destination, with secret-safe contents and appropriate exclusion from version control. Existing evidence is not authorization to delete or move it.

Deployment URLs and historical observations in a test contract are target context, not universal repository defaults. Reconfirm the current target and installed capabilities before an authorized live run.

## Loading and maintenance

Keep the root routing section concise and maintain the documents together when acceptance changes. Do not silently relax a hard acceptance requirement; obtain the owner's direction for substantive contract changes.

Changing these files does not update existing workers or prove instruction adoption. Verify the effective project-context loader with a fresh process and start a fresh agent session for actual adoption. `/reload-skills` reloads skills, not evidence that repository instructions have been freshly loaded.

Reference: https://hermes-agent.nousresearch.com/docs/user-guide/features/context-files
