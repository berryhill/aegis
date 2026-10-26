package implementation

import (
	"context"
	"encoding/json"
	"github.com/berryhill/aegis/internal/loop"
)

// WithRecoveryBoundary excludes the prior cooperating source owner using the
// existing workspace flock and canonical owner record. It neither clears unknown
// effects nor changes a kernel record. The callback must finish before unlock.
func (e *ManagedContribution) WithRecoveryBoundary(ctx context.Context, run, contract, attempt, base string, c loop.VerifiedImplementation, p loop.ContributionExecutionPolicy, action func(context.Context) error) error {
	if action == nil || e.DB == nil {
		return ErrContributionQuiescenceUnavailable
	}
	unlock, err := lockWorkspace(c.Workspace)
	if err != nil {
		return err
	}
	defer unlock()
	r, err := e.Read(run)
	if err != nil {
		return err
	}
	owner, err := e.DB.Get([]byte("contribution/workspace/" + digest([]byte(c.Workspace))))
	identity, _ := json.Marshal(c)
	if err != nil || string(owner) != run || r.Workspace != c.Workspace || r.Contract != contract || r.QueueAttempt != attempt || r.Base != base || r.Implementation != digest(identity) || contributionPolicyDigest(r.Policy) != contributionPolicyDigest(p) {
		return ErrContributionQuiescenceUnavailable
	}
	// Recovery performs only bounded read-only discovery, not source commands or
	// publication. Do not route these reads through old expired role authority.
	if r.State == "phase_boundary" {
		if err = e.resumeBoundary(ctx, r, run, contract, attempt, base, c, p); err != nil {
			return err
		}
	} else if r.State == "candidate_accepted" {
		if len(r.Passes) == 0 {
			return ErrContributionQuiescenceUnavailable
		}
		if err = e.revalidate(ctx, c, p, r.Passes[len(r.Passes)-1].Candidate); err != nil {
			return err
		}
	} else {
		return ErrContributionQuiescenceUnavailable
	}
	return action(ctx)
}
