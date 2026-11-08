package app

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/reference"
)

func TestContributionTransportValidationAndDecisionView(t *testing.T) {
	for _, raw := range []string{"https://github.com/owner/repo/issues/1", "https://github.com/owner/repo/issues/1?forged=1", "http://github.com/owner/repo/issues/1", "https://foreign.invalid/owner/repo/issues/1", "https://github.com/owner/repo/pull/1", ""} {
		_, expected := contribution.ParseIssueURL(raw)
		if got := ValidateContributionIssueURL(raw); (got == nil) != (expected == nil) {
			t.Fatalf("canonical issue validation drift for %q: %v / %v", raw, got, expected)
		}
	}
	for _, id := range []string{"loop", "../forged", ""} {
		for _, revision := range []uint64{0, 1} {
			for _, digest := range []string{"sha256:" + strings.Repeat("a", 64), "bad"} {
				expected := reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: id, Revision: revision, Digest: digest}
				got, err := NewContributionLoopReference(id, revision, digest)
				if got != expected || (err == nil) != (expected.Validate() == nil) {
					t.Fatal("reference facade drift", got, err)
				}
			}
		}
	}
	status := ContributionEngineeringStatus{}
	if status.DecisionView() != nil {
		t.Fatal("invented decision")
	}
	d := contribution.Decision{Stage: "prepare", Role: "builder", PlanID: "plan", PlanDigest: "plan-digest", Candidate: "candidate", ContractDigest: "contract", Status: "unavailable", ExpiresAt: time.Unix(100, 0).UTC()}
	d.Binding.Agent = reference.RevisionRef{ID: "agent", Revision: 2, Digest: "agent-digest"}
	d.Binding.Charter = reference.RevisionRef{ID: "charter", Revision: 3, Digest: "charter-digest"}
	status.Decision = &d
	before := d
	view := status.DecisionView()
	want := &ContributionPreparationDecisionView{Stage: "prepare", Role: "builder", Agent: "agent revision 2 · agent-digest", Charter: "charter revision 3 · charter-digest", PlanID: "plan", PlanDigest: "plan-digest", Candidate: "candidate", ContractDigest: "contract", Status: "unavailable", ExpiresAt: d.ExpiresAt}
	if !reflect.DeepEqual(view, want) || !reflect.DeepEqual(d, before) {
		t.Fatalf("allowlisted projection drift: %+v", view)
	}
	view.Status = "approved"
	if d.Status != "unavailable" {
		t.Fatal("transport DTO mutated canonical decision")
	}
}
