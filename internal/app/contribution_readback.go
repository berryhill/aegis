package app

import (
	"context"
	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/reference"
	"sort"
)

// ContributionContinuityView is an allowlisted observation, never authority or
// whole-contribution success. Raw process and native command data stay internal.
type ContributionContinuityView struct {
	Stage                string                     `json:"stage"`
	CandidateLineage     string                     `json:"candidate_lineage"`
	FrozenDigest         string                     `json:"frozen_digest"`
	Loop                 reference.RevisionRef      `json:"loop"`
	RunID                string                     `json:"run_id"`
	QueueItemID          string                     `json:"queue_item_id"`
	Used                 contribution.Counters      `json:"used"`
	Limits               contribution.Counters      `json:"limits"`
	Roles                []ContributionRoleView     `json:"roles"`
	Decisions            []ContributionDecisionView `json:"decisions"`
	UnknownNativeIntents []string                   `json:"unknown_native_intents"`
	CancelAcknowledged   bool                       `json:"cancel_acknowledged"`
	CancelRequested      bool                       `json:"cancel_requested"`
	HeldReason           string                     `json:"held_reason"`
}
type ContributionRoleView struct {
	Role             string `json:"role"`
	Epoch            uint64 `json:"epoch"`
	StopRequested    bool   `json:"stop_requested"`
	StopAcknowledged bool   `json:"stop_acknowledged"`
}
type ContributionDecisionView struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

func projectContributionContinuity(c contribution.Contract, state contribution.Continuity) ContributionContinuityView {
	v := ContributionContinuityView{Stage: state.Stage, CandidateLineage: state.Candidate, FrozenDigest: state.CandidateEvidence.Digest, Loop: c.Loop, RunID: c.RunID, QueueItemID: c.QueueItemID, Used: state.Used, Limits: state.Limits, CancelAcknowledged: state.CancelAcknowledged, CancelRequested: state.CancelRequested, HeldReason: "retained_checkpoint_not_completion"}
	// Intake/prepare evidence identifies the contract, not a frozen candidate.
	if state.Stage == "" || state.Stage == "resolve" || state.Stage == "seal" || state.Stage == "prepare" || state.Stage == "build" || v.FrozenDigest == c.Digest {
		v.FrozenDigest = ""
	}
	for _, role := range []string{"controller", "builder", "tester"} {
		stop, requested := state.RoleStops[role]
		v.Roles = append(v.Roles, ContributionRoleView{role, state.RoleEpochs[role], requested, requested && stop.Acknowledged})
	}
	for id, status := range state.Decisions {
		switch status {
		case "requested", "delivered", "unanswered", "denied", "expired", "dialog-cancelled", "unavailable", "timeout", "unknown":
			v.Decisions = append(v.Decisions, ContributionDecisionView{id, status})
		}
	}
	sort.Slice(v.Decisions, func(i, j int) bool { return v.Decisions[i].ID < v.Decisions[j].ID })
	for id, effect := range state.NativeEffects {
		if effect.State == "unknown" || effect.State == "reserved" {
			v.UnknownNativeIntents = append(v.UnknownNativeIntents, id)
		}
	}
	sort.Strings(v.UnknownNativeIntents)
	if len(v.UnknownNativeIntents) > 0 {
		v.HeldReason = "native_effect_unresolved"
	}
	if state.CancelRequested {
		v.HeldReason = "cancellation_requested"
	}
	if state.CancelAcknowledged {
		v.HeldReason = "cancellation_acknowledged"
	}
	return v
}
func (s *Service) ReadContributionContinuityViewAs(ctx context.Context, sub core.Subject, key string) (ContributionContinuityView, error) {
	c, state, _, err := s.ObserveContributionContinuityAs(ctx, sub, key)
	if err != nil {
		return ContributionContinuityView{}, err
	}
	v := projectContributionContinuity(c, state)
	if !state.CancelAcknowledged && !s.Now().Before(c.Deadline) {
		v.HeldReason = "contribution_deadline_exhausted"
	}
	return v, nil
}
