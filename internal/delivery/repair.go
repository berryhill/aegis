package delivery

import "context"

// ForwardRepair sends a persisted exact-intent failure reference to the existing
// cumulative budget owner. The owner MUST reload the authoritative failure,
// deduplicate Intent, prove quiescence and admit the same builder/freeze/tester
// lineage. This port does not grant scope, start a builder or reset any budget.
func ForwardRepair(ctx context.Context, owner RepairOwner, r Record) error {
	if owner == nil || r.Intent.ID == "" || r.Revision < 2 || r.Outcome != Failed || (r.Code != "ci_failed" && r.Code != "merge_conflict") {
		return ErrDenied
	}
	return owner.RequestRepair(ctx, RepairFailure{Reference: r.Intent.Reference, Intent: r.Intent.ID, Class: r.Code, Criterion: r.Code, Evidence: digest(r), Head: r.Intent.Head, Base: r.Intent.BaseHead})
}
