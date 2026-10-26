package badger

import (
	"context"
	"errors"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	"path/filepath"
	"testing"
)

func TestGraphNodeProtocolRejectsSingleNodeAndSurvivesReopen(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "fleet-v1")
	s, c, blobs := completionFixture(t, ctx, root)
	child, err := s.GetLoopExecution(ctx, c.Disposition.LoopExecutionID)
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := s.GetAttempt(ctx, c.Disposition.AttemptID)
	if err != nil {
		t.Fatal(err)
	}
	a := fleet.GraphNodeAdmission{SchemaVersion: fleet.GraphNodeSchemaVersion, Claim: c.Claim, Execution: child, Attempt: attempt, MaxImplementationPasses: 2, AdmittedAt: c.Claim.ClaimedAt}
	if err = s.AdmitGraphNode(ctx, a, audit("fleet.graph.node.admitted", child.GraphNodeID)); !errors.Is(err, fleet.ErrConflict) {
		t.Fatalf("single node admitted to two-node protocol: %v", err)
	}
	if err = s.CheckpointGraphNode(ctx, fleet.GraphNodeCheckpoint{Admission: a, Completion: c}, audit("fleet.graph.node.checkpointed", child.GraphNodeID), blobs); !errors.Is(err, fleet.ErrConflict) {
		t.Fatalf("unadmitted checkpoint: %v", err)
	}
	if err = s.CompleteGraph(ctx, c, completionAudit(c), blobs); !errors.Is(err, fleet.ErrConflict) {
		t.Fatalf("early aggregate: %v", err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err = s.GetGraphNodeCheckpoint(ctx, attempt.GraphRunID, child.GraphNodeID); !errors.Is(err, fleet.ErrNotFound) {
		t.Fatalf("rejected checkpoint persisted: %v", err)
	}
	if err = s.CompleteQueueItem(ctx, c, completionAudit(c), blobs); err != nil {
		t.Fatalf("single-node regression: %v", err)
	}
}
