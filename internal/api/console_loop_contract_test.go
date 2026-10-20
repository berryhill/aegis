package api

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/loop"
)

func TestConsoleLoopContractProjectionV4(t *testing.T) {
	empty := ""
	hostile := "  <script>alert(\"contract\")</script>\nsecond line\n  "
	for _, tc := range []struct {
		name  string
		text  *string
		exact bool
		mode  string
	}{
		{"presence-only", nil, false, "presence-only"},
		{"empty-trimmed", &empty, false, "trimmed-text"},
		{"empty-exact", &empty, true, "exact-bytes"},
		{"hostile-trimmed", &hostile, false, "trimmed-text"},
		{"hostile-exact", &hostile, true, "exact-bytes"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			revision, validation, err := loop.NewDoerRevision("loop.contract", 1, "", loop.DoerContract{
				Task: "Preserve <b>canonical</b> task\nand next line", Workspace: "/workspace/contract",
				WritableFiles: []string{"selected.txt", "notes.txt"}, VerifyFile: "selected.txt",
				ExpectedText: tc.text, ExactBytes: tc.exact, MaxAttempts: 3,
			})
			if err != nil {
				t.Fatalf("construct v4: %v (%+v)", err, validation)
			}
			before, err := json.Marshal(revision)
			if err != nil {
				t.Fatal(err)
			}
			digest, err := revision.Doer.Digest()
			if err != nil {
				t.Fatal(err)
			}
			record := consoleLoopRecord(app.LoopView{Revision: revision})
			projected := record.Loop.DoerContract
			if projected == nil {
				t.Fatal("missing sealed v4 contract projection")
			}
			if !record.Loop.DoerV4 || record.Loop.DoerV5 || record.Digest != revision.Digest || record.Loop.Digest != revision.Digest {
				t.Fatalf("lost exact revision identity: %+v", record)
			}
			if projected.Task != revision.Doer.Task || projected.Workspace != revision.Doer.Workspace ||
				!reflect.DeepEqual(projected.WritableFiles, revision.Doer.WritableFiles) || projected.VerifyFile != revision.Doer.VerifyFile ||
				projected.ExactBytes != revision.Doer.ExactBytes || projected.MaxAttempts != revision.Doer.MaxAttempts ||
				projected.Digest != digest || projected.VerificationMode != tc.mode {
				t.Fatalf("contract differs from canonical definition: %+v", projected)
			}
			if (projected.ExpectedText == nil) != (revision.Doer.ExpectedText == nil) {
				t.Fatal("expected text nil/empty distinction lost")
			}
			if projected.ExpectedText != nil {
				if *projected.ExpectedText != *revision.Doer.ExpectedText || projected.ExpectedText == revision.Doer.ExpectedText {
					t.Fatal("expected text not faithfully cloned")
				}
				*projected.ExpectedText = "presentation mutation"
			}
			projected.WritableFiles[0] = "presentation-only.txt"
			after, err := json.Marshal(revision)
			if err != nil {
				t.Fatal(err)
			}
			if string(before) != string(after) {
				t.Fatal("projection or presentation mutation changed canonical revision/digest")
			}
		})
	}
}

func TestConsoleLoopContractProjectionUnsealedDefinitions(t *testing.T) {
	reusable, validation, err := loop.NewDoerReusableRevision("loop.reusable", 1, "", loop.DoerReusableContract{MaxAttempts: 2})
	if err != nil {
		t.Fatalf("construct v5: %v (%+v)", err, validation)
	}
	generic, validation, err := loop.NewRevision(loop.LoopRevision{
		LoopID: "loop.generic", Revision: 1, EntryStepID: "act",
		Steps: []loop.Step{
			{ID: "act", Kind: loop.StepAction, Retry: loop.RetryPolicy{MaxAttempts: 1}},
			{ID: "done", Kind: loop.StepTerminal, Retry: loop.RetryPolicy{MaxAttempts: 1}, Terminal: &loop.TerminalDefinition{Outcome: loop.OutcomeSucceeded}},
		},
		Transitions: []loop.Transition{{ID: "complete", FromStepID: "act", ToStepID: "done"}},
	})
	if err != nil {
		t.Fatalf("construct generic: %v (%+v)", err, validation)
	}
	for _, revision := range []loop.LoopRevision{reusable, generic} {
		t.Run(revision.SchemaVersion, func(t *testing.T) {
			before, _ := json.Marshal(revision)
			detail := consoleLoopRecord(app.LoopView{Revision: revision}).Loop
			if detail.DoerContract != nil || detail.DoerV4 || detail.DoerV5 != (revision.SchemaVersion == loop.DoerReusableSchemaVersion) {
				t.Fatalf("invented sealed contract or wrong definition kind: %+v", detail)
			}
			if len(detail.Inputs) != len(revision.Inputs) {
				t.Fatal("lost typed inputs")
			}
			for i, port := range revision.Inputs {
				if got := detail.Inputs[i]; got.ID != port.ID || got.Type != string(port.Type) || got.Required != port.Required {
					t.Fatalf("input differs from canonical port: %+v != %+v", got, port)
				}
			}
			if revision.SchemaVersion == loop.DoerReusableSchemaVersion {
				if len(detail.Inputs) != 5 {
					t.Fatal("v5 must retain four required and one optional input")
				}
				for _, port := range detail.Inputs {
					if port.Required != (port.ID != "expected_text") {
						t.Fatalf("incorrect v5 optionality: %+v", port)
					}
				}
			}
			after, _ := json.Marshal(revision)
			if string(before) != string(after) || detail.Digest != revision.Digest {
				t.Fatal("projection changed revision/digest")
			}
		})
	}
}

func TestConsoleLoopContractProjectionRequiresV4Schema(t *testing.T) {
	// Even an unexpected non-v4 payload must not be presented as a sealed v4 contract.
	for _, schema := range []string{loop.RevisionSchemaVersion, loop.DoerReusableSchemaVersion} {
		detail := consoleLoopRecord(app.LoopView{Revision: loop.LoopRevision{
			SchemaVersion: schema, Doer: &loop.DoerContract{Task: "not a v4 revision"},
		}}).Loop
		if detail.DoerContract != nil || detail.DoerV4 {
			t.Fatalf("non-v4 schema %s acquired sealed values", schema)
		}
	}
}
