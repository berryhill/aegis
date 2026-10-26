package api

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"testing"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/graph"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/orchestration"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	"github.com/berryhill/aegis/internal/queue"
	"github.com/berryhill/aegis/internal/reference"
)

// Application-service qualification only: the configured Unix-peer authenticator
// admits this test's OS subject; no socket or packaged CLI is exercised here.
// Two canonical Registry owners receive sealed workspace contexts. This does not
// authenticate two native profiles, exercise a provider, or qualify a live pilot.
func TestIssue351TwoRecordedOwnersSharedReusableLoopAdmission(t *testing.T) {
	ctx := context.Background()
	svc := apiService(t)
	configureAPIFleet(t, svc)
	subject, err := svc.AuthenticateUnixPeer(ctx, uint32(os.Getuid()))
	if err != nil {
		t.Fatal(err)
	}
	a := registerDoerWorkspaceAgent(t, svc, subject, "pilot-a", "pilot-owner-a")
	b := registerDoerWorkspaceAgent(t, svc, subject, "pilot-b", "pilot-owner-b")
	wa, err := svc.RegisteredAgentWorkspaceAs(ctx, subject, a.Revision.AgentID)
	if err != nil {
		t.Fatal(err)
	}
	wb, err := svc.RegisteredAgentWorkspaceAs(ctx, subject, b.Revision.AgentID)
	if err != nil {
		t.Fatal(err)
	}
	if wa.OwnerID == wb.OwnerID || wa.OwnerID != a.Revision.Ownership.OwnerID || wb.OwnerID != b.Revision.Ownership.OwnerID || wa.PrincipalID != subject.PrincipalID || wb.PrincipalID != subject.PrincipalID || wa.Ref() == wb.Ref() {
		t.Fatal("fixture must have distinct recorded owners and exact authenticated workspace contexts")
	}
	for _, agent := range []app.FleetAgent{a, b} {
		got, err := svc.GetFleetAgentAs(ctx, subject, agent.Revision.AgentID, agent.Revision.Revision)
		if err != nil || !reflect.DeepEqual(got, agent) {
			t.Fatalf("canonical registration readback: %+v %v", got, err)
		}
	}
	// Selecting an existing name does not authenticate a caller.
	if _, err := svc.RegisteredAgentWorkspaceAs(ctx, core.Subject{}, a.Revision.AgentID); !errors.Is(err, app.ErrDenied) {
		t.Fatalf("unauthenticated selector: %v", err)
	}
	revision, validation, err := loop.NewDoerReusableRevision("pilot-shared", 1, "", loop.DoerReusableContract{MaxAttempts: 2})
	if err != nil || len(validation.Issues) != 0 {
		t.Fatalf("reusable definition: %v %+v", err, validation)
	}
	published, err := svc.PublishLoopAs(ctx, subject, app.PublishLoopInput{AgentID: a.Revision.AgentID, Revision: revision, IdempotencyKey: "pilot-publish"})
	if err != nil {
		t.Fatal(err)
	}
	ref := reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: revision.LoopID, Revision: revision.Revision, Digest: published.Revision.Digest}
	view, err := svc.GetLoopViewAs(ctx, subject, ref.ID, ref.Revision)
	if err != nil || view.Provenance.OwnerID != wa.OwnerID || view.Provenance.PrincipalID != subject.PrincipalID || view.Provenance.PublisherAgent.ID != wa.Agent.ID {
		t.Fatalf("publication provenance: %+v %v", view, err)
	}
	if _, err := svc.SetLoopLifecycleAs(ctx, subject, ref.ID, app.SetLoopLifecycleInput{AgentID: b.Revision.AgentID, Loop: ref, State: loop.LifecycleActive, EventID: "pilot-cross-activate"}); !errors.Is(err, orchestration.ErrDenied) {
		t.Fatalf("cross-owner activation: %v", err)
	}
	if _, err := svc.SetLoopLifecycleAs(ctx, subject, ref.ID, app.SetLoopLifecycleInput{AgentID: a.Revision.AgentID, Loop: ref, State: loop.LifecycleActive, EventID: "pilot-owner-activate"}); err != nil {
		t.Fatal(err)
	}
	// Same immutable publication, two distinct task inputs and separate Runs.
	for i, workspace := range []orchestration.WorkspaceAuthority{wa, wb} {
		key := workspace.Agent.ID
		encode := func(value any) json.RawMessage {
			data, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			return data
		}
		inputs := []graph.NormalizedInput{
			{PortID: "task", Type: graph.TypeString, Value: encode("Write the bounded " + key + " task")},
			{PortID: "workspace", Type: graph.TypeString, Value: encode(t.TempDir())},
			{PortID: "writable_files", Type: graph.TypeArray, Value: encode([]string{"result.txt"})},
			{PortID: "verify_file", Type: graph.TypeString, Value: encode("result.txt")},
			{PortID: "expected_text", Type: graph.TypeString, Value: encode(key)},
		}
		request := orchestration.SubmitLoopRequest{SubmitGraphRequest: orchestration.SubmitGraphRequest{
			SubmissionID: key + "-submission", IdempotencyKey: key + "-intent", SnapshotID: key + "-snapshot", QueueItemID: key + "-queue", GraphRunID: key + "-run", TransitionID: key + "-transition", RejectionID: key + "-reject", MaxAttempts: 1, Inputs: inputs,
		}, Participant: workspace.Agent, Loop: ref, NodeID: "direct"}
		decision, err := svc.SubmitLoopAs(ctx, subject, request)
		if err != nil || !decision.Created || decision.Accepted == nil {
			t.Fatalf("participant %d admission: %+v %v", i, decision, err)
		}
		accepted := decision.Accepted
		if accepted.Snapshot.SchemaVersion != graph.DirectRunSnapshotSchemaVersion || accepted.Snapshot.Graph != (reference.RevisionRef{}) || len(accepted.Snapshot.Participants) != 1 || accepted.Snapshot.Participants[0] != workspace.Agent || accepted.Submission.OwnerID != workspace.OwnerID || accepted.Submission.Authority != workspace.Ref() || accepted.QueueItem.MaxAttempts != 1 {
			t.Fatalf("exact direct participant/owner/budget lost: %+v", accepted)
		}
		topology, err := svc.Fleet.ResolveRunTopology(ctx, accepted.Snapshot)
		if err != nil || len(topology.Nodes) != 1 || topology.Nodes[0].Loop != ref || topology.Nodes[0].Participant != workspace.Agent {
			t.Fatalf("shared exact Loop use: %+v %v", topology, err)
		}
		replay, err := svc.SubmitLoopAs(ctx, subject, request)
		if err != nil || replay.Created || !reflect.DeepEqual(replay.Accepted, accepted) {
			t.Fatalf("same-intent replay: %+v %v", replay, err)
		}
		changed := request
		changed.MaxAttempts = 2
		if _, err := svc.SubmitLoopAs(ctx, subject, changed); !errors.Is(err, fleet.ErrConflict) {
			t.Fatalf("same-key budget expansion: %v", err)
		}
		wrong := request
		wrong.IdempotencyKey, wrong.SubmissionID, wrong.RejectionID = key+"-wrong-intent", key+"-wrong-submission", key+"-wrong-reject"
		wrong.Participant.Digest = a.Revision.Charter.Digest
		denied, err := svc.SubmitLoopAs(ctx, subject, wrong)
		if err != nil || denied.Accepted != nil || denied.Rejection == nil || denied.Rejection.ReasonCode != "workspace_denied" {
			t.Fatalf("substituted participant: %+v %v", denied, err)
		}
		item, err := svc.GetQueueItemAs(ctx, subject, request.QueueItemID)
		if err != nil || item.Projection.State != queue.StatePreparationPending || item.Disposition != nil || len(item.Claims) != 0 || len(item.Attempts) != 0 {
			t.Fatalf("admission must not invent runtime evidence: %+v %v", item, err)
		}
	}
	// Owner can append; foreign owner cannot. Original publication stays exact.
	next := revision
	next.Revision, next.PreviousDigest, next.Digest = 2, ref.Digest, ""
	input := app.PublishLoopInput{AgentID: b.Revision.AgentID, Revision: next, ExpectedPreviousDigest: ref.Digest, IdempotencyKey: "pilot-cross-revise"}
	if _, err := svc.PublishLoopAs(ctx, subject, input); !errors.Is(err, orchestration.ErrDenied) {
		t.Fatalf("cross-owner revision: %v", err)
	}
	input.AgentID, input.IdempotencyKey = a.Revision.AgentID, "pilot-owner-revise"
	if _, err := svc.PublishLoopAs(ctx, subject, input); err != nil {
		t.Fatal(err)
	}
	old, err := svc.GetLoopAs(ctx, subject, ref.ID, ref.Revision)
	if err != nil || old.Digest != ref.Digest || old.DoerReusable.MaxAttempts != 2 {
		t.Fatalf("historical definition changed: %+v %v", old, err)
	}
	loops, err := svc.FleetRepository.ListLoopRevisions(ctx)
	if err != nil || len(loops) != 2 {
		t.Fatalf("revision count: %d %v", len(loops), err)
	}
	graphs, err := svc.FleetRepository.ListGraphRevisions(ctx)
	if err != nil || len(graphs) != 0 {
		t.Fatalf("direct Runs must not publish Graphs: %d %v", len(graphs), err)
	}
	items, err := svc.ListQueueAs(ctx, subject)
	if err != nil || len(items) != 2 {
		t.Fatalf("expected exactly two admitted Runs: %d %v", len(items), err)
	}
}
