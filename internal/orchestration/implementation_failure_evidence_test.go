package orchestration

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/berryhill/aegis/internal/execution"
	"github.com/berryhill/aegis/internal/implementation"
	"github.com/berryhill/aegis/internal/loop"
	hermesruntime "github.com/berryhill/aegis/internal/runtime/hermes"
)

func TestManagedFailureBoundary(t *testing.T) {
	c := loop.VerifiedImplementation{SchemaVersion: loop.VerifiedImplementationSchema, Task: "Repair source", Acceptance: "TestValue passes", Workspace: "/workspace", WritableFiles: []string{"value.go"}, MaxPasses: 1, Policy: loop.GoTestPolicy{Kind: "go-test.v1", TimeoutSeconds: 60, Packages: []string{"."}, RequiredTests: []loop.RequiredGoTest{{Package: "fixture", Name: "TestValue"}}}}
	for _, reason := range []string{"runtime_provider_format_error", "SECRET malicious provider body"} {
		t.Run(reason, func(t *testing.T) {
			var observed implementation.ManagedFailure
			delivered := false
			req := implementation.Request{Contract: c, PassID: "fixture/pass/1", InputObserver: func(o implementation.InputObservation) error { delivered = o.Completed; return nil }, FailureObserver: func(v implementation.ManagedFailure) error { observed = v; return nil }}
			p := HermesPatchProposer{Adapter: inputCaptureAdapter(func(context.Context, hermesruntime.AttemptTurnRequest) (hermesruntime.AttemptTurnResult, error) {
				return hermesruntime.AttemptTurnResult{Attempt: execution.Turn{State: execution.StateFailed, Reason: reason}, Output: "SECRET raw output"}, errors.New("SECRET SDK error")
			})}
			_, err := p.ProposeReport(context.Background(), req)
			if err == nil || delivered {
				t.Fatal("false delivery")
			}
			v := codingHistoryView(implementation.History{Passes: []implementation.Pass{{Failure: &observed}}})
			b, _ := json.Marshal(v)
			if strings.Contains(string(b), "SECRET") || strings.Contains(err.Error(), "SECRET") {
				t.Fatal("unsafe metadata")
			}
			if observed.State != "failed" || observed.Code == "" {
				t.Fatal(observed)
			}
			if reason == "runtime_provider_format_error" && observed.Reason != reason {
				t.Fatal("classification discarded")
			}
		})
	}
}
