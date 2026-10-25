package orchestration

import (
	"context"
	"errors"
	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	"github.com/berryhill/aegis/internal/reference"
	"github.com/berryhill/aegis/internal/registry"
)

func (w *QueueWorker) contributionContract(ctx context.Context, id string, r loop.LoopRevision) (loop.VerifiedImplementation, contribution.Contract, error) {
	deny := errors.New("sealed contribution is absent, expired or inconsistent")
	repo, ok := w.repository.(fleet.ContributionRepository)
	if !ok {
		return loop.VerifiedImplementation{}, contribution.Contract{}, deny
	}
	c, e := repo.GetContributionForQueue(ctx, id)
	if e != nil || !w.now().Before(c.Deadline) {
		return loop.VerifiedImplementation{}, c, deny
	}
	control, e := repo.GetContributionControl(ctx, id)
	if e != nil || control.Stop {
		return loop.VerifiedImplementation{}, c, deny
	}
	effect := c.Effect
	if control.Sequence > 0 {
		if control.ContractDigest != c.Digest || !contribution.DeliveryNarrows(c.Effect, control.Delivery) {
			return loop.VerifiedImplementation{}, c, deny
		}
		effect = control.Delivery
	}
	// Candidate execution is independent of the later delivery target. It never
	// performs or attests GitHub effects, nor falls back to selected-file v3.
	if !contribution.DeliveryNarrows(c.Effect, effect) {
		return loop.VerifiedImplementation{}, c, deny
	}
	for _, ref := range []reference.RevisionRef{c.Controller, c.Builder, c.Tester} {
		a, e := w.repository.LatestAgentRevision(ctx, ref.ID)
		if e != nil || a.Digest != ref.Digest || a.Lifecycle != registry.LifecycleEnabled || a.Ownership.OwnerID != c.OwnerID {
			return loop.VerifiedImplementation{}, c, deny
		}
	}
	v, e := contribution.BindLoop(r, c)
	return v, c, e
}
