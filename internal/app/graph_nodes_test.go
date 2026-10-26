package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/berryhill/aegis/internal/disposition"
	"github.com/berryhill/aegis/internal/execution"
	"github.com/berryhill/aegis/internal/graph"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	"github.com/berryhill/aegis/internal/queue"
	"github.com/berryhill/aegis/internal/reference"
)

type nodeReadRepository struct {
	fleet.Repository
	fleet.GraphNodeRepository
	admissions  map[string]fleet.GraphNodeAdmission
	checkpoints map[string]fleet.GraphNodeCheckpoint
	err         error
}

func (r *nodeReadRepository) GetGraphNode(_ context.Context, _, node string) (fleet.GraphNodeAdmission, error) {
	if r.err != nil {
		return fleet.GraphNodeAdmission{}, r.err
	}
	a, ok := r.admissions[node]
	if !ok {
		return a, fleet.ErrNotFound
	}
	return a, nil
}
func (r *nodeReadRepository) GetGraphNodeCheckpoint(_ context.Context, _, node string) (fleet.GraphNodeCheckpoint, error) {
	c, ok := r.checkpoints[node]
	if !ok {
		return c, fleet.ErrNotFound
	}
	return c, nil
}
func TestQueueGraphNodeReadback(t *testing.T) {
	item := queue.Item{ItemID: "queue", Digest: "digest", GraphRunID: "run"}
	nodes := []graph.Node{{ID: "producer"}, {ID: "consumer"}, {ID: "not-admitted"}}
	repo := &nodeReadRepository{admissions: map[string]fleet.GraphNodeAdmission{}, checkpoints: map[string]fleet.GraphNodeCheckpoint{}}
	for _, n := range nodes[:2] {
		a := fleet.GraphNodeAdmission{Claim: queue.Claim{QueueItem: reference.DigestRef{ID: item.ItemID, Digest: item.Digest}}, Execution: execution.LoopExecution{GraphRunID: item.GraphRunID, GraphNodeID: n.ID}, MaxImplementationPasses: 2}
		repo.admissions[n.ID] = a
		repo.checkpoints[n.ID] = fleet.GraphNodeCheckpoint{Admission: a, Completion: fleet.Completion{Disposition: disposition.Record{DispositionID: n.ID}}}
	}
	// Public projection must retain both checkpoints even when root has no artifact.
	got, err := readGraphNodes(context.Background(), repo, item, nodes)
	if err != nil || len(got) != 2 || got[0].Disposition.DispositionID != "producer" || got[1].Disposition.DispositionID != "consumer" {
		t.Fatalf("%+v %v", got, err)
	}
	b, err := json.Marshal(QueueExecutionView{GraphNodes: got})
	if err != nil || !strings.Contains(string(b), `"graph_nodes"`) || strings.Contains(string(b), "managed_doer") {
		t.Fatalf("%s %v", b, err)
	}
	a := repo.admissions["consumer"]
	a.Execution.GraphRunID = "wrong"
	repo.admissions["consumer"] = a
	if _, err = readGraphNodes(context.Background(), repo, item, nodes); !errors.Is(err, fleet.ErrCorrupt) {
		t.Fatal(err)
	}
	repo.err = fleet.ErrCorrupt
	if _, err = readGraphNodes(context.Background(), repo, item, nodes); !errors.Is(err, fleet.ErrCorrupt) {
		t.Fatal(err)
	}
}
