package userservice

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"
)

// InstalledDigest admits only the current renderer or the exact historical
// renderer for this same executable, configuration and authenticated principal.
// Recognition does not authorize activation or rewriting the historical unit.
func InstalledDigest(plan Plan) (string, error) {
	if _, err := os.Lstat(plan.UnitPath); err != nil {
		return "", err
	}
	if err := inspectUnit(plan.UnitPath, plan.unit); err == nil {
		return plan.UnitDigest, nil
	}
	if len(plan.legacyUnit) != 0 && inspectUnit(plan.UnitPath, plan.legacyUnit) == nil {
		return plan.LegacyUnitDigest, nil
	}
	return "", ErrForeignUnit
}

// MigrateOwnedUnit is called only inside the authenticated upgrade intent. The
// caller durably binds both digests before calling; replay after atomic rename
// accepts the exact desired bytes and repeats daemon-reload.
func MigrateOwnedUnit(ctx context.Context, runner Runner, plan Plan, previousDigest string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if runner == nil {
		return errors.New("user service manager is unavailable")
	}
	current, err := Preview(plan.Executable, plan.ConfigPath)
	if err != nil {
		return err
	}
	if !samePlan(current, plan) || previousDigest != plan.LegacyUnitDigest {
		return ErrForeignUnit
	}
	digest, err := InstalledDigest(plan)
	if err != nil {
		return err
	}
	if err = validateLoadedIdentity(ctx, runner, plan); err != nil {
		return err
	}
	if digest != plan.UnitDigest {
		root, err := os.OpenRoot(filepath.Dir(plan.UnitPath))
		if err != nil {
			return err
		}
		defer root.Close()
		f, err := os.CreateTemp(filepath.Dir(plan.UnitPath), ".aegis-migration-*")
		if err != nil {
			return err
		}
		name := filepath.Base(f.Name())
		defer root.Remove(name)
		if err = f.Chmod(0600); err == nil {
			_, err = f.Write(plan.unit)
		}
		if err == nil {
			err = f.Sync()
		}
		err = errors.Join(err, f.Close())
		if err != nil {
			return err
		}
		current, err = Preview(plan.Executable, plan.ConfigPath)
		if err != nil {
			return err
		}
		if !samePlan(current, plan) {
			return ErrForeignUnit
		}
		if err = inspectUnit(plan.UnitPath, plan.legacyUnit); err != nil {
			return err
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		if err = root.Rename(name, filepath.Base(plan.UnitPath)); err != nil {
			return err
		}
		dir, err := root.Open(".")
		if err != nil {
			return err
		}
		err = dir.Sync()
		dir.Close()
		if err != nil {
			return err
		}
	}
	if err = runner.Run(ctx, "daemon-reload"); err != nil {
		return err
	}
	current, err = Preview(plan.Executable, plan.ConfigPath)
	if err != nil {
		return err
	}
	if !samePlan(current, plan) {
		return ErrForeignUnit
	}
	if err = inspectUnit(plan.UnitPath, plan.unit); err != nil {
		return err
	}
	return validateLoadedIdentity(ctx, runner, plan)
}

// StopBudget includes the supervisor's full drain/flush budget plus time for
// manager readback. Callers may still impose a shorter explicit cancellation.
func StopBudget(plan Plan) time.Duration { return plan.StopTimeout + 10*time.Second }
