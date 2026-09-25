package command

import (
	"bytes"
	"encoding/json"
	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/loop"
	"os"
	"path/filepath"
	"testing"
)

func TestImplementationDraftCLI(t *testing.T) {
	dir := t.TempDir()
	input := implementationDraftInput{AgentID: "existing-builder", LoopID: "bounded-add", Revision: 1, IdempotencyKey: "draft-one", Implementation: loop.VerifiedImplementation{SchemaVersion: loop.VerifiedImplementationSchema, Task: "Implement addition", Acceptance: "TestAdd passes", Workspace: dir, WritableFiles: []string{"add.go"}, MaxPasses: 2, Policy: loop.GoTestPolicy{Kind: "go-test.v1", Packages: []string{"."}, RequiredTests: []loop.RequiredGoTest{{Package: "example.test/add", Name: "TestAdd"}}, TimeoutSeconds: 30}}}
	source := filepath.Join(dir, "input.json")
	save := func() {
		b, _ := json.Marshal(input)
		if err := os.WriteFile(source, b, 0600); err != nil {
			t.Fatal(err)
		}
	}
	run := func(args ...string) ([]byte, error) {
		var out, diag bytes.Buffer
		root := NewRoot(Dependencies{Out: &out, Err: &diag})
		root.SetArgs(args)
		err := root.Execute()
		return out.Bytes(), err
	}
	save()
	out, err := run("loops", "implementation", source)
	if err != nil {
		t.Fatal(err)
	}
	var publication app.PublishLoopInput
	if err := json.Unmarshal(out, &publication); err != nil {
		t.Fatal(err)
	}
	if publication.AgentID != input.AgentID || publication.Revision.SchemaVersion != loop.ImplementationRevisionSchemaVersion || publication.Revision.Steps[1].Implementation == nil || publication.Revision.Steps[1].Implementation.Task != input.Implementation.Task {
		t.Fatalf("wrong draft: %s", out)
	}
	destination := filepath.Join(dir, "draft.json")
	if _, err := run("loops", "implementation", source, "--output", destination); err != nil {
		t.Fatal(err)
	}
	if _, err := run("loops", "implementation", source, "--output", destination); err == nil {
		t.Fatal("overwrote file")
	}
	input.Implementation.DecisionMode = "doer.v1"
	input.Implementation.MaxPasses = 3
	save()
	doerDraft, err := run("loops", "implementation", source)
	if err != nil {
		t.Fatal(err)
	}
	var doerPublication app.PublishLoopInput
	if err := json.Unmarshal(doerDraft, &doerPublication); err != nil {
		t.Fatal(err)
	}
	if doerPublication.Revision.Digest == publication.Revision.Digest || doerPublication.Revision.Steps[1].Implementation == nil || doerPublication.Revision.Steps[1].Implementation.DecisionMode != "doer.v1" || doerPublication.Revision.Steps[1].Implementation.MaxPasses != 3 {
		t.Fatalf("doer contract was not bound to exact draft: %s", doerDraft)
	}
	input.Implementation.DecisionMode = ""
	save()
	if _, err := run("loops", "implementation", source); err == nil {
		t.Fatal("accepted three passes without decision mode")
	}
	input.Implementation.Policy.RequiredTests = nil
	save()
	if _, err := run("loops", "implementation", source); err == nil {
		t.Fatal("accepted missing tests")
	}
}
