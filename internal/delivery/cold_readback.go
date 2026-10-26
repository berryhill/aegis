package delivery

import "context"

// ReconcileHistorical reads an already reserved, exact immutable target. It does
// not acquire source custody, reserve an effect, reset a budget, or call Apply.
// The concrete closed GitHub adapter cannot be replaced by a caller HTTP client.
func ReconcileHistorical(ctx context.Context, owner Repository, github *GitHub, old Record, plan Plan) (Record, error) {
	if owner == nil || github == nil || old.Revision == 0 {
		return old, ErrDenied
	}
	expected, err := intentFor(plan, old.Intent.Operation)
	if err != nil || expected != old.Intent {
		return old, ErrDenied
	}
	switch old.Intent.Operation {
	case Publish, PullRequest, UpdatePullRequest, CheckDelivery, Merge, ReconcileIssue:
	default:
		return old, ErrDenied // Graph/object/local artifacts are not reconstructed.
	}
	if old.Outcome != Unknown {
		return old, nil
	}
	guard := func(c context.Context) error {
		if c == nil || c.Err() != nil || ctx.Err() != nil {
			return ErrDenied
		}
		return owner.Fence(c, old.Intent, false)
	}
	if err = guard(ctx); err != nil {
		return old, err
	}
	observed := github.Reconcile(ctx, old.Intent, plan, guard)
	if err = validateObservation(old.Intent, plan, observed); err != nil {
		return old, err
	}
	// Absence/denial is not proof that a reserved mutation did not happen.
	if observed.Outcome != Confirmed && observed.Head == "" && observed.Tree == "" && observed.Merge == "" && observed.PR == 0 {
		return old, ErrDenied
	}
	observed, err = RetainObservation(old, observed)
	if err != nil {
		return old, err
	}
	if old.Intent.Operation != CheckDelivery && observed.Outcome != Confirmed {
		observed.Outcome = Unknown
	}
	return owner.Finish(ctx, old, observed)
}

// VerifyHistoricalPlan checks the full operation-specific immutable payload,
// including fields that a remote GET alone cannot establish.
func VerifyHistoricalPlan(i Intent, p Plan) error {
	want, err := intentFor(p, i.Operation)
	if err != nil || want != i {
		return ErrDenied
	}
	return nil
}
