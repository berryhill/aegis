package api

import (
	"strings"
	"testing"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/graph"
	"github.com/berryhill/aegis/internal/reference"
)

func TestGraphWorkspaceProjectsExactHistoryMappingsAndDenials(t *testing.T) {
	revision := graph.GraphRevision{GraphID: "g", Revision: 1, Digest: "sha256:old", Nodes: []graph.Node{{ID: "a", Participant: reference.RevisionRef{ID: "agent", Revision: 7, Digest: "sha256:agent"}, Loop: reference.RevisionRef{ID: "loop", Revision: 4, Digest: "sha256:loop"}}}, InputMappings: []graph.InputMapping{{GraphInput: "brief", ToNodeID: "a", ToPort: "input"}}, OutputMappings: []graph.OutputMapping{{FromNodeID: "a", FromPort: "artifact", GraphOutput: "result"}}}
	view := app.GraphView{Revision: revision, Lifecycle: graph.Lifecycle{State: graph.LifecycleActive, ActiveRevision: 2, ActiveDigest: "sha256:new"}, Validations: []graph.GraphValidationResult{{GraphID: "other", Revision: 1, RevisionDigest: "sha256:old", Outcome: graph.ValidationValid, Digest: "wrong-record"}, {GraphID: "g", Revision: 1, RevisionDigest: "sha256:old", Outcome: graph.ValidationValid, Digest: "stored-result"}}}
	r := consoleGraphRecord(view, app.SubmissionHistory{}, "{}", []app.GraphView{view, {Revision: graph.GraphRevision{GraphID: "g", Revision: 2}}})
	if r.Graph.GraphID != "g" || r.Graph.LatestVersion != "r2 · viewing historical revision" || r.Lifecycle != "inactive" {
		t.Fatalf("historical projection: %+v", r)
	}
	if !strings.Contains(r.Graph.Validation, "stored-result") || strings.Contains(r.Graph.Validation, "wrong-record") {
		t.Fatal("stored result not bound to exact revision")
	}
	if !strings.HasPrefix(r.Graph.CurrentValidation, "invalid") || len(r.Graph.ValidationIssues) == 0 {
		t.Fatal("current structural failures hidden")
	}
	codes := map[string]bool{}
	for _, issue := range r.Graph.SubmissionIssues {
		codes[issue.Code] = true
	}
	for _, want := range []string{"graph.invalid", "graph.inactive_revision", "admission.not_evaluated"} {
		if !codes[want] {
			t.Errorf("missing %s", want)
		}
	}
	node := r.Graph.Nodes[0]
	if len(node.Links) != 2 || !strings.Contains(node.Links[1].URL, "loop%3A4") || !strings.Contains(node.Links[0].URL, "7") {
		t.Fatalf("unpinned links: %+v", node.Links)
	}
	if len(node.InputMappings) != 1 || node.InputMappings[0].Value != "input" || len(node.OutputMappings) != 1 || node.OutputMappings[0].Value != "result" {
		t.Fatal("mappings lost")
	}
	if r.Key != "g:1" || r.Graph.Digest != "sha256:old" {
		t.Fatal("historical selection upgraded")
	}
}
