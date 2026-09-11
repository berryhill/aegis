package api

import (
	"context"
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
	for _, name := range []string{"matched", "no-match", "ambiguous", "disabled-stanza", "runtime-mismatch", "digest-mismatch", "missing-charter", "disabled", "retired", "historical"} {
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
			if name == "ambiguous" {
				charter.Stanzas[1].Authentication.Selectors[0].SubjectIDs = []string{subject.ID}
			}
			if name == "disabled-stanza" {
				charter.Stanzas[0].Enabled = false
			}
			canonical, err := core.Canonicalize(charter)
			if name == "ambiguous" {
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
			if name == "runtime-mismatch" {
				selected.Revision.Runtime.Target = "different-target"
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
			record, err := consoleAgentDetail(ctx, svc, subject, selected, latest, app.FleetSurface{})
			if err != nil {
				t.Fatal(err)
			}
			detail := record.Agent
			switch name {
			case "digest-mismatch", "missing-charter", "runtime-mismatch":
				if detail.AuthorityState != "unavailable" || len(detail.Stanzas) != 0 || len(detail.AuthorityFields) != 0 {
					t.Fatalf("unverified charter exposed authority: %+v", detail)
				}
			case "no-match", "ambiguous", "disabled-stanza":
				if detail.AuthorityState != "denied" || len(detail.AuthorityFields) != 0 {
					t.Fatalf("no-match exposed grant: %+v", detail)
				}
			default:
				if detail.AuthorityState != "matched" || len(detail.Stanzas) != 2 {
					t.Fatalf("exact single-stanza evaluation missing: %+v", detail)
				}
				for _, field := range detail.AuthorityFields {
					if field.Label == "Tools" && field.Value != "file" {
						t.Fatalf("permissions unioned: %+v", field)
					}
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
			if strings.Contains(record.Readiness, "Ready") {
				t.Fatal("read-only projection claimed execution readiness")
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
		})
	}
}
