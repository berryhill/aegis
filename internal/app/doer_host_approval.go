package app

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/hostapproval"
	"github.com/berryhill/aegis/internal/loop"

	"github.com/berryhill/aegis/internal/reference"
	"github.com/berryhill/aegis/internal/registry"
	"time"
)

// An independent protected principal action, never inferred from publish/Run.
// Only this exact server-held draft may be reviewed and approved.
type DoerHostApprovalInput struct {
	DraftID                 string `json:"draft_id"`
	DraftVersion            uint64 `json:"draft_version"`
	ExpectedCandidateDigest string `json:"expected_candidate_digest"`
	ExpectedContractDigest  string `json:"expected_contract_digest"`
	Decision                string `json:"decision"` // exactly approve-host-write
}

func (i *DoerHostApprovalInput) UnmarshalJSON(b []byte) error {
	if len(b) > 4096 || reference.RejectDuplicateObjectKeys(b) != nil || bytes.Equal(bytes.TrimSpace(b), []byte("null")) {
		return ErrDenied
	}
	type wire DoerHostApprovalInput
	var v wire
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if e := d.Decode(&v); e != nil {
		return e
	}
	*i = DoerHostApprovalInput(v)
	return nil
}

type DoerHostApproval = hostapproval.Record

func (s *Service) hostApprovalAgent(ctx context.Context, ref reference.RevisionRef) (registry.AgentRevision, error) {
	if ctx == nil || ctx.Err() != nil || s.Store == nil || s.FleetRepository == nil || s.Config.Credentials.Authority.DeploymentID == "" {
		return registry.AgentRevision{}, ErrDenied
	}
	exact, e := s.FleetRepository.GetAgentRevision(ctx, ref.ID, ref.Revision)
	if e != nil {
		return exact, e
	}
	latest, e := s.FleetRepository.LatestAgentRevision(ctx, ref.ID)
	if e != nil {
		return exact, e
	}
	registration, e := s.FleetRepository.GetAgentRegistration(ctx, ref.ID)
	if e != nil {
		return exact, e
	}
	initial, e := s.FleetRepository.GetAgentRevision(ctx, ref.ID, registration.InitialRevision.Revision)
	if e != nil {
		return exact, e
	}
	if exact.Validate() != nil || latest.Validate() != nil || registration.Validate() != nil || initial.Validate() != nil || agentRevisionRef(initial) != registration.InitialRevision || agentRevisionRef(exact) != ref || agentRevisionRef(latest) != ref || exact.Lifecycle != registry.LifecycleEnabled || latest.Lifecycle != registry.LifecycleEnabled || initial.Ownership.OwnerID != exact.Ownership.OwnerID || exact.Ownership.OwnerID != s.Config.Principal.ID {
		return exact, ErrDenied
	}
	return exact, nil
}
func (s *Service) ApproveDoerHostWriteAs(ctx context.Context, subject core.Subject, input DoerHostApprovalInput) (DoerHostApproval, error) {
	if ctx == nil || s.requirePrincipal(subject) != nil || (subject.Kind != "human" && subject.Kind != "principal") || (subject.Kind == "principal" && (subject.Method != "password" || subject.Issuer != "aegis-principal-auth")) || s.Config.Principal.AuthTTL <= 0 || subject.AuthenticatedAt.IsZero() || subject.AuthenticatedAt.After(s.Now()) || !s.Now().Before(subject.AuthenticatedAt.Add(s.Config.Principal.AuthTTL)) || input.Decision != "approve-host-write" {
		return DoerHostApproval{}, ErrDenied
	}
	if subject.Kind == "human" {
		origin := localHermesImportBootstrapCLI
		if subject.Issuer == "linux-so-peercred" {
			origin = localHermesImportUnixPeer
		}
		if s.requireLocalHermesImportPrincipal(subject, origin) != nil {
			return DoerHostApproval{}, ErrDenied
		}
	}
	draft, e := s.ValidateDoerDraftPublicationAs(ctx, subject, input.DraftID, input.DraftVersion)
	if e != nil {
		return DoerHostApproval{}, e
	}
	agent, e := s.hostApprovalAgent(ctx, draft.Agent)
	if e != nil {
		return DoerHostApproval{}, e
	}
	candidate, _, e := loop.NewDoerRevision(draft.LoopID, draft.Revision, draft.PreviousDigest, draft.Contract)
	if e != nil {
		return DoerHostApproval{}, e
	}
	digest, e := draft.Contract.Digest()
	if e != nil || input.ExpectedContractDigest != digest || input.ExpectedCandidateDigest != candidate.Digest {
		return DoerHostApproval{}, ErrConflict
	}
	if e = s.validateDoerControllerWorkspace(draft.Contract); e != nil {
		return DoerHostApproval{}, e
	}
	now := s.Now().UTC()
	expires := now.Add(24 * time.Hour)
	// Host consent is bounded independently of authentication continuation.
	r := hostapproval.Record{Purpose: hostapproval.Purpose, DeploymentID: s.Config.Credentials.Authority.DeploymentID, PrincipalID: subject.PrincipalID, OwnerID: agent.Ownership.OwnerID, Agent: draft.Agent, Candidate: reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: candidate.LoopID, Revision: candidate.Revision, Digest: candidate.Digest}, PreviousDigest: draft.PreviousDigest, Contract: draft.Contract, ContractDigest: digest, ApprovedAt: now, ExpiresAt: expires}
	if s.requirePrincipal(subject) != nil || !s.Now().Before(subject.AuthenticatedAt.Add(s.Config.Principal.AuthTTL)) {
		return DoerHostApproval{}, ErrDenied
	}
	if _, e = s.ValidateDoerDraftPublicationAs(ctx, subject, input.DraftID, input.DraftVersion); e != nil {
		return DoerHostApproval{}, e
	}
	if _, e = s.hostApprovalAgent(ctx, draft.Agent); e != nil {
		return DoerHostApproval{}, e
	}
	if e = s.audit(ctx, core.AuditEvent{Type: "doer.host.contract.approval", SubjectID: subject.ID, PrincipalID: subject.PrincipalID, AgentID: agent.AgentID, Outcome: "approved", Reason: "independent_exact_host_write_decision", Metadata: map[string]string{"contract_digest": digest, "candidate_digest": candidate.Digest}}); e != nil {
		return DoerHostApproval{}, e
	}
	approved, e := s.Store.SaveDoerHostApproval(r)
	if e != nil {
		return DoerHostApproval{}, e
	}
	return s.Store.ReadDoerHostApproval(approved.DeploymentID, approved.OwnerID, approved.Agent, approved.Contract, s.Now())
}

