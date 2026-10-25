package app

import (
	"context"
	"fmt"
	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	"github.com/berryhill/aegis/internal/registry"
	"reflect"
	"time"
)

type ContributionDecisionRepository interface {
	GetContributionDecision(context.Context, string, string) (contribution.Decision, error)
	ReserveContributionDecision(context.Context, contribution.Decision) error
	UpdateContributionDecision(context.Context, contribution.Decision, contribution.Decision) error
}
type ContributionDecisionInput struct {
	Key  string `json:"key"`
	ID   string `json:"id"`
	Role string `json:"role"`
}
type ContributionDecisionReview struct {
	Intent  contribution.Decision `json:"intent"`
	Charter core.CanonicalCharter `json:"charter"`
	Plan    core.Plan             `json:"plan"`
	Receipt string                `json:"receipt,omitempty"`
}

func (s *Service) ReadContributionDecisionAs(ctx context.Context, sub core.Subject, key, id string) (contribution.Decision, error) {
	c, _, _, e := s.ObserveContributionContinuityAs(ctx, sub, key)
	if e != nil {
		return contribution.Decision{}, e
	}
	r, ok := s.FleetRepository.(ContributionDecisionRepository)
	if !ok {
		return contribution.Decision{}, ErrDenied
	}
	d, e := r.GetContributionDecision(ctx, c.QueueItemID, id)
	if e == nil && !s.Now().Before(d.ExpiresAt) && (d.Status == "requested" || d.Status == "delivered") {
		next := d
		next.Status = "expired"
		if e = r.UpdateContributionDecision(ctx, d, next); e != nil {
			return d, e
		}
		d = next
	}
	return d, e
}

func (s *Service) checkContributionDecision(ctx context.Context, sub core.Subject, d contribution.Decision) error {
	if e := s.requirePrincipal(sub); e != nil {
		return e
	}
	c, state, _, e := s.ObserveContributionContinuityAs(ctx, sub, d.Key)
	if e != nil {
		return e
	}
	role, ok := contribution.SealedRole(c, d.Role)
	if !ok || role != d.Binding || c.Digest != d.ContractDigest || c.QueueItemID != d.QueueItemID || c.OwnerID != d.OwnerID || state.CancelRequested || state.Stage != d.Stage || state.Candidate != d.Candidate {
		return fmt.Errorf("%w: decision binding 6", ErrConflict)
	}
	control, e := s.FleetRepository.(fleet.ContributionRepository).GetContributionControl(ctx, c.QueueItemID)
	if e != nil || control.Stop || control.Digest != d.ControlDigest {
		return fmt.Errorf("%w: decision binding 7", ErrConflict)
	}
	if !s.Now().Before(d.ExpiresAt) || !s.Now().Before(c.Deadline) {
		return ErrExpired
	}
	a, e := s.FleetRepository.LatestAgentRevision(ctx, role.Agent.ID)
	if e != nil || a.Revision != role.Agent.Revision || a.Digest != role.Agent.Digest || a.Charter != role.Charter || a.Runtime != role.Runtime || a.Lifecycle != registry.LifecycleEnabled || a.Ownership.OwnerID != c.OwnerID {
		return ErrDenied
	}
	return nil
}

// Prepare creates no canonical approval. Only the protected, password-authenticated
// exact-review decision endpoint requests/decides/applies an ordinary approval.
func (s *Service) PrepareContributionDecisionAs(ctx context.Context, sub core.Subject, in ContributionDecisionInput) (ContributionDecisionReview, error) {
	var out ContributionDecisionReview
	if e := s.requirePrincipal(sub); e != nil {
		return out, e
	}
	c, state, _, e := s.ObserveContributionContinuityAs(ctx, sub, in.Key)
	if e != nil {
		return out, e
	}
	role, ok := contribution.SealedRole(c, in.Role)
	if !ok || !contribution.ValidKey(in.ID) {
		return out, ErrDenied
	}
	repo, ok := s.FleetRepository.(ContributionDecisionRepository)
	if !ok {
		return out, ErrDenied
	}
	// Uncertain responses are read back, never automatically re-prompted.
	if existing, readErr := repo.GetContributionDecision(ctx, c.QueueItemID, in.ID); readErr == nil {
		if existing.Status != "requested" || existing.Role != in.Role || s.checkContributionDecision(ctx, sub, existing) != nil {
			return out, ErrConflict
		}
		plan, err := s.GetPlan(existing.PlanID)
		if err != nil || plan.Digest != existing.PlanDigest || core.PlanDigest(plan) != plan.Digest {
			return out, ErrConflict
		}
		charter, err := s.GetCharter(existing.Binding.Charter.ID, existing.Binding.Charter.Revision)
		if err != nil || charter.Digest != existing.Binding.Charter.Digest {
			return out, ErrConflict
		}
		return ContributionDecisionReview{Intent: existing, Plan: plan, Charter: charter}, nil
	} else if readErr != fleet.ErrNotFound {
		return out, readErr
	}
	control, e := s.FleetRepository.(fleet.ContributionRepository).GetContributionControl(ctx, c.QueueItemID)
	if e != nil {
		return out, e
	}
	now := s.Now()
	expiry := now.Add(3 * time.Minute)
	if c.Deadline.Before(expiry) {
		expiry = c.Deadline
	}
	d := contribution.Decision{ID: in.ID, Key: c.Key, OwnerID: c.OwnerID, QueueItemID: c.QueueItemID, ContractDigest: c.Digest, ControlDigest: control.Digest, Role: in.Role, Binding: role, Stage: state.Stage, Candidate: state.Candidate, RequestedAt: now, ExpiresAt: expiry, Status: "requested"}
	if e = s.checkContributionDecision(ctx, sub, d); e != nil {
		return out, e
	}
	charter, e := s.GetCharter(role.Charter.ID, role.Charter.Revision)
	if e != nil || charter.Digest != role.Charter.Digest {
		return out, ErrConflict
	}
	review, e := s.PreviewPlanAs(ctx, sub, role.Charter.ID, role.Charter.Revision, core.Environment{Name: "local"})
	if e != nil {
		return out, e
	}
	d.PlanID = review.Plan.ID
	d.PlanDigest = review.Plan.Digest
	if e = repo.ReserveContributionDecision(ctx, d); e != nil {
		return out, e
	}
	return ContributionDecisionReview{Intent: d, Plan: review.Plan, Charter: charter}, nil
}

