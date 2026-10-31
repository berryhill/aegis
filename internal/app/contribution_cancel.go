package app

import (
	"context"
	"errors"
	"time"

	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	"github.com/berryhill/aegis/internal/queue"
)

// A retained exact invocation, not evidence that a missing process has stopped.
// Entries survive completion so explicit cancellation can join the same owner.
type contributionRunControl struct {
	contract string
	epoch    uint64
	cancel   context.CancelFunc
	done     chan struct{}
}

func (s *Service) beginContributionRun(ctx context.Context, c contribution.Contract) (context.Context, func(), error) {
	s.loopRunMu.Lock()
	defer s.loopRunMu.Unlock()
	if s.loopRunWorkers[c.QueueItemID] {
		return nil, nil, contribution.ErrContinuityFenced
	}
	if s.contributionRuns == nil {
		s.contributionRuns = make(map[string]*contributionRunControl)
	}
	epoch := uint64(1)
	if old := s.contributionRuns[c.QueueItemID]; old != nil {
		epoch = old.epoch + 1
	}
	control, err := s.FleetRepository.(fleet.ContributionRepository).GetContributionControl(ctx, c.QueueItemID)
	if err != nil {
		return nil, nil, err
	}
	if control.Stop {
		return nil, nil, contribution.ErrContinuityFenced
	}
	// Detach transport cancellation, not the original invocation's lifetime.
	// Manager entry supplies its immutable session expiry as a deadline; it
	// cannot be widened to the contribution deadline after the client leaves.
	deadline := c.Deadline
	if original, ok := ctx.Deadline(); ok && original.Before(deadline) {
		deadline = original
	}
	runCtx, cancel := context.WithDeadline(context.WithoutCancel(ctx), deadline)
	run := &contributionRunControl{c.Digest, epoch, cancel, make(chan struct{})}
	s.contributionRuns[c.QueueItemID] = run
	if s.loopRunWorkers == nil {
		s.loopRunWorkers = make(map[string]bool)
	}
	s.loopRunWorkers[c.QueueItemID] = true
	return runCtx, func() {
		cancel()
		s.loopRunMu.Lock()
		defer s.loopRunMu.Unlock()
		// Worker return includes its synchronous native Wait and role cleanup.
		if s.contributionRuns[c.QueueItemID] == run {
			delete(s.loopRunWorkers, c.QueueItemID)
		}
		close(run.done)
	}, nil
}

// Request fencing is committed before any signal. A pending cancellation returns
// normally for truthful manager readback, but never publishes Queue cancellation.
func (s *Service) cancelContributionInSession(ctx context.Context, sub core.Subject, session, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.requirePrincipal(sub); err != nil {
		return err
	}
	if session == "" {
		return ErrDenied
	}
	c, view, err := s.ReadContributionAs(ctx, sub, key)
	if err != nil {
		return err
	}
	repo, ok := s.FleetRepository.(fleet.ContributionContinuityRepository)
	if !ok {
		return ErrDenied
	}
	// Existing terminal outcomes, especially confirmed success, are immutable.
	if view.Projection.State == queue.StateRevoked || view.Projection.State == queue.StateSucceeded || view.Projection.State == queue.StateFailed || view.Projection.State == queue.StateCancelled || view.Projection.State == queue.StateDenied || view.Projection.State == queue.StateExpired {
		return nil
	}
	for tries := 0; tries < 16; tries++ {
		state, _, e := repo.GetContributionContinuity(ctx, c.QueueItemID)
		if e != nil {
			return e
		}
		if state.CancelRequested {
			break
		}
		_, e = s.RecordContributionContinuityAs(ctx, sub, session, key, ContributionContinuityInput{ContractDigest: c.Digest, ExpectedSequence: state.Sequence, ExpectedDigest: state.Digest, Kind: "cancel-requested"})
		if e == nil {
			break
		}
		if !errors.Is(e, contribution.ErrContinuityConflict) {
			return e
		}
		if tries == 15 {
			return e
		}
	}
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	defer cancel()
	s.loopRunMu.Lock()
	run := s.contributionRuns[c.QueueItemID]
	busy := s.loopRunWorkers[c.QueueItemID]
	s.loopRunMu.Unlock()
	joined := false
	if run != nil && run.contract == c.Digest {
		run.cancel()
		select {
		case <-run.done:
			joined = true
		case <-cleanup.Done():
			return nil
		}
	} else if busy {
		return nil
	}
	state, _, err := repo.GetContributionContinuity(cleanup, c.QueueItemID)
	if err != nil {
		return err
	}
	if !state.CancelAcknowledged {
		// Local-only admits no remote effects. Every other scope requires the
		// real publication owner's exact fence/readback join, never absence inference.
		if !s.quiesceContributionPublication(cleanup, c, state) {
			return nil
		}
		_, current, e := s.ReadContributionAs(cleanup, sub, key)
		if e != nil {
			return e
		}
		zero := !joined && len(state.RoleAuthorities) == 0 && len(state.NativeEffects) == 0 && state.Stage == "" && current.RuntimeAuthorityBinding == nil && len(current.Attempts) == 0 && len(current.Claims) == 0 && (current.Projection.State == queue.StateQueued || current.Projection.State.IsPreparation())
		if !joined && !zero {
			return nil
		}
		for _, name := range []string{"controller", "builder", "tester"} {
			r, exists := state.RoleAuthorities[name]
			if !exists {
				continue
			}
			if b, exists := state.NativeBoundaries[name]; exists && !b.Closed {
				b.Closed = true
				event, e := contribution.SealContinuityEvent(contribution.ContinuityEvent{SchemaVersion: contribution.ContinuitySchema, QueueItemID: c.QueueItemID, ContractDigest: c.Digest, OwnerID: c.OwnerID, SessionID: r.SessionID, Sequence: state.Sequence + 1, PreviousDigest: state.Digest, Kind: "native-boundary-closed", NativeBoundary: &b, At: s.Now()})
				if e != nil {
					return e
				}
				state, e = repo.AppendContributionContinuity(cleanup, event)
				if e != nil {
					return nil
				} // unknown native child retains custody
			}
			state, e = s.stopContributionRole(cleanup, c, state, name, ContributionContinuityInput{ContractDigest: c.Digest, ExpectedSequence: state.Sequence, ExpectedDigest: state.Digest})
			if e != nil {
				return nil
			}
		}
		// Even a joined invocation may have failed before registering its sessions.
		if len(state.RoleAuthorities) == 0 && !zero && (current.RuntimeAuthorityBinding != nil || state.Stage != "") {
			return nil
		}
		event, e := contribution.SealContinuityEvent(contribution.ContinuityEvent{SchemaVersion: contribution.ContinuitySchema, QueueItemID: c.QueueItemID, ContractDigest: c.Digest, OwnerID: c.OwnerID, SessionID: c.SessionID, Sequence: state.Sequence + 1, PreviousDigest: state.Digest, Kind: "cancel-acknowledged", At: s.Now()})
		if e != nil {
			return e
		}
		state, e = repo.AppendContributionContinuity(cleanup, event)
		if e != nil {
			return nil
		}
	}
	if !state.CancelAcknowledged || s.QueueWorker == nil {
		return nil
	}
	_, err = s.CancelQueueItemAs(cleanup, sub, TerminalQueueItemInput{QueueItemID: c.QueueItemID, CancellationID: c.QueueItemID + "-owner-stop", TransitionID: c.QueueItemID + "-owner-stop-transition", WorkspaceAgentID: c.Controller.ID})
	return err
}
