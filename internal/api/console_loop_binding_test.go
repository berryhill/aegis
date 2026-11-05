package api

import (
	"strings"
	"testing"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/loop"
	consoleweb "github.com/berryhill/aegis/web/console"
)

func TestConsoleLoopStoredStepBindingsRemainInspectable(t *testing.T) {
	definition := loop.LoopRevision{LoopID: "binding-loop", Revision: 1, EntryStepID: "implement", Steps: []loop.Step{
		{ID: "implement", Kind: loop.StepAction, Executable: &loop.DoerStepBinding{Operation: loop.DoerImplement, Backend: "hermes", Authority: "controller-bound"}, Implementation: &loop.VerifiedImplementation{SchemaVersion: loop.VerifiedImplementationSchema, Task: "task <untrusted>", Acceptance: "acceptance <untrusted>", Workspace: "/isolated/workspace", WritableFiles: []string{"main.go"}, MaxPasses: 2, Policy: loop.GoTestPolicy{Kind: "go-test.v1", Packages: []string{"./..."}, TimeoutSeconds: 60, RequiredTests: []loop.RequiredGoTest{{Package: "example", Name: "TestExact"}}}}},
		{ID: "done", Kind: loop.StepTerminal},
	}}
	record := consoleLoopRecord(app.LoopView{Revision: definition})
	first, second := record.Loop.Steps[0], record.Loop.Steps[1]
	if len(first.Bindings) == 0 || len(second.Bindings) != 0 {
		t.Fatal("stored binding omitted or invented")
	}
	if first.Tool != "" || first.TimeoutSeconds != 0 {
		t.Fatal("adapter backend or checker deadline promoted to generic step tool/timeout")
	}
	var out strings.Builder
	if err := consoleweb.LoopWorkspace(consoleweb.SurfaceModel{Domain: "loops"}, &record, consoleweb.LoopTopology{}).Render(t.Context(), &out); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Executable operation", "hermes.implement", "controller-bound", "Implementation task", "task &lt;untrusted&gt;", "acceptance &lt;untrusted&gt;", "Implementation pass bound", "Checker timeout (seconds)", "example / TestExact"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("inspector omitted %q", want)
		}
	}
	definition.Steps[0].Executable.Backend = "mutated"
	definition.Steps[0].Implementation.WritableFiles[0] = "mutated.go"
	for _, field := range first.Bindings {
		if strings.Contains(field.Value, "mutated") {
			t.Fatal("projection aliases stored definition")
		}
	}
}
