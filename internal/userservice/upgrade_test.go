package userservice

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type cancellingUpgradeRunner struct {
	*recordingRunner
	cancel context.CancelFunc
}

func (r cancellingUpgradeRunner) Output(ctx context.Context, args ...string) ([]byte, error) {
	out, err := r.recordingRunner.Output(ctx, args...)
	if len(args) > 3 && args[3] == "ExecStart" {
		r.cancel()
	}
	return out, err
}

func TestUpgradeExactStop(t *testing.T) {
	for _, name := range []string{"healthy", "stopped", "foreign", "unavailable", "cancelled", "cancelled_after_identity"} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "config"))
			exe, cfg := serviceFixture(t)
			plan, err := Preview(exe, cfg)
			if err != nil {
				t.Fatal(err)
			}
			if err = os.MkdirAll(filepath.Dir(plan.UnitPath), 0700); err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(plan.UnitPath, plan.unit, 0600); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(exe)
			if err != nil {
				t.Fatal(err)
			}
			recorded := &recordingRunner{activeState: "active", fragmentPath: plan.UnitPath, execStart: loadedExecStartFixture(exe, cfg)}
			var runner Runner = recorded
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch name {
			case "stopped":
				recorded.activeState = "inactive"
			case "foreign":
				recorded.execStart = loadedExecStartFixture("/foreign", cfg)
			case "unavailable":
				runner = nil
			case "cancelled":
				cancel()
			}
			err = PreflightStop(ctx, runner, plan)
			if name == "cancelled_after_identity" {
				runner = cancellingUpgradeRunner{recorded, cancel}
			}
			if err == nil {
				_, err = Action(ctx, runner, plan, "stop", time.Second)
			}
			good := name == "healthy" || name == "stopped"
			mutations := 0
			for _, call := range recorded.calls {
				if call[0] == "stop" {
					mutations++
				}
			}
			if (err == nil) != good || (mutations != 0) != good {
				t.Fatalf("err=%v mutations=%d calls=%v", err, mutations, recorded.calls)
			}
			if name == "cancelled_after_identity" && !errors.Is(err, context.Canceled) {
				t.Fatalf("lost cancellation: %v", err)
			}
			after, err := os.ReadFile(exe)
			if err != nil || string(before) != string(after) {
				t.Fatal("executable mutated")
			}
			unit, err := os.ReadFile(plan.UnitPath)
			if err != nil || string(unit) != string(plan.unit) {
				t.Fatal("unit mutated")
			}
		})
	}
}
