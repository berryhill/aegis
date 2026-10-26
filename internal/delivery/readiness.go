package delivery

import (
	"context"
	"errors"
	"time"
)

// ReadinessBudget is the trusted #377 infrastructure counter owner, NOT a new
// counter. ReserveReadiness must durably consume one root-global recheck before
// returning nil, bound to this exact Reference and Check/Merge intent. maximum
// is a ceiling on the EXISTING cumulative root counter, across candidates,
// phases, processes and Run invocations. Never reset or silently renew it.
// Implementations must validate private Source context and fresh continuity;
// unavailable owner integration must deny. No accepting production default exists.
type ReadinessBudget interface {
	ReserveReadiness(context.Context, Intent, uint8) error
}

var ErrReadinessExhausted = errors.New("readiness budget exhausted")
var errReadinessHeld = errors.New("readiness held")

type readinessHold string

func (e readinessHold) Error() string { return string(e) }
func (e readinessHold) Unwrap() error { return errReadinessHeld }

// RecheckPolicy is closed controller configuration, never runtime/user grants.
// Zero means disabled. Construction rejects rather than clamps wider policies.
type RecheckPolicy struct {
	maximum uint8
	delay   time.Duration
}

func NewRecheckPolicy(maximum uint8, delay time.Duration) (RecheckPolicy, error) {
	if maximum < 1 || maximum > 2 || delay < 0 || delay > 30*time.Second {
		return RecheckPolicy{}, ErrDenied
	}
	return RecheckPolicy{maximum, delay}, nil
}

// ReadinessOption is supplied only by the trusted controller composition root.
// A policy without a persistent budget owner does not admit any extra reads.
type ReadinessOption struct {
	policy RecheckPolicy
	budget ReadinessBudget
}

func WithReadiness(policy RecheckPolicy, budget ReadinessBudget) ReadinessOption {
	return ReadinessOption{policy, budget}
}
func (o ReadinessOption) reserve(ctx, caller context.Context, p Plan, i Intent, used *uint8) error {
	if o.policy.maximum == 0 {
		return readinessHold("readiness_policy_unavailable")
	}
	if o.budget == nil {
		return readinessHold("readiness_budget_unavailable")
	}
	if *used >= o.policy.maximum {
		return readinessHold("readiness_budget_exhausted")
	}
	if ctx.Err() != nil || caller.Err() != nil {
		return readinessHold("readiness_cancelled")
	}
	if p.Intake.Deadline.IsZero() || !time.Now().Add(o.policy.delay).Before(p.Intake.Deadline) {
		return readinessHold("readiness_deadline")
	}
	// Never replace or detach Source's context, including through the timer.
	timer := time.NewTimer(o.policy.delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return readinessHold("readiness_cancelled")
	case <-caller.Done():
		return readinessHold("readiness_cancelled")
	case <-timer.C:
	}
	if ctx.Err() != nil || caller.Err() != nil {
		return readinessHold("readiness_cancelled")
	}
	if !time.Now().Before(p.Intake.Deadline) {
		return readinessHold("readiness_deadline")
	}
	if err := o.budget.ReserveReadiness(ctx, i, o.policy.maximum); err != nil {
		if errors.Is(err, ErrReadinessExhausted) {
			return readinessHold("readiness_budget_exhausted")
		}
		return readinessHold("readiness_budget_unavailable")
	}
	*used++
	if ctx.Err() != nil || caller.Err() != nil {
		return readinessHold("readiness_cancelled")
	}
	return nil
}
