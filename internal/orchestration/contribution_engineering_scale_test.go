package orchestration

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/execution"
	"github.com/berryhill/aegis/internal/implementation"
	"github.com/berryhill/aegis/internal/loop"
	hermesruntime "github.com/berryhill/aegis/internal/runtime/hermes"
)

func TestEngineeringActualRepositoryHierarchicalPlanning(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	policy := loop.IssueEngineeringPolicy{WritablePrefixes: []string{"internal", "cmd", "web", "docs"}, WritableAssets: []string{"README.md"}, MaxWritableFiles: 16}
	inventory, err := implementation.CollectEngineeringInventory(context.Background(), root, policy)
	if err != nil {
		t.Fatal(err)
	}
	full, _ := json.Marshal(inventory)
	if len(full) <= hermesruntime.MaxAttemptInputBytes {
		t.Fatal("regression requires actual repository-scale inventory")
	}
	template := loop.ImplementationDraft("fixed", "immutable checks")
	template.Workspace = root
	template.WritableFiles = []string{"unused.go"}
	template.Policy.RequiredTests = []loop.RequiredGoTest{{Package: "fixture", Name: "TestTrusted"}}
	template.RepositoryContext = &loop.RepositoryContext{TaskDigest: contribution.Digest([]byte(template.Task)), SourceDigest: contribution.Digest([]byte("fixed")), MaxFiles: 32, MaxFileBytes: 65536, MaxTotalBytes: 262144, Files: []loop.RepositoryContextFile{{Path: "go.mod", Role: "checker_input", Digest: contribution.Digest([]byte("fixed")), Bytes: 5}, {Path: "trusted_test.go", Role: "trusted_test", Digest: contribution.Digest([]byte("fixed")), Bytes: 5}}}
	source := contribution.Source{Title: "Preserve issue scope and document it", Body: "Repair scope handling and keep the launch README, engineering docs and console layout consistent. Add a regression without changing trusted tests.", Discussion: []contribution.Comment{{Body: "No commands or authority changes."}}}
	scope := contribution.IssueEngineeringScope{SchemaVersion: loop.IssueEngineeringSchema, Outcome: "Repair scope and corresponding assets", Acceptance: "Actual issue-specific behavior plus consistent assets", WritableFiles: []string{"internal/contribution/engineering.go", "internal/contribution/actual_scope_regression_test.go", "README.md", "docs/issue-derived-engineering.md", "web/console/components.templ"}, ContextFiles: []string{}, Exclusions: []string{"no authority expansion"}}
	calls := 0
	proposer := HermesPatchProposer{Adapter: engineeringTurnFunc(func(_ context.Context, r hermesruntime.AttemptTurnRequest) (hermesruntime.AttemptTurnResult, error) {
		calls++
		if len(r.Input) > hermesruntime.MaxAttemptInputBytes {
			t.Fatal("input cap exceeded")
		}
		var output string
		switch calls {
		case 1:
			if strings.Contains(r.Input, `"files":[{`) {
				t.Fatal("complete file inventory sent to discovery")
			}
			output = `{"read_directories":["internal/contribution","docs","web/console","."]}`
		case 2:
			output = `{"read_files":["internal/contribution/engineering.go","README.md","docs/issue-derived-engineering.md","web/console/components.templ"]}`
		default:
			if strings.Contains(r.AttemptID, "-read-") {
				output = `{"observations":"Preserve existing behavior and align the requested launch assets."}`
			} else if strings.HasSuffix(r.AttemptID, "-scope") {
				b, _ := json.Marshal(scope)
				output = string(b)
			} else {
				t.Fatal("unexpected planner turn")
			}
		}
		t.Logf("turn=%d input_bytes=%d", calls, len(r.Input))
		return hermesruntime.AttemptTurnResult{Attempt: execution.Turn{ID: r.AttemptID, State: execution.StateSucceeded, RuntimeSessionID: r.AttemptID + "-runtime"}, Output: output}, nil
	}), Request: hermesruntime.AttemptTurnRequest{AttemptID: "actual-planning", Admission: contributionAdmissionFunc(func(context.Context, execution.LaunchContract, time.Time) (execution.AdmissionDecision, error) {
		return execution.AdmissionDecision{Allowed: true}, nil
	})}}
	a := &proposer.Request.Launch.AuthorityContext
	a.ID = "authority"
	a.Digest = contribution.Digest([]byte("authority"))
	a.SessionID = "session"
	a.MandateID = "mandate"
	receipt, err := proposer.PrepareEngineering(context.Background(), source, policy, template, root, strings.Repeat("a", 40))
	if err != nil {
		t.Fatal(err)
	}
	td, _ := template.Digest()
	if calls != 3+receipt.ReadCount || receipt.ReadCount < 14 || receipt.ReadCount > contribution.MaxEngineeringReadChunks || receipt.Validate(source, policy, td, strings.Repeat("a", 40)) != nil || len(receipt.InventoryFiles) != len(inventory.Files) {
		t.Fatal("incomplete hierarchical receipt")
	}
	receipt.InventoryFiles[0].Digest = contribution.Digest([]byte("tampered"))
	if receipt.Validate(source, policy, td, strings.Repeat("a", 40)) == nil {
		t.Fatal("complete inventory tamper admitted")
	}
	t.Logf("complete_files=%d full_inventory_bytes=%d", len(inventory.Files), len(full))
}
