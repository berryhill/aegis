# Known-boundary owning-service recovery

Explicit authenticated same-owner Resume now has a narrowly scoped recovery
path through the existing QueueWorker and kernel workspace flock. It reloads the
canonical workspace owner and immutable implementation/policy/attempt, revalidates
source/Git/candidate inventory, and requires complete registered native command
coverage with every effect completed. Reserved, in-flight, unknown, missing
coverage and publication-stage effects remain held. It changes neither the Queue
claim nor its attempt or original runtime binding.

New role admissions retain adapter-owned PID, start identity, host boot ID and UID.
Legacy admission records are never backfilled. After service restart a fresh
adapter may stop only the exact recorded boot/start/UID-bound process through a
PIDFD. Bounded TERM/KILL, pinned PIDFD exit readiness and no executing members of
the bound process group yield `recovered-pidfd-exit.v1`, not parent Wait/reaping.
Zombie processes cannot execute; the recovered proof does not claim they were
reaped. Ordinary stops retain `owned-wait.v1`. Missing leaders, mismatched start,
boot or UID, and still-owned adapter sessions deny. Host boot changes deliberately
remain denied rather than inferring reconciliation from boot or PID absence.

The controller appends a durable exact-role stop acknowledgement before closing
an inventoried native boundary or permitting a compatible fresh role epoch.
Scope, permissions, authority provenance, counters and candidate lineage remain
unchanged. Generic session/Doer stop behavior is unchanged. Advisory source custody
still excludes only cooperating writers; this is not a host sandbox.

## Verification boundary

`TestContributionManagerHardRestartFrozenCandidateResume` kills the actual owning
service subprocess at a durably frozen candidate, then starts a fresh process
against the same state. Authenticated authority and fleet physical recovery verify
preserved working copies before activation. Recovered PIDFD stop acknowledgements
exclude old roles before fresh role admission; Queue, claim, attempt, worktree,
frozen candidate and consumed budgets remain unchanged. Its RESERVED-before-start
fault case preserves the exact unresolved intent and denies duplicate execution.
This is controlled Hermes-protocol qualification with real Go, OS processes,
Badger recovery and source custody, not real-provider or installed/live proof.

The separate `TestContributionRecoveredAdapterCrash` and
`TestContributionRecoveredPIDFD` tests exercise primitive process recovery and
denials. `TestContributionRecoveryUnknownEffectsRemainHeld` verifies that unknown
native outcomes deny before stopping a role or appending replacement authority.
Primitive tests alone are not the full owning-service journey.

No release, installed or live-provider qualification is implied. No new daemon,
Queue/store, architecture allowance, provider configuration or publication path
was added. Existing launch assets retain their prior acceptance tier; this scoped
recovery note does not upgrade whole-contribution or #378/#379 delivery claims.
