package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"

	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/persistence/fleet"
)

// Decision delivery shares the exact existing protected review/apply path. Only
// its pre-Queue custody differs: a pending engineering intent is not fabricated
// into a sealed Fleet contract or an executable Queue admission.
func (s *Service) contributionDecisionContext(ctx context.Context, sub core.Subject, key string) (contribution.Contract, contribution.Continuity, contribution.Control, error) {
	c, state, _, e := s.ObserveContributionContinuityAs(ctx, sub, key)
	if e == nil {
		control, e := s.ReadContributionControlAs(ctx, sub, key)
		return c, state, control, e
	}
	if !errors.Is(e, fleet.ErrNotFound) {
		return c, state, contribution.Control{}, e
	}
	status, e := s.ReadContributionEngineeringAs(ctx, sub, key)
	if e != nil {
		return c, state, contribution.Control{}, e
	}
	var intent contributionEngineeringIntent
	if e = s.Store.Load("contribution-engineering-intents", status.IntentID, &intent); e != nil {
		return c, state, contribution.Control{}, e
	}
	c = intent.Contract
	c.Digest = core.Digest(intent)
	state = contribution.Continuity{Sequence: 1, Stage: "engineering-preparation", Candidate: c.RunID, CancelRequested: status.Control.Stop}
	return c, state, status.Control, nil
}
func (s *Service) contributionDecisionRepository(c contribution.Contract) ContributionDecisionRepository {
	var intent contributionEngineeringIntent
	id := engineeringIntentID(c.OwnerID, c.Key)
	if s.Store.Load("contribution-engineering-intents", id, &intent) == nil && c.Digest == core.Digest(intent) {
		return engineeringDecisionRepository{s, id, intent}
	}
	return s.FleetRepository.(ContributionDecisionRepository)
}

type engineeringDecisionRepository struct {
	s      *Service
	id     string
	intent contributionEngineeringIntent
}

func (r engineeringDecisionRepository) load(root, id string) (contribution.Decision, error) {
	if root != r.intent.Contract.QueueItemID {
		return contribution.Decision{}, ErrDenied
	}
	var found contribution.Decision
	for n := uint64(1); n <= 2048; n++ {
		var event ContributionEngineeringStatus
		e := r.s.Store.Load("contribution-engineering-history", fmt.Sprintf("%s-%06d", r.id, n), &event)
		if errors.Is(e, os.ErrNotExist) {
			if found.ID == "" {
				return found, fleet.ErrNotFound
			}
			return found, nil
		}
		if e != nil {
			return found, e
		}
		if event.Decision != nil && event.Decision.ID == id {
			found = *event.Decision
		}
	}
	return found, ErrDenied
}
func (r engineeringDecisionRepository) GetContributionDecision(ctx context.Context, root, id string) (contribution.Decision, error) {
	r.s.contributionEngineeringMu.Lock()
	defer r.s.contributionEngineeringMu.Unlock()
	if _, e := r.s.engineeringStatus(r.id, r.intent); e != nil {
		return contribution.Decision{}, e
	}
	return r.load(root, id)
}
func (r engineeringDecisionRepository) ReserveContributionDecision(ctx context.Context, d contribution.Decision) error {
	s := r.s
	s.contributionEngineeringMu.Lock()
	defer s.contributionEngineeringMu.Unlock()
	head, e := s.engineeringStatus(r.id, r.intent)
	if e != nil {
		return e
	}
	_, e = r.load(d.QueueItemID, d.ID)
	if !errors.Is(e, fleet.ErrNotFound) {
		return ErrConflict
	}
	role, ok := contribution.SealedRole(r.intent.Contract, d.Role)
	if !ok || d.Role != "builder" || role != d.Binding || d.ContractDigest != core.Digest(r.intent) || d.Key != r.intent.Contract.Key || d.OwnerID != r.intent.Contract.OwnerID || head.Control.Stop || head.Control.Digest != d.ControlDigest || head.DecisionDeliveries >= 8 || d.Status != "requested" || d.Stage != "engineering-preparation" || d.Candidate != r.intent.Contract.RunID || d.PlanID == "" || d.PlanDigest == "" || !s.Now().Before(d.ExpiresAt) || d.ExpiresAt.After(r.intent.Contract.Deadline) {
		return ErrDenied
	}
	next := head
	next.DecisionDeliveries++
	next.Decision = &d
	_, e = s.appendEngineeringStatus(head, next)
	return e
}
func (r engineeringDecisionRepository) UpdateContributionDecision(ctx context.Context, before, after contribution.Decision) error {
	s := r.s
	s.contributionEngineeringMu.Lock()
	defer s.contributionEngineeringMu.Unlock()
	head, e := s.engineeringStatus(r.id, r.intent)
	if e != nil {
		return e
	}
	current, e := r.load(before.QueueItemID, before.ID)
	if e != nil {
		return e
	}
	immutable := after
	immutable.Status = before.Status
	immutable.ApprovalID = before.ApprovalID
	immutable.ReceiptID = before.ReceiptID
	if !reflect.DeepEqual(current, before) || !reflect.DeepEqual(immutable, before) || !contribution.DecisionTransition(before.Status, after.Status) || (before.ApprovalID != "" && before.ApprovalID != after.ApprovalID) || (before.ReceiptID != "" && before.ReceiptID != after.ReceiptID) {
		return ErrConflict
	}
	next := head
	next.Decision = &after
	_, e = s.appendEngineeringStatus(head, next)
	return e
}
