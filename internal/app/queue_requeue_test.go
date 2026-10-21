package app

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/disposition"
	"github.com/berryhill/aegis/internal/execution"
	"github.com/berryhill/aegis/internal/graph"
	"github.com/berryhill/aegis/internal/orchestration"
	"github.com/berryhill/aegis/internal/queue"
	"github.com/berryhill/aegis/internal/reference"
)

func requeueFixture(t *testing.T) (core.Subject, RequeueQueueItemInput, QueueExecutionView, graph.GraphRunSnapshot, graph.ExecutionTopology) {
	t.Helper()
	ref := func(id, marker string) reference.RevisionRef {
		return reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: id, Revision: 1, Digest: "sha256:" + strings.Repeat(marker, 64)}
	}
	sub := core.Subject{ID: "operator", PrincipalID: "principal-1"}
	node := graph.Node{ID: "run-loop", Participant: ref("agent", "a"), Loop: ref("loop", "b"), Inputs: []graph.Port{{ID: "value", Type: graph.TypeObject, Required: true}}, Outputs: []graph.Port{{ID: "result", Type: graph.TypeArtifact, Required: true}}}
	direct, err := graph.NewDirectLoopSnapshot("snapshot", node, []graph.NormalizedInput{{PortID: "value", Type: graph.TypeObject, Value: []byte(`{"task":"unchanged"}`)}})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := graph.NewDirectRunSnapshot(direct)
	if err != nil {
		t.Fatal(err)
	}
	in := RequeueQueueItemInput{QueueItem: reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: "old", Digest: "sha256:" + strings.Repeat("c", 64)}, IdempotencyKey: "successor"}
	old := QueueExecutionView{Item: queue.Item{ItemID: in.QueueItem.ID, Digest: in.QueueItem.Digest, Snapshot: reference.DigestRef{ID: snapshot.SnapshotID, Digest: snapshot.Digest}}, Submission: queue.Submission{OwnerID: sub.PrincipalID}, Projection: queue.Projection{State: queue.StateFailed}, Attempts: []execution.Attempt{{}}, LoopExecutions: []execution.LoopExecution{{}}, RuntimeAuthorityBinding: &queue.RuntimeBinding{}, Disposition: &disposition.Record{State: execution.StateFailed, ReasonCode: "doer_loop_failed", DispositionID: "failed", Digest: "sha256:" + strings.Repeat("d", 64)}}
	return sub, in, old, snapshot, direct.Topology
}

func TestQueueRequeueReconstructsImmutableInput(t *testing.T) {
	sub, in, old, snapshot, topology := requeueFixture(t)
	before := core.Digest(old)
	got, err := reconstructQueueRequeue(sub, in, old, snapshot, topology)
	if err != nil {
		t.Fatal(err)
	}
	if got.Agent != topology.Nodes[0].Participant || got.Loop != topology.Nodes[0].Loop || !reflect.DeepEqual(got.Inputs, snapshot.Inputs) || got.Reexecution.PreviousQueue != in.QueueItem || got.Reexecution.PreviousDisposition.Digest != old.Disposition.Digest || got.Activate || got.QueueItemID != "" {
		t.Fatalf("reconstruction: %+v", got)
	}
	got.Inputs[0].Value[0] = '!'
	if string(snapshot.Inputs[0].Value) != `{"task":"unchanged"}` || core.Digest(old) != before {
		t.Fatal("old snapshot/evidence mutated")
	}
}

func TestQueueRequeueDenialSubsets(t *testing.T) {
	for _, name := range []string{"state", "claim", "reason", "disposition", "attempts", "children", "binding", "graph", "snapshot", "owner", "pin", "inputs"} {
		t.Run(name, func(t *testing.T) {
			sub, in, old, snapshot, topology := requeueFixture(t)
			switch name {
			case "state":
				old.Projection.State = queue.StateSucceeded
			case "claim":
				old.Projection.ActiveClaimID = "live"
			case "reason":
				old.Disposition.ReasonCode = "unknown_failure"
			case "disposition":
				old.Disposition = nil
			case "attempts":
				old.Attempts = append(old.Attempts, execution.Attempt{})
			case "children":
				old.LoopExecutions = nil
			case "binding":
				old.RuntimeAuthorityBinding = nil
			case "graph":
				snapshot.SchemaVersion = "aegis.graph.run-snapshot.v1"
			case "snapshot":
				snapshot.Digest = "changed"
			case "owner":
				sub.PrincipalID = "other"
			case "pin":
				topology.Nodes[0].Loop.Digest = "sha256:" + strings.Repeat("e", 64)
			case "inputs":
				snapshot.Inputs[0].Value = []byte(`{"task":"changed"}`)
			}
			if _, err := reconstructQueueRequeue(sub, in, old, snapshot, topology); !errors.Is(err, ErrDenied) {
				t.Fatalf("accepted %s: %v", name, err)
			}
		})
	}
}

func TestQueueRequeueRequiresFreshAuthority(t *testing.T) {
	now := time.Now().UTC()
	svc, sub := graphReadService(nil, now)
	sub.Method = "password"
	sub.AuthenticatedAt = now.Add(-svc.Config.Principal.AuthTTL)
	if got, err := svc.RequeueQueueItemAs(context.Background(), sub, RequeueQueueItemInput{}); !errors.Is(err, ErrDenied) || got.Reason != "requeue_fresh_authority_required" {
		t.Fatalf("stale authority: %+v %v", got, err)
	}
}

func TestQueueRequeueConservativePassCounting(t *testing.T) {
	steps := []orchestration.DoerStepView{{StepID: "implement", Visit: 1}, {StepID: "implement", Visit: 2}, {Terminal: true, Digest: "head"}}
	count, head, err := consumedDoerPasses(steps)
	if err != nil || count != 2 || head != "head" {
		t.Fatalf("count: %d %s %v", count, head, err)
	}
	for _, bad := range [][]orchestration.DoerStepView{nil, {{StepID: "implement", Visit: 1}}, {{Terminal: true}}, {{StepID: "implement", Visit: 4, Terminal: true}}} {
		if _, _, err := consumedDoerPasses(bad); !errors.Is(err, ErrDenied) {
			t.Fatal("invalid history counted")
		}
	}
}
