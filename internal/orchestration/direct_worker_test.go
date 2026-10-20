package orchestration

import (
	"context"
	"github.com/berryhill/aegis/internal/evidence"
	"github.com/berryhill/aegis/internal/execution"
	"github.com/berryhill/aegis/internal/graph"
	"github.com/berryhill/aegis/internal/store"
	"testing"
	"time"
)

func TestDirectLoopWorkerRunsToDurableEvidenceDisposition(t *testing.T) {
	service, repository, _, subject, authorityRef, graphRef := fleetServiceFixture(t)
	decision, err := service.PrepareGraphRun(context.Background(), SubmitGraphRequest{Subject: subject, Authority: authorityRef, Graph: graphRef, SubmissionID: "submission-worker", IdempotencyKey: "submit-worker", SnapshotID: "snapshot-worker", QueueItemID: "queue-worker", GraphRunID: "run-worker", TransitionID: "queued-worker", RejectionID: "rejected-worker", MaxAttempts: 1})
	if err != nil || decision.Accepted == nil {
		t.Fatalf("prepare run: decision=%+v err=%v", decision, err)
	}
	direct, err := graph.NewDirectLoopSnapshot("direct-worker-snapshot", repository.graph.Nodes[0], nil)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := graph.NewDirectRunSnapshot(direct)
	if err != nil {
		t.Fatal(err)
	}
	repository.accepted.Snapshot = snapshot
	repository.accepted.QueueItem.Snapshot = digestRef(snapshot.SnapshotID, snapshot.Digest)

	blobs, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := evidence.NewBlobVerifier(blobs)
	if err != nil {
		t.Fatal(err)
	}
	worker, err := NewQueueWorker(repository, service, blobs, verifier, fixedRuntimeAdapter{}, service.now)
	if err != nil {
		t.Fatal(err)
	}
	result, err := worker.Process(context.Background(), WorkRequest{Subject: subject, Authority: authorityRef, QueueItemID: "queue-worker", WorkerID: "worker-1", LoopExecutionID: "loop-execution-worker", ClaimID: "claim-worker", AttemptID: "attempt-worker", ClaimTransitionID: "claimed-worker", TerminalTransitionID: "terminal-worker", DispositionID: "disposition-worker", ArtifactID: "artifact-worker", LeaseDuration: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	if result.Disposition.State != execution.StateSucceeded || result.Artifact == nil || result.Artifact.ContentRef == "" || len(result.Receipts) != 1 || result.Receipts[0].Outcome != evidence.Passed || repository.completion.Disposition.Digest != result.Disposition.Digest {
		t.Fatalf("injected runtime execution did not reach evidence disposition: %+v completion=%+v", result, repository.completion)
	}
	if repository.claim.Authority != authorityRef || repository.attempt.LoopExecutionID != repository.loopExecution.LoopExecutionID {
		t.Fatalf("claim causality or authority was lost: claim=%+v attempt=%+v loop=%+v", repository.claim, repository.attempt, repository.loopExecution)
	}
}
