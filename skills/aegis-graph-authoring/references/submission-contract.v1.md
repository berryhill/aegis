# Workspace submission contract v1

Applies to the bundle's exact source revision and an Aegis executable whose
`aegis graphs submit --help` advertises `--check`. Older executables without
that flag cannot run this preflight; report the missing capability, do not
invent an alternate adapter. Installing a skill does not upgrade Aegis.

`workspace-submit.v1.json` is a complete strict request-shape example for a
Graph with no inputs. Its Agent, Graph, digest and operation identities are
synthetic, not authenticated records and not an executable admission fixture.
Never submit it unchanged. It contains no credential, mandate or workspace
object. Use exact authenticated Graph readback and the intended registered
Agent selector; supply the actual Graph's normalized typed inputs. No source
checkout or repository-relative documentation is needed to check its shape.

From this installed skill directory:

```
aegis graphs submit --check references/workspace-submit.v1.json
```

Expected complete check response (JSON object order is not significant):

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
using `aegis graphs submit FILE`. Normal submission retains authoritative
service admission and durable rejection; the check is optional and does not
replace that path. On `control_plane_online`, stop the direct-store path and
report the unavailable product adapter; never extract tokens, stop a daemon,
create another writer or improvise an authenticated HTTP client.

An accepted response is `{accepted: ..., created: boolean}`; a rejected
response is `{rejection: ..., created: boolean}`. These two descriptions are
projections, not complete wire examples. Workspace acceptance waits in
`awaiting_runtime` for controller binding and is not execution success. After
unknown outcomes, reconcile authenticated submission history and exact Queue
readback before deciding whether replay is safe. This example does not prove
that readback or installed-agent behavior; unresolved public readback failures
remain product prerequisites, not reasons to repeat mutations.
