package api

import (
	"bytes"
	"context"
	"strings"
	"testing"

	consoleweb "github.com/berryhill/aegis/web/console"
)

func TestQueueOverviewOmitsGenericPrerequisitesAndPreparation(t *testing.T) {
	actions := []consoleweb.ActionModel{}
	for i, label := range []string{"Prepare execution request", "Claim", "Runtime effect", "Verify evidence", "Disposition"} {
		actions = append(actions, consoleweb.ActionModel{Label: label, Primary: i == 0, State: "denied", ReasonCode: "authority_context_required", RepairActions: []string{"select_exact_authority_context"}})
	}
	var out bytes.Buffer
	err := consoleweb.Document(consoleweb.PageModel{Authenticated: true, Surface: consoleweb.SurfaceModel{Domain: "queue", Title: "Execution Queue", State: "empty", Authoritative: true, Actions: actions}}).Render(context.Background(), &out)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"Action prerequisites", "authority_context_required", "select_exact_authority_context", "Operation-specific authority", "/console/preparations", "Execution preparation", ">Claim<", ">Runtime effect<", ">Verify evidence<", ">Disposition<"} {
		if strings.Contains(out.String(), forbidden) {
			t.Fatalf("Queue overview retained %q", forbidden)
		}
	}
	if !strings.Contains(out.String(), `href="/console/graphs/run"`) || !strings.Contains(out.String(), "Prepare execution request") {
		t.Fatal("existing entry point removed")
	}
}
