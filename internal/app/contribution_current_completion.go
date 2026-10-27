package app

import (
	"context"
	"encoding/json"
	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/evidence"
	"github.com/berryhill/aegis/internal/execution"
	"github.com/berryhill/aegis/internal/queue"
	"github.com/berryhill/aegis/internal/reference"
)

func (s *Service) admitContributionCompleter(ctx context.Context, sub core.Subject, c contribution.Contract, st contribution.Continuity, claim queue.Claim) (*evidence.ContributionCurrentCompletion, error) {
	role := st.RoleAuthorities["controller"]
	if role.Authority == claim.Authority {
		if !s.Now().Before(claim.ExpiresAt) {
			return nil, ErrDenied
		}
		return nil, nil
	}
	launch, admission, err := s.currentContributionAdmission(ctx, sub, c, claim)
	if err != nil || admission == nil {
		return nil, ErrDenied
	}
	decision, err := admission.CheckRuntimeAdmission(ctx, launch, s.Now())
	if err != nil || execution.ValidateAdmission(launch, decision, s.Now()) != nil {
		return nil, ErrDenied
	}
	if role.Authority.ID != launch.AuthorityContext.ID || role.Authority.Digest != launch.AuthorityContext.Digest || st.Continuation == nil || st.Continuation.ExpiresAt.After(c.Deadline) {
		return nil, ErrDenied
	}
	original, err := s.Authority.GetAuthorityContext(ctx, claim.Authority.ID)
	if err != nil || original.Digest != claim.Authority.Digest {
		return nil, ErrDenied
	}
	wire, _ := json.Marshal(st.Continuation)
	return &evidence.ContributionCurrentCompletion{Authority: role.Authority, OriginalAuthority: claim.Authority, Role: role.Ref(), Epoch: role.Epoch, Mandate: role.MandateID, OriginalMandate: original.MandateID, Stanza: launch.AuthorityContext.Authority.StanzaID, Continuation: reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: claim.ClaimID, Digest: contribution.Digest(wire)}}, nil
}

// Only a canonical controller stop/re-admission/continuation tail may extend a
// verified preclosure head. Candidate/native/check counters never reset or move.
func compatiblePreclosureContinuity(c contribution.Contract, st contribution.Continuity, events []contribution.ContinuityEvent, anchor string) bool {
	if st.Digest == anchor {
		return true
	}
	var prior contribution.Continuity
	found := false
	for _, e := range events {
		if found {
			switch e.Kind {
			case "observation", "role-stop-requested", "role-stop-acknowledged", "native-boundary-closed", "role-admitted", "continuation-admitted":
			default:
				return false
			}
			if e.RoleAuthority != nil && e.RoleAuthority.Role != "controller" {
				return false
			}
			if e.RoleStop != nil && e.RoleStop.Authority.Role != "controller" {
				return false
			}
			if e.NativeBoundary != nil && e.NativeBoundary.Role != "controller" {
				return false
			}
		}
		var err error
		prior, err = contribution.ApplyContinuity(c, prior, e)
		if err != nil {
			return false
		}
		if e.Digest == anchor {
			found = true
			if prior.Used != st.Used || prior.Candidate != st.Candidate || prior.CandidateEvidence != st.CandidateEvidence {
				return false
			}
		}
	}
	return found && prior.Digest == st.Digest
}
