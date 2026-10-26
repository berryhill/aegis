package app

import (
	"context"
	"errors"
	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/persistence/fleet"
)

// DeliverContributionDecisionAs owns the exact intent and reserves its budget
// before launch. The launcher is trusted composition, never request input.
func (s *Service) DeliverContributionDecisionAs(ctx context.Context, sub core.Subject, key, role string, launch func(context.Context, ContributionDecisionInput) string) (string, error) {
	s.contributionDecisionMu.Lock()
	defer s.contributionDecisionMu.Unlock()
	if err := s.requirePrincipal(sub); err != nil {
		return "", err
	}
	c, state, _, err := s.ObserveContributionContinuityAs(ctx, sub, key)
	if err != nil {
		return "", err
	}
	if _, ok := contribution.SealedRole(c, role); !ok {
		return "", ErrDenied
	}
	control, err := s.FleetRepository.(fleet.ContributionRepository).GetContributionControl(ctx, c.QueueItemID)
	if err != nil {
		return "", err
	}
	// Stable across login/request sessions, but never across changed sealed scope.
	id := "decision-" + contribution.Digest([]byte(c.Digest + "/" + state.Stage + "/" + state.Candidate + "/" + control.Digest + "/" + role))[7:]
	in := ContributionDecisionInput{Key: key, ID: id, Role: role}
	d, err := s.ReadContributionDecisionAs(ctx, sub, key, id)
	if err == nil {
		return s.contributionDecisionDeliveryReadback(ctx, sub, d)
	}
	if !errors.Is(err, fleet.ErrNotFound) {
		return "", err
	}
	if state.Sequence == 0 {
		_, err = s.RecordContributionContinuityAs(ctx, sub, c.SessionID, key, ContributionContinuityInput{ContractDigest: c.Digest, Kind: "initialize"})
		if err != nil {
			return "", err
		}
	}
	_, err = s.PrepareContributionDecisionAs(ctx, sub, in)
	if err != nil {
		return "", err
	}
	status := "unavailable"
	if launch != nil {
		status = launch(ctx, in)
	}
	// Never authorize from a companion's claimed outcome. Canonical readback wins.
	d, err = s.ReadContributionDecisionAs(ctx, sub, key, id)
	if err != nil {
		return "", err
	}
	if d.Status == "requested" {
		switch status {
		case "unavailable", "timeout", "dialog-cancelled":
		default:
			status = "unknown"
		}
		next := d
		next.Status = status
		if err = s.FleetRepository.(ContributionDecisionRepository).UpdateContributionDecision(ctx, d, next); err != nil {
			return "", err
		}
		d = next
	}
	return s.contributionDecisionDeliveryReadback(ctx, sub, d)
}

func (s *Service) contributionDecisionDeliveryReadback(ctx context.Context, sub core.Subject, d contribution.Decision) (string, error) {
	if d.Status != "approved" {
		switch d.Status {
		case "requested", "delivered", "applying":
			return "unknown", nil
		}
		return d.Status, nil
	}
	if err := s.checkContributionDecision(ctx, sub, d); err != nil {
		return "", err
	}
	p, err := s.GetPlan(d.PlanID)
	if err != nil || p.Digest != d.PlanDigest || core.PlanDigest(p) != p.Digest || p.CharterDigest != d.Binding.Charter.Digest || p.AgentID != d.Binding.Charter.ID || p.Revision != d.Binding.Charter.Revision {
		return "unknown", ErrDenied
	}
	a, err := s.GetApproval(d.ApprovalID)
	if err != nil || a.Status != "consumed" || a.ConsumedAt == nil {
		return "unknown", ErrDenied
	}
	receipt, err := s.GetReceipt(d.ReceiptID)
	if err != nil || receipt.Status != "verified" || receipt.PlanID != p.ID || receipt.PlanDigest != p.Digest || receipt.CharterDigest != p.CharterDigest || receipt.ApprovalID != a.ID {
		return "unknown", ErrDenied
	}
	return "approved", nil
}
