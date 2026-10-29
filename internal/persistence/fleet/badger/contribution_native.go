package badger

import (
	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/queue"
	badgerdb "github.com/dgraph-io/badger/v4"
)

func checkContributionNativeFence(txn *badgerdb.Txn, c contribution.Contract, prior contribution.Continuity, e contribution.ContinuityEvent) error {
	if e.Kind != "native-reserved" {
		return nil
	}
	if e.NativeEffect == nil {
		return contribution.ErrContinuityConflict
	}
	b := e.NativeEffect.Binding
	p, err := loadQueueProjection(txn, c.QueueItemID)
	if err != nil {
		return err
	}
	claim, err := loadClaim(txn, b.ClaimID)
	if err != nil {
		return err
	}
	if p.State != queue.StateClaimed || p.ActiveClaimID != b.ClaimID || claim.QueueItem.ID != c.QueueItemID || claim.AttemptID != b.AttemptID {
		return contribution.ErrContinuityFenced
	}
	// An expired original lease is historical only. The existing canonical,
	// separately admitted controller continuation must authorize this same
	// immutable claim/attempt; never extend or rewrite the original lease.
	if !e.At.Before(claim.ExpiresAt) || prior.Continuation != nil {
		lease := prior.Continuation
		controller, ok := prior.RoleAuthorities["controller"]
		_, stopped := prior.RoleStops["controller"]
		if lease == nil || !ok || stopped || prior.CancelRequested || lease.ClaimID != claim.ClaimID || lease.AttemptID != claim.AttemptID || lease.Controller != controller.Ref() || lease.Epoch != controller.Epoch || !e.At.Before(lease.ExpiresAt) || !e.At.Before(c.Deadline) {
			return contribution.ErrContinuityFenced
		}
	}
	return nil
}
