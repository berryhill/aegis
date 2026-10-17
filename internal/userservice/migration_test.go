package userservice

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLegacyMigrationExactScopeAndReplay(t *testing.T) {
	for _, mode := range []string{"legacy", "foreign", "cancelled", "wrong-digest", "crash-after-write"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "config"))
			exe, cfg := serviceFixture(t)
			plan, err := Preview(exe, cfg)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(plan.unit), "StartLimitIntervalSec=60s\nStartLimitBurst=3\n") || plan.StopTimeout != 70*time.Second || !strings.Contains(string(plan.unit), "TimeoutStopSec=1m10s") || StopBudget(plan) <= plan.StopTimeout {
				t.Fatalf("unbounded service policy: %s", plan.unit)
			}
			before := plan.legacyUnit
			if mode == "foreign" {
				before = append(append([]byte(nil), before...), []byte("# foreign edit\n")...)
			}
			if mode == "crash-after-write" {
				before = plan.unit
			}
			if err = os.MkdirAll(filepath.Dir(plan.UnitPath), 0700); err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(plan.UnitPath, before, 0600); err != nil {
				t.Fatal(err)
			}
			runner := &recordingRunner{fragmentPath: plan.UnitPath, execStart: "{ path=" + exe + " ; argv[]=" + exe + " serve --config " + cfg + " ; ignore_errors=no ; }"}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			previous := plan.LegacyUnitDigest
			if mode == "cancelled" {
				cancel()
			}
			if mode == "wrong-digest" {
				previous = "foreign"
			}
			if mode == "legacy" {
				if ok, err := Installed(plan); !ok || err != nil {
					t.Fatalf("legacy inspection: %v", err)
				}
				if _, err = Action(ctx, runner, plan, "start", time.Second); !errors.Is(err, ErrForeignUnit) {
					t.Fatalf("legacy activation admitted: %v", err)
				}
				if len(runner.calls) != 0 {
					t.Fatal("activation had manager effects")
				}
				if err = PreflightStop(ctx, runner, plan); err != nil {
					t.Fatal(err)
				}
				runner.calls = nil
			}
			err = MigrateOwnedUnit(ctx, runner, plan, previous)
			actual, readErr := os.ReadFile(plan.UnitPath)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if mode == "legacy" || mode == "crash-after-write" {
				if err != nil || !bytes.Equal(actual, plan.unit) {
					t.Fatalf("migration/replay: %v", err)
				}
				if err = MigrateOwnedUnit(ctx, runner, plan, previous); err != nil {
					t.Fatalf("repeat: %v", err)
				}
			} else {
				if err == nil || !bytes.Equal(actual, before) || len(runner.calls) != 0 {
					t.Fatalf("denial mutated: err=%v calls=%v", err, runner.calls)
				}
			}
		})
	}
}
