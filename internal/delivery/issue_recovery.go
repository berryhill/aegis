package delivery

import "context"

// IssueRecoveryOwner reloads verifier-owned original preclosure custody. Its
// result is read-only: it is never sent through Reserve or Apply.
type HistoricalIssueReadOwner interface {
	ReconcileContributionIssue(context.Context, Record, Plan) (Record, error)
}

type IssueRecoveryOwner interface {
	RecoverContributionIssue(context.Context, Reference, Plan) (Record, Plan, bool, error)
}

func (e *Engine) reconcileOriginalIssue(ctx context.Context, old Record, plan Plan) (Record, error) {
	if old.Revision == 0 || old.Intent.Operation != ReconcileIssue || VerifyHistoricalPlan(old.Intent, plan) != nil {
		return old, ErrDenied
	}
	guard := func(c context.Context) error {
		if ctx.Err() != nil || c == nil || c.Err() != nil {
			return ErrDenied
		}
		if e.authority.Admit(c, old.Intent, false) != nil {
			return ErrDenied
		}
		return e.owner.Fence(c, old.Intent, false)
	}
	if err := guard(ctx); err != nil {
		return old, err
	}
	// The canonical confirmed observation is immutable. Re-reading it cannot
	// replace that observation with a newly timestamped proof or consume its CAS.
	if old.Outcome == Confirmed {
		return old, nil
	}
	observed := e.adapter.Reconcile(ctx, old.Intent, plan, guard)
	if err := validateObservation(old.Intent, plan, observed); err != nil {
		return old, err
	}
	// Open/absent/unavailable cannot negate an UNKNOWN PATCH. Preserve it, with
	// no new history, mutation, readiness reservation or candidate budget.
	if observed.Outcome != Confirmed {
		return old, nil
	}
	retained, err := RetainObservation(old, observed)
	if err != nil {
		return old, err
	}
	if err = guard(ctx); err != nil {
		return old, err
	}
	return e.owner.Finish(ctx, old, retained)
}
