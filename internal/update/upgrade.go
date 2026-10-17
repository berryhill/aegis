package update

import (
	"context"
	"errors"
	"fmt"
)

// UpgradeBoundary verifies exact installation identity and authenticates the
// principal before returning a narrowly scoped stop action. It must not mutate
// the installation during preflight. The action repeats identity admission.
type UpgradeBoundary func(context.Context) (func(context.Context) error, error)
type BinaryUpdater interface {
	Run(context.Context, bool) (Result, error)
}

// RunUpgrade implements prepare -> authenticate -> exact stop -> install. A current
// binary still passes lifecycle admission. Completion requires later verified
// migration/activation/readiness/Console phases; none are inferred here.
func RunUpgrade(ctx context.Context, binary BinaryUpdater, boundary UpgradeBoundary) (Result, error) {
	r := Result{Phase: "preflight", ResumeRequired: true}
	fail := func(err error) (Result, error) { return r, fmt.Errorf("upgrade incomplete at %s: %w", r.Phase, err) }
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	if boundary == nil {
		return fail(errors.New("protected upgrade boundary unavailable"))
	}
	preparer, ok := binary.(interface {
		Prepare(context.Context) (*Prepared, error)
	})
	if !ok {
		return fail(errors.New("verified candidate preparation unavailable"))
	}
	r.Phase = "candidate_prepare"
	prepared, err := preparer.Prepare(ctx)
	if err != nil {
		return fail(err)
	}
	defer prepared.Close()
	r.Phase = "protected_authentication"
	stop, err := boundary(ctx)
	if err != nil {
		return fail(err)
	}
	if stop == nil {
		return fail(errors.New("exact gateway stop boundary unavailable"))
	}
	if err = ctx.Err(); err != nil {
		return fail(err)
	}
	r.Phase = "gateway_stop"
	if err = prepared.Validate(ctx); err != nil {
		return fail(err)
	}
	if err = stop(ctx); err != nil {
		return fail(err)
	}
	if err = ctx.Err(); err != nil {
		return fail(err)
	}
	r.Phase = "binary_install"
	installed, err := prepared.Install(ctx)
	installed.Phase = r.Phase
	installed.ResumeRequired = true
	installed.UpgradeComplete = false
	r = installed
	if err != nil {
		return fail(err)
	}
	r.Phase = "activation_pending"
	r.RequiredAction = "Upgrade incomplete: migration eligibility, authenticated recovery if eligible, activation, provenance, audit-current readiness and Console verification remain pending"
	return r, nil
}
