package api

import (
	"context"
	"encoding/json"
	"github.com/berryhill/aegis/internal/evidence"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/disposition"
	"github.com/berryhill/aegis/internal/execution"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	"github.com/berryhill/aegis/internal/queue"
)

// Synthetic no-provider history, but real atomic canonical completion. This
// must remain separate from the interpreter-only claimed-state fixture.
func recordDebuggerFailure(t *testing.T, svc *app.Service, subject core.Subject) {
	recordDebuggerDisposition(t, svc, subject, false)
}

func recordDebuggerDisposition(t *testing.T, svc *app.Service, subject core.Subject, success bool) {
	t.Helper()
	ctx := context.Background()
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	claim, err := svc.FleetRepository.GetClaim(ctx, "debug-claim")
	check(err)
	sub, err := svc.FleetRepository.GetSubmission(ctx, "debug-submission")
	check(err)
	child, err := svc.FleetRepository.GetLoopExecution(ctx, "debug-execution")
	check(err)
	now := svc.Now()
	state, reason := execution.StateFailed, "implementation_failed"
	completion := fleet.Completion{Claim: claim}
	if success {
		state, reason = execution.StateSucceeded, "evidence_satisfied"
		revision, err := svc.FleetRepository.GetLoopRevision(ctx, child.Loop.ID, child.Loop.Revision)
		check(err)
		contract := revision.Doer
		check(os.WriteFile(filepath.Join(contract.Workspace, contract.VerifyFile), []byte("hello"), 0600))
		ref, err := svc.Store.PutBlob([]byte("hello"))
		check(err)
		artifact := evidence.RuntimeArtifact{ID: "debug-artifact", AttemptID: claim.AttemptID, OwnerID: child.Participant.ID, ActionID: "verify", RunID: child.LoopExecutionID, AuthorityContextID: claim.Authority.ID, AuthorityContextDigest: claim.Authority.Digest, Digest: ref, ContentRef: ref, MediaType: "application/octet-stream", CreatedAt: now}
		policy := evidence.SelectedFilePolicy{Version: evidence.SelectedFilePolicyV1, RelativePath: contract.VerifyFile, Mode: evidence.SelectedFileText, Text: "hello"}
		pin, err := policy.Digest()
		check(err)
		digest, err := contract.Digest()
		check(err)
		verifier, err := evidence.NewBlobVerifier(svc.Store)
		check(err)
		binding := evidence.SelectedFileBinding{AttemptID: claim.AttemptID, ActionID: artifact.ActionID, RunID: child.LoopExecutionID, OwnerID: child.Participant.ID, AuthorityContextID: claim.Authority.ID, AuthorityContextDigest: claim.Authority.Digest}
		receipt, proof, err := verifier.VerifySelectedFileArtifact(ctx, artifact, contract.Workspace, policy, pin, digest, ref, binding)
		check(err)
		completion.Artifact, completion.Receipts, completion.Provenance = &artifact, []evidence.VerificationReceipt{receipt}, proof
	}
	tr, err := queue.NewTransition(queue.QueueTransition{TransitionID: "debug-failed", QueueItemID: "debug-queue", From: queue.StateClaimed, To: queue.State(state), ClaimID: claim.ClaimID, Reason: reason, OccurredAt: now})
	check(err)
	d, err := disposition.New(disposition.Record{DispositionID: "debug-disposition", GraphRunID: "debug-run", LoopExecutionID: child.LoopExecutionID, AttemptID: claim.AttemptID, QueueItem: claim.QueueItem, Authority: claim.Authority, State: state, ReasonCode: tr.Reason, ArtifactIDs: debuggerArtifactIDs(completion), ReceiptIDs: debuggerReceiptIDs(completion), OccurredAt: now})
	check(err)
	fact := fleet.AuditFact{Event: core.AuditEvent{Type: "fleet.disposition.recorded", SubjectID: subject.ID, PrincipalID: subject.PrincipalID, AgentID: child.Participant.ID, StanzaID: "principal", MandateID: sub.MandateID, Outcome: string(d.State), Reason: d.ReasonCode, Metadata: map[string]string{"disposition_id": d.DispositionID, "transition_id": tr.TransitionID, "terminal_at": now.UTC().Format("2006-01-02T15:04:05.999999999Z07:00")}}}
	completion.Disposition, completion.Transition = d, tr
	check(svc.FleetRepository.CompleteQueueItem(ctx, completion, fact, svc.Store))
}

func debuggerArtifactIDs(c fleet.Completion) []string {
	if c.Artifact != nil {
		return []string{c.Artifact.ID}
	}
	return nil
}
func debuggerReceiptIDs(c fleet.Completion) []string {
	if len(c.Receipts) > 0 {
		return []string{c.Receipts[0].ID}
	}
	return nil
}

func TestExecutionDebuggerSuccessOutcomeConsoleBrowser(t *testing.T) {
	executionDebuggerConsoleBrowser(t, false, true, true)
}

func TestExecutionDebuggerTerminalOutcomeRead(t *testing.T) {
	svc, subject, _, executor, done := debuggerFixture(t)
	ctx := context.Background()
	before, err := svc.ExecutionDebuggerAs(ctx, subject, "debug-queue", "")
	if err != nil {
		t.Fatal(err)
	}
	close(executor.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	recordDebuggerFailure(t, svc, subject)
	after, err := svc.ExecutionDebuggerAs(ctx, subject, "debug-queue", before.Watermark)
	if err != nil {
		t.Fatal(err)
	}
	if !after.Terminal || after.State != "failed" || after.DispositionReason != "implementation_failed" || after.GraphRunID != before.GraphRunID {
		t.Fatalf("wrong terminal outcome: %+v", after)
	}
	html, err := consoleQueueCurrentOutcome(ctx, svc, subject, after)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Current execution outcome", "implementation_failed", "A more detailed cause was not recorded", "unavailable"} {
		if !strings.Contains(html, want) {
			t.Fatalf("missing %q: %s", want, html)
		}
	}
	if strings.Contains(html, "secret-canary") {
		t.Fatal("private task exposed")
	}
	original := svc.FleetRepository
	for _, mutate := range []func(*disposition.Record){
		func(d *disposition.Record) { d.GraphRunID = "foreign-run" },
		func(d *disposition.Record) { d.QueueItem.ID = "foreign-queue" },
		func(d *disposition.Record) { d.QueueItem.Digest = "sha256:" + strings.Repeat("0", 64) },
	} {
		svc.FleetRepository = resultReadRepository{Repository: original, disposition: mutate}
		if _, err := svc.ExecutionDebuggerAs(ctx, subject, "debug-queue", ""); err == nil {
			t.Fatal("substituted disposition accepted")
		}
	}
	svc.FleetRepository = original
	after.DispositionReason = "secret-stderr-canary"
	wire, _ := json.Marshal(debuggerPresentation(after))
	if strings.Contains(string(wire), "secret-stderr-canary") {
		t.Fatal("unmapped diagnostic disclosed")
	}
}

func TestExecutionDebuggerTerminalOutcomeConsoleBrowser(t *testing.T) {
	executionDebuggerConsoleBrowser(t, false, true)
}
