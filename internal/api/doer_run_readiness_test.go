package api

import (
	"context"
	"encoding/json"
	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/graph"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/reference"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestExactReusableReadinessObservesSameRunBinding(t *testing.T) {
	s, subject, old := candidateReadinessFixture(t, "none")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	revision, _, err := loop.NewDoerReusableRevision("exact-readiness", 1, "", loop.DoerReusableContract{MaxAttempts: 2})
	if err != nil {
		t.Fatal(err)
	}
	published, err := s.PublishLoopAs(ctx, subject, app.PublishLoopInput{AgentID: old.Agent.ID, Revision: revision, IdempotencyKey: "publish-readiness"})
	if err != nil {
		t.Fatal(err)
	}
	ref := reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: revision.LoopID, Revision: 1, Digest: published.Revision.Digest}
	workspace := t.TempDir()
	inputs := []graph.NormalizedInput{}
	for _, p := range []struct {
		id    string
		typ   graph.ValueType
		value any
	}{{"task", graph.TypeString, "Create result.txt"}, {"workspace", graph.TypeString, workspace}, {"writable_files", graph.TypeArray, []string{"result.txt"}}, {"verify_file", graph.TypeString, "result.txt"}, {"expected_text", graph.TypeString, "exact result\n"}} {
		wire, _ := json.Marshal(p.value)
		inputs = append(inputs, graph.NormalizedInput{PortID: p.id, Type: p.typ, Value: wire})
	}
	input := app.DoerReadinessInput{Agent: old.Agent, Loop: ref, Inputs: inputs, IdempotencyKey: "same-intent", Activate: true}
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, s) }()
	defer func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	}()
	waitFor(t, "unix", s.Config.API.UnixSocket)
	client := unixClient(s.Config.API.UnixSocket)
	var observed app.DoerCandidateReadiness
	apiRequest(t, client, http.MethodPost, "/v1/loops/doer/readiness", input, &observed, http.StatusOK)
	var reserved string
	if err := s.Store.Load("queue-loop-intent", observed.RequestID, &reserved); !os.IsNotExist(err) {
		t.Fatalf("readiness reserved intent %v", err)
	}
	if observed.Reason != "doer_model_required" || observed.ContractDigest == "" || observed.Candidate != ref || observed.Helper.State != "not_checked" {
		t.Fatalf("readiness %+v", observed)
	}
	// Readiness must not reserve or activate anything.
	queues, err := s.ListQueueAs(ctx, subject)
	if err != nil || len(queues) != 0 {
		t.Fatalf("queues %+v %v", queues, err)
	}
	graphs, err := s.ListGraphsAs(ctx, subject)
	if err != nil || len(graphs) != 0 {
		t.Fatalf("graphs %+v %v", graphs, err)
	}
	run := app.QueueLoopInput{Agent: old.Agent, Loop: ref, Inputs: inputs, IdempotencyKey: "same-intent", Activate: true}
	for i := 0; i < 2; i++ {
		var blocked app.QueueLoopResult
		apiRequest(t, client, http.MethodPost, "/v1/loops/queue", run, &blocked, http.StatusOK)
		if blocked.Readiness == nil || blocked.Readiness.ContractDigest != observed.ContractDigest || blocked.Reason != observed.Reason || blocked.QueueItemID != "" || blocked.RequestID != observed.RequestID {
			t.Fatalf("run %+v", blocked)
		}
	}
	view, err := s.GetLoopViewAs(ctx, subject, ref.ID, ref.Revision)
	if err != nil || view.Lifecycle.State == loop.LifecycleActive {
		t.Fatalf("activation %+v %v", view, err)
	}
	if _, err := os.Stat(filepath.Join(workspace, "result.txt")); !os.IsNotExist(err) {
		t.Fatalf("task effect %v", err)
	}
	bad := input
	bad.Activate = false
	if _, err := s.ReadDoerReadinessAs(ctx, subject, bad); err == nil {
		t.Fatal("readiness accepted changed retained intent")
	}
	bad = input
	bad.Inputs = inputs[:1]
	if _, err := s.ReadDoerReadinessAs(ctx, subject, bad); err == nil {
		t.Fatal("missing required inputs accepted")
	}
}
