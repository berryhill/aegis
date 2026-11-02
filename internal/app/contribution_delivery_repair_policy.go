package app

import (
	"github.com/berryhill/aegis/internal/delivery"
	"github.com/berryhill/aegis/internal/orchestration"
)

// Reconstruct successor pins from the independently reloaded native owner, never
// from the caller's policy. The original admitted base remains unchanged.
func contributionRepairPolicy(p ContributionDeliveryPolicy, candidate orchestration.ContributionCandidate, repairs []orchestration.ContributionDeliveryRepair) (ContributionDeliveryPolicy, error) {
	p.RepairFailure, p.ExpectedOld = "", ""
	if len(repairs) == 0 {
		if p.OriginalBase != "" || candidate.CurrentBase != "" {
			return p, delivery.ErrDenied
		}
		return p, nil
	}
	r := repairs[len(repairs)-1]
	if r.AfterPass >= candidate.Pass || r.AfterPass == 0 || r.Failure == "" || r.Head == "" || r.PR == 0 || r.Candidate == candidate.Digest || r.Base != candidate.Base {
		return p, delivery.ErrDenied
	}
	if (r.Class == "base_advanced" && (r.CurrentBase == "" || candidate.CurrentBase != r.CurrentBase)) || (r.Class != "base_advanced" && candidate.CurrentBase != "") {
		return p, delivery.ErrDenied
	}
	if r.Class == "base_advanced" {
		p.OriginalBase, p.BaseHead = candidate.Base, r.CurrentBase
	}
	p.RepairFailure, p.ExpectedOld, p.PR = r.Failure, r.Head, r.PR
	return p, nil
}
