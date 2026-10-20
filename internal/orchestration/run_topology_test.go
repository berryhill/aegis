package orchestration

import (
	"context"
	"errors"
	"github.com/berryhill/aegis/internal/core"
	"reflect"
	"strings"
	"testing"

	"github.com/berryhill/aegis/internal/graph"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	"github.com/berryhill/aegis/internal/queue"
)

type noGraphTopologyRepository struct {
	*fleetServiceRepository
	graphReads int
}

func (r *noGraphTopologyRepository) GetGraphRevision(context.Context, string, uint64) (graph.GraphRevision, error) {
	r.graphReads++
	return graph.GraphRevision{}, fleet.ErrNotFound
}

func TestResolveRunTopologyDirectAndHistorical(t *testing.T) {
	service, repo, _, _, _, _ := fleetServiceFixture(t)
	ctx := context.Background()
	historical, err := graph.NewRunSnapshot("historical", repo.graph, nil)
	if err != nil {
		t.Fatal(err)
	}
	old, err := service.ResolveRunTopology(ctx, historical)
	if err != nil {
		t.Fatal(err)
	}
	node := old.Nodes[0]
	direct, err := graph.NewDirectLoopSnapshot("direct", node, nil)
	if err != nil {
		t.Fatal(err)
	}
	envelope, err := graph.NewDirectRunSnapshot(direct)
	if err != nil {
		t.Fatal(err)
	}
	isolated := &noGraphTopologyRepository{fleetServiceRepository: repo}
	service.repository = isolated
	got, err := service.ResolveRunTopology(ctx, envelope)
	if err != nil || !reflect.DeepEqual(got.Nodes, old.Nodes) {
		t.Fatalf("direct topology=%+v err=%v", got, err)
	}
	if isolated.graphReads != 0 {
		t.Fatal("direct resolution consulted Graph library")
	}
	if _, err = service.ResolveRunTopology(ctx, historical); !errors.Is(err, fleet.ErrNotFound) {
		t.Fatalf("historical Graph absence=%v", err)
	}
	service.repository = repo
	substituted := historical
	substituted.Graph.Digest = "sha256:" + strings.Repeat("f", 64)
	if _, err = service.ResolveRunTopology(ctx, substituted); err == nil {
		t.Fatal("historical substitution accepted")
	}
	repo.loop.Digest = "sha256:" + strings.Repeat("f", 64)
	if _, err = service.ResolveRunTopology(ctx, envelope); err == nil {
		t.Fatal("direct Loop substitution accepted")
	}
}

func TestDirectTopologyLifecycleDoesNotBorrowGraphPermission(t *testing.T) {
	service, repo, _, subject, authority, _ := fleetServiceFixture(t)
	service.authorize = func(_ context.Context, action FleetAction, _ core.Subject) error {
		if action == FleetActionDirectLoopRuntime {
			return errors.New("Graph only")
		}
		return nil
	}
	node := repo.graph.Nodes[0]
	direct, err := graph.NewDirectLoopSnapshot("direct", node, nil)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := graph.NewDirectRunSnapshot(direct)
	if err != nil {
		t.Fatal(err)
	}
	// Existing fake repository reads the immutable accepted snapshot. No Graph
	// definition exists and a Graph-capable authority must not authorize direct.
	repo.accepted = &fleet.AcceptedSubmission{Snapshot: snapshot}
	worker := &QueueWorker{service: service, repository: repo}
	err = worker.lifecycleAdmission(context.Background(), FleetActionRuntimeEffect, subject, queue.Item{Authority: authority, Snapshot: digestRef(snapshot.SnapshotID, snapshot.Digest)})
	if !errors.Is(err, ErrWorkerDenied) || !strings.Contains(err.Error(), "direct_loop_runtime_permission_required") {
		t.Fatalf("direct effect admission=%v", err)
	}
	if _, err = service.ResolveRunTopology(context.Background(), graph.GraphRunSnapshot{SchemaVersion: graph.DirectRunSnapshotSchemaVersion}); err == nil {
		t.Fatal("malformed direct snapshot accepted")
	}
}
