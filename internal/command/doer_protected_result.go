package command

import (
	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/config"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/hostapproval"
	"github.com/berryhill/aegis/internal/reference"
	"reflect"
	"time"
)

// This verifies returned commitments, not signatures or independent live proof.
// The owning service reloads canonical state; helper exit text is never authority.
func verifyDoerProtectedResult(cfg config.Config, review app.DoerProtectedSetupReview, result app.DoerProtectedSetupResult) bool {
	before, after := review.Draft, result.Draft
	if before.ID != after.ID || before.PrincipalID != after.PrincipalID || !reflect.DeepEqual(before.Contract, after.Contract) || !before.CreatedAt.Equal(after.CreatedAt) || !before.ExpiresAt.Equal(after.ExpiresAt) {
		return false
	}
	switch review.Action {
	case "successor":
		if review.Proposal == nil || result.Successor == nil || result.Host != nil || result.Provisioning != nil {
			return false
		}
		a := result.Successor
		p := review.Proposal
		return a.Validate() == nil && after.Version == before.Version+1 && after.Agent != before.Agent && after.Agent.ID == before.Agent.ID && after.Agent.Revision == before.Agent.Revision+1 && after.PublicationKey != before.PublicationKey && a.CharterSuccessor != nil && a.CharterSuccessor.Previous == before.Agent && a.Charter.ID == p.Proposed.Charter.AgentID && a.Charter.Revision == p.Proposed.Charter.Revision && a.Charter.Digest == p.Proposed.Digest && after.Agent == (reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: a.AgentID, Revision: a.Revision, Digest: a.Digest})
	case "host":
		h := result.Host
		if !reflect.DeepEqual(before, after) || review.Host == nil || h == nil || result.Successor != nil || result.Provisioning != nil {
			return false
		}
		digest, e := after.Contract.Digest()
		return e == nil && h.Purpose == hostapproval.Purpose && h.Agent.Validate() == nil && h.Candidate.Validate() == nil && h.KeyID != "" && len(h.Signature) == 64 && !h.ApprovedAt.IsZero() && h.ApprovedAt.Before(h.ExpiresAt) && h.ExpiresAt.Sub(h.ApprovedAt) <= 24*time.Hour && h.DeploymentID == cfg.Credentials.Authority.DeploymentID && h.PrincipalID == before.PrincipalID && h.OwnerID == before.PrincipalID && h.Agent == after.Agent && reflect.DeepEqual(h.Contract, after.Contract) && h.ContractDigest == digest && h.ContractDigest == review.Host.ExpectedContractDigest && h.Candidate.Digest == review.Host.ExpectedCandidateDigest && h.Candidate.ID == after.LoopID && h.Candidate.Revision == after.Revision && h.PreviousDigest == after.PreviousDigest && time.Now().Before(h.ExpiresAt)
	case "provision":
		r, p := result.Provisioning, review.Plan
		if !reflect.DeepEqual(before, after) || p == nil || review.Charter == nil || r == nil || result.Successor != nil || result.Host != nil {
			return false
		}
		if p.Digest != core.PlanDigest(*p) || r.ID == "" || r.ApprovalID == "" || r.Status != "verified" || r.Failure != "" || r.FinishedAt.IsZero() || r.PlanID != p.ID || r.PlanDigest != p.Digest || r.CharterDigest != p.CharterDigest || r.CharterDigest != review.Charter.Digest || len(r.Artifacts) != len(p.Effects) {
			return false
		}
		for i, e := range p.Effects {
			a := r.Artifacts[i]
			if !a.Verified || a.Path != e.Target || a.Digest != e.Digest || a.Action != e.Kind {
				return false
			}
		}
		return true
	}
	return false
}
