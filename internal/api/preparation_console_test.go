package api

import (
	"bytes"
	"context"
	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/queue"
	consoleweb "github.com/berryhill/aegis/web/console"
	"strings"
	"testing"
)

func TestPreparationNativeCollectionAndDetail(t *testing.T) {
	for _, state := range []queue.State{queue.StatePreparationPending, queue.StateCancelled} {
		v := app.QueueExecutionView{Item: queue.Item{ItemID: "preparation-native", State: queue.StateAwaitingRuntime}, Projection: queue.Projection{State: state}}
		model, err := consoleSurfaceModel(app.FleetSurface{Preparations: []app.QueueExecutionView{v}, Readiness: map[string]app.SurfaceReadiness{"preparations": {State: "ready", Authoritative: true}}}, consolePreparations)
		if err != nil {
			t.Fatal(err)
		}
		for _, detail := range []bool{false, true} {
			if detail {
				model.InspectorOpen = true
				model.Inspector = &model.Records[0]
			}
			var out bytes.Buffer
			if err := consoleweb.Workspace(model).Render(context.Background(), &out); err != nil {
				t.Fatal(err)
			}
			html := out.String()
			if !strings.Contains(html, "preparation-native") || strings.Contains(html, "credential-workspace") {
				t.Fatalf("wrong native renderer: %s", html)
			}
			if !strings.Contains(html, "/console/preparations") {
				t.Fatal("preparation route lost")
			}
			if detail && (!strings.Contains(html, "Initial state (historical)") || !strings.Contains(html, "Preparation recorded")) {
				t.Fatal("historical detail missing")
			}
		}
		fields := model.Records[0].Queue.Admission
		for _, f := range fields {
			if f.Label == "State" && f.Value != string(state) {
				t.Fatal("initial state presented as current")
			}
		}
	}
	model, err := consoleSurfaceModel(app.FleetSurface{Readiness: map[string]app.SurfaceReadiness{"preparations": {State: "empty", Authoritative: true}}}, consolePreparations)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := consoleweb.Workspace(model).Render(context.Background(), &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "No records") || !strings.Contains(out.String(), "Not executable admission") {
		t.Fatal("missing native empty preparation")
	}
}
