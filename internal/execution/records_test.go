package execution

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/reference"
)

func TestExecutionRecordsRoundTripAndBindCausalIdentity(t *testing.T) {
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	run, err := NewGraphRun(GraphRun{
		GraphRunID: "run-1",
		QueueItem:  executionDigestRef("item-1", 'a'),
		Snapshot:   executionDigestRef("snapshot-1", 'b'),
		Authority:  executionDigestRef("authority-1", 'c'),
		State:      StateSucceeded,
		CreatedAt:  now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if run.State != StateRequested {
		t.Fatalf("Graph run constructor did not force requested state: %q", run.State)
	}
	wire, err := MarshalGraphRun(run)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := UnmarshalGraphRun(wire)
	if err != nil || decoded != run {
		t.Fatalf("Graph run round trip: got=%+v err=%v", decoded, err)
	}

	loopExecution, err := NewLoopExecution(LoopExecution{
		LoopExecutionID: "loop-execution-1",
		GraphRunID:      run.GraphRunID,
		GraphNodeID:     "node-1",
		Loop:            executionRevisionRef("loop-1", 'd'),
		Participant:     executionRevisionRef("agent-1", 'e'),
		CreatedAt:       now,
	})
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := NewAttempt(Attempt{
		AttemptID:       "attempt-1",
		GraphRunID:      run.GraphRunID,
		LoopExecutionID: loopExecution.LoopExecutionID,
		QueueItem:       run.QueueItem,
		ClaimID:         "claim-1",
		AttemptNumber:   1,
		CreatedAt:       now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if attempt.State != StateRequested || attempt.GraphRunID != loopExecution.GraphRunID {
		t.Fatalf("attempt did not preserve requested causal identity: %+v", attempt)
	}
}

func TestExecutionCodecRejectsMutationUnknownsAndUnboundedAttempts(t *testing.T) {
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	attempt, err := NewAttempt(Attempt{AttemptID: "attempt-1", GraphRunID: "run-1", LoopExecutionID: "loop-execution-1", QueueItem: executionDigestRef("item-1", 'a'), ClaimID: "claim-1", AttemptNumber: 1, CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	wire, err := MarshalAttempt(attempt)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = UnmarshalAttempt(bytes.Replace(wire, []byte(`"claim_id":"claim-1"`), []byte(`"claim_id":"claim-2"`), 1)); err == nil {
		t.Fatal("execution decoder accepted digest-changing mutation")
	}
	if _, err = UnmarshalAttempt(bytes.Replace(wire, []byte(`"attempt_id":"attempt-1"`), []byte(`"attempt_id":"attempt-1","attempt_id":"attempt-2"`), 1)); err == nil {
		t.Fatal("execution decoder accepted duplicate identity")
	}
	if _, err = UnmarshalAttempt(bytes.Replace(wire, []byte(`"digest":`), []byte(`"unknown":true,"digest":`), 1)); err == nil {
		t.Fatal("execution decoder accepted unknown field")
	}
	if _, err = NewAttempt(Attempt{AttemptID: "attempt-2", GraphRunID: "run-1", LoopExecutionID: "loop-execution-1", QueueItem: executionDigestRef("item-1", 'a'), ClaimID: "claim-2", AttemptNumber: 101, CreatedAt: now}); err == nil {
		t.Fatal("execution attempt accepted an unbounded attempt number")
	}
}

func executionDigestRef(id string, fill byte) reference.DigestRef {
	return reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: id, Digest: "sha256:" + strings.Repeat(string(fill), 64)}
}

func executionRevisionRef(id string, fill byte) reference.RevisionRef {
	return reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: id, Revision: 1, Digest: "sha256:" + strings.Repeat(string(fill), 64)}
}
