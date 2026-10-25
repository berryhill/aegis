# #377 effect admission seam for #378

Exact compiled API:

```go
func (s *Service) CheckContributionEffectAs(ctx context.Context, sub core.Subject,
    session, key string, in contribution.EffectAdmissionRequest,
    writing bool) (contribution.RoleAuthority, error)

// contribution.EffectAdmissionRequest:
// Contract reference.DigestRef (ID = sealed QueueItemID, Digest = contract digest)
// RunID, QueueItemID, Candidate string
// CandidateEvidence reference.DigestRef
// Role string; Epoch uint64; RoleAuthority reference.DigestRef; Scope string

func (r contribution.RoleAuthority) Ref() reference.DigestRef
```

The stable role association reference includes the retained epoch, clean session,
mandate, authority, exact sealed role binding and original immutable runtime binding.
It is independent of the current continuity head. Admission reloads that head,
current exact enabled registry/Loop revision, normal session, canonical mandate and
AuthorityCommands admission, absolute deadline, Queue projection and sticky stop.
Contract/run/candidate/evidence/role reference/epoch/scope substitutions deny.
Candidate checkpoint evidence is an observation, NOT independently accepted source.

#378 must wrap this check inside #376's lock-held pre/post
WithAcceptedContributionCandidateAs gate and repeat it around Fence/Reserve/Finish.
No sibling implementation files or imports are copied here. No effect store added.
`writing=false` grants no write and is only one prerequisite for delivery-owned
unknown-outcome reconciliation; delivery must separately prove the unknown intent.
It does not bypass expiry/revocation/cancellation/role stops.

**Native source/check quiescence is not yet joined.** `writing=true` returns
`contribution.ErrNativeQuiescenceUnavailable` after all ordinary authority checks.
There is deliberately no callback/bool/reference that can assert native quiescence.
The source owner must provide a trusted exact receipt join before this denial can
be replaced. This seam is fail-closed integration scaffolding, not working delivery.

Role epochs are retained in Continuity.RoleEpochs; admission derives the next epoch
from retained state, and stop checks exact current RoleAuthority.Epoch. A role-process
Wait/group acknowledgement remains distinct from native source/check quiescence.
A successor after that acknowledgement returns ErrNativeQuiescenceUnavailable,
without changing the old role, epoch, counters, root binding or custody. No release,
renewal or successor-success API is claimed.

Positive application regression uses ordinary PreviewSessionAs, StartSessionAs,
AuthorityCommands.AuthorityAdmission, BindQueueRuntimeAs, observation/admission and
fresh checks for three separate roles. Its Hermes executable/provisioning receipt
are explicit test fixtures, not real-provider or native-stop acceptance evidence.
