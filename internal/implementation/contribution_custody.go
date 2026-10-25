package implementation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"

	"github.com/berryhill/aegis/internal/loop"
)

var ErrContributionQuiescenceUnavailable = errors.New("exact contribution quiescence gate unavailable")

// ContributionQuiescenceGate is supplied only by the controller implementing
// exact stop/epoch admission. It must reject unknown or unacknowledged writers.
// This seam does not manufacture stop evidence from role.Close or a lease.
type ContributionQuiescenceGate func(context.Context, ContributionCandidate) error

// WithAcceptedCandidate holds the existing kernel workspace lock across a
// synchronous controller action. Diagnostic readback is not a lease. The action
// must not retain work beyond return; this grants no source edits, Git commits,
// publication, authority renewal or replacement-writer admission.
func (e *ManagedContribution) WithAcceptedCandidate(ctx context.Context, run, contract string, policy loop.ContributionExecutionPolicy, gate ContributionQuiescenceGate, action func(context.Context, ContributionCandidate) error) error {
	if gate == nil {
		return ErrContributionQuiescenceUnavailable
	}
	if e == nil || e.DB == nil || e.Admit == nil || !filepath.IsAbs(e.GoBinary) || action == nil {
		return ErrContributionHandoffDenied
	}
	if err := e.admit(ctx, "candidate-custody"); err != nil {
		return err
	}
	r, err := e.Read(run)
	if err != nil {
		return err
	}
	unlock, err := lockWorkspace(r.Workspace)
	if err != nil {
		return err
	}
	defer unlock()
	// Reload under custody; the first record was used only to locate the lock.
	current, err := e.Read(run)
	if err != nil {
		return err
	}
	if current.Workspace != r.Workspace || current.State != "candidate_accepted" || current.Contract != contract || contributionPolicyDigest(current.Policy) != contributionPolicyDigest(policy) {
		return ErrContributionHandoffDenied
	}
	r = current
	before, _ := json.Marshal(r)
	v := r.Passes[len(r.Passes)-1].Candidate
	// Do not let callback mutation of slice-backed inventories change our anchor.
	clone := func() ContributionCandidate {
		b, _ := json.Marshal(v)
		var out ContributionCandidate
		_ = json.Unmarshal(b, &out)
		return out
	}
	if err = gate(ctx, clone()); err != nil {
		return err
	}
	if err = e.revalidate(ctx, loop.VerifiedImplementation{Workspace: r.Workspace}, policy, v); err != nil {
		return err
	}
	// The gate itself is controller code: evidence must still match after it.
	check := func() error {
		after, err := e.Read(run)
		if err != nil {
			return err
		}
		wire, _ := json.Marshal(after)
		if !bytes.Equal(before, wire) {
			return ErrContributionHandoffDenied
		}
		return e.revalidate(ctx, loop.VerifiedImplementation{Workspace: r.Workspace}, policy, v)
	}
	if err = check(); err != nil {
		return err
	}
	callbackErr := action(ctx, clone())
	// Even a failed action cannot suppress drift or authority revocation. Join
	// preserves errors.Is/As for the original callback error.
	gateErr := gate(ctx, clone())
	return errors.Join(callbackErr, gateErr, check())
}
