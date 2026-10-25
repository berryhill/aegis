# Contribution continuity: bounded retained-control slice (#377)

## Implemented in this slice

`app.Service.ObserveContributionContinuityAs(ctx, subject, key)` reloads the
same owner's sealed intake, Queue identity, retained counters and complete ordered
history. It requires authenticated principal identity, does not renew anything,
and does not require the original conversation to still exist.

`app.Service.RecordContributionContinuityAs(ctx, subject, session, key, input)`
requires **fresh** configured principal authentication, the exact contract digest,
expected sequence/digest, original session (except cancellation), and exact latest
enabled role/charter/runtime definitions. New-session observation is intentionally
not new-session execution. Substituted/disabled/successor participants cannot use
this path. This is an internal application API, not a new public endpoint.

`fleet.ContributionContinuityRepository` is implemented on the **existing** fleet
Badger `Store`. Every append, head and audit fact shares its transaction. Reads
reconstruct the entire digest-linked history and reject missing/rolled-back heads,
missing/tampered events and unexpected records. No RuntimeBinding is modified.

The once-sealed limits are candidate <= original intake budget (currently <=2),
Queue attempts 1, runtime turns 128, infrastructure rechecks 2, decision deliveries
8, with the original absolute deadline. Counters are cumulative **reservations**,
not proof an effect happened. Appending a checkpoint retains its evidence reference,
not a passing verifier verdict. Every canonical stage is an allowed observation;
the slice does not implement or claim to execute the whole stage graph.

Decision states are requested, delivered, unanswered, denied, expired,
dialog-cancelled, unavailable, timeout and unknown. History preserves whether
request delivery occurred. Unavailable cannot turn into a pending human decision;
a terminal outcome cannot be silently redispatched under the same ID. A requested
record consumes a separately reserved delivery. There is deliberately **no**
caller-authored approved state or approval receipt. These are transport observations,
not protected-authority evidence. Real adapter delivery/readback is still required.

Cancellation from a fresh same-owner session atomically appends cancellation intent
and the **existing** sticky `contribution.Control.Stop` fence consumed by the
execution owner. It does not mark Queue cancelled, assert runtime stop, mint a stop
acknowledgement or release source/publication custody. It works before initialization
and after the absolute execution deadline. HTTP cancellation alone writes nothing.
Cancellation is not implemented as `StopContributionInSessionAs`, whose existing
Queue transition does not provide the required process-Wait proof.

## Required central consumer integration (not implemented here)

Only new contribution-domain, dedicated app and fleet files were authorized;
central runtime/Queue/delivery wiring is owned concurrently. No sibling worktree
source was copied, edited, run or represented as merged.

1. **Orchestration lifecycle:** initialize once at the existing invocation; reserve
   candidate/Queue/runtime-turn/infrastructure/decision-delivery counters before
   the corresponding existing owning-service effect. Reload after an uncertain
   response before retry; never create another root or reset a counter. Current
   public app append deliberately accepts only the original session and does not
   admit a worker role. The production worker must use a narrowly scoped service
   boundary that independently admits its exact role authority, not impersonate
   this principal API or accept a boolean callback. Stage checkpoints must reference
   actual controller-validated evidence and retain candidate/Queue/run identity.
2. **Transport lifetime:** the controller must retain the existing service-owned
   bounded execution after HTTP disconnect. Do not use request `ctx` as the run
   lifetime. Retain admitted hard deadlines; do not extend grants with console TTL.
3. **Role continuation:** implemented dedicated app APIs now attach a normally
   authenticated observation, admit each separately launched role with actual
   AuthorityCommands admission, and repeat fresh admission through
   `CheckContributionRoleAs`. The original RuntimeBinding stays immutable.
   RoleAuthority includes a retained epoch and stable `Ref()` independent of
   checkpoint head. Successors remain denied: role-process acknowledgement alone
   yields `ErrNativeQuiescenceUnavailable`, never release or replacement success.
4. **Stop proof:** dedicated Hermes `StopContributionRole` produces a private
   sealed role-process proof only after actual `command.Wait` and process-group
   quiescence. `StopContributionRoleAs` first persists a fence then persists the
   exact proof binding/boot/process-start acknowledgement. Timeout/restart leaves
   requested custody. This proof explicitly does NOT cover separately owned native
   source/check effects; the trusted native join is still absent. Session ended,
   PID absence/reuse, lease expiry or `Close()==nil` cannot substitute for that join.
   Runtime authority expiry still needs integrated native-child quiescence.
5. **Delivery:** #378 `Authority.Admit(ctx, intent, writing)` must reload cancellation,
   current epoch/role admission/deadline before each effect. `Repository.Reserve`,
   `Finish`, `Fence` remain the sole delivery intent/outcome owner. `Source.WithAccepted`
   must preserve accepted candidate binding. Unknown effects quarantine conflicting
   work until authoritative same-target reconciliation; confirmed merge is immutable.
   Do not add a second delivery log here or turn a continuity checkpoint into a
   successful GitHub effect. Cancellation fence and reservation need a shared
   serializable owner boundary, not a check-then-unfenced remote write.
6. **Protected decision delivery:** qualify the real protected adapter before
   claiming executing. Record actual request/delivery/readback and independent
   approval references. An unavailable adapter yields no pending human request and
   no unchanged redispatch; no account/channel switching, fake terminal or agent
   operation of confirmation controls. Observation records cannot authorize effects.
7. **Cancellation:** consumers must read the sticky existing control at every
   consequential boundary, request bounded child shutdown and retain custody until
   durable exact stop proof. The current source cancellation API records intent,
   not completed cancellation. `#379` must independently join these boundaries.

## Evidence tier and launch impact

Tests exercise actual application admission and Badger persistence/reopen,
controlled-clock deadline behavior, concurrent CAS, failure rollback, corrupt history,
original-session/new-owner denial and transport-state negatives. Transport subjects
and source issue reader are controlled fixtures, not real password/native approval
or managed-provider proof. No live service, release, installed target, protected
human decision, runtime stop or GitHub effect was exercised.

No CLI/config/deployment/runtime-provider changes, release archive, recording,
checksum or contributor issue is supplied by this internal first slice. The canonical
contribution contract, README/security/threat-model/architecture/quickstart/demo and
release claims must not be promoted to complete continuity on this basis. Central
integration plus independent acceptance is still required; this is not whole #377
or #379 acceptance.
