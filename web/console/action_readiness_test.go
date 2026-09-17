package consoleweb

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestActionReadinessSeparatesOperationAuthorityFromSetup(t *testing.T) {
	for _, key := range []string{"loop_publish", "graph_publish", "submission", "claim", "runtime_effect", "evidence_verify", "disposition"} {
		t.Run(key, func(t *testing.T) {
			action := ActionModel{Key: key, Label: key, State: "denied", ReasonCode: "authority_context_required", RepairActions: []string{"select_authority"}}
			var out bytes.Buffer
			if err := ActionReadiness([]ActionModel{action}).Render(context.Background(), &out); err != nil {
				t.Fatal(err)
			}
			body := out.String()
			if strings.Contains(body, "Finish setup") {
				t.Fatal("missing operation authority was presented as unfinished setup")
			}
			for _, want := range []string{"Action prerequisites", "Operation-specific authority", "does not mean initial setup is incomplete", `data-state="denied"`, "authority_context_required", "select_authority"} {
				if !strings.Contains(body, want) {
					t.Errorf("missing %q: %s", want, body)
				}
			}
			if strings.Contains(body, `data-state="ready"`) {
				t.Fatal("missing authority became ready")
			}
		})
	}
}

func TestActionReadinessPreservesRealFailures(t *testing.T) {
	actions := []ActionModel{
		{Label: "Publish", State: "denied", ReasonCode: "authority_context_required"},
		{Label: "Authenticate", State: "denied", ReasonCode: "subject_not_authorized", RepairActions: []string{"authenticate"}},
		{Label: "Read", State: "unavailable", ReasonCode: "fleet_unavailable"},
		{Label: "Repair", State: "degraded_repair_required", ReasonCode: "fleet_corrupt", RepairActions: []string{"repair_store"}},
	}
	var out bytes.Buffer
	if err := ActionReadiness(actions).Render(context.Background(), &out); err != nil {
		t.Fatal(err)
	}
	for _, action := range actions {
		if !strings.Contains(out.String(), action.ReasonCode) || !strings.Contains(out.String(), `data-state="`+action.State+`"`) {
			t.Errorf("failure hidden: %+v", action)
		}
	}
}

func TestActionReadinessReadyDoesNotRenderWarning(t *testing.T) {
	var out bytes.Buffer
	if err := ActionReadiness([]ActionModel{{State: "ready", ReasonCode: "ready"}}).Render(context.Background(), &out); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out.String()) != "" {
		t.Fatalf("ready action rendered warning: %s", out.String())
	}
}
