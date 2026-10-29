package orchestration

import (
	"context"
	"github.com/berryhill/aegis/internal/execution"
	"time"
)

// Both independent authorities must remain live at every effect boundary.
func admitContributionEffects(ctx context.Context, controller execution.LaunchContract, admission execution.AdmissionChecker, child *ContributionRole, at time.Time) error {
	check := func(launch execution.LaunchContract, checker execution.AdmissionChecker) error {
		if checker == nil {
			return ErrWorkerDenied
		}
		decision, err := checker.CheckRuntimeAdmission(ctx, launch, at)
		if err != nil {
			return err
		}
		return execution.ValidateAdmission(launch, decision, at)
	}
	if err := check(controller, admission); err != nil {
		return err
	}
	if child != nil {
		return check(child.Launch, child.Admission)
	}
	return nil
}