// Readback verifies signed custody and fresh latest/stable-owner Registry state.
func (s *Service) ReadDoerHostWriteApprovalAs(ctx context.Context, subject core.Subject, agent reference.RevisionRef, c loop.DoerContract) (DoerHostApproval, error) {
	if s.requirePrincipal(subject) != nil {
		return DoerHostApproval{}, ErrDenied
	}
	if _, e := s.hostApprovalAgent(ctx, agent); e != nil {
		return DoerHostApproval{}, e
	}
	return s.Store.ReadDoerHostApproval(s.Config.Credentials.Authority.DeploymentID, subject.PrincipalID, agent, c, s.Now())
}

// Resolve is installed in the worker, not exposed as browser authority input.
// No cached record or derived allowlist is used; every effect reloads custody.
func (s *Service) resolveDoerHostApproval(ctx context.Context, c loop.DoerContract, agent registry.AgentRevision) error {
	if e := s.validateDoerControllerWorkspace(c); e != nil {
		return e
	}
	ref := agentRevisionRef(agent)
	fresh, e := s.hostApprovalAgent(ctx, ref)
	if e != nil {
		return e
	}
	if fresh.Digest != agent.Digest {
		return ErrDenied
	}
	_, e = s.Store.ReadDoerHostApproval(s.Config.Credentials.Authority.DeploymentID, fresh.Ownership.OwnerID, ref, c, s.Now())
	return e
}
