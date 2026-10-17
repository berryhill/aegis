package userservice

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

type statusRunner struct{ recordingRunner }

func (r *statusRunner) Output(ctx context.Context, args ...string) ([]byte, error) {
	if len(args) > 0 && args[0] == "is-active" {
		return []byte(r.activeState + "\n"), nil
	}
	return r.recordingRunner.Output(ctx, args...)
}

func TestStatusSeparatesActivityFromAuthenticatedReadiness(t *testing.T) {
	// Do not contact the host login manager.
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "loginctl"), []byte("#!/bin/sh\nprintf 'no\\n'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	for _, tc := range []struct {
		name     string
		code     int
		body     string
		active   string
		mismatch bool
		missing  bool
		want     GatewayState
	}{
		{"healthy", 200, `{"status":"ready","audit":{"current":true,"verifiable":true}}`, "active", false, false, GatewayHealthy},
		{"pending", 503, `{"status":"not_ready","audit":{"current":false,"verifiable":true,"pending":1}}`, "active", false, false, GatewayUnhealthy},
		{"denied", http.StatusUnauthorized, `{}`, "active", false, false, GatewayUnhealthy},
		{"stopped", 200, `{}`, "inactive", false, false, GatewayStopped},
		{"mismatched", 200, `{}`, "active", true, false, GatewayMismatched},
		{"missing", 200, `{}`, "inactive", false, true, GatewayNotInstalled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plan, stop := readyServicePlan(t, tc.code, tc.body)
			defer stop()
			if !tc.missing {
				if err := os.MkdirAll(filepath.Dir(plan.UnitPath), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(plan.UnitPath, plan.unit, 0600); err != nil {
					t.Fatal(err)
				}
			}
			runner := &statusRunner{recordingRunner{activeState: tc.active, fragmentPath: plan.UnitPath, execStart: loadedExecStartFixture(plan.Executable, plan.ConfigPath)}}
			if tc.mismatch {
				runner.execStart = loadedExecStartFixture("/foreign", plan.ConfigPath)
			}
			result, err := Status(context.Background(), runner, plan)
			if err != nil {
				t.Fatal(err)
			}
			if result["active"] != tc.active || result["ready"] != (tc.want == GatewayHealthy) || result["readiness_state"] != tc.want || result["readiness_reason"] == "" {
				t.Fatalf("status = %+v", result)
			}
			for _, call := range runner.calls {
				if call[0] != "show" {
					t.Fatalf("status mutated service: %v", call)
				}
			}
		})
	}
}
