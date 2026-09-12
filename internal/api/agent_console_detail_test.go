package api

import (
	"context"
	"maps"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/reference"
	"github.com/berryhill/aegis/internal/registry"
)

func TestAgentDetailExactAuthorityEvidenceAndReadOnlyBoundary(t *testing.T) {
	for _, name := range []string{"matched", "no-match", "ambiguous-declaration-rejected-before-storage", "disabled-stanza", "runtime-mismatch", "adapter-mismatch", "runtime-name-mismatch", "digest-mismatch", "charter-id-mismatch", "charter-revision-mismatch", "missing-charter", "disabled", "retired", "historical"} {
		t.Run(name, func(t *testing.T) {
			svc := apiService(t)
			configureAPIFleet(t, svc)
			ctx := context.Background()
			data, err := os.ReadFile("../../examples/office-charter.json")
			if err != nil {
				t.Fatal(err)
			}
			charter, err := core.DecodeCharter(strings.NewReader(string(data)))
			if err != nil {
				t.Fatal(err)
			}
			subject := core.Subject{ID: "local-uid:1000", Kind: "human", PrincipalID: svc.Config.Principal.ID, Issuer: "local-os", Method: "local-os", AuthenticatedAt: svc.Now(), ExpiresAt: svc.Now().Add(time.Minute)}
			charter.Stanzas[0].Authentication.Selectors[0].SubjectIDs = []string{subject.ID}
			if name == "no-match" {
				charter.Stanzas[0].Authentication.Selectors[0].SubjectIDs = []string{"local-uid:9999"}
			}
			if name == "ambiguous-declaration-rejected-before-storage" {
				charter.Stanzas[1].Authentication.Selectors[0].SubjectIDs = []string{subject.ID}
			}
			if name == "disabled-stanza" {
				charter.Stanzas[0].Enabled = false
			}
			canonical, err := core.Canonicalize(charter)
			if name == "ambiguous-declaration-rejected-before-storage" {
				// Ambiguous declarations are rejected before storage, rather than
				// becoming a detail view that could union their permissions.
				if err == nil || !strings.Contains(err.Error(), "overlaps") {
					t.Fatalf("ambiguous charter was not rejected: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if name != "missing-charter" {
				if err := svc.Store.SaveCharter(canonical); err != nil {
					t.Fatal(err)
				}
			}
			selected := app.FleetAgent{Revision: registry.AgentRevision{AgentID: "office", Revision: 1, Digest: "sha256:" + strings.Repeat("a", 64), Lifecycle: registry.LifecycleEnabled, Charter: reference.RevisionRef{ID: "office", Revision: 1, Digest: canonical.Digest}, Runtime: registry.RuntimeBinding{Adapter: "hermes", Runtime: "hermes-agent", Target: "aegis-owned-ephemeral"}}}
			if name == "digest-mismatch" {
				selected.Revision.Charter.Digest = "sha256:" + strings.Repeat("b", 64)
			}
			if name == "charter-id-mismatch" {
				selected.Revision.Charter.ID = "another-charter"
			}
			if name == "charter-revision-mismatch" {
				selected.Revision.Charter.Revision = 2
			}
			if name == "runtime-mismatch" {
				selected.Revision.Runtime.Target = "different-target"
			}
			if name == "adapter-mismatch" {
				selected.Revision.Runtime.Adapter = "different-adapter"
			}
			if name == "runtime-name-mismatch" {
				selected.Revision.Runtime.Runtime = "different-runtime"
			}
			if name == "disabled" {
				selected.Revision.Lifecycle = registry.LifecycleDisabled
			}
			if name == "retired" {
				selected.Revision.Lifecycle = registry.LifecycleRetired
			}
			latest := selected
			if name == "historical" {
				latest.Revision.Revision = 2
				latest.Revision.Digest = "sha256:" + strings.Repeat("c", 64)
			}
			before, err := svc.AuditEventsAs(subject)
			if err != nil {
				t.Fatal(err)
			}
			stateBefore := agentReadOnlyState(t, svc)
			record, err := consoleAgentDetail(ctx, svc, subject, selected, latest, app.FleetSurface{})
			if err != nil {
				t.Fatal(err)
			}
			detail := record.Agent
			switch name {
			case "digest-mismatch", "charter-id-mismatch", "charter-revision-mismatch", "missing-charter", "runtime-mismatch", "adapter-mismatch", "runtime-name-mismatch":
				if detail.AuthorityState != "unavailable" || len(detail.Stanzas) != 0 || len(detail.AuthorityFields) != 0 {
					t.Fatalf("unverified charter exposed authority: %+v", detail)
				}
			case "no-match", "disabled-stanza":
				if detail.AuthorityState != "denied" || len(detail.AuthorityFields) != 0 {
					t.Fatalf("no-match exposed grant: %+v", detail)
				}
			default:
				if detail.AuthorityState != "matched" || len(detail.Stanzas) != 2 {
					t.Fatalf("exact single-stanza evaluation missing: %+v", detail)
				}
				// A missing effective field must not vacuously pass the no-union check.
				fields := make(map[string]string)
				for _, field := range detail.AuthorityFields {
					if _, exists := fields[field.Label]; exists {
						t.Fatalf("duplicate authority field: %s", field.Label)
					}
					fields[field.Label] = field.Value
					if field.Label == "Tools" && field.Value != "file" {
						t.Fatalf("permissions unioned: %+v", field)
					}
				}
				// Bind every field to the selected declaration, not just Tools.
				wantFields := map[string]string{
					"Selected stanza":                     charter.Stanzas[0].ID,
					"Authentication":                      agentPolicyJSON(charter.Stanzas[0].Authentication),
					"Capabilities":                        "interactive-chat, local-file-access",
					"Tools":                               "file",
					"Memory scopes":                       "office-principal",
					"Credential scopes (references only)": "provider:openai",
					"Session lifetime and delegation":     agentPolicyJSON(charter.Stanzas[0].Session),
					"Approvals":                           agentPolicyJSON(charter.Stanzas[0].Approval),
				}
				if !maps.Equal(fields, wantFields) {
					t.Fatalf("missing or incorrect effective authority fields: %+v", fields)
				}
			}
			if name == "historical" && (!detail.Historical || detail.LifecycleEligible) {
				t.Fatal("historical revision exposed mutation")
			}
			if name == "retired" && detail.LifecycleEligible {
				t.Fatal("retired revision exposed mutation")
			}
			if !strings.Contains(detail.ProvisioningEvidence, "No provisioning receipt") {
				t.Fatal("missing receipt was not explicit")
			}
			wantReadiness := "Current readiness unavailable: runtime compatibility, provisioning state and fresh execution admission are not established by this read."
			switch name {
			case "no-match", "disabled-stanza":
				wantReadiness = "Blocked: authenticated operator authority denied. Fresh execution admission remains required."
			case "historical":
				wantReadiness = "Historical revision: not eligible for lifecycle changes or current execution through this detail."
			case "disabled":
				wantReadiness = "Disabled: execution denied until a new enabled revision."
			case "retired":
				wantReadiness = "Retired: terminal Registry lifecycle; execution denied."
			}
			if record.Readiness != wantReadiness {
				t.Fatalf("incorrect read-only readiness: got %q, want %q", record.Readiness, wantReadiness)
			}
			after, err := svc.AuditEventsAs(subject)
			if err != nil {
				t.Fatal(err)
			}
			if len(before) != len(after) {
				t.Fatal("read-only detail emitted consequential audit events")
			}
			sessions, err := svc.ListSessions()
			if err != nil || len(sessions) != 0 {
				t.Fatalf("detail created sessions: %v", err)
			}
			subject.PrincipalID = "not-the-configured-operator"
			if _, err := consoleAgentDetail(ctx, svc, subject, selected, latest, app.FleetSurface{}); err == nil {
				t.Fatal("non-operator accepted")
			}
			subject.PrincipalID = svc.Config.Principal.ID
			subject.ExpiresAt = svc.Now().Add(-time.Second)
			if _, err := consoleAgentDetail(ctx, svc, subject, selected, latest, app.FleetSurface{}); err == nil {
				t.Fatal("expired operator accepted")
			}
			if agentReadOnlyState(t, svc) != stateBefore {
				t.Fatal("detail success or denial mutated canonical authority or fleet evidence")
			}
		})
	}
}
