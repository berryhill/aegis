package userservice

import (
	"context"
	"errors"
	"fmt"
	"github.com/berryhill/aegis/internal/config"
	"strings"
	"time"
)

// EnsureUpgradeReadiness belongs to an admitted upgrade, not read-only status.
// It may deliver narrowly classified pending audit through the owning service.
func EnsureUpgradeReadiness(ctx context.Context, runner Runner, plan Plan, timeout time.Duration) error {
	if err := PreflightStop(ctx, runner, plan); err != nil {
		return err
	}
	if err := inspectUnit(plan.UnitPath, plan.unit); err != nil {
		return err
	}
	cfg, err := config.Load(plan.ConfigPath, nil)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return waitServiceReady(ctx, runner, cfg)
}

// Watch the service while readiness is pending: a crashed process is not an
// audit-delivery timeout. Context cancellation bounds both manager calls and HTTP.
func waitServiceReady(ctx context.Context, runner Runner, cfg config.Config) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	ready := make(chan error, 1)
	go func() { ready <- waitReady(ctx, cfg) }()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case err := <-ready:
			return err
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			output, err := runner.Output(ctx, "show", UnitName, "--property", "ActiveState", "--value")
			if err != nil {
				return err
			}
			state := strings.TrimSpace(string(output))
			if state != "active" {
				status, statusErr := runner.Output(ctx, "show", UnitName, "--property", "ExecMainStatus", "--value")
				result, resultErr := runner.Output(ctx, "show", UnitName, "--property", "Result", "--value")
				return errors.Join(fmt.Errorf("gateway exited during startup: state=%q result=%q exit_status=%q", state, strings.TrimSpace(string(result)), strings.TrimSpace(string(status))), statusErr, resultErr)
			}
		}
	}
}
