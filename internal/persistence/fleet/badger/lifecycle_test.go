package badger

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/disposition"
	"github.com/berryhill/aegis/internal/evidence"
	"github.com/berryhill/aegis/internal/execution"
	"github.com/berryhill/aegis/internal/graph"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	queue "github.com/berryhill/aegis/internal/queue"
	"github.com/berryhill/aegis/internal/reference"
	badgerdb "github.com/dgraph-io/badger/v4"
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
	loopExecutions, err := store.ListLoopExecutions(ctx)
	if err != nil || len(loopExecutions) != 1 || loopExecutions[0] != loopExecution {
		t.Fatalf("Loop execution collection included a binding or lost the record: got=%+v err=%v", loopExecutions, err)
	}
	attempts, err := store.ListAttempts(ctx)
	if err != nil || len(attempts) != 1 || attempts[0] != attempt {
		t.Fatalf("attempt collection readback: got=%+v err=%v", attempts, err)
	}

	artifactDigest := "sha256:" + strings.Repeat("a", 64)
	artifact := evidence.RuntimeArtifact{ID: "artifact-1", OwnerID: "agent-1", ActionID: "work", RunID: loopExecution.LoopExecutionID, AuthorityContextID: claim.Authority.ID, AuthorityContextDigest: claim.Authority.Digest, Digest: artifactDigest, ContentRef: artifactDigest, MediaType: "application/json", CreatedAt: claim.ClaimedAt}
	receipt := evidence.VerificationReceipt{ID: "receipt-1", ArtifactID: artifact.ID, ActionID: artifact.ActionID, RunID: artifact.RunID, OwnerID: artifact.OwnerID, AuthorityContextID: artifact.AuthorityContextID, AuthorityContextDigest: artifact.AuthorityContextDigest, VerifierID: evidence.ArtifactVerifierID, PolicyVersion: evidence.VerifierPolicyV1, Claim: "exact-output", ExpectedDigest: artifactDigest, ObservedDigest: artifactDigest, Outcome: evidence.Passed, EvidenceRef: "sha256:" + strings.Repeat("b", 64), ObservedAt: claim.ClaimedAt}
	dispositionRecord, err := disposition.New(disposition.Record{DispositionID: "disposition-1", GraphRunID: attempt.GraphRunID, LoopExecutionID: attempt.LoopExecutionID, AttemptID: attempt.AttemptID, QueueItem: attempt.QueueItem, Authority: claim.Authority, State: execution.StateSucceeded, ReasonCode: "evidence_satisfied", ArtifactIDs: []string{artifact.ID}, ReceiptIDs: []string{receipt.ID}, OccurredAt: claim.ClaimedAt})
	if err != nil {
		t.Fatal(err)
	}
	terminal, err := queue.NewTransition(queue.QueueTransition{TransitionID: "transition-terminal-1", QueueItemID: accepted.QueueItem.ItemID, From: queue.StateClaimed, To: queue.StateSucceeded, ClaimID: claim.ClaimID, Reason: "evidence_satisfied", OccurredAt: claim.ClaimedAt})
	if err != nil {
		t.Fatal(err)
	}
	if err = store.CompleteQueueItem(ctx, fleet.Completion{Claim: claim, Artifact: &artifact, Receipts: []evidence.VerificationReceipt{receipt}, Disposition: dispositionRecord, Transition: terminal}, audit("queue.completed", dispositionRecord.DispositionID)); err != nil {
		t.Fatalf("complete queue item: %v", err)
	}
	if got, err := store.GetDisposition(ctx, dispositionRecord.DispositionID); err != nil || got.Digest != dispositionRecord.Digest {
		t.Fatalf("disposition readback: got=%+v err=%v", got, err)
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
	if got, err := store.GetRuntimeArtifact(ctx, artifact.ID); err != nil || got.ID != artifact.ID {
		t.Fatalf("durable artifact readback: got=%+v err=%v", got, err)
	}
	if got, err := store.GetVerificationReceipt(ctx, receipt.ID); err != nil || got.ID != receipt.ID {
		t.Fatalf("durable receipt readback: got=%+v err=%v", got, err)
	}
	if got, err := store.GetDisposition(ctx, dispositionRecord.DispositionID); err != nil || got.Digest != dispositionRecord.Digest {
		t.Fatalf("durable disposition readback: got=%+v err=%v", got, err)
	}
}

