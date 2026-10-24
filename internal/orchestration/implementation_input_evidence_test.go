package orchestration

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"testing"

	"github.com/berryhill/aegis/internal/execution"
	"github.com/berryhill/aegis/internal/implementation"
	"github.com/berryhill/aegis/internal/loop"
	hermesruntime "github.com/berryhill/aegis/internal/runtime/hermes"
)

type inputCaptureAdapter func(context.Context, hermesruntime.AttemptTurnRequest) (hermesruntime.AttemptTurnResult, error)

func (f inputCaptureAdapter) AttemptTurn(c context.Context, r hermesruntime.AttemptTurnRequest) (hermesruntime.AttemptTurnResult, error) {
	return f(c, r)
}

// Synthetic adapter at the actual proposer serialization boundary; not provider acceptance.
func TestImplementationInputEvidenceBoundary(t *testing.T) {
	c := loop.VerifiedImplementation{SchemaVersion: loop.VerifiedImplementationSchema, Task: "Repair source", Acceptance: "TestValue passes", Workspace: "/workspace", WritableFiles: []string{"value.go"}, MaxPasses: 1, Policy: loop.GoTestPolicy{Kind: "go-test.v1", TimeoutSeconds: 60, Packages: []string{"."}, RequiredTests: []loop.RequiredGoTest{{Package: "fixture", Name: "TestValue"}}}}
	source := "package fixture\nfunc Value() int { return 0 }\n"
	tests := "package fixture\n// trusted test bytes\n"
	req := implementation.Request{Contract: c, PassID: "run/pass/1", RepositoryContext: &implementation.CollectedRepositoryContext{Files: []implementation.RepositoryContextText{{Path: "value.go", Text: source}, {Path: "value_test.go", Text: tests}}}}
	for _, mode := range []string{"success", "malformed", "unknown", "persist_failed"} {
		t.Run(mode, func(t *testing.T) {
			var prepared []byte
			completed, calls := 0, 0
			req.InputObserver = func(o implementation.InputObservation) error {
				if !o.Completed {
					prepared = append([]byte(nil), o.Bytes...)
					if mode == "persist_failed" {
						return errors.New("private custody unavailable")
					}
					return nil
				}
				completed++
				if !bytes.Equal(prepared, o.Bytes) || o.TurnID != req.PassID || o.RuntimeSessionID != "actual-session" {
					t.Fatal("completion drift")
				}
				return nil
			}
			p := HermesPatchProposer{Adapter: inputCaptureAdapter(func(_ context.Context, r hermesruntime.AttemptTurnRequest) (hermesruntime.AttemptTurnResult, error) {
				calls++
				if !bytes.Equal([]byte(r.Input), prepared) || sha256.Sum256([]byte(r.Input)) != sha256.Sum256(prepared) {
					t.Fatal("wire mismatch")
				}
				var envelope struct {
					Request implementation.Request `json:"request"`
				}
				if json.Unmarshal([]byte(r.Input), &envelope) != nil || envelope.Request.RepositoryContext.Files[0].Text != source || envelope.Request.RepositoryContext.Files[1].Text != tests {
					t.Fatal("source/test bytes lost")
				}
				if bytes.Contains(prepared, []byte("InputObserver")) {
					t.Fatal("callback serialized")
				}
				if mode == "unknown" {
					return hermesruntime.AttemptTurnResult{}, errors.New("synthetic unknown")
				}
				out := `{"edits":[{"path":"value.go","content":"eA=="}]}`
				if mode == "malformed" {
					out = "malformed"
				}
				return hermesruntime.AttemptTurnResult{Attempt: execution.Turn{ID: r.AttemptID, RuntimeSessionID: "actual-session"}, Output: out}, nil
			})}
			_, err := p.Propose(context.Background(), req)
			if mode == "success" && err != nil {
				t.Fatal(err)
			}
			if mode != "success" && err == nil {
				t.Fatal("expected error")
			}
			want := 1
			if mode == "persist_failed" {
				want = 0
			}
			if calls != want {
				t.Fatal("model effect after persistence failure")
			}
			want = 1
			if mode == "unknown" || mode == "persist_failed" {
				want = 0
			}
			if completed != want {
				t.Fatal("false/missing delivery")
			}
		})
	}
}
