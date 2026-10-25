//go:build linux

package app

import (
	"bytes"
	"log/slog"
	"reflect"
	"strings"
	"testing"

	"github.com/berryhill/aegis/internal/config"
	"github.com/berryhill/aegis/internal/orchestration"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	hermesruntime "github.com/berryhill/aegis/internal/runtime/hermes"
)

type installedStartupRepository struct{ fleet.Repository }

// Synthetic startup wiring only: nil service adapter makes discovery unavailable
// before examining installed dependencies. No model or native process is run.
func TestConfigureFleetPreparesInstalledDoerWithNativeTrust(t *testing.T) {
	var logs bytes.Buffer
	native := config.Implementation{GoBinary: "/synthetic/trusted-go", AuthorizedContracts: []string{"sha256:67b258b972d3264b4e93f2bacfac500c5c52a7a9815322e2aefb475674a7da58"}}
	s := &Service{Config: config.Config{StateDir: "/synthetic/state", Implementation: native}, Log: slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))}
	w := &orchestration.QueueWorker{}
	if err := w.ConfigureImplementation(native, s.Config.StateDir, &hermesruntime.Adapter{}); err != nil {
		t.Fatal(err)
	}
	if err := s.ConfigureFleet(&installedStartupRepository{}, &orchestration.FleetService{}, w); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(logs.String(), "qualified_local_dependencies_missing") {
		t.Fatalf("native-only startup did not invoke installed preparation: %s", logs.String())
	}
	if !reflect.DeepEqual(s.Config.Implementation, native) {
		t.Fatal("startup modified owner configuration")
	}
}