func TestCompletionRejectsArtifactCausalitySubstitutionAtomically(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name   string
		mutate func(*fleet.Completion)
	}{
		{
			name: "authority context ID",
			mutate: func(completion *fleet.Completion) {
				completion.Artifact.AuthorityContextID = "authority-substituted"
				completion.Receipts[0].AuthorityContextID = completion.Artifact.AuthorityContextID
			},
		},
		{
			name: "authority context digest",
			mutate: func(completion *fleet.Completion) {
				completion.Artifact.AuthorityContextDigest = "sha256:" + strings.Repeat("c", 64)
				completion.Receipts[0].AuthorityContextDigest = completion.Artifact.AuthorityContextDigest
			},
		},
		{
			name: "run ID",
			mutate: func(completion *fleet.Completion) {
				completion.Artifact.RunID = "loop-execution-substituted"
				completion.Receipts[0].RunID = completion.Artifact.RunID
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store, completion := completionFixture(t, ctx, filepath.Join(t.TempDir(), schemaVersion))
			defer store.Close()
			test.mutate(&completion)

			eventsBefore, err := store.AuditEvents(ctx)
			if err != nil {
				t.Fatal(err)
			}
			err = store.CompleteQueueItem(ctx, completion, audit("queue.completed", completion.Disposition.DispositionID))
			if !errors.Is(err, fleet.ErrConflict) {
				t.Fatalf("substituted %s did not fail closed: %v", test.name, err)
			}
			for name, read := range map[string]func() error{
				"artifact":    func() error { _, e := store.GetRuntimeArtifact(ctx, completion.Artifact.ID); return e },
				"receipt":     func() error { _, e := store.GetVerificationReceipt(ctx, completion.Receipts[0].ID); return e },
				"disposition": func() error { _, e := store.GetDisposition(ctx, completion.Disposition.DispositionID); return e },
			} {
				if readErr := read(); !errors.Is(readErr, fleet.ErrNotFound) {
					t.Fatalf("rejected completion persisted %s: %v", name, readErr)
				}
			}
			if err = store.view(ctx, func(txn *badgerdb.Txn) error {
				_, found, readErr := optional(txn, key(familyQueueTransition, completion.Claim.QueueItem.ID, completion.Transition.TransitionID))
				if readErr != nil {
					return readErr
				}
				if found {
					t.Fatal("rejected completion persisted terminal queue transition")
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			eventsAfter, err := store.AuditEvents(ctx)
			if err != nil || len(eventsAfter) != len(eventsBefore) {
				t.Fatalf("rejected completion changed audit chain: before=%d after=%d err=%v", len(eventsBefore), len(eventsAfter), err)
			}
		})
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

func TestClaimFailsClosedWhenDependencyIsMissing(t *testing.T) {
	ctx := context.Background()
	store, accepted := lifecycleFixture(t, ctx, filepath.Join(t.TempDir(), schemaVersion), "submit-key-missing-dependency")
	defer store.Close()
	accepted.QueueItem.Dependencies = []reference.DigestRef{lifecycleDigestRef("missing-dependency", "sha256:"+strings.Repeat("d", 64))}
	item, err := queue.NewItem(accepted.QueueItem)
	if err != nil {
		t.Fatal(err)
	}
	accepted.QueueItem = item
	run, err := execution.NewGraphRun(execution.GraphRun{GraphRunID: accepted.GraphRun.GraphRunID, QueueItem: lifecycleDigestRef(item.ItemID, item.Digest), Snapshot: accepted.GraphRun.Snapshot, Authority: accepted.GraphRun.Authority, CreatedAt: accepted.GraphRun.CreatedAt})
	if err != nil {
		t.Fatal(err)
	}
	accepted.GraphRun = run
	if _, err = store.AcceptSubmission(ctx, accepted, audit("submission.accepted", accepted.Submission.SubmissionID)); err != nil {
		t.Fatal(err)
	}
	loopExecution, err := execution.NewLoopExecution(execution.LoopExecution{LoopExecutionID: "loop-execution-dependency", GraphRunID: accepted.GraphRun.GraphRunID, GraphNodeID: "echo", Loop: accepted.Snapshot.Loops[0], Participant: accepted.Snapshot.Participants[0], CreatedAt: accepted.Submission.SubmittedAt})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.CreateLoopExecution(ctx, loopExecution, audit("loop-execution.created", loopExecution.LoopExecutionID)); err != nil {
		t.Fatal(err)
	}
	claim, attempt, transition := claimFixture(t, accepted, loopExecution, "claim-dependency", "attempt-dependency")
	if err = store.ClaimQueueItem(ctx, claim, attempt, transition, audit("queue.claimed", claim.ClaimID)); !errors.Is(err, fleet.ErrConflict) {
		t.Fatalf("claim with unresolved dependency did not fail closed: %v", err)
	}
	if _, err = store.GetClaim(ctx, claim.ClaimID); !errors.Is(err, fleet.ErrNotFound) {
		t.Fatalf("denied dependency claim persisted claim: %v", err)
	}
	projection, err := store.GetQueueProjection(ctx, item.ItemID)
	if err != nil || projection.State != queue.StateQueued || projection.Attempts != 0 {
		t.Fatalf("denied dependency claim mutated projection: got=%+v err=%v", projection, err)
	}
}

func TestClaimRejectsProjectionWithLoweredCanonicalAttemptCountAtomically(t *testing.T) {
	ctx := context.Background()
	store, accepted, loopExecution, projection := retriedQueueFixture(t, ctx, "lowered-attempts")
	defer store.Close()

	projection.Attempts--
	tamperQueueProjection(t, store, projection)
	claim, attempt, transition := nextClaimFixture(t, accepted, loopExecution, projection.AvailableAt, 1, "lowered-attempts")
	assertClaimDeniedAtomically(t, ctx, store, claim, attempt, transition)
}

func TestClaimRejectsProjectionWithShortenedCanonicalRetryAvailabilityAtomically(t *testing.T) {
	ctx := context.Background()
	store, accepted, loopExecution, projection := retriedQueueFixture(t, ctx, "shortened-availability")
	defer store.Close()

	projection.AvailableAt = projection.AvailableAt.Add(-time.Second)
	tamperQueueProjection(t, store, projection)
	claim, attempt, transition := nextClaimFixture(t, accepted, loopExecution, projection.AvailableAt, 2, "shortened-availability")
	assertClaimDeniedAtomically(t, ctx, store, claim, attempt, transition)
}

func TestClaimRejectsProjectedDependencySuccessWithoutCanonicalDispositionAtomically(t *testing.T) {
	ctx := context.Background()
	store, accepted := lifecycleFixture(t, ctx, filepath.Join(t.TempDir(), schemaVersion), "submit-key-false-dependency-success")
	defer store.Close()

	dependency, err := queue.NewItem(queue.Item{ItemID: "dependency-false-success", Submission: accepted.QueueItem.Submission, Snapshot: accepted.QueueItem.Snapshot, Authority: accepted.QueueItem.Authority, GraphRunID: "dependency-graph-run", MaxAttempts: 1, EnqueuedAt: accepted.QueueItem.EnqueuedAt, AvailableAt: accepted.QueueItem.AvailableAt})
	if err != nil {
		t.Fatal(err)
	}
	accepted.QueueItem.Dependencies = []reference.DigestRef{lifecycleDigestRef(dependency.ItemID, dependency.Digest)}
	accepted.QueueItem, err = queue.NewItem(accepted.QueueItem)
	if err != nil {
		t.Fatal(err)
	}
	accepted.GraphRun, err = execution.NewGraphRun(execution.GraphRun{GraphRunID: accepted.GraphRun.GraphRunID, QueueItem: lifecycleDigestRef(accepted.QueueItem.ItemID, accepted.QueueItem.Digest), Snapshot: accepted.GraphRun.Snapshot, Authority: accepted.GraphRun.Authority, CreatedAt: accepted.GraphRun.CreatedAt})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.AcceptSubmission(ctx, accepted, audit("submission.accepted", accepted.Submission.SubmissionID)); err != nil {
		t.Fatal(err)
	}
	dependencyTransition := mustQueueTransition(t, queue.QueueTransition{TransitionID: "dependency-projected-success", QueueItemID: dependency.ItemID, From: queue.StateClaimed, To: queue.StateSucceeded, ClaimID: "dependency-claim", Reason: "projected only", OccurredAt: accepted.Submission.SubmittedAt})
	dependencyProjection, err := queue.NewProjection(queue.Projection{QueueItemID: dependency.ItemID, State: queue.StateSucceeded, Attempts: 1, AvailableAt: dependency.AvailableAt, LastTransitionID: dependencyTransition.TransitionID, UpdatedAt: dependencyTransition.OccurredAt})
	if err != nil {
		t.Fatal(err)
	}
	dependencyWire, err := queue.MarshalItem(dependency)
	if err != nil {
		t.Fatal(err)
	}
	transitionWire, err := queue.MarshalTransition(dependencyTransition)
	if err != nil {
		t.Fatal(err)
	}
	projectionWire, err := queue.MarshalProjection(dependencyProjection)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.update(ctx, func(txn *badgerdb.Txn) error {
		for _, entry := range []struct{ k, v []byte }{
			{key(familyQueueItem, dependency.ItemID), dependencyWire},
			{key(familyQueueTransition, dependency.ItemID, dependencyTransition.TransitionID), transitionWire},
			{key(familyQueueProjection, dependency.ItemID), projectionWire},
		} {
			if writeErr := create(txn, entry.k, entry.v); writeErr != nil {
				return writeErr
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	loopExecution, err := execution.NewLoopExecution(execution.LoopExecution{LoopExecutionID: "loop-execution-false-dependency", GraphRunID: accepted.GraphRun.GraphRunID, GraphNodeID: "echo", Loop: accepted.Snapshot.Loops[0], Participant: accepted.Snapshot.Participants[0], CreatedAt: accepted.Submission.SubmittedAt})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.CreateLoopExecution(ctx, loopExecution, audit("loop-execution.created", loopExecution.LoopExecutionID)); err != nil {
		t.Fatal(err)
	}
	claim, attempt, transition := claimFixture(t, accepted, loopExecution, "claim-false-dependency", "attempt-false-dependency")
	assertClaimDeniedAtomically(t, ctx, store, claim, attempt, transition)
}

func TestRetryReclaimAndCancellationAreAtomicDurableLifecycleFacts(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), schemaVersion)
	store, accepted := lifecycleFixture(t, ctx, root, "submit-key-retry-reclaim")
	if _, err := store.AcceptSubmission(ctx, accepted, audit("submission.accepted", accepted.Submission.SubmissionID)); err != nil {
		t.Fatal(err)
	}
	initial, err := store.GetQueueProjection(ctx, accepted.QueueItem.ItemID)
	if err != nil || initial.State != queue.StateQueued || initial.Attempts != 0 || initial.ActiveClaimID != "" {
		t.Fatalf("initial projection: got=%+v err=%v", initial, err)
	}
	loopExecution, err := execution.NewLoopExecution(execution.LoopExecution{LoopExecutionID: "loop-execution-retry", GraphRunID: accepted.GraphRun.GraphRunID, GraphNodeID: "echo", Loop: accepted.Snapshot.Loops[0], Participant: accepted.Snapshot.Participants[0], CreatedAt: accepted.Submission.SubmittedAt})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.CreateLoopExecution(ctx, loopExecution, audit("loop-execution.created", loopExecution.LoopExecutionID)); err != nil {
		t.Fatal(err)
	}
	claim1, attempt1, claimed1 := claimFixture(t, accepted, loopExecution, "claim-retry-1", "attempt-retry-1")
	if err = store.ClaimQueueItem(ctx, claim1, attempt1, claimed1, audit("queue.claimed", claim1.ClaimID)); err != nil {
		t.Fatal(err)
	}
	claimedProjection, err := store.GetQueueProjection(ctx, accepted.QueueItem.ItemID)
	if err != nil || claimedProjection.State != queue.StateClaimed || claimedProjection.Attempts != 1 || claimedProjection.ActiveClaimID != claim1.ClaimID {
		t.Fatalf("claimed projection: got=%+v err=%v", claimedProjection, err)
	}

	tooEarly := mustQueueRetry(t, queue.Retry{RetryID: "retry-too-early", QueueItem: claim1.QueueItem, ClaimID: claim1.ClaimID, AttemptNumber: 1, AvailableAt: claim1.ExpiresAt.Add(time.Second), Reclaimed: true, Reason: "lease expired", OccurredAt: claim1.ExpiresAt.Add(-time.Nanosecond)})
	tooEarlyTransition := mustQueueTransition(t, queue.QueueTransition{TransitionID: "transition-retry-too-early", QueueItemID: accepted.QueueItem.ItemID, From: queue.StateClaimed, To: queue.StateQueued, ClaimID: claim1.ClaimID, Reason: tooEarly.Reason, OccurredAt: tooEarly.OccurredAt})
	if err = store.RetryQueueItem(ctx, fleet.RetryMutation{Retry: tooEarly, Transition: tooEarlyTransition}, audit("queue.reclaimed", tooEarly.RetryID)); !errors.Is(err, fleet.ErrConflict) {
		t.Fatalf("pre-expiry reclaim did not fail closed: %v", err)
	}

	retryAt := claim1.ExpiresAt
	availableAt := retryAt.Add(time.Minute)
	expiredOrdinaryRetry := mustQueueRetry(t, queue.Retry{RetryID: "retry-expired-without-reclaim", QueueItem: claim1.QueueItem, ClaimID: claim1.ClaimID, AttemptNumber: 1, AvailableAt: availableAt, Reclaimed: false, Reason: "ordinary retry after expiry", OccurredAt: retryAt})
	expiredOrdinaryTransition := mustQueueTransition(t, queue.QueueTransition{TransitionID: "transition-expired-without-reclaim", QueueItemID: accepted.QueueItem.ItemID, From: queue.StateClaimed, To: queue.StateQueued, ClaimID: claim1.ClaimID, Reason: expiredOrdinaryRetry.Reason, OccurredAt: retryAt})
	if err = store.RetryQueueItem(ctx, fleet.RetryMutation{Retry: expiredOrdinaryRetry, Transition: expiredOrdinaryTransition}, audit("queue.retried", expiredOrdinaryRetry.RetryID)); !errors.Is(err, fleet.ErrConflict) {
		t.Fatalf("expired lease retried without canonical reclaim: %v", err)
	}
	retry := mustQueueRetry(t, queue.Retry{RetryID: "retry-1", QueueItem: claim1.QueueItem, ClaimID: claim1.ClaimID, AttemptNumber: 1, AvailableAt: availableAt, Reclaimed: true, Reason: "lease expired", OccurredAt: retryAt})
	retryTransition := mustQueueTransition(t, queue.QueueTransition{TransitionID: "transition-retry-1", QueueItemID: accepted.QueueItem.ItemID, From: queue.StateClaimed, To: queue.StateQueued, ClaimID: claim1.ClaimID, Reason: retry.Reason, OccurredAt: retryAt})
	if err = store.RetryQueueItem(ctx, fleet.RetryMutation{Retry: retry, Transition: retryTransition}, audit("queue.reclaimed", retry.RetryID)); err != nil {
		t.Fatalf("reclaim expired lease: %v", err)
	}
	retriedProjection, err := store.GetQueueProjection(ctx, accepted.QueueItem.ItemID)
	if err != nil || retriedProjection.State != queue.StateQueued || retriedProjection.Attempts != 1 || retriedProjection.ActiveClaimID != "" || !retriedProjection.AvailableAt.Equal(availableAt) {
		t.Fatalf("retried projection: got=%+v err=%v", retriedProjection, err)
	}

	claim2 := mustQueueClaim(t, queue.Claim{ClaimID: "claim-retry-2", QueueItem: claim1.QueueItem, AttemptID: "attempt-retry-2", WorkerID: "worker-2", Authority: accepted.QueueItem.Authority, ClaimedAt: availableAt, ExpiresAt: availableAt.Add(time.Minute)})
	attempt2, err := execution.NewAttempt(execution.Attempt{AttemptID: claim2.AttemptID, GraphRunID: accepted.GraphRun.GraphRunID, LoopExecutionID: loopExecution.LoopExecutionID, QueueItem: claim2.QueueItem, ClaimID: claim2.ClaimID, AttemptNumber: 2, CreatedAt: claim2.ClaimedAt})
	if err != nil {
		t.Fatal(err)
	}
	claimed2 := mustQueueTransition(t, queue.QueueTransition{TransitionID: "transition-claimed-2", QueueItemID: accepted.QueueItem.ItemID, From: queue.StateQueued, To: queue.StateClaimed, ClaimID: claim2.ClaimID, Reason: "worker lease acquired", OccurredAt: claim2.ClaimedAt})
	claimBeforeBackoff := claim2
	claimBeforeBackoff.ClaimID = "claim-before-backoff"
	claimBeforeBackoff.AttemptID = "attempt-before-backoff"
	claimBeforeBackoff.ClaimedAt = availableAt.Add(-time.Nanosecond)
	claimBeforeBackoff.ExpiresAt = availableAt.Add(time.Minute)
	claimBeforeBackoff = mustQueueClaim(t, claimBeforeBackoff)
	attemptBeforeBackoff, err := execution.NewAttempt(execution.Attempt{AttemptID: claimBeforeBackoff.AttemptID, GraphRunID: accepted.GraphRun.GraphRunID, LoopExecutionID: loopExecution.LoopExecutionID, QueueItem: claimBeforeBackoff.QueueItem, ClaimID: claimBeforeBackoff.ClaimID, AttemptNumber: 2, CreatedAt: claimBeforeBackoff.ClaimedAt})
	if err != nil {
		t.Fatal(err)
	}
	transitionBeforeBackoff := mustQueueTransition(t, queue.QueueTransition{TransitionID: "transition-before-backoff", QueueItemID: accepted.QueueItem.ItemID, From: queue.StateQueued, To: queue.StateClaimed, ClaimID: claimBeforeBackoff.ClaimID, Reason: "too early", OccurredAt: claimBeforeBackoff.ClaimedAt})
	if err = store.ClaimQueueItem(ctx, claimBeforeBackoff, attemptBeforeBackoff, transitionBeforeBackoff, audit("queue.claimed", claimBeforeBackoff.ClaimID)); !errors.Is(err, fleet.ErrConflict) {
		t.Fatalf("claim before retry availability did not fail closed: %v", err)
	}
	if err = store.ClaimQueueItem(ctx, claim2, attempt2, claimed2, audit("queue.claimed", claim2.ClaimID)); err != nil {
		t.Fatalf("second bounded attempt: %v", err)
	}

	cancelAt := claim2.ClaimedAt.Add(time.Second)
	cancellation := mustQueueCancellation(t, queue.Cancellation{CancellationID: "cancel-1", QueueItem: claim2.QueueItem, ClaimID: claim2.ClaimID, Reason: "operator cancelled", OccurredAt: cancelAt})
	cancelled := mustQueueTransition(t, queue.QueueTransition{TransitionID: "transition-cancelled-1", QueueItemID: accepted.QueueItem.ItemID, From: queue.StateClaimed, To: queue.StateCancelled, ClaimID: claim2.ClaimID, Reason: cancellation.Reason, OccurredAt: cancelAt})
	if err = store.CancelQueueItem(ctx, fleet.CancellationMutation{Cancellation: cancellation, Transition: cancelled}, audit("queue.cancelled", cancellation.CancellationID)); err != nil {
		t.Fatalf("cancel claimed item: %v", err)
	}
	if err = store.CancelQueueItem(ctx, fleet.CancellationMutation{Cancellation: cancellation, Transition: cancelled}, audit("queue.cancelled", cancellation.CancellationID)); !errors.Is(err, fleet.ErrConflict) {
		t.Fatalf("repeat cancellation did not fail closed: %v", err)
	}
	cancelledProjection, err := store.GetQueueProjection(ctx, accepted.QueueItem.ItemID)
	if err != nil || cancelledProjection.State != queue.StateCancelled || cancelledProjection.Attempts != 2 || cancelledProjection.ActiveClaimID != "" {
		t.Fatalf("cancelled projection: got=%+v err=%v", cancelledProjection, err)
	}

	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	got, err := store.GetQueueProjection(ctx, accepted.QueueItem.ItemID)
	if err != nil || got.Digest != cancelledProjection.Digest {
		t.Fatalf("durable cancelled projection: got=%+v err=%v", got, err)
	}
	if err = store.view(ctx, func(txn *badgerdb.Txn) error {
		for _, recordKey := range [][]byte{key(familyQueueRetry, retry.RetryID), key(familyQueueCancellation, cancellation.CancellationID)} {
			if _, readErr := get(txn, recordKey); readErr != nil {
				return readErr
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("durable lifecycle fact readback: %v", err)
	}
}

func mustQueueRetry(t *testing.T, value queue.Retry) queue.Retry {
	t.Helper()
	got, err := queue.NewRetry(value)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func mustQueueCancellation(t *testing.T, value queue.Cancellation) queue.Cancellation {
	t.Helper()
	got, err := queue.NewCancellation(value)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func mustQueueClaim(t *testing.T, value queue.Claim) queue.Claim {
	t.Helper()
	got, err := queue.NewClaim(value)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func mustQueueTransition(t *testing.T, value queue.QueueTransition) queue.QueueTransition {
	t.Helper()
	got, err := queue.NewTransition(value)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func retriedQueueFixture(t *testing.T, ctx context.Context, suffix string) (*Store, fleet.AcceptedSubmission, execution.LoopExecution, queue.Projection) {
	t.Helper()
	store, accepted := lifecycleFixture(t, ctx, filepath.Join(t.TempDir(), schemaVersion), "submit-key-"+suffix)
	if _, err := store.AcceptSubmission(ctx, accepted, audit("submission.accepted", accepted.Submission.SubmissionID)); err != nil {
		t.Fatal(err)
	}
	loopExecution, err := execution.NewLoopExecution(execution.LoopExecution{LoopExecutionID: "loop-execution-" + suffix, GraphRunID: accepted.GraphRun.GraphRunID, GraphNodeID: "echo", Loop: accepted.Snapshot.Loops[0], Participant: accepted.Snapshot.Participants[0], CreatedAt: accepted.Submission.SubmittedAt})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.CreateLoopExecution(ctx, loopExecution, audit("loop-execution.created", loopExecution.LoopExecutionID)); err != nil {
		t.Fatal(err)
	}
	claim, attempt, claimed := claimFixture(t, accepted, loopExecution, "claim-first-"+suffix, "attempt-first-"+suffix)
	if err = store.ClaimQueueItem(ctx, claim, attempt, claimed, audit("queue.claimed", claim.ClaimID)); err != nil {
		t.Fatal(err)
	}
	retryAt := claim.ExpiresAt
	retry := mustQueueRetry(t, queue.Retry{RetryID: "retry-" + suffix, QueueItem: claim.QueueItem, ClaimID: claim.ClaimID, AttemptNumber: 1, AvailableAt: retryAt.Add(time.Minute), Reclaimed: true, Reason: "lease expired", OccurredAt: retryAt})
	retried := mustQueueTransition(t, queue.QueueTransition{TransitionID: "transition-retry-" + suffix, QueueItemID: accepted.QueueItem.ItemID, From: queue.StateClaimed, To: queue.StateQueued, ClaimID: claim.ClaimID, Reason: retry.Reason, OccurredAt: retryAt})
	if err = store.RetryQueueItem(ctx, fleet.RetryMutation{Retry: retry, Transition: retried}, audit("queue.reclaimed", retry.RetryID)); err != nil {
		t.Fatal(err)
	}
	projection, err := store.GetQueueProjection(ctx, accepted.QueueItem.ItemID)
	if err != nil {
		t.Fatal(err)
	}
	return store, accepted, loopExecution, projection
}

func tamperQueueProjection(t *testing.T, store *Store, projection queue.Projection) {
	t.Helper()
	projection, err := queue.NewProjection(projection)
	if err != nil {
		t.Fatal(err)
	}
	wire, err := queue.MarshalProjection(projection)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.update(context.Background(), func(txn *badgerdb.Txn) error {
		return txn.Set(key(familyQueueProjection, projection.QueueItemID), wire)
	}); err != nil {
		t.Fatal(err)
	}
}

func nextClaimFixture(t *testing.T, accepted fleet.AcceptedSubmission, loopExecution execution.LoopExecution, claimedAt time.Time, attemptNumber uint32, suffix string) (queue.Claim, execution.Attempt, queue.QueueTransition) {
	t.Helper()
	claim := mustQueueClaim(t, queue.Claim{ClaimID: "claim-next-" + suffix, QueueItem: lifecycleDigestRef(accepted.QueueItem.ItemID, accepted.QueueItem.Digest), AttemptID: "attempt-next-" + suffix, WorkerID: "worker-2", Authority: accepted.QueueItem.Authority, ClaimedAt: claimedAt, ExpiresAt: claimedAt.Add(time.Minute)})
	attempt, err := execution.NewAttempt(execution.Attempt{AttemptID: claim.AttemptID, GraphRunID: accepted.GraphRun.GraphRunID, LoopExecutionID: loopExecution.LoopExecutionID, QueueItem: claim.QueueItem, ClaimID: claim.ClaimID, AttemptNumber: attemptNumber, CreatedAt: claimedAt})
	if err != nil {
		t.Fatal(err)
	}
	transition := mustQueueTransition(t, queue.QueueTransition{TransitionID: "transition-next-" + suffix, QueueItemID: accepted.QueueItem.ItemID, From: queue.StateQueued, To: queue.StateClaimed, ClaimID: claim.ClaimID, Reason: "worker lease acquired", OccurredAt: claimedAt})
	return claim, attempt, transition
}

func assertClaimDeniedAtomically(t *testing.T, ctx context.Context, store *Store, claim queue.Claim, attempt execution.Attempt, transition queue.QueueTransition) {
	t.Helper()
	eventsBefore, err := store.AuditEvents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	projectionBefore, err := store.GetQueueProjection(ctx, claim.QueueItem.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.ClaimQueueItem(ctx, claim, attempt, transition, audit("queue.claimed", claim.ClaimID)); !errors.Is(err, fleet.ErrConflict) {
		t.Fatalf("divergent projection admitted claim: %v", err)
	}
	if _, err = store.GetClaim(ctx, claim.ClaimID); !errors.Is(err, fleet.ErrNotFound) {
		t.Fatalf("denied claim persisted claim record: %v", err)
	}
	if _, err = store.GetAttempt(ctx, attempt.AttemptID); !errors.Is(err, fleet.ErrNotFound) {
		t.Fatalf("denied claim persisted attempt record: %v", err)
	}
	projectionAfter, err := store.GetQueueProjection(ctx, claim.QueueItem.ID)
	if err != nil || projectionAfter != projectionBefore {
		t.Fatalf("denied claim mutated projection: before=%+v after=%+v err=%v", projectionBefore, projectionAfter, err)
	}
	eventsAfter, err := store.AuditEvents(ctx)
	if err != nil || len(eventsAfter) != len(eventsBefore) {
		t.Fatalf("denied claim changed audit chain: before=%d after=%d err=%v", len(eventsBefore), len(eventsAfter), err)
	}
}

func completionFixture(t *testing.T, ctx context.Context, root string) (*Store, fleet.Completion) {
	t.Helper()
	store, accepted := lifecycleFixture(t, ctx, root, "submit-key-completion-boundary")
	if _, err := store.AcceptSubmission(ctx, accepted, audit("submission.accepted", accepted.Submission.SubmissionID)); err != nil {
		t.Fatal(err)
	}
	loopExecution, err := execution.NewLoopExecution(execution.LoopExecution{LoopExecutionID: "loop-execution-completion-boundary", GraphRunID: accepted.GraphRun.GraphRunID, GraphNodeID: "echo", Loop: accepted.Snapshot.Loops[0], Participant: accepted.Snapshot.Participants[0], CreatedAt: accepted.Submission.SubmittedAt})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.CreateLoopExecution(ctx, loopExecution, audit("loop-execution.created", loopExecution.LoopExecutionID)); err != nil {
		t.Fatal(err)
	}
	claim, attempt, claimed := claimFixture(t, accepted, loopExecution, "claim-completion-boundary", "attempt-completion-boundary")
	if err = store.ClaimQueueItem(ctx, claim, attempt, claimed, audit("queue.claimed", claim.ClaimID)); err != nil {
		t.Fatal(err)
	}
	artifactDigest := "sha256:" + strings.Repeat("a", 64)
	artifact := evidence.RuntimeArtifact{ID: "artifact-completion-boundary", OwnerID: "agent-1", ActionID: "work", RunID: loopExecution.LoopExecutionID, AuthorityContextID: claim.Authority.ID, AuthorityContextDigest: claim.Authority.Digest, Digest: artifactDigest, ContentRef: artifactDigest, MediaType: "application/json", CreatedAt: claim.ClaimedAt}
	receipt := evidence.VerificationReceipt{ID: "receipt-completion-boundary", ArtifactID: artifact.ID, ActionID: artifact.ActionID, RunID: artifact.RunID, OwnerID: artifact.OwnerID, AuthorityContextID: artifact.AuthorityContextID, AuthorityContextDigest: artifact.AuthorityContextDigest, VerifierID: evidence.ArtifactVerifierID, PolicyVersion: evidence.VerifierPolicyV1, Claim: "exact-output", ExpectedDigest: artifactDigest, ObservedDigest: artifactDigest, Outcome: evidence.Passed, EvidenceRef: "sha256:" + strings.Repeat("b", 64), ObservedAt: claim.ClaimedAt}
	dispositionRecord, err := disposition.New(disposition.Record{DispositionID: "disposition-completion-boundary", GraphRunID: attempt.GraphRunID, LoopExecutionID: attempt.LoopExecutionID, AttemptID: attempt.AttemptID, QueueItem: attempt.QueueItem, Authority: claim.Authority, State: execution.StateSucceeded, ReasonCode: "evidence_satisfied", ArtifactIDs: []string{artifact.ID}, ReceiptIDs: []string{receipt.ID}, OccurredAt: claim.ClaimedAt})
	if err != nil {
		t.Fatal(err)
	}
	terminal, err := queue.NewTransition(queue.QueueTransition{TransitionID: "transition-terminal-completion-boundary", QueueItemID: accepted.QueueItem.ItemID, From: queue.StateClaimed, To: queue.StateSucceeded, ClaimID: claim.ClaimID, Reason: "evidence_satisfied", OccurredAt: claim.ClaimedAt})
	if err != nil {
		t.Fatal(err)
	}
	return store, fleet.Completion{Claim: claim, Artifact: &artifact, Receipts: []evidence.VerificationReceipt{receipt}, Disposition: dispositionRecord, Transition: terminal}
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
	if _, err = store.PublishLoop(ctx, loop.PublishRequest{Revision: loopRevision, Validation: loopValidation, Provenance: loopPublicationProvenance(t, loopRevision, loopValidation), IdempotencyKey: "loop-lifecycle"}, audit("loop.published", loopRevision.LoopID)); err != nil {
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
	if got, err := store.GetQueueItem(ctx, accepted.QueueItem.ItemID); err != nil || !reflect.DeepEqual(got, accepted.QueueItem) {
		t.Fatalf("queue item readback: got=%+v err=%v", got, err)
	}
	if got, err := store.GetGraphRun(ctx, accepted.GraphRun.GraphRunID); err != nil || got != accepted.GraphRun {
		t.Fatalf("Graph run readback: got=%+v err=%v", got, err)
	}
	items, err := store.ListQueueItems(ctx)
	if err != nil || len(items) != 1 || !reflect.DeepEqual(items[0], accepted.QueueItem) {
		t.Fatalf("queue collection readback: got=%+v err=%v", items, err)
	}
	runs, err := store.ListGraphRuns(ctx)
	if err != nil || len(runs) != 1 || runs[0] != accepted.GraphRun {
		t.Fatalf("Graph run collection readback: got=%+v err=%v", runs, err)
	}
}
