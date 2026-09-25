package command

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/loop"
)

func TestDoerDraftCLISealsSelectedFileAndRejectsUnsafePath(t *testing.T) {
	root := t.TempDir()
	expected := "hello"
	input := doerDraftInput{AgentID: "existing-agent", LoopID: "doer-task", Revision: 1, IdempotencyKey: "doer-draft", Doer: loop.DoerContract{Task: "Create result.txt containing hello", Workspace: root, WritableFiles: []string{"result.txt"}, VerifyFile: "result.txt", ExpectedText: &expected, MaxAttempts: 3}}
	source := filepath.Join(root, "input.json")
	save := func() {
		wire, err := json.Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(source, wire, 0600); err != nil {
			t.Fatal(err)
		}
	}
	run := func(args ...string) ([]byte, error) {
		var out, diag bytes.Buffer
		command := NewRoot(Dependencies{Out: &out, Err: &diag})
		command.SetArgs(args)
		err := command.Execute()
		return out.Bytes(), err
	}
	save()
	wire, err := run("loops", "doer", source)
	if err != nil {
		t.Fatal(err)
	}
	var publication app.PublishLoopInput
	if err := json.Unmarshal(wire, &publication); err != nil {
		t.Fatal(err)
	}
	if publication.Revision.SchemaVersion != loop.DoerRevisionSchemaVersion || publication.Revision.Doer == nil || publication.Revision.Doer.VerifyFile != "result.txt" || publication.Revision.Doer.MaxAttempts != 3 {
		t.Fatalf("bad v4 draft: %s", wire)
	}
	destination := filepath.Join(root, "publication.json")
	if _, err := run("loops", "doer", source, "--output", destination); err != nil {
		t.Fatal(err)
	}
	if _, err := run("loops", "doer", source, "--output", destination); err == nil {
		t.Fatal("overwrote draft")
	}
	input.Doer.VerifyFile = "../escape"
	save()
	if _, err := run("loops", "doer", source); err == nil {
		t.Fatal("accepted escaping path")
	}
}
