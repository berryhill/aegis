package userservice

import (
	"context"
	"errors"
	"strings"
)

// RequireStopped binds offline maintenance to the exact installed and loaded
// plan, with inactive/failed activity and MainPID=0 on both sides of readback.
func RequireStopped(ctx context.Context, runner Runner, plan Plan) error {
	if runner == nil {
		return errors.New("offline maintenance requires service manager")
	}
	current, err := Preview(plan.Executable, plan.ConfigPath)
	if err != nil {
		return err
	}
	if !samePlan(current, plan) {
		return ErrForeignUnit
	}
	for range 2 {
		digest, err := InstalledDigest(plan)
		if err != nil {
			return err
		}
		if digest != plan.UnitDigest && digest != plan.LegacyUnitDigest {
			return ErrForeignUnit
		}
		if err := validateLoadedIdentity(ctx, runner, plan); err != nil {
			return err
		}
		state, err := runner.Output(ctx, "show", UnitName, "--property", "ActiveState", "--value")
		if err != nil {
			return err
		}
		if s := strings.TrimSpace(string(state)); s != "inactive" && s != "failed" {
			return errors.New("offline maintenance requires stopped exact gateway")
		}
		pid, err := runner.Output(ctx, "show", UnitName, "--property", "MainPID", "--value")
		if err != nil {
			return err
		}
		if strings.TrimSpace(string(pid)) != "0" {
			return errors.New("offline maintenance requires MainPID=0")
		}
	}
	return ctx.Err()
}
