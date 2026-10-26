package app

import (
	"context"

	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/reference"
)

// ContributionPublicationBinding identifies the original publication owner.
// Candidate is the frozen evidence, not a mutable branch or pass label.
type ContributionPublicationBinding struct {
	Contract    reference.DigestRef
	RunID       string
	QueueItemID string
	Candidate   reference.DigestRef
	Controller  reference.DigestRef
	Epoch       uint64
}

// ContributionPublicationQuiescer is installed only by trusted controller
// composition. #378 must join its actual Repository.Fence and durable effect
// records: fence new writes, drain in-flight effects, reconcile unknown outcomes
// against exact original intents/targets, and preserve confirmed remote history.
// Return nil ONLY when that exact publication owner is durably quiescent. Missing
// records, unknown outcomes and failed readbacks return error (pending stop).
// This safety-only port grants no credential, runtime, retry or remote-write rights.
// It is not an HTTP/input surface, caller boolean or second remote effect log.
type ContributionPublicationQuiescer interface {
	QuiesceContributionPublication(context.Context, ContributionPublicationBinding) error
}

func (s *Service) quiesceContributionPublication(ctx context.Context, c contribution.Contract, state contribution.Continuity) bool {
	if ctx.Err() != nil || !state.CancelRequested {
		return false
	}
	if c.Effect == "local-only" {
		return true
	}
	r, exists := state.RoleAuthorities["controller"]
	if !exists || s.ContributionPublicationQuiescer == nil || state.CandidateEvidence.Validate() != nil || r.Epoch == 0 || r.Epoch != state.RoleEpochs["controller"] {
		return false
	}
	binding := ContributionPublicationBinding{Contract: reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: c.QueueItemID, Digest: c.Digest}, RunID: c.RunID, QueueItemID: c.QueueItemID, Candidate: state.CandidateEvidence, Controller: r.Ref(), Epoch: r.Epoch}
	if binding.Contract.Validate() != nil || binding.Controller.Validate() != nil || binding.RunID == "" {
		return false
	}
	return s.ContributionPublicationQuiescer.QuiesceContributionPublication(ctx, binding) == nil
}
