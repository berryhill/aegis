//go:build linux

package app

import (
	"context"
	"errors"
	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	"github.com/berryhill/aegis/internal/runtime/hermes"
	"os/exec"
)

// runContributionNativeCommandAs is for the source owner's lock-held, already
// qualified native-command path ONLY. It is deliberately not an API command or
// arbitrary shell route. The caller must retain accepted-source custody.
// Completion never admits replacement or renews the original claim/attempt.
func (s *Service) runContributionNativeCommandAs(ctx context.Context, sub core.Subject, session, key string, b contribution.NativeBinding, cmd *exec.Cmd) error {
	c, state, _, err := s.ObserveContributionContinuityAs(ctx, sub, key)
	if err != nil {
		return err
	}
	repo, ok := s.FleetRepository.(fleet.ContributionContinuityRepository)
	if !ok {
		return ErrDenied
	}
	if b.CommandDigest != hermes.NativeCommandDigest(cmd) {
		return ErrDenied
	}
	appendFact := func(ctx context.Context, kind, status string) error {
		for i := 0; i < 16; i++ {
			fresh, _, err := repo.GetContributionContinuity(ctx, c.QueueItemID)
			if err != nil {
				return err
			}
			state = fresh
			n := contribution.NativeEffect{Binding: b, State: status}
			e, err := contribution.SealContinuityEvent(contribution.ContinuityEvent{SchemaVersion: contribution.ContinuitySchema, QueueItemID: c.QueueItemID, ContractDigest: c.Digest, OwnerID: c.OwnerID, SessionID: session, Sequence: state.Sequence + 1, PreviousDigest: state.Digest, Kind: kind, NativeEffect: &n, At: s.Now()})
			if err != nil {
				return err
			}
			_, err = repo.AppendContributionContinuity(ctx, e)
			if !errors.Is(err, contribution.ErrContinuityConflict) {
				return err
			}
		}
		return contribution.ErrContinuityConflict
	}
	return hermes.RunContributionNative(ctx, b, cmd, func(ctx context.Context) error {
		_, err := s.CheckContributionRoleAs(ctx, sub, session, key, b.Role, ContributionContinuityInput{ContractDigest: c.Digest, ExpectedSequence: state.Sequence, ExpectedDigest: state.Digest})
		if err != nil {
			return err
		}
		return appendFact(ctx, "native-reserved", "reserved")
	}, func(ctx context.Context, p hermes.NativeCompletion) error {
		if p.Binding() != b {
			return ErrDenied
		}
		if p.Joined() {
			return appendFact(ctx, "native-completed", "completed")
		}
		return appendFact(ctx, "native-unknown", "unknown")
	})
}
