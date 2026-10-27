package app

import (
	"context"
	"errors"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/delivery"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	"time"
)

var errContributionTerminalWinnerUnavailable = errors.New("contribution terminal writer ended without an exact indexed winner")

const contributionWinnerWait = 5 * time.Second

func contributionWinnerMatches(result ContributionResult, auth *ContributionPreClosureAuthorization, preclosure string, ref delivery.Reference) bool {
	if auth == nil {
		return false
	}
	r := result.Report
	return ref.Run == auth.run && ref.Contract == auth.contract && ref.Candidate == auth.candidate && r.Owner == auth.owner && r.Key == auth.key && r.Run == ref.Run && r.Contract == auth.contract && r.Candidate == auth.candidate && r.Policy == auth.policy && r.Native == auth.native && r.Claim == auth.claim && r.Attempt == auth.attempt && r.Scope == auth.scope && r.Outcome == auth.outcome && r.Cause == auth.cause && r.PreClosure == preclosure
}

// Called ONLY after typed custody contention from real finalizer admission.
// Waits on the existing cross-process directory flock, never re-executes work.
// Unlock is followed by independent canonical index/artifact/receipt readback.
func (v *ContributionFinalVerifier) reconcileContributionWinner(ctx context.Context, sub core.Subject, key string, auth *ContributionPreClosureAuthorization, preclosure string, ref delivery.Reference) (ContributionResult, error) {
	var empty ContributionResult
	s := v.service
	bounded, cancel := context.WithTimeout(ctx, contributionWinnerWait)
	defer cancel()
	if err := bounded.Err(); err != nil {
		return empty, err
	}
	c, _, _, err := s.ObserveContributionContinuityAs(bounded, sub, key)
	if err != nil {
		return empty, err
	}
	if c.OwnerID != auth.owner || c.RunID != ref.Run || c.Digest != auth.contract {
		return empty, ErrDenied
	}
	if s.QueueWorker == nil {
		return empty, ErrDenied
	}
	if result, e := s.GetContributionResultAs(bounded, sub, key); e == nil {
		if !contributionWinnerMatches(result, auth, preclosure, ref) {
			return empty, ErrDenied
		}
		return result, nil
	} else if !errors.Is(e, fleet.ErrNotFound) {
		return empty, e
	}
	if err = s.QueueWorker.WaitContributionFinalization(bounded, sub, c.QueueItemID, c.RunID, c.Digest); err != nil {
		return empty, err
	}
	result, err := s.GetContributionResultAs(bounded, sub, key)
	if err != nil {
		return empty, errors.Join(errContributionTerminalWinnerUnavailable, err)
	}
	if !contributionWinnerMatches(result, auth, preclosure, ref) {
		return empty, ErrDenied
	}
	return result, nil
}
