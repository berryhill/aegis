package badger

import (
	"context"
	"github.com/berryhill/aegis/internal/delivery"
	badgerdb "github.com/dgraph-io/badger/v4"
)

func (r *ContributionDeliveryRepository) RepairPullRequest(ctx context.Context, p delivery.Plan) (pr uint64, err error) {
	if r == nil || p.Reference != r.binding.Reference || p.RepairFailure == "" {
		return 0, delivery.ErrDenied
	}
	err = r.store.view(ctx, func(txn *badgerdb.Txn) error {
		chain, e := loadDeliveryHistory(txn, p.RepairFailure)
		if e != nil || len(chain) == 0 {
			return delivery.ErrDenied
		}
		old := chain[len(chain)-1].Record
		i := delivery.Intent{Reference: p.Reference, RepairFailure: p.RepairFailure, Operation: delivery.UpdatePullRequest, Repository: p.Repository, RepositoryID: p.RepositoryID, Base: p.Base, Branch: p.Branch, ExpectedOld: p.ExpectedOld, PR: old.Intent.PR}
		if e = r.validateRepairSuccessor(txn, i); e != nil {
			return e
		}
		pr = old.Intent.PR
		return nil
	})
	return
}

// Successors append new immutable slots only for a known pre-merge failure.
// This transaction cannot attest native acceptance (the external authority must
// do that); it independently denies UNKNOWN/merged history and target rotation.
func (r *ContributionDeliveryRepository) validateRepairSuccessor(txn *badgerdb.Txn, i delivery.Intent) error {
	failure, err := loadDeliveryHistory(txn, i.RepairFailure)
	if err != nil || len(failure) == 0 {
		return delivery.ErrDenied
	}
	old := failure[len(failure)-1].Record
	if old.Outcome != delivery.Failed || old.Observation.Outcome != delivery.Failed || old.Merge != "" ||
		(old.Intent.Operation != delivery.CheckDelivery && old.Intent.Operation != delivery.Merge) ||
		(old.Code != "ci_failed" && old.Code != "merge_conflict") || old.Intent.Run != i.Run || old.Intent.Contract != i.Contract || old.Intent.Candidate == i.Candidate ||
		old.Intent.RepositoryID != i.RepositoryID || old.Intent.Repository != i.Repository || old.Intent.Base != i.Base || old.Intent.Branch != i.Branch || old.Intent.Head != i.ExpectedOld || old.Intent.PR == 0 {
		return delivery.ErrDenied
	}
	wire, err := get(txn, key(familyContributionPublication, "root", r.binding.OwnerID, r.binding.Key))
	if err != nil {
		return err
	}
	var root delivery.PublicationRoot
	if strictDelivery(wire, &root) != nil {
		return delivery.ErrDenied
	}
	histories, err := publicationHistories(txn, root)
	if err != nil {
		return err
	}
	found := false
	for _, chain := range histories {
		last := chain[len(chain)-1].Record
		if last.Intent.Candidate == i.Candidate {
			continue
		}
		if last.Outcome == delivery.Unknown || last.Merge != "" || last.Intent.Operation == delivery.ReconcileIssue {
			return delivery.ErrDenied
		}
		if last.Intent.Operation == delivery.PullRequest && last.Outcome == delivery.Confirmed && last.PR == old.Intent.PR {
			found = true
		}
	}
	if !found {
		return delivery.ErrDenied
	}
	if (i.Operation == delivery.UpdatePullRequest || i.Operation == delivery.CheckDelivery || i.Operation == delivery.Merge || i.Operation == delivery.ReconcileIssue) && i.PR != old.Intent.PR {
		return delivery.ErrDenied
	}
	return nil
}
