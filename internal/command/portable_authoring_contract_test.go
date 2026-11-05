package command

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/graph"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/skillbundle"
)

// Exercise only the distributed artifact and public command decoders/builders.
// This is portable schema/builder evidence, not admitted publication or execution.
func TestPortableBundledAuthoringContracts(t *testing.T) {
	root := "../.."
	revision := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	archive := filepath.Join(t.TempDir(), "skills.tar.gz")
	digest, err := skillbundle.BuildArchive(root, archive, "1.2.3", revision)
	if err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	if err := os.Chmod(home, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := skillbundle.InstallArchive(context.Background(), archive, digest, revision, home); err != nil {
		t.Fatal(err)
	}
	base := filepath.Join(home, "skills")
	publication := filepath.Join(t.TempDir(), "publication.json")
	cmd := loopHelloCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{filepath.Join(base, "aegis-loop-authoring/references/hello-draft.v1.json"), "--output", publication})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	var lp app.PublishLoopInput
	if err := decodeJSONFile(publication, &lp); err != nil {
		t.Fatal(err)
	}
	if lp.AgentID == "" || lp.IdempotencyKey == "" {
		t.Fatal("missing workspace publication identities")
	}
	if _, _, err := loop.NewRevision(lp.Revision); err != nil {
		t.Fatal(err)
	}
	var gp app.PublishGraphInput
	if err := decodeJSONFile(filepath.Join(base, "aegis-graph-authoring/references/workspace-publish.v1.json"), &gp); err != nil {
		t.Fatal(err)
	}
	canonical, validation, err := graph.NewRevision(gp.Revision)
	if err != nil || validation.Outcome != graph.ValidationValid {
		t.Fatalf("graph shape: %v %+v", err, validation)
	}
	if gp.AgentID != lp.AgentID || canonical.Nodes[0].Participant.ID != lp.AgentID || canonical.Nodes[0].Loop.ID != lp.Revision.LoopID {
		t.Fatal("portable authoring pair has inconsistent selectors")
	}
	loopPorts, _ := json.Marshal(lp.Revision.Inputs)
	graphPorts, _ := json.Marshal(canonical.Nodes[0].Inputs)
	if !bytes.Equal(loopPorts, graphPorts) {
		t.Fatalf("input interface drift: %s != %s", loopPorts, graphPorts)
	}
	loopPorts, _ = json.Marshal(lp.Revision.Outputs)
	graphPorts, _ = json.Marshal(canonical.Nodes[0].Outputs)
	if !bytes.Equal(loopPorts, graphPorts) {
		t.Fatalf("output interface drift: %s != %s", loopPorts, graphPorts)
	}
	var submission app.SubmitGraphInput
	if err := decodeJSONFile(filepath.Join(base, "aegis-graph-authoring/references/workspace-submit.v1.json"), &submission); err != nil {
		t.Fatal(err)
	}
	if submission.WorkspaceAgentID != gp.AgentID || submission.Graph.ID != canonical.GraphID {
		t.Fatal("publication/submission selectors drifted")
	}
	if _, err := graph.NewRunSnapshot(submission.SnapshotID, canonical, submission.Inputs); err != nil {
		t.Fatalf("bundled submission inputs do not match publication: %v", err)
	}
	if _, err := graph.NewRunSnapshot(submission.SnapshotID, canonical, nil); err == nil {
		t.Fatal("missing required typed inputs accepted")
	}
	// Compare the complete documented response with the real public check command.
	check := NewRoot(Dependencies{})
	var out bytes.Buffer
	check.SetOut(&out)
	check.SetErr(&bytes.Buffer{})
	check.SetArgs([]string{"--config", filepath.Join(t.TempDir(), "absent.yaml"), "graphs", "submit", "--check", filepath.Join(base, "aegis-graph-authoring/references/workspace-submit.v1.json")})
	if err := check.Execute(); err != nil {
		t.Fatal(err)
	}
	var actual, expected map[string]any
	if err := json.Unmarshal(out.Bytes(), &actual); err != nil {
		t.Fatal(err)
	}
	if err := decodeJSONFile(filepath.Join(base, "aegis-graph-authoring/references/submission-check-result.v1.json"), &expected); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("literal response drift: got %v want %v", actual, expected)
	}
}
