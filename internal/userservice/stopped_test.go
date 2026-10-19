package userservice

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestRequireStoppedExactAdmission(t *testing.T) {
	for _, kind := range []string{"inactive", "failed", "active", "pid", "pid-change", "foreign-unit", "foreign-loaded", "cancelled"} {
		t.Run(kind, func(t *testing.T) {
			exe, cfg := serviceFixture(t)
			plan, err := Preview(exe, cfg)
			if err != nil {
				t.Fatal(err)
			}
			if err = os.MkdirAll(filepath.Dir(plan.UnitPath), 0700); err != nil {
				t.Fatal(err)
			}
			data := plan.unit
			if kind == "foreign-unit" {
				data = []byte("foreign")
			}
			if err = os.WriteFile(plan.UnitPath, data, 0600); err != nil {
				t.Fatal(err)
			}
			runner := &recordingRunner{fragmentPath: plan.UnitPath, execStart: loadedExecStartFixture(exe, cfg), mainPID: "0", activeState: "inactive"}
			switch kind {
			case "failed":
				runner.activeState = "failed"
			case "active":
				runner.activeState = "active"
			case "pid":
				runner.mainPID = "123"
			case "pid-change":
				runner.nextMainPID = "123"
			case "foreign-loaded":
				runner.fragmentPath = "/foreign/aegis.service"
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if kind == "cancelled" {
				cancel()
			}
			err = RequireStopped(ctx, runner, plan)
			want := kind == "inactive" || kind == "failed"
			if (err == nil) != want {
				t.Fatalf("admission=%v want success=%v", err, want)
			}
			for _, call := range runner.calls {
				if len(call) == 0 || call[0] != "show" {
					t.Fatalf("admission performed mutation: %v", call)
				}
			}
		})
	}
}
