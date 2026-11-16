# Registration → provider connection → exact successor (source candidate)

This adds a supported file-free composition, **not live-provider readiness**.
Registration, operational source/account consent, exact charter approval and
runtime execution remain different decisions. The installed Codex qualification
gate described in [the gateway boundary report](native-codex-gateway-boundary.md)
remains unqualified; the 0.21.3/fingerprint gate is unchanged. Antigravity stays represented
by `configured-provider.v1`, not an executable adapter.

## Supported principal journey

Ordinary explicit default-profile registration now **offers** model-read consent
in the actual authoritative manager rendering. It still performs only metadata
inspection until the operator chooses `/agents import hermes default with-models`.
That review displays the configured default and allowed lanes, then returns its
exact registration confirmation. Registration confirmation returns the exact
`/agents providers <actual-agent-id> setup default` input, not a placeholder recipe.

`setup default [CREDENTIAL_ID]` is an explicit source inspection request for the
already-approved immutable default. It composes the existing owning services:

- A single eligible native grant produces a source/account review and exact
  `connect` input, **never an automatic connection**.
- Multiple grants produce eligible fingerprint/entry choices with exact manager
  and CLI inputs. The operator chooses; no default account is inferred.
- `connect` returns verified operational readback and prepares (does not approve)
  the exact default-lane successor. The next input already contains its review ID
  and canonical charter digest. No separate generic `promote` handoff is needed.
- Exact `approve` returns the immutable successor and readback action. The
  original configured default is preserved. This proves neither runtime
  qualification nor execution.

The owning CLI returns `message`, `next`, `next_cli`, and any exact `choices`;
`next_cli` retains the owning configuration and target. Native manager rendering
prints the authoritative human-readable decisions, not nested Go maps. No files,
new onboarding store, extra confirmation, grant, profile edit or model fallback
are introduced. If successor preparation fails after connection, the response
explicitly retains connected readback and offers a fresh setup review, never
replay of consumed connection consent. An existing connection to a different
source/entry conflicts rather than being silently replaced.

An Antigravity default returns `antigravity_native_transport_unavailable` with
`qualification: unavailable`, preserved model and no login inspection. It is not
converted to Gemini billing or replaced with a Codex fallback. A metadata-only
Agent routes to explicit `models` consent instead of reading config implicitly.

Manual advanced commands remain supported:

```text
/agents providers AGENT status
/agents providers AGENT candidates default
/agents providers AGENT review default CREDENTIAL_ID
/agents providers AGENT connect CONNECTION_REVIEW_ID
/agents providers AGENT promote openai-codex EXACT_APPROVED_MODEL
/agents providers AGENT approve SUCCESSOR_REVIEW_ID EXACT_CHARTER_DIGEST
/agents show AGENT REVISION
```

Values above are placeholders, not approvals. `default` explicitly selects the
configured principal's canonical `.hermes` source; it is never fallback. An exact
absolute native profile home accepted by the existing connection service is also
supported. Candidates return only opaque IDs, account fingerprints and closed
eligibility. Choose an entry only after owner review. Omit `CREDENTIAL_ID` only
when the existing unique-grant rule permits it; ambiguity denies. No label,
priority, model inference or registration digest selects an account.

The same `providers AGENT ACTION [VALUES...]` arguments are available through:

```text
aegis --config OWNER_CONFIG --target CONSOLE_URL agents providers AGENT status
```

Use that supported owning-service CLI when the manager cannot carry fresh human
`local-os` / `linux-so-peercred` authority. The service never synthesizes it from
an agent, browser password session, stale manager session or prompt. CLI requests
use the existing Unix transport and never open a second store. The protected API
is `POST /v1/agents/:agent/providers`, with closed `action` and only that action's
fields (`profile_home`, `credential_id`, `provider`, `model`, `review_id`,
`charter_digest`). Duplicate/unknown keys and extraneous nonempty fields deny.

`review`/`connect` compose the original ControllerCodex services. The connection
review pins the exact current Agent reference in this surface in addition to the
native service's independent subject/config/source/account/entry commitments.
Connection confirmation grants **no charter authority**. `promote` then creates a
separate five-minute successor review containing the exact current Agent,
canonical proposed charter/digest and exact existing connection metadata.
`approve` requires that review ID and its **charter digest**, not the registration
digest or connection-review ID. It rechecks fresh subject, current Agent, config
bytes and exact source/account/entry. After source validation, fresh admission is
repeated at canonical-charter publication and again at the successor transaction
boundary. Exact post-write readback is returned.

Only the explicit provider/model pair is promoted. The default's authentication
metadata changes only when that pair is the default; the default provider/model,
all other lanes, ownership, runtime, lifecycle, source, capabilities and policies
remain unchanged. No credentials are granted to the Agent. Existing approved
Loop/Graph references are not rewritten: new definitions must deliberately use
the returned exact Agent revision. Their default and explicit lanes resolve from
that immutable charter, not a later mutable profile.

An existing connection can be inspected with `status` and explicitly reviewed for
a selected lane with `promote`; this does not replace its source/account. Explicit
connection replacement remains the existing `controller-codex preview --replace`
workflow, not an automatic repair or a new account-selection rule.

## Historical metadata-only registrations

