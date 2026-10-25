package orchestration

import (
	"context"
	"strings"
	"testing"

	"github.com/berryhill/aegis/internal/implementation"
	"github.com/berryhill/aegis/internal/loop"
	hermesruntime "github.com/berryhill/aegis/internal/runtime/hermes"
)

func TestImplementationSerializedInputBudget(t *testing.T) {
	c := loop.VerifiedImplementation{SchemaVersion: loop.VerifiedImplementationSchema, Task: "Repair source", Acceptance: "TestValue passes", Workspace: "/workspace", WritableFiles: []string{"value.go"}, MaxPasses: 1, Policy: loop.GoTestPolicy{}}
	// Reuse the exact qualified policy from the kernel's public contract shape.
	c.Policy = loop.GoTestPolicy{Kind: "go-test.v1", TimeoutSeconds: 60, Packages: []string{"."}, RequiredTests: []loop.RequiredGoTest{{Package: "fixture", Name: "TestValue"}}}
	p := HermesPatchProposer{Adapter: &hermesruntime.Adapter{}}
	_, err := p.Propose(context.Background(), implementation.Request{Contract: c, PreviousCheck: []byte(strings.Repeat("x", hermesruntime.MaxAttemptInputBytes))})
	if err == nil || !strings.Contains(err.Error(), "serialized implementation input exceeds budget") {
		t.Fatalf("expected serialized input denial before runtime: %v", err)
	}
}
