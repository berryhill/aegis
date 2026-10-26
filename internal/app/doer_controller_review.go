package app

import (
	"context"

	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/reference"
	"github.com/berryhill/aegis/internal/registry"
)

// This is host review only. Definition publication/successor/lifecycle admission
// retains its independent ownership checks. Neither a draft nor this preflight
// issues authority, approves consent or creates an execution intent.
func (s *Service) ValidateDoerDraftHostReviewAs(ctx context.Context, sub core.Subject, id string, version uint64) (DoerDraft, error) {
	if err := s.RequireDashboardPrincipal(sub); err != nil {
		return DoerDraft{}, err
	}
	d, err := s.ReadDoerDraftAs(ctx, sub, id)
	if err != nil {
		return DoerDraft{}, err
	}
	if d.Version != version {
		return DoerDraft{}, ErrConflict
	}
	a, err := s.hostApprovalAgent(ctx, d.Agent)
	if err != nil {
		return DoerDraft{}, err
	}
	if a.Ownership.OwnerID != sub.PrincipalID {
		// The independent password approver is not the local-OS runtime subject.
		// Bind the canonical charter here; select it only at fresh runtime admission.
		ch, err := s.GetCharter(a.Charter.ID, a.Charter.Revision)
		if err != nil || ch.Digest != a.Charter.Digest || ch.Charter.Runtime.Target != a.Runtime.Target || a.Runtime.Adapter != "hermes" || a.Runtime.Runtime != "hermes-agent" || a.Runtime.Target != "aegis-owned-ephemeral" {
			return DoerDraft{}, ErrDenied
		}
	}
	return d, nil
}

func (s *Service) doerControllerCharterAllowed(ctx context.Context, sub core.Subject, a registry.AgentRevision) bool {
	if ctx == nil || ctx.Err() != nil || s.requirePrincipal(sub) != nil || a.Runtime.Adapter != "hermes" || a.Runtime.Runtime != "hermes-agent" || a.Runtime.Target != "aegis-owned-ephemeral" {
		return false
	}
	ch, err := s.GetCharter(a.Charter.ID, a.Charter.Revision)
	if err != nil || ch.Digest != a.Charter.Digest || ch.Charter.Runtime.Target != a.Runtime.Target {
		return false
	}
	decision, err := s.Select(ch, sub, "", core.Environment{Name: "local"})
	if err != nil || decision.Selected == nil {
		return false
	}
	st := decision.Selected
	return len(st.Grant.Capabilities) == 0 && len(st.Grant.Tools) == 0 && len(st.Hermes.Toolsets) == 0 && len(st.Scopes.Memory) == 0 && len(st.Scopes.Credentials) == 0 && st.Hermes.Profile == "" && !st.Hermes.PersistentHome && len(st.Hermes.MCPServers) == 0 && len(st.Hermes.Plugins) == 0
}

// The optional retained scope refers to an already published exact v5, not the
// v4 revision synthesized from the draft. No new Loop/Graph/Queue is published.
func (s *Service) doerHostReviewCandidate(ctx context.Context, sub core.Subject, d DoerDraft) (loop.LoopRevision, error) {
	if d.HostRun == nil {
		c, _, err := loop.NewDoerRevision(d.LoopID, d.Revision, d.PreviousDigest, d.Contract)
		return c, err
	}
	if validateDoerDraftInput(DoerDraftInput{HostRun: d.HostRun, ID: d.ID, ExpectedVersion: d.Version, Agent: d.Agent, LoopID: d.LoopID, Revision: d.Revision, PreviousDigest: d.PreviousDigest, Contract: d.Contract}) != nil {
		return loop.LoopRevision{}, ErrDenied
	}
	run, _, err := normalizeLoopRunIdentity(*d.HostRun)
	if err != nil {
		return loop.LoopRevision{}, err
	}
	c, err := s.GetLoopAs(ctx, sub, run.Loop.ID, run.Loop.Revision)
	if err != nil || c.SchemaVersion != loop.DoerReusableSchemaVersion || c.Digest != run.Loop.Digest || c.PreviousDigest != d.PreviousDigest {
		return loop.LoopRevision{}, ErrDenied
	}
	bound, err := bindDoerRun(ctx, run, c)
	if err != nil {
		return loop.LoopRevision{}, err
	}
	contract, err := queuedDoerContract(bound, c)
	want, wantErr := d.Contract.Digest()
	got, gotErr := contract.Digest()
	if err != nil || wantErr != nil || gotErr != nil || got != want {
		return loop.LoopRevision{}, ErrDenied
	}
	return c, nil
}

func (s *Service) PrepareDoerHostReviewAs(ctx context.Context, sub core.Subject, id string, version uint64) (DoerHostApprovalInput, registry.AgentRevision, error) {
	d, err := s.ValidateDoerDraftHostReviewAs(ctx, sub, id, version)
	if err != nil {
		return DoerHostApprovalInput{}, registry.AgentRevision{}, err
	}
	c, err := s.doerHostReviewCandidate(ctx, sub, d)
	if err != nil {
		return DoerHostApprovalInput{}, registry.AgentRevision{}, err
	}
	a, err := s.hostApprovalAgent(ctx, d.Agent)
	if err != nil {
		return DoerHostApprovalInput{}, registry.AgentRevision{}, err
	}
	digest, err := d.Contract.Digest()
	return DoerHostApprovalInput{DraftID: d.ID, DraftVersion: d.Version, ExpectedCandidateDigest: c.Digest, ExpectedContractDigest: digest, Decision: "approve-host-write"}, a, err
}

func loopRevisionRef(c loop.LoopRevision) reference.RevisionRef {
	return reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: c.LoopID, Revision: c.Revision, Digest: c.Digest}
}
