//go:build linux

package app

import (
	"context"
	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	"github.com/berryhill/aegis/internal/runtime/hermes"
	"os/exec"
)

// runIsolatedContributionNativeBatchAs owns the entire native producer lifetime.
// It cannot certify legacy ManagedContribution producers: those require their
// source owner's integration. No public shell route or caller completion flag.
func (s *Service) runIsolatedContributionNativeBatchAs(ctx context.Context, sub core.Subject, session, key string, bindings []contribution.NativeBinding, commands []*exec.Cmd) error {
	if s.QueueWorker != nil || len(bindings) == 0 || len(bindings) != len(commands) {
		return contribution.ErrNativeQuiescenceUnavailable
	}
	c, state, _, err := s.ObserveContributionContinuityAs(ctx, sub, key)
	if err != nil {
		return err
	}
	role := bindings[0].Role
	if _, err = s.CheckContributionRoleAs(ctx, sub, session, key, role, ContributionContinuityInput{ContractDigest: c.Digest, ExpectedSequence: state.Sequence, ExpectedDigest: state.Digest}); err != nil {
		return err
	}
	for i, b := range bindings {
		if b.CommandDigest != hermes.NativeCommandDigest(commands[i]) {
			return ErrDenied
		}
	}
	boundary := contribution.NativeBoundary{Role: role, Epoch: bindings[0].Epoch, Bindings: append([]contribution.NativeBinding(nil), bindings...)}
	repo := s.FleetRepository.(fleet.ContributionContinuityRepository)
	appendBoundary := func(kind string) error {
		fresh, _, e := repo.GetContributionContinuity(ctx, c.QueueItemID)
		if e != nil {
			return e
		}
		event, e := contribution.SealContinuityEvent(contribution.ContinuityEvent{SchemaVersion: contribution.ContinuitySchema, QueueItemID: c.QueueItemID, ContractDigest: c.Digest, OwnerID: c.OwnerID, SessionID: session, Sequence: fresh.Sequence + 1, PreviousDigest: fresh.Digest, Kind: kind, NativeBoundary: &boundary, At: s.Now()})
		if e != nil {
			return e
		}
		_, e = repo.AppendContributionContinuity(ctx, event)
		return e
	}
	if err = appendBoundary("native-boundary-open"); err != nil {
		return err
	}
	for i, b := range bindings {
		if err = s.runContributionNativeCommandAs(ctx, sub, session, key, b, commands[i]); err != nil {
			return err
		}
	}
	boundary.Closed = true
	return appendBoundary("native-boundary-closed")
}
