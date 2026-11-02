package delivery

import "context"

func publicationParents(base, previous, repair, original string) []string {
	parents := []string{publicationParent(base, previous, repair)}
	if original != "" {
		parents = append(parents, base)
	}
	return parents
}

// publicationParent keeps the admitted base distinct from a repaired branch's
// previous owned head. This is structural validation, never repair admission.
func publicationParent(base, previous, repair string) string {
	if repair == "" {
		return base
	}
	if !roleDigest.MatchString(repair) || !sha.MatchString(previous) {
		return ""
	}
	return previous
}

// ForwardRepair sends a persisted exact-intent failure reference to the existing
// cumulative budget owner. The owner MUST reload the authoritative failure,
// deduplicate Intent, prove quiescence and admit the same builder/freeze/tester
// lineage. This port does not grant scope, start a builder or reset any budget.
func ForwardRepair(ctx context.Context, owner RepairOwner, r Record) error {
	if ctx == nil || ctx.Err() != nil || owner == nil || !repairableFailure(r) {
		return ErrDenied
	}
	return owner.RequestRepair(ctx, RepairFailure{Reference: r.Intent.Reference, Intent: r.Intent.ID, Class: r.Code, Criterion: r.Code, Evidence: digest(r), Head: r.Intent.Head, Base: r.Intent.BaseHead})
}

// This is eligibility only, not admission. The productive owner must reload
// canonical history and prove that NO other effect is unknown or merged before
// admitting managed repair. A failure label on another operation cannot turn an
// unresolved publication or issue mutation into a new builder permission.
func repairableFailure(r Record) bool {
	if _, err := VerifiedBaseAdvance(r); err == nil {
		return true
	}
	return r.Intent.ID != "" && r.Revision >= 2 && r.Outcome == Failed && r.Observation.Outcome == Failed && r.Merge == "" &&
		(r.Intent.Operation == CheckDelivery || r.Intent.Operation == Merge) &&
		(r.Code == "ci_failed" || r.Code == "merge_conflict")
}
