package badger

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/execution"
	"github.com/berryhill/aegis/internal/graph"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	queue "github.com/berryhill/aegis/internal/queue"
	"github.com/berryhill/aegis/internal/reference"
)

func TestAcceptedSubmissionAndInitialClaimAreAtomicDurableFacts(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), schemaVersion)
	store, accepted := lifecycleFixture(t, ctx, root, "submit-key-1")

	created, err := store.AcceptSubmission(ctx, accepted, audit("submission.accepted", accepted.Submission.SubmissionID))
	if err != nil || !created {
		t.Fatalf("accept submission: created=%v err=%v", created, err)
	}
	created, err = store.AcceptSubmission(ctx, accepted, audit("submission.accepted", accepted.Submission.SubmissionID))
	if err != nil || created {
		t.Fatalf("exact accepted replay: created=%v err=%v", created, err)
	}
	assertAcceptedReadback(t, store, accepted)

	loopExecution, err := execution.NewLoopExecution(execution.LoopExecution{
		LoopExecutionID: "loop-execution-1",
		GraphRunID:      accepted.GraphRun.GraphRunID,
		GraphNodeID:     "echo",
		Loop:            accepted.Snapshot.Loops[0],
		Participant:     accepted.Snapshot.Participants[0],
		CreatedAt:       accepted.Submission.SubmittedAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	created, err = store.CreateLoopExecution(ctx, loopExecution, audit("loop-execution.created", loopExecution.LoopExecutionID))
	if err != nil || !created {
		t.Fatalf("create Loop execution: created=%v err=%v", created, err)
	}

	claim, attempt, transition := claimFixture(t, accepted, loopExecution, "claim-1", "attempt-1")
	if err = store.ClaimQueueItem(ctx, claim, attempt, transition, audit("queue.claimed", claim.ClaimID)); err != nil {
		t.Fatalf("claim queue item: %v", err)
	}
	if err = store.ClaimQueueItem(ctx, claim, attempt, transition, audit("queue.claimed", claim.ClaimID)); !errors.Is(err, fleet.ErrConflict) {
		t.Fatalf("second claim did not fail closed as a single-winner conflict: %v", err)
	}
	if got, err := store.GetClaim(ctx, claim.ClaimID); err != nil || got != claim {
		t.Fatalf("claim readback: got=%+v err=%v", got, err)
	}
	if got, err := store.GetAttempt(ctx, attempt.AttemptID); err != nil || got != attempt {
		t.Fatalf("attempt readback: got=%+v err=%v", got, err)
	}

	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	assertAcceptedReadback(t, store, accepted)
	if _, err = store.GetClaim(ctx, claim.ClaimID); err != nil {
		t.Fatalf("durable claim readback: %v", err)
	}
	if _, err = store.GetAttempt(ctx, attempt.AttemptID); err != nil {
		t.Fatalf("durable attempt readback: %v", err)
	}
}

func TestSubmissionOutcomeConflictsAndFailedAuditRollBackEveryRecord(t *testing.T) {
	ctx := context.Background()
	t.Run("accepted mutation rolls back with invalid authoritative audit", func(t *testing.T) {
		store, accepted := lifecycleFixture(t, ctx, filepath.Join(t.TempDir(), schemaVersion), "submit-key-rollback")
		defer store.Close()
		badAudit := audit("submission.accepted", accepted.Submission.SubmissionID)
		badAudit.Event.ID = "caller-assigned"
		if _, err := store.AcceptSubmission(ctx, accepted, badAudit); err == nil {
			t.Fatal("accepted submission allowed caller-assigned authoritative audit identity")
		}
		for name, read := range map[string]func() error{
			"snapshot":   func() error { _, e := store.GetGraphRunSnapshot(ctx, accepted.Snapshot.SnapshotID); return e },
			"submission": func() error { _, e := store.GetSubmission(ctx, accepted.Submission.SubmissionID); return e },
			"queue item": func() error { _, e := store.GetQueueItem(ctx, accepted.QueueItem.ItemID); return e },
			"Graph run":  func() error { _, e := store.GetGraphRun(ctx, accepted.GraphRun.GraphRunID); return e },
		} {
			if err := read(); !errors.Is(err, fleet.ErrNotFound) {
				t.Fatalf("failed transaction persisted %s: %v", name, err)
			}
		}
		events, err := store.AuditEvents(ctx)
		if err != nil || len(events) != 3 {
			t.Fatalf("failed transaction changed audit chain: count=%d err=%v", len(events), err)
		}
	})

	t.Run("one idempotency key cannot resolve to both outcomes", func(t *testing.T) {
		store, accepted := lifecycleFixture(t, ctx, filepath.Join(t.TempDir(), schemaVersion), "submit-key-outcome")
		defer store.Close()
		rejection, err := queue.NewRejection(queue.Rejection{RejectionID: "rejection-1", SubmissionID: accepted.Submission.SubmissionID, IdempotencyKey: accepted.Submission.IdempotencyKey, ReasonCode: "authority_denied", Reason: "fresh authority admission denied", RejectedAt: accepted.Submission.SubmittedAt})
		if err != nil {
			t.Fatal(err)
		}
		created, err := store.RejectSubmission(ctx, rejection, audit("submission.rejected", rejection.RejectionID))
		if err != nil || !created {
			t.Fatalf("reject submission: created=%v err=%v", created, err)
		}
		created, err = store.RejectSubmission(ctx, rejection, audit("submission.rejected", rejection.RejectionID))
		if err != nil || created {
			t.Fatalf("exact rejection replay: created=%v err=%v", created, err)
		}
		if _, err = store.AcceptSubmission(ctx, accepted, audit("submission.accepted", accepted.Submission.SubmissionID)); !errors.Is(err, fleet.ErrConflict) {
			t.Fatalf("rejected idempotency key was rebound to acceptance: %v", err)
		}
		if got, err := store.GetRejection(ctx, rejection.RejectionID); err != nil || got != rejection {
			t.Fatalf("durable rejection readback: got=%+v err=%v", got, err)
		}
		if _, err = store.GetQueueItem(ctx, accepted.QueueItem.ItemID); !errors.Is(err, fleet.ErrNotFound) {
			t.Fatalf("outcome conflict partially created queue item: %v", err)
		}
	})
}

func TestConcurrentInitialClaimsHaveExactlyOneWinner(t *testing.T) {
	ctx := context.Background()
	store, accepted := lifecycleFixture(t, ctx, filepath.Join(t.TempDir(), schemaVersion), "submit-key-concurrent")
	defer store.Close()
	if _, err := store.AcceptSubmission(ctx, accepted, audit("submission.accepted", accepted.Submission.SubmissionID)); err != nil {
		t.Fatal(err)
	}
	loopExecution, err := execution.NewLoopExecution(execution.LoopExecution{LoopExecutionID: "loop-execution-concurrent", GraphRunID: accepted.GraphRun.GraphRunID, GraphNodeID: "echo", Loop: accepted.Snapshot.Loops[0], Participant: accepted.Snapshot.Participants[0], CreatedAt: accepted.Submission.SubmittedAt})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.CreateLoopExecution(ctx, loopExecution, audit("loop-execution.created", loopExecution.LoopExecutionID)); err != nil {
		t.Fatal(err)
	}
	claimA, attemptA, transitionA := claimFixture(t, accepted, loopExecution, "claim-a", "attempt-a")
	claimB, attemptB, transitionB := claimFixture(t, accepted, loopExecution, "claim-b", "attempt-b")
	transitionB.TransitionID = "transition-claimed-b"
	transitionB, err = queue.NewTransition(transitionB)
	if err != nil {
		t.Fatal(err)
	}

	type candidate struct {
		claim      queue.Claim
		attempt    execution.Attempt
		transition queue.QueueTransition
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	var workers sync.WaitGroup
	for _, value := range []candidate{{claimA, attemptA, transitionA}, {claimB, attemptB, transitionB}} {
		value := value
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			results <- store.ClaimQueueItem(ctx, value.claim, value.attempt, value.transition, audit("queue.claimed", value.claim.ClaimID))
		}()
	}
	close(start)
	workers.Wait()
	close(results)
	successes, conflicts := 0, 0
	for result := range results {
		switch {
		case result == nil:
			successes++
		case errors.Is(result, fleet.ErrConflict):
			conflicts++
		default:
			t.Fatalf("unexpected concurrent claim result: %v", result)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("initial claim was not single-winner: successes=%d conflicts=%d", successes, conflicts)
	}
	stored := 0
	for _, claimID := range []string{claimA.ClaimID, claimB.ClaimID} {
		if _, err := store.GetClaim(ctx, claimID); err == nil {
			stored++
		} else if !errors.Is(err, fleet.ErrNotFound) {
			t.Fatalf("claim readback %q: %v", claimID, err)
		}
	}
	if stored != 1 {
		t.Fatalf("concurrent claim transaction persisted %d claim records", stored)
	}
}

func lifecycleFixture(t *testing.T, ctx context.Context, root, idempotencyKey string) (*Store, fleet.AcceptedSubmission) {
	t.Helper()
	store, err := Open(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	registration, agent := agentFixture(t)
	if _, err = store.RegisterAgent(ctx, registration, agent, audit("agent.registered", agent.AgentID)); err != nil {
		t.Fatal(err)
	}
	loopRevision, loopValidation := loopFixture(t)
	if _, err = store.PublishLoop(ctx, loop.PublishRequest{Revision: loopRevision, Validation: loopValidation, IdempotencyKey: "loop-lifecycle"}, audit("loop.published", loopRevision.LoopID)); err != nil {
		t.Fatal(err)
	}
	graphRevision, graphValidation := graphFixture(t, agent, loopRevision)
	if _, err = store.PublishGraph(ctx, graph.PublishRequest{Revision: graphRevision, Validation: graphValidation, IdempotencyKey: "graph-lifecycle"}, audit("graph.published", graphRevision.GraphID)); err != nil {
		t.Fatal(err)
	}
	snapshot, err := graph.NewRunSnapshot("snapshot-lifecycle", graphRevision, []graph.NormalizedInput{{PortID: "value", Type: graph.TypeString, Value: []byte(`"hello"`)}})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	authority := reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: "authority-1", Digest: "sha256:" + strings.Repeat("f", 64)}
	submission, err := queue.NewSubmission(queue.Submission{SubmissionID: "submission-1", IdempotencyKey: idempotencyKey, Snapshot: lifecycleDigestRef(snapshot.SnapshotID, snapshot.Digest), Authority: authority, MandateID: "mandate-1", Runtime: "hermes-agent", SubmittedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	item, err := queue.NewItem(queue.Item{ItemID: "queue-item-1", Submission: lifecycleDigestRef(submission.SubmissionID, submission.Digest), Snapshot: submission.Snapshot, Authority: authority, GraphRunID: "graph-run-1", MaxAttempts: 3, EnqueuedAt: now, AvailableAt: now})
	if err != nil {
		t.Fatal(err)
	}
	run, err := execution.NewGraphRun(execution.GraphRun{GraphRunID: item.GraphRunID, QueueItem: lifecycleDigestRef(item.ItemID, item.Digest), Snapshot: submission.Snapshot, Authority: authority, CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	transition, err := queue.NewTransition(queue.QueueTransition{TransitionID: "transition-queued-1", QueueItemID: item.ItemID, To: queue.StateQueued, Reason: "submission accepted", OccurredAt: now})
	if err != nil {
		t.Fatal(err)
	}
	return store, fleet.AcceptedSubmission{Snapshot: snapshot, Submission: submission, QueueItem: item, GraphRun: run, InitialTransition: transition}
}

func claimFixture(t *testing.T, accepted fleet.AcceptedSubmission, loopExecution execution.LoopExecution, claimID, attemptID string) (queue.Claim, execution.Attempt, queue.QueueTransition) {
	t.Helper()
	now := accepted.Submission.SubmittedAt.Add(time.Second)
	claim, err := queue.NewClaim(queue.Claim{ClaimID: claimID, QueueItem: lifecycleDigestRef(accepted.QueueItem.ItemID, accepted.QueueItem.Digest), AttemptID: attemptID, WorkerID: "worker-1", Authority: accepted.QueueItem.Authority, ClaimedAt: now, ExpiresAt: now.Add(time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := execution.NewAttempt(execution.Attempt{AttemptID: attemptID, GraphRunID: accepted.GraphRun.GraphRunID, LoopExecutionID: loopExecution.LoopExecutionID, QueueItem: claim.QueueItem, ClaimID: claimID, AttemptNumber: 1, CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	transition, err := queue.NewTransition(queue.QueueTransition{TransitionID: "transition-claimed-1", QueueItemID: accepted.QueueItem.ItemID, From: queue.StateQueued, To: queue.StateClaimed, ClaimID: claimID, Reason: "worker lease acquired", OccurredAt: now})
	if err != nil {
		t.Fatal(err)
	}
	return claim, attempt, transition
}

func lifecycleDigestRef(id, digest string) reference.DigestRef {
	return reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: id, Digest: digest}
}

func assertAcceptedReadback(t *testing.T, store *Store, accepted fleet.AcceptedSubmission) {
	t.Helper()
	ctx := context.Background()
	if got, err := store.GetSubmission(ctx, accepted.Submission.SubmissionID); err != nil || got != accepted.Submission {
		t.Fatalf("submission readback: got=%+v err=%v", got, err)
	}
	if got, err := store.GetQueueItem(ctx, accepted.QueueItem.ItemID); err != nil || got != accepted.QueueItem {
		t.Fatalf("queue item readback: got=%+v err=%v", got, err)
	}
	if got, err := store.GetGraphRun(ctx, accepted.GraphRun.GraphRunID); err != nil || got != accepted.GraphRun {
		t.Fatalf("Graph run readback: got=%+v err=%v", got, err)
	}
}