Do not try to re-register them with `with-models` or create a replacement identity.
Use `/agents providers AGENT models`, review the bounded current configuration
snapshot and exact successor, then use its generated `approve` command. This is
explicit opt-in migration by immutable successor; the initial metadata-only
charter, registration digest, source identity and lifecycle history are retained.
This operation is limited to the original metadata-only charter, not arbitrary
later charters. It snapshots selections as **non-executable** markers and connects
nothing. Ordinary legacy import remains metadata-only.

## Drift, concurrency and interrupted results

- Review caches are process-local, bounded and expire in five minutes. Restart
  invalidates unconsumed reviews; obtain a new review, not an invented approval.
- Foreign subjects, stale authentication, stale Agent/charter, config/account/entry
  drift, unsupported lanes and changed unconsumed model-source fingerprints deny.
- Concurrent composition operations serialize. Supported Agent lifecycle and
  generic charter-successor mutations share an effect fence, acquired only after
  native validation returns. The exact Agent pin is checked under that fence;
  the native review carries it even when applied through the direct connection
  endpoint. Lock order is composition review → native connection → Agent effect
  → stores; native validation does not hold the Agent effect lock.
- Connect and all replacement paths recheck cancellation, subject freshness and
  review expiry after staging/fsync, immediately before atomic no-replace
  publication. Charter publication similarly rechecks after staging/fsync, and
  the Fleet transaction rechecks before committing the successor and its audit.
  A denied staged file is removed without publishing the binding/charter.
  These are admission boundaries, not a wall-clock guarantee about syscall or
  commit completion: scheduling and kernel/storage I/O may finish after admission
  expires. They are not filesystem CAS against a malicious same-UID writer or a
  cross-store atomic transaction.
- Native connection reviews remain single use. After an uncertain connection,
  use `status` / `controller-codex show` before another action. An audit/readback
  failure can follow the durable connection; do not claim rollback or reconnect.
- A legitimately admitted charter-first interruption may leave one exact
  immutable orphan. Only the same review that admitted that publication can
  recover it, with fresh subject and still-unexpired consent, exact bytes and
  original expected Agent. An identical pre-existing charter not admitted by
  that review is not recovery evidence. Expiry at successor commit leaves the
  already-admitted charter but no Agent append; it does not renew the review.
- Within the review lifetime, a consumed successor approval returns exact stored
  history without re-reading the mutable profile/login. Current Agent history
  remains intact. After restart/expiry, use exact Agent/charter readback; the
  existing generic exact-successor service remains available for independent
  reconciliation. No durable approval ticket or background orchestrator is added.

`executable: false` and `qualification: not_checked` deliberately mean this setup
surface proves no executable admission. Status reads operational binding metadata
without probing the login; runtime resolution independently revalidates account,
transport, authority and expiry. `configured-provider.v1` always denies execution.
Connected metadata is never substituted for qualified inference or verified task
output. No production profile, native auth, installed runtime or daemon was edited.

## Synthetic acceptance and remaining gates

Focused ordinary/race tests cover fresh registration, connection/replacement,
exact successors, migration, single-use reviews, source-free historical replay,
admitted-orphan recovery and durable absence after validation-time expiry,
cancellation or Agent drift. Additional tests deny after file staging and inside
the successor transaction, and overlap supported lifecycle/successor changes with
native validation. Native metadata success uses a private startup-only controlled
fixture, **not live or native-parser qualification**.

The earlier missing shared-port-pool report is historical: the parent restored
the prescribed pool under explicit operator authorization. Complete production
`Serve` router/middleware journeys now exercise real Unix peer authentication,
native-manager registration and the owning CLI, using a dynamic shared 8450–8469
claim held through server shutdown. External test-only overlays additionally
inject native validation failures through the existing metadata seam and prove
durable absence through full Serve. The unchanged real adapter still denies an
unqualified native runtime. Standalone local-manager `local-os` issuance is not
Unix-peer authentication and still denies; no subject issuer is rewritten.

Installed-target acceptance, actual Codex inference and complete real Loop/Graph
evidence acceptance remain separate, unpassed gates. Ordinary import is still
metadata-only until explicit model-read consent; its rendered continuation now
joins the guided model/source/account/exact-charter decisions. Native transport
qualification, not that guided setup, remains the execution gate.

Use Makefile-pinned Go with `GOMAXPROCS=2 GOFLAGS=-p=1 GOMEMLIMIT=512MiB` and private
task scratch. Test commands and sanitized outputs are retained under ignored
`.aegis-e2e/native-profile-connection-resume/`, the immutable independent audit in
`native-profile-connection-independent/`, and new `native-profile-consent-repair/`
evidence. No full local CI matrix is claimed.

## Launch-asset impact

README, CHANGELOG, SECURITY, threat model, architecture prose, slash/service
inventory, quickstart pointer and both source product skills are updated. Existing
architecture diagrams still show the same service → adapter boundaries; no daemon,
store or authority engine was added. LICENSE, CONTRIBUTING and CODE_OF_CONDUCT are
unchanged. The existing missing private reporting destination remains a launch
blocker. No-key demo and historical terminal recording are unchanged and do not
prove this provider journey. Release scripts/checksum handling and local contributor
backlog remain applicable; no release, checksum, issue or publication is fabricated.
Installed skills and all prior frozen candidates are untouched.
