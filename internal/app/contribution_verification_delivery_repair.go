package app

import "github.com/berryhill/aegis/internal/delivery"

// A successor may reuse only the creation belonging to its proven failed
// predecessor. Policy PR pins alone never establish creation provenance.
func retainedRepairPRAncestor(h delivery.HistorySnapshot, p ContributionDeliveryPolicy, creation delivery.Record) bool {
	if p.RepairFailure == "" || p.PR == 0 || creation.PR != p.PR || creation.Outcome != delivery.Confirmed || creation.Intent.Operation != delivery.PullRequest {
		return false
	}
	if creation.Intent.Run != p.Binding.Run || creation.Intent.Contract != p.Binding.Contract || creation.Intent.Branch != p.Branch || creation.Intent.RepairFailure != "" {
		return false
	}
	id, head := p.RepairFailure, p.ExpectedOld
	seen := map[string]bool{}
	for id != "" && !seen[id] {
		seen[id] = true
		var failed delivery.Record
		found := false
		for _, chain := range h.Histories {
			if len(chain) == 0 {
				return false
			}
			last := chain[len(chain)-1].Record
			if last.Intent.ID == id {
				if found {
					return false
				}
				failed, found = last, true
			}
		}
		if !found || failed.Outcome != delivery.Failed || failed.Observation.Outcome != delivery.Failed || failed.Merge != "" ||
			(failed.Code != "ci_failed" && failed.Code != "merge_conflict") ||
			(failed.Intent.Operation != delivery.CheckDelivery && failed.Intent.Operation != delivery.Merge) ||
			failed.Intent.Run != p.Binding.Run || failed.Intent.Contract != p.Binding.Contract ||
			failed.Intent.PR != p.PR || failed.Intent.Head != head || failed.Intent.Branch != p.Branch || failed.Intent.Base != p.Binding.Base ||
			creation.Intent.Repository != failed.Intent.Repository || creation.Intent.RepositoryID != failed.Intent.RepositoryID {
			return false
		}
		if failed.Intent.RepairFailure == "" {
			return failed.Intent.Reference == creation.Intent.Reference && head == creation.Intent.Head
		}
		// A later failed candidate must itself retain its successful update of
		// the original PR. Following a failure pointer alone proves no publication.
		updated := false
		for _, chain := range h.Histories {
			last := chain[len(chain)-1].Record
			i := last.Intent
			if i.Operation == delivery.UpdatePullRequest && last.Outcome == delivery.Confirmed && last.PR == p.PR && i.PR == p.PR &&
				i.Reference == failed.Intent.Reference && i.Head == head && i.ExpectedOld == failed.Intent.ExpectedOld &&
				i.RepairFailure == failed.Intent.RepairFailure && i.Repository == failed.Intent.Repository && i.RepositoryID == failed.Intent.RepositoryID &&
				i.Branch == p.Branch && i.Base == p.Binding.Base {
				if updated {
					return false
				}
				updated = true
			}
		}
		if !updated || failed.Intent.ExpectedOld == "" {
			return false
		}
		id, head = failed.Intent.RepairFailure, failed.Intent.ExpectedOld
	}
	return false
}
