package app

import (
	"context"
	"errors"

	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/persistence/fleet"
)

// This is the cleanup callback used by the actual independently admitted role.
// Only a typed terminal conflict with the complete exact join is reconcilable.
func (s *Service) closeContributionRole(ctx context.Context, c contribution.Contract, original core.Session, role string) error {
	err := s.endSession(ctx, original.ID, "stopped", "contribution_role_finished", true)
	if errors.Is(err, ErrConflict) && s.contributionRoleCloseJoined(ctx, c, original, role) {
		return nil
	}
	return err
}

// contributionRoleCloseJoined reconciles cleanup only. It never grants execution,
// changes a terminal session outcome, renews authority or infers stop from a PID.
// The supervisor may win the canonical terminal transition after the role owner
// has already obtained exact native Wait and durable stop acknowledgement.
func (s *Service) contributionRoleCloseJoined(ctx context.Context, c contribution.Contract, original core.Session, role string) bool {
	if ctx == nil || ctx.Err() != nil {
		return false
	}
	repo, ok := s.FleetRepository.(fleet.ContributionContinuityRepository)
	if !ok {
		return false
	}
	state, _, err := repo.GetContributionContinuity(ctx, c.QueueItemID)
	if err != nil || state.ContractDigest != c.Digest || !contribution.NativeRoleQuiescent(state, role) {
		return false
	}
	admitted := state.RoleAuthorities[role]
	if admitted.SessionID != original.ID || admitted.MandateID != original.Mandate.ID || admitted.RuntimeSessionID != original.RuntimeSessionID {
		return false
	}
	current, err := s.GetSession(original.ID)
	if err != nil || current.Status == "running" || current.EndedAt == nil || core.Digest(current.Mandate) != core.Digest(original.Mandate) || current.RuntimeSessionID != original.RuntimeSessionID || current.RuntimePID != original.RuntimePID || current.ProcessStart != original.ProcessStart {
		return false
	}
	authority, err := s.authorityContextForSession(ctx, original.ID)
	if err != nil || authority.ID != admitted.Authority.ID || authority.Digest != admitted.Authority.Digest {
		return false
	}
	projection, err := s.AuthorityCommands.CurrentAuthorityProjection(ctx, authority.ID)
	return err == nil && (projection.State == core.AuthorityStateRevoked || projection.State == core.AuthorityStateExpired)
}
