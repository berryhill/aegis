package app

import (
	"context"
	"time"

	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/hostapproval"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/reference"
	"github.com/berryhill/aegis/internal/registry"
)

// This policy prepares only an owner-principal's zero-authority disposable
// Doer session. It is NOT provisioning: no provisioned mapping is read or
// written by Hermes.Launch. RequiredOperations (including provision) remain
// unchanged and enforced for actual provisioning and general session entry.
func (s *Service) doerEphemeralPreparationAllowed(ctx context.Context, subject core.Subject, candidate loop.LoopRevision, agent registry.AgentRevision) bool {
	if ctx == nil || ctx.Err() != nil || s.requirePrincipal(subject) != nil || subject.AuthenticatedAt.IsZero() || subject.AuthenticatedAt.After(s.Now()) || s.Config.Principal.AuthTTL <= 0 || !s.Now().Before(subject.AuthenticatedAt.Add(s.Config.Principal.AuthTTL)) {
		return false
	}
	if subject.Kind == "human" {
		origin := localHermesImportBootstrapCLI
		if subject.Issuer == "linux-so-peercred" {
			origin = localHermesImportUnixPeer
		}
		if s.requireLocalHermesImportPrincipal(subject, origin) != nil {
			return false
		}
	} else if subject.Kind != "principal" || subject.Method != "password" || subject.Issuer != "aegis-principal-auth" {
		return false
	}
	canonical, _, err := loop.NewRevision(candidate)
	if err != nil || candidate.SchemaVersion != loop.DoerRevisionSchemaVersion || candidate.Doer == nil || canonical.Digest != candidate.Digest {
		return false
	}
	fresh, err := s.hostApprovalAgent(ctx, agentRevisionRef(agent))
	if err != nil || fresh.Digest != agent.Digest || agent.Runtime.Adapter != "hermes" || agent.Runtime.Runtime != "hermes-agent" || agent.Runtime.Target != "aegis-owned-ephemeral" {
		return false
	}
	charter, err := s.GetCharter(agent.Charter.ID, agent.Charter.Revision)
	if err != nil || charter.Digest != agent.Charter.Digest || charter.Charter.Runtime.Target != agent.Runtime.Target {
		return false
	}
	decision, err := s.Select(charter, subject, "", core.Environment{Name: "local"})
	if err != nil || decision.Selected == nil {
		return false
	}
	st := decision.Selected
	return len(st.Grant.Capabilities) == 0 && len(st.Grant.Tools) == 0 && len(st.Hermes.Toolsets) == 0 && len(st.Scopes.Memory) == 0 && len(st.Scopes.Credentials) == 0 && st.Hermes.Profile == "" && !st.Hermes.PersistentHome && len(st.Hermes.MCPServers) == 0 && len(st.Hermes.Plugins) == 0
}

// Only QueueLoop's exact, intent-fenced authenticated Run installs this private
// marker. Readiness, publishing, activation and workspace actors cannot grant.
type exactDoerRunKey struct{}
type exactDoerRun struct {
	requestID string
	candidate reference.RevisionRef
}

func (s *Service) prepareDoerRunHostScope(ctx context.Context, subject core.Subject, candidate loop.LoopRevision, agent registry.AgentRevision) error {
	intent, ok := ctx.Value(exactDoerRunKey{}).(exactDoerRun)
	if !ok || intent.requestID == "" || !s.doerEphemeralPreparationAllowed(ctx, subject, candidate, agent) {
		return nil
	}
	if err := s.validateDoerControllerWorkspace(*candidate.Doer); err != nil {
		return err
	}
	// Reuse signed exact scope, never a controller-wide allowlist or an Agent
	// grant. Every runtime effect still reloads it and repeats normal admission.
	var lineage LoopReexecutionFact
	linked := s.Store.Load("queue-loop-reexecution", intent.requestID, &lineage) == nil
	if linked && (lineage.Digest != reexecutionDigest(lineage) || lineage.RequestID != intent.requestID || lineage.Agent != agentRevisionRef(agent) || lineage.Loop != intent.candidate || lineage.SubjectID != subject.ID || lineage.PrincipalID != subject.PrincipalID) {
		return ErrDenied
	}
	if s.resolveDoerHostApproval(ctx, *candidate.Doer, agent) == nil {
		if !linked {
			return nil
		}
		record, err := s.Store.ReadDoerHostApproval(s.Config.Credentials.Authority.DeploymentID, agent.Ownership.OwnerID, agentRevisionRef(agent), *candidate.Doer, s.Now())
		if err == nil && record.RunRequestID == intent.requestID && record.ReexecutionDigest == lineage.Digest && record.ImplementationPassCap == lineage.Remaining {
			return nil
		}
	}
	now := s.Now().UTC()
	expires := subject.ExpiresAt
	if freshUntil := subject.AuthenticatedAt.Add(s.Config.Principal.AuthTTL); freshUntil.Before(expires) {
		expires = freshUntil
	}
	if limit := now.Add(24 * time.Hour); limit.Before(expires) {
		expires = limit
	}
	digest, err := candidate.Doer.Digest()
	if err != nil {
		return err
	}
	record := hostapproval.Record{Purpose: hostapproval.Purpose, DeploymentID: s.Config.Credentials.Authority.DeploymentID, PrincipalID: subject.PrincipalID, OwnerID: agent.Ownership.OwnerID, Agent: agentRevisionRef(agent), Candidate: reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: candidate.LoopID, Revision: candidate.Revision, Digest: candidate.Digest}, PreviousDigest: candidate.PreviousDigest, Contract: *candidate.Doer, ContractDigest: digest, ApprovedAt: now, ExpiresAt: expires, RunRequestID: intent.requestID}
	if linked {
		record.ImplementationPassCap, record.ReexecutionDigest = lineage.Remaining, lineage.Digest
	}
	if err = s.audit(ctx, core.AuditEvent{Type: "doer.host.run-scope", SubjectID: subject.ID, PrincipalID: subject.PrincipalID, AgentID: agent.AgentID, Outcome: "authorized", Reason: "authenticated_exact_loop_run", Metadata: map[string]string{"request_id": intent.requestID, "contract_digest": digest, "candidate_digest": candidate.Digest}}); err != nil {
		return err
	}
	if !s.doerEphemeralPreparationAllowed(ctx, subject, candidate, agent) {
		return ErrDenied
	}
	if _, err = s.Store.SaveDoerHostApproval(record); err != nil {
		return err
	}
	return s.resolveDoerHostApproval(ctx, *candidate.Doer, agent)
}
