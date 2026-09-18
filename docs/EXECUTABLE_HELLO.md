# Executable v2 hello

This fixed-output demonstration is not general implementation verification. The expected artifact is UTF-8 `hello` with **no newline**, media type `text/plain`. The builder computes the real SHA-256 and binds the existing artifact verifier policy before execution. It never creates evidence or receipts.

## Local authoring

Prepare `hello-input.json` with an owner-selected existing `agent_id`, `loop_id`, positive `revision`, stable `idempotency_key`, and (for a successor only) the exact prior Loop `previous_digest`. The builder preserves the predecessor in both revision and publication fields.

```sh
aegis loops hello hello-input.json --output hello-publication.json
```

The destination must not exist. This canonical v2 draft has one action, one terminal and matching required evidence, but no publication, activation or execution authority.

## Authenticated publication and Graph submission

OWNER_CONFIG and CONSOLE_URL select the existing controller. AGENT, LOOP, GRAPH and REVISION below are actual owner-selected identifiers from readback, not invented authority.

```sh
aegis --config OWNER_CONFIG --target CONSOLE_URL agents list
aegis --config OWNER_CONFIG --target CONSOLE_URL agents show AGENT REVISION
aegis --config OWNER_CONFIG --target CONSOLE_URL loops validate hello-publication.json
aegis --config OWNER_CONFIG --target CONSOLE_URL loops publish hello-publication.json
aegis --config OWNER_CONFIG --target CONSOLE_URL loops show LOOP REVISION
```

After explicit activation approval, prepare a lifecycle file with `agent_id`, exact `loop` reference, fresh `event_id` and actual lifecycle predecessor digest. Run `loops activate LOOP lifecycle.json` with the same owning-service flags and read back the exact Loop.

Follow the shipped Graph authoring contract to prepare a single-node Graph binding these exact Agent and Loop revision/digests. Expose `task` and `acceptance_criteria` string inputs. Request exactly `hello` without newline; prompt instructions are not evidence. Use the same owning-service flags for `graphs publish graph.json`, `graphs show GRAPH REVISION`, and `graphs submit submission.json`. Preserve the actual returned Queue ID. Workspace submission begins awaiting-runtime; it does not execute.

## Runtime preparation is separate

The authenticated operator must approve the actual charter/model/runtime route. All commands below use the same owning daemon, protected Unix socket and kernel peer identity; no command opens a second store. IDs and revisions are placeholders to replace with exact returned values. Do not run the sequence as an automatic approval script.

If a successor charter is needed, import the reviewed JSON/YAML source unchanged, read its exact revision, then separately approve the Agent successor using strict `expected` Agent and `charter` references in `approval.json`:

```sh
aegis --config OWNER_CONFIG --target CONSOLE_URL charter validate charter.yaml
aegis --config OWNER_CONFIG --target CONSOLE_URL charter import charter.yaml
aegis --config OWNER_CONFIG --target CONSOLE_URL charter show AGENT CHARTER_REVISION
aegis --config OWNER_CONFIG --target CONSOLE_URL agents approve-charter AGENT approval.json
aegis --config OWNER_CONFIG --target CONSOLE_URL agents show AGENT AGENT_REVISION
```

Bind any new Graph to this exact successor Agent **before submission** (perform this preparation before the Graph submission section when a successor is required). Charter approval is not provisioning approval. Preview the exact charter revision and inspect the returned plan ID/digest and deterministic changes:

```sh
aegis --config OWNER_CONFIG --target CONSOLE_URL plan preview AGENT --revision CHARTER_REVISION --environment local
aegis --config OWNER_CONFIG --target CONSOLE_URL plan show PLAN_ID
aegis --config OWNER_CONFIG --target CONSOLE_URL approval request PLAN_ID --ttl 5m
aegis --config OWNER_CONFIG --target CONSOLE_URL approval show APPROVAL_ID
```

Stop for explicit operator review of the exact plan. Only after approval, execute the individual decision; `approval reject APPROVAL_ID` is the supported negative decision. Apply only that approved plan, inspect the returned provisioning receipt, then preview a clean session:

```sh
aegis --config OWNER_CONFIG --target CONSOLE_URL approval approve APPROVAL_ID
aegis --config OWNER_CONFIG --target CONSOLE_URL approval show APPROVAL_ID
aegis --config OWNER_CONFIG --target CONSOLE_URL provision PLAN_ID APPROVAL_ID
aegis --config OWNER_CONFIG --target CONSOLE_URL session preview AGENT --revision CHARTER_REVISION --stanza STANZA --environment local
```

Review the returned mandate and decision. The stanza flag is a request, not authentication. Only with explicit activation authorization, use that returned mandate ID:

```sh
aegis --config OWNER_CONFIG --target CONSOLE_URL session start MANDATE_ID
aegis --config OWNER_CONFIG --target CONSOLE_URL session show SESSION_ID
aegis --config OWNER_CONFIG --target CONSOLE_URL session authority SESSION_ID
```

Use the returned authority reference unchanged. Never fabricate session or authority IDs. Provision/session start allow a bounded six-minute transport wait; cancellation or a lost/malformed mutation response is an unknown outcome, not permission to retry. Read exact plan/approval/session records (and `session list` to reconcile a lost start response); unresolved provisioning requires owning-service receipt reconciliation, not another apply.

Only with actual controller-issued runtime authority, prepare `bind.json` containing `agent_id`, `authority`, `queue_item_id`, `binding_id`, and `transition_id`. Prepare `process.json` using the installed Queue contract: exact authority/item, stable operation identities, worker and bounded lease.

```sh
aegis --config OWNER_CONFIG --target CONSOLE_URL queue bind-runtime bind.json
aegis --config OWNER_CONFIG --target CONSOLE_URL queue show ITEM
aegis --config OWNER_CONFIG --target CONSOLE_URL queue process process.json
aegis --config OWNER_CONFIG --target CONSOLE_URL queue show ITEM
```

Unknown execution outcome requires exact readback before retry. Success requires actual content-addressed artifact, passed independently reloaded receipts, disposition and durable Queue history. Any newline or changed response fails the precommitted output policy.

Local inference rechecks the configured exact model digest at launch: **drift detection, not atomic model pinning**. Disposable homes and process-group cleanup are bounded process custody, **not a host sandbox**. Synthetic tests do not prove live provider/model acceptance; live Javi access was unavailable. Graph lifecycle application, automatic binding, general scheduling and multi-node processing remain unsupported.
