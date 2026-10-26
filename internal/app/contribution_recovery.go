package app

import (
	"context"
	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/persistence/fleet"
)

// Called only by explicit authenticated Resume, while the existing kernel
// workspace flock and canonical owner record are held. Never infer completion
// from process absence, expired leases or missing adapter state.
func (s *Service) recoverContributionRoles(ctx context.Context, c contribution.Contract, state contribution.Continuity) (contribution.Continuity, error) {
	if state.Stage == "publish" || state.Stage == "checks" || state.Stage == "merge" || state.Stage == "dispose" {
		return state, contribution.ErrNativeQuiescenceUnavailable
	}
	// Validate ALL coverage before stopping any role or closing a boundary.
	for _, n := range state.NativeEffects {
		if n.State != "completed" {
			return state, contribution.ErrNativeQuiescenceUnavailable
		}
	}
	for role, r := range state.RoleAuthorities {
		if contribution.NativeRoleQuiescent(state, role) {
			continue
		}
		b, ok := state.NativeBoundaries[role]
		if r.BootID == "" || r.PID <= 0 || r.ProcessStart == "" || !ok || b.Epoch != r.Epoch || len(b.Bindings) == 0 {
			return state, contribution.ErrNativeQuiescenceUnavailable
		}
		seen := map[string]bool{}
		for _, binding := range b.Bindings {
			n, ok := state.NativeEffects[binding.IntentID]
			if seen[binding.IntentID] || !ok || n.State != "completed" || n.Binding != binding {
				return state, contribution.ErrNativeQuiescenceUnavailable
			}
			seen[binding.IntentID] = true
		}
		for id, n := range state.NativeEffects {
			if n.Binding.Role == role && n.Binding.Epoch == r.Epoch && !seen[id] {
				return state, contribution.ErrNativeQuiescenceUnavailable
			}
		}
	}
	for _, role := range []string{"controller", "builder", "tester"} {
		r, exists := state.RoleAuthorities[role]
		if !exists || contribution.NativeRoleQuiescent(state, role) {
			continue
		}
		var err error
		state, err = s.stopContributionRoleMode(ctx, c, state, role, ContributionContinuityInput{ContractDigest: c.Digest, ExpectedSequence: state.Sequence, ExpectedDigest: state.Digest}, true)
		if err != nil {
			return state, err
		}
		b := state.NativeBoundaries[role]
		if !b.Closed {
			b.Closed = true
			event, err := contribution.SealContinuityEvent(contribution.ContinuityEvent{SchemaVersion: contribution.ContinuitySchema, QueueItemID: c.QueueItemID, ContractDigest: c.Digest, OwnerID: c.OwnerID, SessionID: r.SessionID, Sequence: state.Sequence + 1, PreviousDigest: state.Digest, Kind: "native-boundary-closed", NativeBoundary: &b, At: s.Now()})
			if err != nil {
				return state, err
			}
			state, err = s.FleetRepository.(fleet.ContributionContinuityRepository).AppendContributionContinuity(ctx, event)
			if err != nil {
				return state, err
			}
		}
	}
	return state, nil
}
