package orchestration

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/execution"
	"github.com/berryhill/aegis/internal/graph"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	fleetbadger "github.com/berryhill/aegis/internal/persistence/fleet/badger"
	"github.com/berryhill/aegis/internal/queue"
	"github.com/berryhill/aegis/internal/registry"
)

// Exercise the actual worker terminalization and canonical Badger lifecycle;
// no network listener, runtime/provider invocation or projected attempt forgery.
func TestQueueWorkerExhaustFinalClaimBadgerDurable(t *testing.T) {
	ctx := context.Background()
	service, base, _, subject, authority, _ := fleetServiceFixture(t)
	now := service.now()
	service.now = func() time.Time { return now }
	root := filepath.Join(t.TempDir(), "fleet-v1")
	disk, err := fleetbadger.Open(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = disk.Close() }()
	service.repository = disk
	fact := fleet.AuditFact{Event: core.AuditEvent{Type: "fixture.created", SubjectID: subject.ID, PrincipalID: subject.PrincipalID, Outcome: "succeeded", Reason: "isolated exhaustion fixture"}}
	agent := base.agent
	agent.SchemaVersion = registry.AgentRevisionSchemaVersion
	agent.Source = registry.FleetSource{FleetID: "fleet", Kind: "current-fleet", SourceID: agent.AgentID}
	agent.Ownership.OwnerID = subject.PrincipalID
	agent.Digest = ""
	agent, err = registry.SealRevision(agent)
	if err != nil {
		t.Fatal(err)
	}
	participant := revisionRef(agent.AgentID, agent.Revision, agent.Digest)
	if _, err = disk.RegisterAgent(ctx, registry.AgentRegistration{SchemaVersion: registry.AgentRegistrationSchemaVersion, AgentID: agent.AgentID, Source: agent.Source, InitialRevision: participant}, agent, fact); err != nil {
		t.Fatal(err)
	}
	lr, validation, err := loop.NewRevision(base.loop)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.PublishLoop(ctx, PublishLoopRequest{Subject: subject, Authority: authority, Publisher: participant, Publication: loop.PublishRequest{Revision: lr, Validation: validation, IdempotencyKey: "exhaust-loop"}}); err != nil {
		t.Fatal(err)
	}
	if _, _, err = service.SetLoopLifecycle(ctx, SetLoopLifecycleRequest{Subject: subject, Authority: authority, Publisher: participant, Loop: revisionRef(lr.LoopID, lr.Revision, lr.Digest), State: loop.LifecycleActive, EventID: "exhaust-active"}); err != nil {
		t.Fatal(err)
	}
	g := base.graph
	g.Nodes[0].Participant = participant
	g.Digest = ""
	g, gv, err := graph.NewRevision(g)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.PublishGraph(ctx, PublishGraphRequest{Subject: subject, Authority: authority, Publication: graph.PublishRequest{Revision: g, Validation: gv, IdempotencyKey: "exhaust-graph"}}); err != nil {
		t.Fatal(err)
	}
	d, err := service.PrepareGraphRun(ctx, SubmitGraphRequest{Subject: subject, Authority: authority, Graph: revisionRef(g.GraphID, g.Revision, g.Digest), SubmissionID: "submission", IdempotencyKey: "exhaust-submit", SnapshotID: "snapshot", QueueItemID: "queue", GraphRunID: "run", TransitionID: "queued", RejectionID: "rejected", MaxAttempts: 2})
	if err != nil || d.Accepted == nil {
		t.Fatalf("submit: %+v %v", d, err)
	}
	item := d.Accepted.QueueItem
	child, err := execution.NewLoopExecution(execution.LoopExecution{LoopExecutionID: "child", GraphRunID: "run", GraphNodeID: g.Nodes[0].ID, Loop: g.Nodes[0].Loop, Participant: participant, CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = disk.CreateLoopExecution(ctx, child, fact); err != nil {
		t.Fatal(err)
	}
	worker := newLifecycleTestWorker(t, disk, service)
	request := QueueTerminalRequest{Subject: subject, Authority: authority, QueueItemID: item.ItemID, CancellationID: "exhaust", TransitionID: "exhausted", ReasonCode: ReasonRetryExhausted}
	assertPremature := func() {
		t.Helper()
		before, err := disk.GetQueueProjection(ctx, item.ItemID)
		if err != nil {
			t.Fatal(err)
		}
		auditBefore, err := disk.AuditEvents(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := worker.Exhaust(ctx, request); !errors.Is(err, ErrWorkerDenied) {
			t.Fatalf("premature exhaustion admitted: %v", err)
		}
		after, err := disk.GetQueueProjection(ctx, item.ItemID)
		if err != nil {
			t.Fatal(err)
		}
		auditAfter, err := disk.AuditEvents(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(before, after) || !reflect.DeepEqual(auditBefore, auditAfter) {
			t.Fatal("premature denial mutated projection/audit")
		}
		if _, err := disk.GetDisposition(ctx, "exhaust/disposition"); !errors.Is(err, fleet.ErrNotFound) {
			t.Fatalf("premature terminal exists: %v", err)
		}
	}
	assertPremature()
	var finalClaim queue.Claim
	for i, suffix := range []string{"first", "final"} {
		claim, err := queue.NewClaim(queue.Claim{ClaimID: "claim-" + suffix, QueueItem: digestRef(item.ItemID, item.Digest), AttemptID: "attempt-" + suffix, WorkerID: "worker", Authority: authority, ClaimedAt: now, ExpiresAt: now.Add(time.Minute)})
		if err != nil {
			t.Fatal(err)
		}
		attempt, err := execution.NewAttempt(execution.Attempt{AttemptID: claim.AttemptID, GraphRunID: "run", LoopExecutionID: child.LoopExecutionID, QueueItem: claim.QueueItem, ClaimID: claim.ClaimID, AttemptNumber: uint32(i + 1), CreatedAt: now})
		if err != nil {
			t.Fatal(err)
		}
		transition, err := queue.NewTransition(queue.QueueTransition{TransitionID: "claimed-" + suffix, QueueItemID: item.ItemID, From: queue.StateQueued, To: queue.StateClaimed, ClaimID: claim.ClaimID, Reason: "worker lease acquired", OccurredAt: now})
		if err != nil {
			t.Fatal(err)
		}
		if err = disk.ClaimQueueItem(ctx, claim, attempt, transition, fact); err != nil {
			t.Fatal(err)
		}
		finalClaim = claim
		if i == 0 {
			assertPremature()
			now = claim.ExpiresAt
			if _, err := worker.Retry(ctx, QueueRetryRequest{Subject: subject, Authority: authority, QueueItemID: item.ItemID, RetryID: "reclaim", TransitionID: "requeued", Reclaimed: true, ReasonCode: ReasonLeaseReclaimed}); err != nil {
				t.Fatal(err)
			}
		}
	}
	terminal, err := worker.Exhaust(ctx, request)
	if err != nil || terminal.ClaimID != finalClaim.ClaimID || terminal.Reason != "retry_exhausted" {
		t.Fatalf("actual Exhaust: %+v %v", terminal, err)
	}
	before, err := disk.AuditEvents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = disk.Close(); err != nil {
		t.Fatal(err)
	}
	disk, err = fleetbadger.Open(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	projection, err := disk.GetQueueProjection(ctx, item.ItemID)
	if err != nil || projection.State != queue.StateFailed || projection.Attempts != item.MaxAttempts || projection.ActiveClaimID != "" {
		t.Fatalf("reopened failed projection: %+v %v", projection, err)
	}
	outcome, err := disk.GetDispositionByGraphRun(ctx, item.GraphRunID)
	if err != nil || outcome.State != execution.StateFailed || outcome.ReasonCode != "retry_exhausted" || outcome.AttemptID != finalClaim.AttemptID || outcome.LoopExecutionID != child.LoopExecutionID || outcome.QueueItem != finalClaim.QueueItem || len(outcome.ArtifactIDs) != 0 || len(outcome.ReceiptIDs) != 0 {
		t.Fatalf("canonical exhausted outcome: %+v %v", outcome, err)
	}
	cancellations, err := disk.ListQueueCancellations(ctx, item.ItemID)
	if err != nil || len(cancellations) != 1 || cancellations[0] != terminal {
		t.Fatalf("canonical exhaustion record: %+v %v", cancellations, err)
	}
	transitions, err := disk.ListQueueTransitions(ctx, item.ItemID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, tr := range transitions {
		if tr.TransitionID == request.TransitionID {
			found = tr.From == queue.StateClaimed && tr.To == queue.StateFailed && tr.ClaimID == finalClaim.ClaimID && tr.Reason == "retry_exhausted"
		}
	}
	if !found {
		t.Fatal("canonical failed/retry_exhausted transition missing")
	}
	after, err := disk.AuditEvents(ctx)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("audit reopen: %v", err)
	}
	worker.repository = disk
	service.repository = disk
	if _, err := worker.Exhaust(ctx, request); !errors.Is(err, ErrWorkerDenied) {
		t.Fatalf("terminal exhaustion replay admitted: %v", err)
	}
}
