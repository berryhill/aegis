package app

import (
	"context"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/graph"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/orchestration"
	"github.com/berryhill/aegis/internal/reference"
	"github.com/berryhill/aegis/internal/registry"
)

// This is not a read-any-owner predicate. It accepts only a direct exact Run
// from the server's workspace/intent/runtime chain with a separately reviewed
// controller host decision. The normal evidence reader still reloads all bytes.
func (s *Service) authorizeControllerQueueResult(ctx context.Context, sub core.Subject, v QueueExecutionView, node graph.Node, contract loop.DoerContract) error {
	if ctx == nil || ctx.Err() != nil || s.requirePrincipal(sub) != nil || s.Authority == nil || s.AuthorityCommands == nil {
		return ErrDenied
	}
	a, err := s.historicalControllerParticipant(ctx, node.Participant)
	if err != nil {
		return ErrDenied
	}
	// Reconstruct only the original provenance commitment, not a current
	// delegation. A later lifecycle/charter revision cannot rewrite that fact.
	workspace, err := orchestration.NewRegisteredAgentWorkspaceAuthority(sub.PrincipalID, node.Participant, a.Ownership.OwnerID)
	if err != nil || workspace.Agent != node.Participant || workspace.OwnerID != a.Ownership.OwnerID || workspace.Ref() != v.Submission.Authority || v.Item.Authority != workspace.Ref() || v.Submission.OwnerAgentID != a.AgentID || v.Submission.OwnerID != a.Ownership.OwnerID {
		return ErrDenied
	}
	b := v.RuntimeAuthorityBinding
	if b == nil || b.OwnerAgent != node.Participant || b.QueueItem != (reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: v.Item.ItemID, Digest: v.Item.Digest}) || b.Submission != v.Item.Submission {
		return ErrDenied
	}
	authority, err := s.Authority.GetAuthorityContext(ctx, b.Authority.ID)
	if err != nil || authority.Digest != b.Authority.Digest || authority.MandateID != b.MandateID || authority.AgentID != a.AgentID || authority.CharterRevision != a.Charter.Revision || authority.CharterDigest != a.Charter.Digest || authority.Runtime.Runtime != a.Runtime.Runtime {
		return ErrDenied
	}
	mandate, err := s.Authority.GetMandate(ctx, authority.MandateID)
	if err != nil || core.ValidateAuthorityContext(authority, mandate) != nil || mandate.Subject.PrincipalID != sub.PrincipalID {
		return ErrDenied
	}
	charter, err := s.GetCharter(a.Charter.ID, a.Charter.Revision)
	if err != nil || charter.Digest != a.Charter.Digest || charter.Charter.Runtime.Target != a.Runtime.Target {
		return ErrDenied
	}
	selected, err := selectCharterAt(charter, mandate.Subject, "", mandate.Environment, authority.IssuedAt)
	if err != nil || selected.Selected == nil || selected.Selected.ID != authority.Authority.StanzaID || core.Digest(selected.Selected.Hermes) != core.Digest(authority.Authority.Hermes) || len(authority.Authority.Capabilities) != 0 || len(authority.Authority.Tools) != 0 || len(authority.Authority.Memory) != 0 || len(authority.Authority.Credentials) != 0 {
		return ErrDenied
	}
	// The evidence timestamp is not caller input. Current effect admission is
	// intentionally NOT queried or renewed by historical content inspection.
	if v.Disposition == nil || v.Disposition.OccurredAt.IsZero() || v.Disposition.OccurredAt.After(s.Now()) {
		return ErrDenied
	}
	at := v.Disposition.OccurredAt
	history, ok := s.AuthorityCommands.(core.AuthorityHistoryReader)
	if !ok {
		return ErrDenied
	}
	projection, err := history.HistoricalAuthorityProjection(ctx, authority.ID, authority.Digest, at)
	if err != nil || projection.AuthorityContextID != authority.ID || projection.MandateID != authority.MandateID || projection.State != core.AuthorityStateActive {
		return ErrDenied
	}
	requestID := v.Submission.IdempotencyKey
	record, err := s.Store.ReadHistoricalDoerHostApproval(s.Config.Credentials.Authority.DeploymentID, a.Ownership.OwnerID, node.Participant, contract, requestID, at)
	var identity string
	if err != nil || record.PrincipalID != sub.PrincipalID || record.ControllerSubjectID == "" || record.Candidate != node.Loop || record.DoerReusable == nil || record.RunRequestID != requestID || requestID+"-queue" != v.Item.ItemID || requestID+"-run" != v.Item.GraphRunID || s.Store.Load("queue-loop-intent", requestID, &identity) != nil || node.ID != "loop-"+identity || record.RunInputDigest != "sha256:"+identity {
		return ErrDenied
	}
	if v.Disposition != nil && v.Disposition.Authority != b.Authority {
		return ErrDenied
	}
	return nil
}

func (s *Service) historicalControllerParticipant(ctx context.Context, ref reference.RevisionRef) (registry.AgentRevision, error) {
	a, err := s.FleetRepository.GetAgentRevision(ctx, ref.ID, ref.Revision)
	if err != nil || a.Validate() != nil || agentRevisionRef(a) != ref {
		return registry.AgentRevision{}, ErrDenied
	}
	registration, err := s.FleetRepository.GetAgentRegistration(ctx, ref.ID)
	if err != nil || registration.Validate() != nil || registration.AgentID != ref.ID {
		return registry.AgentRevision{}, ErrDenied
	}
	initial, err := s.FleetRepository.GetAgentRevision(ctx, ref.ID, registration.InitialRevision.Revision)
	if err != nil || initial.Validate() != nil || agentRevisionRef(initial) != registration.InitialRevision || initial.Source != registration.Source || initial.Ownership.OwnerID != a.Ownership.OwnerID {
		return registry.AgentRevision{}, ErrDenied
	}
	latest, err := s.FleetRepository.LatestAgentRevision(ctx, ref.ID)
	if err != nil || latest.Validate() != nil || latest.AgentID != ref.ID || latest.Source != registration.Source || latest.Ownership.OwnerID != initial.Ownership.OwnerID {
		return registry.AgentRevision{}, ErrDenied
	}
	return a, nil
}
