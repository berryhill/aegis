package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/delivery"
	"github.com/berryhill/aegis/internal/persistence/fleet"
)

// contributionDeliveryReadiness lives only inside the held private Source callback.
// It has no counter: all spending and replay identity live in root continuity.
type contributionDeliveryReadiness struct {
	live         context.Context
	productive   *productiveContributionAuthority
	instructions *contributionInstructionAuthority
}

var _ delivery.ReadinessBudget = (*contributionDeliveryReadiness)(nil)

func (b *contributionDeliveryReadiness) ReserveReadiness(ctx context.Context, i delivery.Intent, maximum uint8) error {
	if b == nil || b.productive == nil || b.productive.controller == nil || b.productive.controller.service == nil {
		return delivery.ErrDenied
	}
	return b.productive.controller.service.reserveContributionDeliveryReadiness(ctx, b, i, maximum)
}

// reserveContributionDeliveryReadiness is the owning Service's controller-only
// port. It accepts no event, counter, session label or operation ID. All authority
// is reloaded inside the same held private Source context before one canonical CAS.
func (s *Service) reserveContributionDeliveryReadiness(ctx context.Context, b *contributionDeliveryReadiness, i delivery.Intent, maximum uint8) error {
	if b == nil || b.productive == nil || b.productive.controller == nil || b.productive.controller.service != s || b.instructions == nil || b.instructions.base != b.productive || b.instructions.service != s {
		return delivery.ErrDenied
	}
	if ctx == nil || ctx != b.live || ctx.Err() != nil || b.productive == nil || b.instructions == nil || maximum == 0 || maximum > 2 || (i.Operation != delivery.CheckDelivery && i.Operation != delivery.Merge) {
		return delivery.ErrDenied
	}
	// Writing=true is intentional: productive native custody AND fresh upstream
	// instructions must both be checked before a reservation, even for HTTP reads.
	if err := b.instructions.Admit(ctx, i, true); err != nil {
		return err
	}
	a := b.productive
	history, err := s.ReadContributionDeliveryHistoryAs(ctx, a.subject, a.key)
	if err != nil {
		return err
	}
	var revision uint64
	for _, chain := range history.Histories {
		if len(chain) == 0 {
			continue
		}
		r := chain[len(chain)-1].Record
		if r.Intent == i && r.Outcome == delivery.Unknown {
			revision = r.Revision
		}
	}
	if revision == 0 {
		return delivery.ErrDenied
	}
	c, state, _, err := s.ObserveContributionContinuityAs(ctx, a.subject, a.key)
	if err != nil {
		return err
	}
	if c.Digest != i.Contract || c.RunID != i.Run || c.OwnerID != a.subject.PrincipalID || state.CandidateEvidence != a.request.CandidateEvidence || state.CancelRequested || !s.Now().Before(c.Deadline) {
		return delivery.ErrDenied
	}
	control, err := s.ReadContributionControlAs(ctx, a.subject, a.key)
	wire, _ := json.Marshal(control)
	if err != nil || control.Stop || contribution.Digest(wire) != a.control {
		return delivery.ErrDenied
	}
	// Bind the append snapshot to the freshly admitted actual controller role.
	// Counter-only changes do not invalidate private custody; role changes do.
	r, err := s.CheckContributionEffectAs(ctx, a.subject, a.session, a.key, a.request, true)
	if err != nil || r.SessionID != a.session || r.Ref() != a.request.RoleAuthority || r != state.RoleAuthorities["controller"] || r.Epoch != state.RoleEpochs["controller"] || c.Loop != a.plan.Intake.Loop {
		return delivery.ErrDenied
	}
	return s.appendContributionReadinessReservation(ctx, a.session, i, maximum, c, state)
}

// appendContributionReadinessReservation is only the post-admission accounting
// seam, not private proof. The owning port above is the sole productive caller.
func (s *Service) appendContributionReadinessReservation(ctx context.Context, session string, i delivery.Intent, maximum uint8, c contribution.Contract, state contribution.Continuity) error {
	if maximum == 0 || maximum > 2 || state.Used.InfrastructureRechecks >= uint32(maximum) || state.Used.InfrastructureRechecks >= state.Limits.InfrastructureRechecks {
		return delivery.ErrReadinessExhausted
	}
	// Every top-level call consumes the NEXT global slot, even when the exact
	// delivery intent and UNKNOWN revision have not changed. No generation gate.
	identity, _ := json.Marshal(i)
	id := fmt.Sprintf("readiness/%s/%d", strings.TrimPrefix(contribution.Digest(identity), "sha256:"), state.Used.InfrastructureRechecks+1)
	// One CAS against this exact head only. The supported service reports stale
	// heads as ErrContinuityConflict; storage errors do not prove this caller won.
	// Readback of a deterministic ID cannot distinguish our uncertain commit from
	// a concurrent winner. Deny ALL error paths, retaining any committed charge.
	// In particular, never refresh the head, increment again, or grant permission
	// from another caller's found reservation. This is not exactly-once HTTP.
	e, err := contribution.SealContinuityEvent(contribution.ContinuityEvent{SchemaVersion: contribution.ContinuitySchema, QueueItemID: c.QueueItemID, ContractDigest: c.Digest, OwnerID: c.OwnerID, SessionID: session, Sequence: state.Sequence + 1, PreviousDigest: state.Digest, Kind: "reserve", Counter: "infrastructure-recheck", OperationID: id, At: s.Now()})
	if err != nil {
		return err
	}
	_, err = s.FleetRepository.(fleet.ContributionContinuityRepository).AppendContributionContinuity(ctx, e)
	return err
}
