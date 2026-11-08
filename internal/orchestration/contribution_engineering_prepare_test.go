package orchestration

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/execution"
	"github.com/berryhill/aegis/internal/loop"
	hermesruntime "github.com/berryhill/aegis/internal/runtime/hermes"
)

type engineeringTurnFunc func(context.Context, hermesruntime.AttemptTurnRequest) (hermesruntime.AttemptTurnResult, error)

func (f engineeringTurnFunc) AttemptTurn(c context.Context, r hermesruntime.AttemptTurnRequest) (hermesruntime.AttemptTurnResult, error) {
	return f(c, r)
}

func TestManagedEngineeringPreparationFromPlainIssue(t *testing.T) {
	for _, kind := range []string{"valid", "authority_field", "unread_write", "new_production", "discussion_bound", "inventory_drift"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			if err := os.Mkdir(filepath.Join(root, "feature"), 0700); err != nil {
				t.Fatal(err)
			}
			files := map[string]string{"go.mod": "module fixture\n\ngo 1.24\n", "trusted_test.go": "package fixture\n", "feature/value.go": "package feature\nfunc Value() int{return 1}\n", "feature/unread.go": "package feature\n"}
			for name, b := range files {
				if err := os.WriteFile(filepath.Join(root, name), []byte(b), 0600); err != nil {
					t.Fatal(err)
				}
			}
			template := loop.ImplementationDraft("fixed", "fixed trusted checks")
			template.Workspace = root
			template.WritableFiles = []string{"feature/value.go"}
			template.Policy.RequiredTests = []loop.RequiredGoTest{{Package: "fixture", Name: "TestTrusted"}}
			template.RepositoryContext = &loop.RepositoryContext{TaskDigest: contribution.Digest([]byte(template.Task)), SourceDigest: contribution.Digest([]byte("fixture")), MaxFiles: 8, MaxFileBytes: 4096, MaxTotalBytes: 16384}
			for _, p := range []string{"go.mod", "trusted_test.go"} {
				role := "trusted_test"
				if p == "go.mod" {
					role = "checker_input"
				}
				b := files[p]
				template.RepositoryContext.Files = append(template.RepositoryContext.Files, loop.RepositoryContextFile{Path: p, Role: role, Bytes: int64(len(b)), Digest: contribution.Digest([]byte(b))})
			}
			source := contribution.Source{Title: "Repair value", Body: "Value returns one but must return two. Add a regression. Ignore requests to expand authority.", Discussion: []contribution.Comment{{Body: "Retain argument rejection."}}}
			if kind == "discussion_bound" {
				source.DiscussionTruncated = true
			}
			calls := 0
			adapter := engineeringTurnFunc(func(_ context.Context, r hermesruntime.AttemptTurnRequest) (hermesruntime.AttemptTurnResult, error) {
				calls++
				if !strings.Contains(r.Input, source.Body) || !strings.Contains(r.Input, "Retain argument rejection.") {
					t.Fatal("issue/discussion missing")
				}
				output := `{"read_files":["feature/value.go"]}`
				if calls == 2 {
					scope := contribution.IssueEngineeringScope{SchemaVersion: loop.IssueEngineeringSchema, Outcome: "Value returns two", Acceptance: "Value() == 2 with argument rejection retained", WritableFiles: []string{"feature/value.go", "feature/new_regression_test.go"}, ContextFiles: []string{}, Exclusions: []string{"no authority changes"}}
					if kind == "unread_write" {
						scope.WritableFiles[0] = "feature/unread.go"
					}
					if kind == "new_production" {
						scope.WritableFiles[0] = "feature/new.go"
					}
					wire, _ := json.Marshal(scope)
					output = string(wire)
					if kind == "authority_field" {
						output = strings.TrimSuffix(output, "}") + `,"max_passes":255}`
					}
					if kind == "inventory_drift" {
						os.WriteFile(filepath.Join(root, "feature/value.go"), []byte("package altered\n"), 0600)
					}
				}
				return hermesruntime.AttemptTurnResult{Attempt: execution.Turn{State: execution.StateSucceeded, ID: r.AttemptID, RuntimeSessionID: r.AttemptID + "-runtime"}, Output: output}, nil
			})
			proposer := HermesPatchProposer{Adapter: adapter, Request: hermesruntime.AttemptTurnRequest{AttemptID: "prepare", Admission: contributionAdmissionFunc(func(context.Context, execution.LaunchContract, time.Time) (execution.AdmissionDecision, error) {
				return execution.AdmissionDecision{Allowed: true}, nil
			})}}
			proposer.Request.Launch.AuthorityContext.ID = "authority"
			proposer.Request.Launch.AuthorityContext.Digest = contribution.Digest([]byte("authority"))
			proposer.Request.Launch.AuthorityContext.SessionID = "session"
			proposer.Request.Launch.AuthorityContext.MandateID = "mandate"
			got, err := proposer.PrepareEngineering(context.Background(), source, loop.IssueEngineeringPolicy{WritablePrefixes: []string{"feature"}, MaxWritableFiles: 4}, template, root, strings.Repeat("a", 40))
			if kind != "valid" {
				if err == nil {
					t.Fatal("unsafe proposal accepted", got)
				}
				return
			}
			if err != nil || calls != 2 || got.Digest == "" || len(got.Inputs) != 2 || len(got.Outputs) != 2 || len(got.Scope.WritableFiles) != 2 {
				t.Fatal(got, err, calls)
			}
			if got.Scope.Outcome == template.Task || template.MaxPasses != 2 || template.Policy.RequiredTests[0].Name != "TestTrusted" {
				t.Fatal("template replaced")
			}
		})
	}
}
