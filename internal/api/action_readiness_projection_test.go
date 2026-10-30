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

func TestConsoleReadinessCredentialCustodyDoesNotGateFleetCollections(t *testing.T) {
	for _, custody := range []string{"unconfigured", "locked"} {
		for _, domain := range []consoleDomain{consoleAgents, consoleLoops, consoleGraphs, consoleQueue} {
			t.Run(custody+"/"+string(domain), func(t *testing.T) {
				surface := app.FleetSurface{Readiness: map[string]app.SurfaceReadiness{
					"registry":    {State: "empty", Authoritative: true, ReasonCode: "collection_read_succeeded_empty"},
					"loops":       {State: "empty", Authoritative: true, ReasonCode: "collection_read_succeeded_empty"},
					"graphs":      {State: "empty", Authoritative: true, ReasonCode: "collection_read_succeeded_empty"},
					"queue":       {State: "empty", Authoritative: true, ReasonCode: "collection_read_succeeded_empty"},
					"credentials": {State: custody, ReasonCode: "credentials_" + custody},
				}}
				model, err := consoleSurfaceModel(surface, domain)
				if err != nil {
					t.Fatal(err)
				}
				if !model.Authoritative || model.State != "empty" {
					t.Fatalf("custody gated fleet collection: %+v", model)
				}
				var out bytes.Buffer
				if err := consoleweb.Workspace(model).Render(context.Background(), &out); err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(out.String(), "The authoritative collection is empty.") || strings.Contains(out.String(), "credentials_"+custody) {
					t.Fatal("credential state leaked into fleet readiness")
				}
			})
		}
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