// CompleteContributionDecisionAs is called only after the HTTP layer consumes
// its session/purpose-bound review receipt. It never accepts a caller's plan.
func (s *Service) CompleteContributionDecisionAs(ctx context.Context, sub core.Subject, review ContributionDecisionReview, decision string) (contribution.Decision, error) {
	d, e := s.ReadContributionDecisionAs(ctx, sub, review.Intent.Key, review.Intent.ID)
	if e != nil {
		return d, e
	}
	if !reflect.DeepEqual(d, review.Intent) || d.Status != "requested" {
		return d, fmt.Errorf("%w: decision binding 1", ErrConflict)
	}
	repo := s.FleetRepository.(ContributionDecisionRepository)
	finish := func(status string) (contribution.Decision, error) {
		next := d
		next.Status = status
		e := repo.UpdateContributionDecision(ctx, d, next)
		return next, e
	}
	if e = s.checkContributionDecision(ctx, sub, d); e != nil {
		if e == ErrExpired {
			return finish("expired")
		}
		return d, e
	}
	switch decision {
	case "unavailable", "timeout", "unknown", "dialog-cancelled":
		return finish(decision)
	case "approve", "reject":
	default:
		return d, ErrDenied
	}
	p, e := s.GetPlan(d.PlanID)
	if e != nil || !reflect.DeepEqual(p, review.Plan) || p.Digest != d.PlanDigest || core.PlanDigest(p) != p.Digest || p.CharterDigest != d.Binding.Charter.Digest || p.AgentID != d.Binding.Charter.ID || p.Revision != d.Binding.Charter.Revision {
		return d, fmt.Errorf("%w: decision binding 2", ErrConflict)
	}
	charter, e := s.GetCharter(p.AgentID, p.Revision)
	reviewed, canonicalErr := core.Canonicalize(review.Charter.Charter)
	if e != nil || canonicalErr != nil || reviewed.Digest != charter.Digest || review.Charter.Digest != charter.Digest {
		return d, fmt.Errorf("%w: decision binding 3", ErrConflict)
	}
	next, e := finish("delivered")
	if e != nil {
		return d, e
	}
	d = next
	if decision == "reject" {
		return finish("denied")
	}
	next, e = finish("applying")
	if e != nil {
		return d, e
	}
	d = next
	// Applying is durable before effects. A crash/unknown response cannot replay.
	approval, e := s.RequestApprovalAs(ctx, sub, p.ID, time.Minute)
	if e != nil {
		return d, e
	}
	next = d
	next.ApprovalID = approval.ID
	if e = repo.UpdateContributionDecision(ctx, d, next); e != nil {
		return d, e
	}
	d = next
	if _, e = s.DecideApprovalAs(ctx, sub, approval.ID, true); e != nil {
		return d, e
	}
	if e = s.checkContributionDecision(ctx, sub, d); e != nil {
		return d, e
	}
	receipt, e := s.ApplyAs(ctx, sub, p.ID, approval.ID)
	if e != nil {
		return d, e
	}
	verified, e := s.GetReceipt(receipt.ID)
	if e != nil || verified.Status != "verified" || verified.PlanID != p.ID || verified.PlanDigest != p.Digest || verified.CharterDigest != p.CharterDigest || verified.ApprovalID != approval.ID {
		return d, fmt.Errorf("%w: decision binding 4", ErrConflict)
	}
	consumed, e := s.GetApproval(approval.ID)
	if e != nil || consumed.Status != "consumed" || consumed.ConsumedAt == nil {
		return d, fmt.Errorf("%w: decision binding 5", ErrConflict)
	}
	next = d
	next.Status = "approved"
	next.ApprovalID = approval.ID
	next.ReceiptID = receipt.ID
	if e = repo.UpdateContributionDecision(ctx, d, next); e != nil {
		return d, e
	}
	return s.ReadContributionDecisionAs(ctx, sub, d.Key, d.ID)
}
