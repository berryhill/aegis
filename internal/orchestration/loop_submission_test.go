package orchestration

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/berryhill/aegis/internal/graph"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	"github.com/berryhill/aegis/internal/queue"
	"github.com/berryhill/aegis/internal/reference"
)

// This fixture compares the complete immutable admission payload. Actual Badger
// transaction qualification remains in the persistence package's tests.
type directAdmissionRepository struct{ *noGraphTopologyRepository }

func (r *directAdmissionRepository) AcceptSubmission(_ context.Context, value fleet.AcceptedSubmission, _ fleet.AuditFact) (bool, error) {
	if r.accepted != nil {
		if !reflect.DeepEqual(*r.accepted, value) {
			return false, fleet.ErrConflict
		}
		return false, nil
	}
	r.accepted = &value
	return true, nil
}

func TestPrepareLoopRunExactWorkspaceWithoutGraph(t *testing.T) {
	service, base, _, subject, _, _ := fleetServiceFixture(t)
	workspace := workspaceFor(t, subject.PrincipalID, base.agent)
	repo := &directAdmissionRepository{&noGraphTopologyRepository{fleetServiceRepository: base}}
	service.repository = repo
	request := SubmitLoopRequest{SubmitGraphRequest: workspaceSubmit(subject, workspace, reference.RevisionRef{}, "direct"), Participant: workspace.Agent, Loop: revisionRef(base.loop.LoopID, base.loop.Revision, base.loop.Digest), NodeID: "direct-node"}
	decision, err := service.PrepareLoopRun(context.Background(), request)
	if err != nil || decision.Accepted == nil || !decision.Created {
		t.Fatalf("admit=%+v err=%v", decision, err)
	}
	accepted := decision.Accepted
	if accepted.Snapshot.SchemaVersion != graph.DirectRunSnapshotSchemaVersion || accepted.Snapshot.Graph != (reference.RevisionRef{}) || accepted.InitialTransition.To != queue.StatePreparationPending || accepted.Submission.OwnerAgentID != workspace.Agent.ID || accepted.Submission.Authority != workspace.Ref() || accepted.QueueItem.MaxAttempts != 1 {
		t.Fatalf("direct envelope lost binding: %+v", accepted)
	}
	if repo.graphReads != 0 || base.graphPublished {
		t.Fatal("direct admission consulted or published Graph")
	}
	replay, err := service.PrepareLoopRun(context.Background(), request)
	if err != nil || replay.Created || !reflect.DeepEqual(replay.Accepted, accepted) {
		t.Fatalf("replay=%+v err=%v", replay, err)
	}
	changed := request
	changed.MaxAttempts = 2
	if _, err := service.PrepareLoopRun(context.Background(), changed); !errors.Is(err, fleet.ErrConflict) {
		t.Fatalf("changed bound conflict=%v", err)
	}
	topology, err := service.ResolveRunTopology(context.Background(), accepted.Snapshot)
	if err != nil || topology.Nodes[0].Participant != request.Participant || topology.Nodes[0].Loop != request.Loop {
		t.Fatalf("topology=%+v err=%v", topology, err)
	}
}

func TestPrepareLoopRunDoesNotBorrowGraphAuthority(t *testing.T) {
	service, repo, _, subject, authority, graphRef := fleetServiceFixture(t)
	workspace := workspaceFor(t, subject.PrincipalID, repo.agent)
	request := SubmitLoopRequest{SubmitGraphRequest: workspaceSubmit(subject, workspace, reference.RevisionRef{}, "direct-denied"), Participant: workspace.Agent, Loop: revisionRef(repo.loop.LoopID, repo.loop.Revision, repo.loop.Digest), NodeID: "node"}
	request.Workspace = nil
	request.Authority = authority
	decision, err := service.PrepareLoopRun(context.Background(), request)
	if err != nil || decision.Rejection == nil || decision.Rejection.ReasonCode != "direct_permission_required" || repo.accepted != nil {
		t.Fatalf("runtime-only decision=%+v err=%v", decision, err)
	}
	request.Workspace = &workspace
	request.Authority = workspace.Ref()
	request.Graph = graphRef
	decision, err = service.PrepareLoopRun(context.Background(), request)
	if err != nil || decision.Rejection == nil || decision.Rejection.ReasonCode != "direct_reference_invalid" || repo.accepted != nil {
		t.Fatalf("Graph claim decision=%+v err=%v", decision, err)
	}
	request.Graph = reference.RevisionRef{}
	request.Participant.Revision++
	decision, err = service.PrepareLoopRun(context.Background(), request)
	if err != nil || decision.Rejection == nil || decision.Rejection.ReasonCode != "workspace_denied" || repo.accepted != nil {
		t.Fatalf("participant substitution=%+v err=%v", decision, err)
	}
}
