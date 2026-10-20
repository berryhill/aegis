package api

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/queue"
	"github.com/berryhill/aegis/internal/reference"
)

func TestInstalledOnlineDoerLinkedReexecutionSuccess(t *testing.T) {
	installedOnlineDoerExactBytesSuccess(t, true, "linked-success")
}
func TestInstalledOnlineDoerLinkedReexecutionExhaustion(t *testing.T) {
	installedOnlineDoerExactBytesSuccess(t, true, "linked-failure")
}

// Real public CLI/owner transport/worker/verifier; synthetic tool-free Hermes
// protocol and Laya only. No live model, credential or expected-output writes.
func installedLinkedReexecution(t *testing.T, ctx context.Context, s *app.Service, cli *installedLoopQueueClient, input app.QueueLoopInput, workspace, install, successScript string, secondFailure bool) {
	t.Helper()
	python := filepath.Join(install, "venv", "bin", "python")
	failureScript := strings.ReplaceAll(successScript, `\"edits\"`, `\"unknown_edits\"`)
	if failureScript == successScript {
		t.Fatal("fixture malformed proposal substitution missing")
	}
	if err := os.WriteFile(python, []byte(failureScript), 0700); err != nil {
		t.Fatal(err)
	}
	first, err := cli.queue(input)
	if err != nil {
		// Failed execution may return a denial status after durable terminal
		// commit. Reconcile this exact key, never mint a new unknown Run.
		sub, e := s.AuthenticateUnixPeer(ctx, uint32(os.Getuid()))
		if e != nil {
			t.Fatal(e)
		}
		items, e := s.ListQueueAs(ctx, sub)
		if e != nil || len(items) != 1 {
			t.Fatal("missing failure readback", err, e)
		}
		var v app.QueueExecutionView
		if e = cli.run(&v, "queue", "show", items[0].Item.ItemID); e != nil {
			t.Fatal(e)
		}
		first.Execution, first.QueueItemID, first.RequestID, err = &v, v.Item.ItemID, v.Submission.IdempotencyKey, nil
	}
	if err != nil || first.Execution == nil || first.Execution.Projection.State != queue.StateFailed || first.Execution.Disposition == nil {
		t.Fatalf("first failure: %+v %v", first, err)
	}
	old := *first.Execution
	if old.Artifact != nil || len(old.Receipts) != 0 || old.Disposition.ReasonCode != "doer_loop_failed" {
		t.Fatal("failure acquired evidence")
	}
	steps := old.DoerSteps[old.Attempts[0].AttemptID]
	if len(steps) == 0 || steps[len(steps)-1].FailureCode != "doer_proposal_decode_failed" {
		t.Fatalf("closed diagnostic missing: %+v", steps)
	}
	if _, err := os.Stat(filepath.Join(workspace, "result.txt")); !os.IsNotExist(err) {
		t.Fatal("failed proposal wrote output")
	}
	linked := input
	linked.IdempotencyKey = "journey-linked-run"
	linked.Reexecution = &app.LoopReexecution{PreviousQueue: reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: old.Item.ItemID, Digest: old.Item.Digest}, PreviousDisposition: reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: old.Disposition.DispositionID, Digest: old.Disposition.Digest}}
	// Bad references and substituted revision are denied, without a successor.
	bad := linked
	refs := *linked.Reexecution
	refs.PreviousDisposition.Digest = "sha256:" + strings.Repeat("b", 64)
	bad.Reexecution = &refs
	if _, err := cli.queue(bad); err == nil {
		t.Fatal("tampered disposition admitted")
	}
	bad = linked
	bad.Loop.Digest = "sha256:" + strings.Repeat("b", 64)
	if _, err := cli.queue(bad); err == nil {
		t.Fatal("changed Loop admitted")
	}
	// While scope/authority are live the exact new intent can be retained, but
	// cannot issue fresh authority or execute. This is not unknown-outcome retry.
	blocked, err := cli.queue(linked)
	if err != nil || blocked.Reexecution == nil || blocked.Reexecution.Remaining != 1 || blocked.Execution != nil && len(blocked.Execution.Attempts) != 0 {
		t.Fatalf("live predecessor advanced: %+v %v", blocked, err)
	}
	if blocked.Reason == "processed" {
		t.Fatal("live old authority reused")
	}
	subject, err := s.AuthenticateUnixPeer(ctx, uint32(os.Getuid()))
	if err != nil {
		t.Fatal(err)
	}
	authority, err := s.Authority.GetAuthorityContext(ctx, old.RuntimeAuthorityBinding.Authority.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.RevokeSessionAs(ctx, subject, authority.SessionID, "isolated_linked_run_fixture"); err != nil {
		t.Fatal(err)
	}
	// Natural expiry of the old signed host scope, not renewal/rewriting consent.
	time.Sleep(10100 * time.Millisecond)
	if !secondFailure {
		if err = os.WriteFile(python, []byte(successScript), 0700); err != nil {
			t.Fatal(err)
		}
	}
	result, err := cli.queue(linked)
	if err != nil && secondFailure {
		result, err = cli.queue(linked) // exact stable-key authoritative replay
	}
	if err != nil || result.Execution == nil || result.Reexecution == nil {
		t.Fatalf("linked run: %+v %v", result, err)
	}
	expected := queue.StateSucceeded
	if secondFailure {
		expected = queue.StateFailed
	}
	if result.Execution.Projection.State != expected || result.Reexecution.Remaining != 1 || result.Reexecution.Consumed != 1 || result.Execution.Item.ItemID == old.Item.ItemID || result.Execution.Item.GraphRunID == old.Item.GraphRunID || result.Execution.RuntimeAuthorityBinding.Authority == old.RuntimeAuthorityBinding.Authority {
		t.Fatalf("linked result mismatch: %+v", result)
	}
	var oldRead app.QueueExecutionView
	if err = cli.run(&oldRead, "queue", "show", old.Item.ItemID); err != nil || !reflect.DeepEqual(old, oldRead) {
		t.Fatal("first failed history changed", err)
	}
	replay, err := cli.queue(linked)
	if err != nil || replay.Reason != "existing_execution" || !reflect.DeepEqual(replay.Execution, result.Execution) {
		t.Fatal("replay caused effect", err)
	}
	fork := linked
	fork.IdempotencyKey = "journey-fork"
	if _, err = cli.queue(fork); err == nil {
		t.Fatal("second successor admitted")
	}
	changed := linked
	changed.Activate = false
	if _, err = cli.queue(changed); err == nil {
		t.Fatal("same-key changed payload admitted")
	}
	if secondFailure {
		third := input
		third.IdempotencyKey = "journey-third"
		third.Reexecution = &app.LoopReexecution{PreviousQueue: reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: result.Execution.Item.ItemID, Digest: result.Execution.Item.Digest}, PreviousDisposition: reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: result.Execution.Disposition.DispositionID, Digest: result.Execution.Disposition.Digest}}
		if _, err = cli.queue(third); err == nil || !strings.Contains(err.Error(), "doer_reexecution_budget_exhausted") {
			t.Fatalf("third pass not exhausted: %v", err)
		}
		if _, err = os.Stat(filepath.Join(workspace, "result.txt")); !os.IsNotExist(err) {
			t.Fatal("second failed proposal wrote output")
		}
	} else {
		if result.Execution.Artifact == nil || len(result.Execution.Receipts) != 1 || result.Execution.Disposition == nil {
			t.Fatal("success missing verified evidence")
		}
	}
	subject, err = s.AuthenticateUnixPeer(ctx, uint32(os.Getuid()))
	if err != nil {
		t.Fatal(err)
	}
	loops, err := s.ListLoopsAs(ctx, subject)
	if err != nil || len(loops) != 1 || loops[0].Revision.Digest != input.Loop.Digest {
		t.Fatal("Loop republished", err)
	}
}
