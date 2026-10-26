package badger

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/berryhill/aegis/internal/execution"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	"github.com/berryhill/aegis/internal/queue"
	badgerdb "github.com/dgraph-io/badger/v4"
)

// Fault injection preserves each record's canonical codec/digest. A valid child
// record must still prove membership in the exact admitted Graph snapshot.
func TestCompletionRejectsUnadmittedGraphNodeAtomically(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "fleet-v1")
	records, completion, blobs := completionFixture(t, ctx, root)
	child, err := records.GetLoopExecution(ctx, completion.Disposition.LoopExecutionID)
	if err != nil {
		t.Fatal(err)
	}
	child.GraphNodeID = "not-in-admitted-graph"
	child, err = execution.NewLoopExecution(child)
	if err != nil {
		t.Fatal(err)
	}
	wire, err := execution.MarshalLoopExecution(child)
	if err != nil {
		t.Fatal(err)
	}
	if err = records.update(ctx, func(txn *badgerdb.Txn) error { return txn.Set(key(familyLoopExecution, child.LoopExecutionID), wire) }); err != nil {
		t.Fatal(err)
	}
	before, err := records.AuditEvents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = records.CompleteQueueItem(ctx, completion, completionAudit(completion), blobs); !errors.Is(err, fleet.ErrConflict) {
		t.Fatalf("unadmitted child completed Graph: %v", err)
	}
	after, err := records.AuditEvents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(before) != len(after) {
		t.Fatal("rejected completion appended audit")
	}
	if err = records.Close(); err != nil {
		t.Fatal(err)
	}
	records, err = Open(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer records.Close()
	projection, err := records.GetQueueProjection(ctx, completion.Claim.QueueItem.ID)
	if err != nil || projection.State != queue.StateClaimed {
		t.Fatalf("failed completion changed projection: %+v %v", projection, err)
	}
	if _, err = records.GetDispositionByGraphRun(ctx, completion.Disposition.GraphRunID); !errors.Is(err, fleet.ErrNotFound) {
		t.Fatalf("rejected aggregate exists: %v", err)
	}
	if _, err = records.GetRuntimeArtifact(ctx, completion.Artifact.ID); !errors.Is(err, fleet.ErrNotFound) {
		t.Fatalf("rejected artifact exists: %v", err)
	}
}
