package api

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/queue"
	"github.com/berryhill/aegis/internal/reference"
)

// Public CLI -> owning service -> runtime-proposed edits -> real verifier.
// Only the runtime protocol is synthetic. No preauthorized task digests or
// manual selected-file writes, session retirement, or authority-store edits.
func installedReusableFailureContinuity(t *testing.T, ctx context.Context, s *app.Service, cli *installedLoopQueueClient, input app.QueueLoopInput, workspace, install, successScript string) {
	t.Helper()
	python := filepath.Join(install, "venv", "bin", "python")
	failure := strings.ReplaceAll(successScript, `\"edits\"`, `\"unknown_edits\"`)
	if failure == successScript {
		t.Fatal("missing malformed proposal fixture")
	}
	if err := os.WriteFile(python, []byte(failure), 0700); err != nil {
		t.Fatal(err)
	}
	first, runErr := cli.queue(input)
	if runErr != nil || first.Execution == nil || first.Execution.Projection.State != queue.StateFailed {
		t.Fatalf("known failed execution hidden behind admission error: %s %v", first.Reason, runErr)
	}
	sub, err := s.AuthenticateUnixPeer(ctx, uint32(os.Getuid()))
	if err != nil {
		t.Fatal(err)
	}
	items, err := s.ListQueueAs(ctx, sub)
	if err != nil || len(items) != 1 {
		t.Fatal("missing exact failure", err)
	}
	var old app.QueueExecutionView
	if err = cli.run(&old, "queue", "show", items[0].Item.ItemID); err != nil {
		t.Fatal(err)
	}
	if old.Projection.State != queue.StateFailed || old.Disposition == nil || len(old.Attempts) != 1 || old.Artifact != nil || len(old.Receipts) != 0 {
		t.Fatal("failure history invalid")
	}
	first.QueueItemID = old.Item.ItemID
	if _, err = os.Stat(filepath.Join(workspace, "result.txt")); !os.IsNotExist(err) {
		t.Fatal("failed proposal produced output")
	}
	// Same task with a new key is not a fresh implementation budget.
	unlinked := input
	unlinked.IdempotencyKey = "unlinked-failed-task"
	denied, err := cli.queue(unlinked)
	if err != nil || denied.Reason != "same_task_budget_link_required" || denied.Execution != nil {
		t.Fatalf("unlinked task reset: %s %v", denied.Reason, err)
	}
	// A genuinely different owner-requested task, even a different reusable
	// Loop, must not be poisoned by the Agent-scoped predecessor reservation.
	source, _, err := loop.NewDoerReusableRevision("different-reusable-loop", 1, "", loop.DoerReusableContract{MaxAttempts: 2})
	if err != nil {
		t.Fatal(err)
	}
	pub := app.PublishLoopInput{AgentID: input.Agent.ID, Revision: source, IdempotencyKey: "different-loop-publication"}
	raw, _ := json.Marshal(pub)
	file := filepath.Join(t.TempDir(), "publish.json")
	if err = os.WriteFile(file, raw, 0600); err != nil {
		t.Fatal(err)
	}
	var published struct {
		Revision loop.LoopRevision `json:"revision"`
	}
	if err = cli.run(&published, "loops", "publish", file); err != nil {
		t.Fatal(err)
	}
	next := input
	next.IdempotencyKey = "new-task-after-failure"
	next.Loop = reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: source.LoopID, Revision: 1, Digest: source.Digest}
	next.Activate = true
	next.Inputs = append(next.Inputs[:0:0], input.Inputs...)
	otherWorkspace := t.TempDir()
	for i := range next.Inputs {
		if next.Inputs[i].PortID == "workspace" {
			next.Inputs[i].Value, _ = json.Marshal(otherWorkspace)
		}
	}
	if err = os.WriteFile(python, []byte(successScript), 0700); err != nil {
		t.Fatal(err)
	}
	fresh, err := cli.queue(next)
	if err != nil || fresh.Execution == nil || fresh.Execution.Projection.State != queue.StateSucceeded || fresh.Execution.RuntimeAuthorityBinding == nil || fresh.Execution.RuntimeAuthorityBinding.Authority == old.RuntimeAuthorityBinding.Authority {
		t.Fatalf("new task after failure: %s %v", fresh.Reason, err)
	}
	b, err := os.ReadFile(filepath.Join(otherWorkspace, "result.txt"))
	if err != nil || string(b) != "hello" {
		t.Fatal("new task runtime output", err)
	}
	// Interleaving another successful task cannot erase the original budget.
	denied, err = cli.queue(unlinked)
	if err != nil || denied.Reason != "same_task_budget_link_required" {
		t.Fatal("interleaved task hid failure budget", err)
	}
	// Supported exact continuation binds the old failed Queue/disposition and
	// consumes only the remaining pass. All internal lifecycle cleanup is owned
	// by the same single authenticated Run, not a separate operator command.
	linked := input
	linked.IdempotencyKey = "linked-reusable-task"
	linked.Reexecution = &app.LoopReexecution{PreviousQueue: reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: old.Item.ItemID, Digest: old.Item.Digest}, PreviousDisposition: reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: old.Disposition.DispositionID, Digest: old.Disposition.Digest}}
	continued, err := cli.queue(linked)
	if err != nil || continued.Execution == nil || continued.Execution.Projection.State != queue.StateSucceeded || continued.Reexecution == nil || continued.Reexecution.Consumed != 1 || continued.Reexecution.Remaining != 1 || len(continued.Execution.Receipts) != 1 {
		t.Fatalf("bounded continuation: %s %v", continued.Reason, err)
	}
	b, err = os.ReadFile(filepath.Join(workspace, "result.txt"))
	if err != nil || string(b) != "hello" {
		t.Fatal("continued runtime output", err)
	}
	var retained app.QueueExecutionView
	if err = cli.run(&retained, "queue", "show", old.Item.ItemID); err != nil || !reflect.DeepEqual(old, retained) {
		t.Fatal("failed history changed", err)
	}
	replay, err := cli.queue(linked)
	if err != nil || replay.Reason != "existing_execution" || replay.Execution == nil || len(replay.Execution.Attempts) != 1 {
		t.Fatal("same intent duplicated runtime effect", err)
	}
	fork := linked
	fork.IdempotencyKey = "second-successor"
	if _, err = cli.queue(fork); err == nil {
		t.Fatal("second successor reset budget")
	}
}
