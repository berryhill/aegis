# Workspace submission contract v1

Applies to the bundle's exact source revision and an Aegis executable whose
`aegis graphs submit --help` advertises `--check`. Older executables without
that flag cannot run this preflight; report the missing capability, do not
invent an alternate adapter. Installing a skill does not upgrade Aegis.

`workspace-submit.v1.json` is a complete strict request-shape example for a
Graph matching the bundled hello publication interface. Its Agent, Graph, digest
and operation identities are
synthetic, not authenticated records and not an executable admission fixture.
Never submit it unchanged. It contains no credential, mandate or workspace
object. Use exact authenticated Graph readback and the intended registered
Agent selector; supply the actual Graph's normalized typed inputs. No source
checkout or repository-relative documentation is needed to check its shape.

From this installed skill directory:

```
aegis graphs submit --check references/workspace-submit.v1.json
```

Expected complete check response, also supplied as
[`submission-check-result.v1.json`](submission-check-result.v1.json) and compared
with the actual public command (JSON object order is not significant):

```json
{"status":"valid","evidence_class":"request_shape_validation","authority_admission":"not_run","submitted":false}
```

The check strictly decodes the same application request as submission. It
checks required operation identities (including `transition_id`), exact Graph
reference shape, the attempt bound, and exclusive workspace/runtime selectors.
Unknown fields such as `rejection_idempotency_key` and trailing JSON deny.
The command opens no service/store and performs no authentication, reference
resolution, authority admission, input normalization, rejection recording or
mutation. `valid` does not mean the Graph exists or the operation is permitted.
Do not use a successful check as approval or completion evidence.

Prepare a reviewed request with:

- `agent_id`: intended latest enabled Agent, not an identity proof.
- `graph`: schema `aegis.reference.revision.v1`, exact `id`, positive
  `revision`, and canonical `sha256:` digest from authenticated readback.
- `inputs`: one object per supplied port, with `port_id`, `type` and JSON
  `value`; match exact Graph ports. For a no-input Graph use `[]`.
- `submission_id`, `idempotency_key`, `snapshot_id`, `queue_item_id`,
  `graph_run_id`, `transition_id`, `rejection_id`: stable identities for one
  intended submission. Do not invent a second rejection idempotency key.
- `max_attempts`: positive and bounded by installed Aegis.

Omit `authority` and `workspace`: Aegis derives workspace authority only after
fresh authentication. For the runtime-authority path instead omit `agent_id`
and supply an exact authenticated `authority` digest reference with schema
`aegis.reference.digest.v1`, `id`, and `digest`. Preflight never issues it.

After checking the reviewed request, submit only with explicit authorization
using `aegis graphs submit FILE` when the service is offline. Normal submission
retains authoritative service admission and durable rejection; the check is
optional and does not replace that path. With a running gateway use the shipped
typed owning-service adapter:

```text
aegis --config OWNER_CONFIG --target CONSOLE_URL graphs submit REVIEWED_FILE
aegis --config OWNER_CONFIG --target CONSOLE_URL queue preparations
aegis --config OWNER_CONFIG --target CONSOLE_URL queue show RETURNED_ITEM
```

`OWNER_CONFIG` and `CONSOLE_URL` are operator-supplied existing-instance selectors,
not credentials. Omit `--target` for the offline `--check` command: combining them
is rejected. If installed help lacks an operation, report the missing adapter;
`control_plane_online` is not permission to stop the daemon, extract tokens,
open another writer or improvise an authenticated HTTP client. The public
submission endpoint is `POST /v1/queue`; API availability is not an invitation to
construct a transport client. The product-owned adapter retains authentication
inside Aegis.

An accepted response is `{accepted: ..., created: boolean}`; a rejected
response is `{rejection: ..., created: boolean}`. These two descriptions are
projections, not complete wire examples. Current workspace acceptance waits in
`preparation-pending` for fresh controller binding and is not execution success;
legacy `awaiting-runtime` remains readable. Pending work is discovered with
`queue preparations`, not the execution-only `queue list`. Workspace publication
and submission have no runtime-session, provisioning or credential prerequisite;
workspace ownership never supplies runtime-effect or credential authority.
Binding is a separate controller-owned action documented by the Queue skill,
not another ceremony for an already-authorized high-level Loop Run.

After unknown outcomes, reconcile authenticated submission history and exact
Queue readback before deciding whether replay is safe. `graphs list` includes
accepted Graph snapshots, and the public API has `GET /v1/submissions` for both
accepted/rejected history, but there is no standalone CLI submission-history
verb. If the required outcome cannot be read through an available adapter, keep
it unknown and request operator recovery; never rotate IDs or invent a history
command. Correct an unsubmitted malformed request before mutation; a durable
rejection or uncertain mutation must not be treated as an unsubmitted draft.

The separate `workspace-publish.v1.json` is a complete synthetic publication
candidate matching the Loop skill's hello-builder interface. It declares typed
`task` and `acceptance_criteria` inputs and a `report` output. Replace its Agent
and active Loop revision references using authenticated readback. The matching
`workspace-submit.v1.json` supplies both normalized string inputs. Replace its
Graph reference with the exact published readback and keep one intended set of
submission identities; never submit either fixture unchanged. No installed Graph compose/validate command
is implied. Publication canonicalizes the candidate and returns `revision`,
`validation`, and `decision` (an envelope description, not a literal response).

`TestPortableBundledAuthoringContracts` checks the installed-archive publication
input using the actual strict command decoder and Graph validator, compares its
ports with the real public hello builder, and compares the complete check
response. This is executable schema/builder/packaging evidence only. Existing
`TestWorkspaceSkillSubmissionPublicReadback` separately exercises real-store
public service admission, pending readback, same-intent replay, changed-payload
conflict and corrupt participant denial. Neither proves installed-agent behavior
or live-provider execution; unresolved readback remains a gate, not a reason to
repeat mutations.
