package loop

import (
	"encoding/json"
	"github.com/berryhill/aegis/internal/reference"
	"strings"
	"testing"
)

func TestContributionURLOnlySuccessorAndHistoricalBytes(t *testing.T) {
	ref := reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: "agent", Revision: 1, Digest: "sha256:" + strings.Repeat("a", 64)}
	p := ContributionProfile{Controller: ref, Builder: ref, Tester: ref, Repository: "owner/repo", RepositoryID: 7, Base: strings.Repeat("b", 40), ReadAction: "github.read_contribution_issue.v1", DeadlineSeconds: 60, Invariants: []string{"preserve"}, Exclusions: []string{"no widening"}}
	c := ImplementationDraft("task", "acceptance")
	c.Workspace = t.TempDir()
	c.WritableFiles = []string{"value.go"}
	c.Policy.RequiredTests = []RequiredGoTest{{Package: "fixture", Name: "TestValue"}}
	old, _, err := NewContributionRevision("contribution", 1, "", c, p)
	if err != nil {
		t.Fatal(err)
	}
	before, _ := json.Marshal(old)
	if len(old.Inputs) != 0 {
		t.Fatal("legacy changed")
	}
	p.Engineering = &IssueEngineeringPolicy{WritablePrefixes: []string{"internal"}, MaxWritableFiles: 4}
	p.FinalVerification = "aegis.contribution.final.v1"
	m := &ContributionExecutionPolicy{Kind: EngineeringExecutionKind, ReviewMode: "issue-journeys.v1", RepositoryPath: c.Workspace, GitBinary: "/usr/bin/git", BaseBranch: "main", BuildPackage: "."}
	for _, kind := range []string{"behavior", "security", "launch", "denial"} {
		test := RequiredGoTest{Package: "fixture", Name: "Test" + strings.ToUpper(kind[:1]) + kind[1:]}
		m.Criteria = append(m.Criteria, ContributionCriterion{ID: kind, Kind: kind, Test: test})
	}
	for _, kind := range []string{"security", "launch"} {
		m.ReviewAssets = append(m.ReviewAssets, ContributionReviewAsset{ID: kind, Scope: kind, CriterionID: kind, Path: "trusted_test.go", BaselineDigest: ref.Digest})
	}
	p.Managed = m
	next, _, err := NewContributionRevision(old.LoopID, 2, old.Digest, c, p)
	if err != nil {
		t.Fatal(err)
	}
	if len(next.Inputs) != 1 || next.Inputs[0] != (Port{ID: "github_issue_url", Type: "string", Required: true}) {
		t.Fatal("URL-only missing")
	}
	if next.PreviousDigest != old.Digest {
		t.Fatal("history broken")
	}
	for _, bad := range []Port{{ID: "github_issue_url", Type: "string"}, {ID: "workspace", Type: "string", Required: true}, {ID: "github_issue_url", Type: "object", Required: true}} {
		v := next
		v.Inputs = []Port{bad}
		if validContributionInputs(v) {
			t.Fatal("accepted", bad)
		}
	}
	// Previously accepted zero-input Engineering v6 remains digestable and valid;
	// authoring uses the additive URL contract, without rewriting history.
	legacy := next
	legacy.Inputs = nil
	legacy.Steps = append([]Step(nil), next.Steps...)
	for i := range legacy.Steps {
		if legacy.Steps[i].ID == legacy.EntryStepID {
			legacy.Steps[i].InputPorts = nil
		}
	}
	if _, _, err = NewRevision(legacy); err != nil {
		t.Fatal("historical engineering denied", err)
	}
	if ValidateRevision(old).Outcome != ValidationValid {
		t.Fatal("historical validation changed")
	}
	after, _ := json.Marshal(old)
	if string(before) != string(after) {
		t.Fatal("historical bytes changed")
	}
}
