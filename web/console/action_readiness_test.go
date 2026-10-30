package consoleweb

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestActionReadinessProjectionAndDestinations(t *testing.T) {
	tests := []struct {
		name, state, reason, key, heading, url, message string
		repairs                                         []string
	}{
		{name: "no Loop context", state: "denied", reason: "authority_context_required", key: "loop_publish", heading: "Not evaluated for an exact action", url: "/console/loops/compose", message: "does not mean initial setup is incomplete"},
		{name: "no Graph context", state: "denied", reason: "authority_context_required", key: "graph_publish", heading: "Not evaluated for an exact action", url: "/console/graphs/compose"},
		{name: "no submission context", state: "denied", reason: "authority_context_required", key: "submission", url: "/console/graphs/run"},
		{name: "unsupported context", state: "denied", reason: "authority_context_required", key: "runtime_effect", message: "No Console preparation destination"},
		{name: "ready", state: "ready", reason: "ready", heading: "Ready for the evaluated context", message: "confirmation repeats fresh"},
		{name: "denied", state: "denied", reason: "subject_not_authorized", heading: "Denied", message: "does not establish missing credentials"},
		{name: "inactive not guessed", state: "denied", reason: "authority_inactive_or_inconsistent", message: "does not distinguish expiry, revocation"},
		{name: "expired", state: "denied", reason: "authority_expired", heading: "Expired authority", message: "Browsing does not renew"},
		{name: "revoked", state: "denied", reason: "authority_revoked", heading: "Revoked authority", message: "Refreshing does not restore"},
		{name: "missing setup", state: "denied", reason: "agent_not_enabled", heading: "Agent prerequisite not met", url: "/console/agents"},
		{name: "missing definition", state: "empty", reason: "definition_not_found", heading: "Required definition not found", url: "/console/loops", repairs: []string{"publish_exact_loop_revision"}},
		{name: "unavailable", state: "unavailable", reason: "fleet_store_unavailable", message: "Unavailable is not denied"},
		{name: "unknown", state: "denied", reason: "unknown_action", heading: "Operation not supported", message: "not supported here"},
		{name: "repair", state: "degraded_repair_required", reason: "fleet_store_corrupt", message: "Recovery is not supported"},
		{name: "empty credentials prepare", state: "ready", reason: "credential_authority_ready", key: "prepare_credential", heading: "Credential metadata available", url: "/console/credentials/operation?operation=create", message: "not permission to create"},
		{name: "backup unsupported", state: "ready", reason: "credential_authority_ready", key: "prepare_vault_backup", message: "No Console backup"},
		{name: "locked", state: "locked", reason: "credentials_authority_locked", message: "does not block credential-independent"},
		{name: "unconfigured", state: "unconfigured", reason: "credentials_authority_not_configured", message: "secret initialize"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			action := ActionModel{Label: "<script>canary</script>", Key: tt.key, State: tt.state, ReasonCode: tt.reason, RepairActions: tt.repairs}
			before := action
			explanation := explainAction(action)
			if explanation.URL != tt.url {
				t.Fatalf("destination %q want %q", explanation.URL, tt.url)
			}
			var out bytes.Buffer
			if err := ActionReadiness([]ActionModel{action}).Render(context.Background(), &out); err != nil {
				t.Fatal(err)
			}
			body := out.String()
			for _, want := range []string{"Action readiness", tt.heading, tt.message, tt.reason, `data-admission-state="` + tt.state + `"`, "Technical readiness details"} {
				if !strings.Contains(body, want) {
					t.Errorf("missing %q: %s", want, body)
				}
			}
			if strings.Contains(body, "<script>") || strings.Contains(body, "Finish setup") || strings.Contains(body, "Records stay inspectable") {
				t.Fatal("unsafe or misleading copy")
			}
			if tt.url == "" && strings.Contains(body, "href=") {
				t.Fatal("invented repair link")
			}
			if tt.reason == "authority_context_required" {
				if explanation.State != "context_required" || strings.Contains(body, `data-state="denied"`) {
					t.Fatal("missing context shown as denial")
				}
			}
			if action.State != before.State || action.ReasonCode != before.ReasonCode {
				t.Fatal("projection mutated admission")
			}
		})
	}
}

func TestActionReadinessEmptyRendersNothing(t *testing.T) {
	var out bytes.Buffer
	if err := ActionReadiness(nil).Render(context.Background(), &out); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out.String()) != "" {
		t.Fatal(out.String())
	}
}

func TestActionReadinessLoopPreparationOnReadableCollection(t *testing.T) {
	for _, authoritative := range []bool{true, false} {
		var out bytes.Buffer
		surface := SurfaceModel{Domain: DomainLoops, Title: "Loops", State: "empty", Authoritative: authoritative, Actions: []ActionModel{{Key: "loop_publish", Label: "Publish Loop revision", State: "denied", ReasonCode: "authority_context_required", Primary: true}}}
		if err := Workspace(surface).Render(context.Background(), &out); err != nil {
			t.Fatal(err)
		}
		body := out.String()
		want := `class="primary" href="/console/loops/compose"`
		if strings.Contains(body, want) != authoritative {
			t.Fatalf("preparation control read boundary %t: %s", authoritative, body)
		}
	}
}
