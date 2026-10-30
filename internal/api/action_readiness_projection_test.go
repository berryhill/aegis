package api

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/orchestration"
	consoleweb "github.com/berryhill/aegis/web/console"
)

func TestConsoleReadinessCredentialCollectionProjection(t *testing.T) {
	for _, state := range []string{"empty", "ready", "locked", "unconfigured", "unavailable"} {
		t.Run(state, func(t *testing.T) {
			authoritative := state == "empty" || state == "ready"
			surface := app.FleetSurface{Readiness: map[string]app.SurfaceReadiness{"credentials": {State: state, Authoritative: authoritative, ReasonCode: "credentials_" + state}}}
			actions := consoleActions(surface, consoleCredentials)
			if len(actions) != 2 {
				t.Fatal(actions)
			}
			for _, action := range actions {
				if action.ReasonCode == "action_readiness_missing" {
					t.Fatal("known credential collection lost")
				}
				if (action.State == "ready") != authoritative {
					t.Fatal(action)
				}
			}
			var out bytes.Buffer
			if err := consoleweb.ActionReadiness(actions).Render(context.Background(), &out); err != nil {
				t.Fatal(err)
			}
			if authoritative && !strings.Contains(out.String(), "not permission to create") {
				t.Fatal(out.String())
			}
			if !authoritative && strings.Contains(out.String(), "href=") {
				t.Fatal("invented custody/setup link")
			}
		})
	}
}

func TestConsoleReadinessKeepsCanonicalAdmissionUnchanged(t *testing.T) {
	original := orchestration.Readiness{Action: orchestration.FleetActionLoopPublish, State: orchestration.ReadinessDenied, ReasonCode: "authority_context_required", RepairActions: []orchestration.RepairAction{orchestration.RepairSelectAuthority}}
	surface := app.FleetSurface{Actions: map[string]orchestration.Readiness{"loop_publish": original}}
	actions := consoleActions(surface, consoleLoops)
	var out bytes.Buffer
	if err := consoleweb.ActionReadiness(actions).Render(context.Background(), &out); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`data-state="context_required"`, `data-admission-state="denied"`, `href="/console/loops/compose"`} {
		if !strings.Contains(out.String(), want) {
			t.Fatal(want)
		}
	}
	if got := surface.Actions["loop_publish"]; got.State != original.State || got.ReasonCode != original.ReasonCode || got.RepairActions[0] != original.RepairActions[0] {
		t.Fatal("presentation changed admission")
	}
}
