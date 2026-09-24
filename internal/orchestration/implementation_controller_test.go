package orchestration

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/berryhill/aegis/internal/config"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/evidence"
	"github.com/berryhill/aegis/internal/execution"
	"github.com/berryhill/aegis/internal/graph"
	"github.com/berryhill/aegis/internal/implementation"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	fleetbadger "github.com/berryhill/aegis/internal/persistence/fleet/badger"
	"github.com/berryhill/aegis/internal/queue"
	"github.com/berryhill/aegis/internal/registry"
	"github.com/berryhill/aegis/internal/store"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type implementationTestRepository struct {
	fleet.Repository
	calls          int
	beforeWrite    func()
	beforeComplete func()
}

func (r *implementationTestRepository) GetQueueProjection(ctx context.Context, id string) (queue.Projection, error) {
	r.calls++
	if r.calls == 4 && r.beforeWrite != nil {
		r.beforeWrite()
	}
	return r.Repository.GetQueueProjection(ctx, id)
}
func (r *implementationTestRepository) CompleteQueueItem(ctx context.Context, c fleet.Completion, f fleet.AuditFact, e fleet.EvidenceReader) error {
	if r.beforeComplete != nil {
		r.beforeComplete()
	}
	return r.Repository.CompleteQueueItem(ctx, c, f, e)
}

// The runtime is a protocol fixture; native checks and fleet completion are real.
func TestImplementationQueueNativeCompletion(t *testing.T) {
	for _, mode := range []string{"first", "correction", "exhaustion", "unauthorized", "tamper", "cancelled", "revoked", "expired", "doer-needs-input", "doer-success", "doer-retry"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			root := t.TempDir()
			workspace := filepath.Join(root, "source")
			if err := os.Mkdir(workspace, 0700); err != nil {
				t.Fatal(err)
			}
			for name, content := range map[string]string{"go.mod": "module fixture\n\ngo 1.25\n", "value.go": "package fixture\nfunc Value() int {return 0}\n", "value_test.go": "package fixture\nimport \"testing\"\nfunc TestValue(t *testing.T){if Value()!=42{t.Fatal(Value())}}\n"} {
				if err := os.WriteFile(filepath.Join(workspace, name), []byte(content), 0600); err != nil {
					t.Fatal(err)
				}
			}
			contract := loop.ImplementationDraft("Return 42", "TestValue passes")
			contract.Workspace = workspace
			contract.WritableFiles = []string{"value.go"}
			contract.Policy.Packages = []string{"."}
			contract.Policy.RequiredTests = []loop.RequiredGoTest{{Package: "fixture", Name: "TestValue"}}
			contract.Policy.TimeoutSeconds = 120
			if mode == "doer-needs-input" || mode == "doer-success" || mode == "doer-retry" {
				contract.DecisionMode = "doer.v1"
				contract.MaxPasses = 3
			}
			service, fixture, _, subject, _, _ := fleetServiceFixture(t)
			now := time.Now().UTC()
			service.now = func() time.Time { return time.Now().UTC() }
			auth := service.authority.(fleetAuthorityRepository)
			auth.mandate.IssuedAt = now.Add(-time.Minute)
			auth.mandate.ExpiresAt = now.Add(time.Hour)
			subject.AuthenticatedAt = now.Add(-time.Minute)
			subject.ExpiresAt = now.Add(time.Hour)
			auth.mandate.Subject = subject
			auth.mandate.Hermes = core.HermesConfig{Toolsets: []string{"no_mcp"}, Model: "proof-no-key", Provider: "none"}
			auth.authority.Authority.Hermes = auth.mandate.Hermes
			auth.authority.IssuedAt = auth.mandate.IssuedAt
			auth.authority.ExpiresAt = auth.mandate.ExpiresAt
			auth.authority.Digest = core.AuthorityContextDigest(auth.authority)
			service.authority = auth
			service.authorityCommands = fleetAuthorityCommands{authority: auth.authority, admitted: true}
			authorityRef := digestRef(auth.authority.ID, auth.authority.Digest)
			repository, err := fleetbadger.Open(ctx, filepath.Join(root, "fleet-v1"))
			if err != nil {
				t.Fatal(err)
			}
			defer repository.Close()
			service.repository = repository
			agent := fixture.agent
			agent.SchemaVersion = registry.AgentRevisionSchemaVersion
			agent.Source = registry.FleetSource{FleetID: "fleet", Kind: "current-fleet", SourceID: "source"}
			agent.Digest = ""
			agent, err = registry.SealRevision(agent)
			if err != nil {
				t.Fatal(err)
			}
			agentRef := revisionRef(agent.AgentID, agent.Revision, agent.Digest)
			fact := fleet.AuditFact{Event: core.AuditEvent{Type: "fixture.created", SubjectID: subject.ID, PrincipalID: subject.PrincipalID, Outcome: "succeeded", Reason: "fixture"}}
			_, err = repository.RegisterAgent(ctx, registry.AgentRegistration{SchemaVersion: registry.AgentRegistrationSchemaVersion, AgentID: agent.AgentID, Source: agent.Source, InitialRevision: agentRef}, agent, fact)
			if err != nil {
				t.Fatal(err)
			}
			lr, lv, err := loop.NewImplementationRevision("implementation", 1, "", contract)
			if err != nil {
				t.Fatal(err)
			}
			_, err = service.PublishLoop(ctx, PublishLoopRequest{Subject: subject, Authority: authorityRef, Publisher: agentRef, Publication: loop.PublishRequest{Revision: lr, Validation: lv, IdempotencyKey: "publish-loop"}})
			if err != nil {
				t.Fatal(err)
			}
			loopRef := revisionRef(lr.LoopID, lr.Revision, lr.Digest)
			_, _, err = service.SetLoopLifecycle(ctx, SetLoopLifecycleRequest{Subject: subject, Authority: authorityRef, Publisher: agentRef, Loop: loopRef, State: loop.LifecycleActive, EventID: "activate"})
			if err != nil {
				t.Fatal(err)
			}
			gr, gv, err := graph.NewRevision(graph.GraphRevision{GraphID: "graph", Revision: 1, Nodes: []graph.Node{{ID: "node", Participant: agentRef, Loop: loopRef}}})
			if err != nil {
				t.Fatal(err)
			}
			_, err = service.PublishGraph(ctx, PublishGraphRequest{Subject: subject, Authority: authorityRef, Publication: graph.PublishRequest{Revision: gr, Validation: gv, IdempotencyKey: "publish-graph"}})
			if err != nil {
				t.Fatal(err)
			}
			decision, err := service.PrepareGraphRun(ctx, SubmitGraphRequest{Subject: subject, Authority: authorityRef, Graph: revisionRef(gr.GraphID, gr.Revision, gr.Digest), SubmissionID: "submission", IdempotencyKey: "submit", SnapshotID: "snapshot", QueueItemID: "queue", GraphRunID: "run", TransitionID: "queued", RejectionID: "rejected", MaxAttempts: 1})
			if err != nil || decision.Accepted == nil {
				t.Fatalf("submission: %+v %v", decision, err)
			}
			message := func(value string) string {
				proposal := map[string]any{"edits": []implementation.Edit{{Path: "value.go", Content: []byte("package fixture\nfunc Value() int {return " + value + "}\n")}}}
				if mode == "doer-success" || mode == "doer-retry" {
					proposal["report"] = "Implemented the requested value and ran relevant checks"
				}
				patch, _ := json.Marshal(proposal)
				event, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": "event", "params": map[string]any{"type": "message.complete", "session_id": "queue-runtime-session", "payload": map[string]string{"text": string(patch), "status": "complete"}}})
				return string(event)
			}
			first := message("42")
			if mode == "correction" || mode == "exhaustion" || mode == "doer-retry" {
				first = message("0")
			}
			second := message("42")
			if mode == "exhaustion" {
				second = message("0")
			}
			script := fmt.Sprintf(`#!/bin/sh
printf '%%s\n' '{"jsonrpc":"2.0","method":"event","params":{"type":"gateway.ready","payload":{}}}'
[ "$HERMES_TUI_TOOLSETS" = "context_engine" ] || exit 90
IFS= read -r tools || exit 1
case "$tools" in *'"method":"tools.show"'*) ;; *) exit 91;; esac
printf '%%s\n' '{"jsonrpc":"2.0","id":"aegis-tools","result":{"total":0,"sections":[]}}'
IFS= read -r create || exit 0
printf '%%s\n' '{"jsonrpc":"2.0","id":"create","result":{"session_id":"queue-runtime-session"}}'
read prompt
printf '%%s\n' '{"jsonrpc":"2.0","id":"prompt","result":{"accepted":true}}'
printf '%%s\n' '{"jsonrpc":"2.0","method":"event","params":{"type":"message.start","session_id":"queue-runtime-session","payload":{}}}'
completion=$(if [ -f '%s' ]; then printf '%%s\n' '%s'; else printf '%%s\n' '%s'; fi)
: > '%s'
printf '%%s\n' "$completion"
while read rest; do :; done
`, filepath.Join(root, "called"), second, first, filepath.Join(root, "called"))
			adapter := routedHermesTestAdapter(t, root, script)
			blobs, err := store.Open(filepath.Join(root, "blobs"))
			if err != nil {
				t.Fatal(err)
			}
			verifier, _ := evidence.NewBlobVerifier(blobs)
			wrapped := &implementationTestRepository{Repository: repository}
			worker, err := NewQueueWorker(wrapped, service, blobs, verifier, adapter, service.now)
			if err != nil {
				t.Fatal(err)
			}
			goBinary, err := exec.LookPath("go")
			if err != nil {
				t.Fatal(err)
			}
			goBinary, err = filepath.EvalSymlinks(goBinary)
			if err != nil {
				t.Fatal(err)
			}
			digest, _ := contract.Digest()
			if mode != "unauthorized" {
				if err = worker.ConfigureImplementation(config.Implementation{GoBinary: goBinary, AuthorizedContracts: []string{digest}}, filepath.Join(root, "state"), adapter.hermes); err != nil {
					t.Fatal(err)
				}
				if contract.DecisionMode == "doer.v1" {
					worker.implementation.decision = NewLayaDecisionAdapter(fakeLayaProcess(func(_ context.Context, input []byte) ([]byte, error) {
						if mode == "doer-needs-input" {
							return []byte(`{"version":1,"kind":"gate","answers":{"specified":{"choice":"no","answer_confidence":0.9},"result_defined":{"choice":"yes","answer_confidence":0.9}}}`), nil
						}
						if strings.Contains(string(input), `"kind":"verdict"`) {
							return []byte(`{"version":1,"kind":"verdict","answers":{"done":{"choice":"yes","answer_confidence":0.9},"stays_in_scope":{"choice":"yes","answer_confidence":0.9},"fulfills":{"choice":"yes","answer_confidence":0.9},"works":{"choice":"yes","answer_confidence":0.9},"practices":{"choice":"yes","answer_confidence":0.9}}}`), nil
						}
						return []byte(`{"version":1,"kind":"gate","answers":{"specified":{"choice":"yes","answer_confidence":0.9},"result_defined":{"choice":"yes","answer_confidence":0.9}}}`), nil
					}))
				}
			}
			if mode == "tamper" {
				wrapped.beforeComplete = func() {
					if err := os.WriteFile(filepath.Join(workspace, "value.go"), []byte("package fixture\nfunc Value() int {return 0}\n"), 0600); err != nil {
						t.Fatal(err)
					}
				}
			}
			if mode == "cancelled" || mode == "revoked" {
				wrapped.beforeWrite = func() {
					r := QueueTerminalRequest{Subject: subject, Authority: authorityRef, QueueItemID: "queue", CancellationID: "operator-stop", TransitionID: "operator-stopped"}
					var e error
					if mode == "cancelled" {
						r.ReasonCode = ReasonOperatorCancelled
						_, e = worker.Cancel(ctx, r)
					} else {
						r.ReasonCode = ReasonAuthorityRevoked
						_, e = worker.Revoke(ctx, r)
					}
					if e != nil {
						t.Fatal(e)
					}
				}
			}
			lease := 5 * time.Minute
			if mode == "expired" {
				lease = time.Second
				wrapped.beforeWrite = func() { time.Sleep(1100 * time.Millisecond) }
			}
			result, err := worker.Process(ctx, WorkRequest{Subject: subject, Authority: authorityRef, QueueItemID: "queue", WorkerID: "worker", LoopExecutionID: "loop-execution", ClaimID: "claim", AttemptID: "attempt", ClaimTransitionID: "claimed", TerminalTransitionID: "terminal", DispositionID: "disposition", ArtifactID: "artifact", LeaseDuration: lease})
			projection, readErr := repository.GetQueueProjection(ctx, "queue")
			if readErr != nil {
				t.Fatal(readErr)
			}
			if mode == "expired" {
				if err == nil || projection.State != queue.StateExpired || projection.ActiveClaimID != "" {
					t.Fatalf("expired lease stranded: %v %+v", err, projection)
				}
				d, e := repository.GetDispositionByGraphRun(ctx, "run")
				if e != nil || d.State != execution.StateExpired || d.ReasonCode != "implementation_expired" || d.OccurredAt.Before(result.Claim.ExpiresAt) {
					t.Fatalf("expired disposition: %+v %v", d, e)
				}
				kernel := &implementation.Executor{DB: repository.ImplementationStore()}
				record, e := kernel.Read("attempt")
				if e != nil || record.State != "expired" {
					t.Fatalf("kernel expiry: %+v %v", record, e)
				}
				return
			}
			if mode == "tamper" {
				if err == nil || projection.State == queue.StateSucceeded {
					t.Fatalf("tampered completion accepted: %v %+v", err, projection)
				}
				return
			}
			if mode == "cancelled" || mode == "revoked" {
				if err == nil || string(projection.State) != mode {
					t.Fatalf("stop lost: %v %+v", err, projection)
				}
				return
			}
			if mode == "unauthorized" {
				if err == nil || projection.State != queue.StateQueued {
					t.Fatalf("unauthorized: %v %+v", err, projection)
				}
				return
			}
			if mode == "exhaustion" {
				if err == nil || projection.State != queue.StateFailed {
					t.Fatalf("exhaustion: %v %+v", err, projection)
				}
				return
			}
			if mode == "doer-needs-input" {
				if err == nil || projection.State != queue.StateFailed || result.Disposition.ReasonCode != "implementation_needs_input" {
					t.Fatalf("needs-input disposition: %+v %+v %v", result, projection, err)
				}
				kernel := &implementation.Executor{DB: repository.ImplementationStore()}
				record, e := kernel.Read("attempt")
				if e != nil || record.State != "needs_input" || len(record.Passes) != 0 || len(record.Stages) != 1 {
					t.Fatalf("needs-input stage: %+v %v", record, e)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if result.Disposition.State != execution.StateSucceeded || projection.State != queue.StateSucceeded || result.Artifact == nil {
				t.Fatalf("completion: %+v %+v", result, projection)
			}
			if mode == "doer-success" || mode == "doer-retry" {
				kernel := &implementation.Executor{DB: repository.ImplementationStore()}
				record, readErr := kernel.Read("attempt")
				passes := 1
				if mode == "doer-retry" {
					passes = 2
				}
				if readErr != nil || record.State != "succeeded" || len(record.Passes) != passes || len(record.Stages) < 4 || record.Stages[0].Name != "gate" {
					t.Fatalf("doer stage readback: %+v %v", record, readErr)
				}
			}
			output, err := blobs.GetBlob(result.Artifact.ContentRef)
			if err != nil || len(output) == 0 {
				t.Fatalf("output: %v", err)
			}
			if !evidence.ValidateCompletionProvenance(evidence.CompletionProvenance{}, *result.Artifact, result.Receipts) {
			} else {
				t.Fatal("empty proof accepted")
			}
		})
	}
}

func (r *implementationTestRepository) ImplementationStore() implementation.Store {
	return r.Repository.(interface{ ImplementationStore() implementation.Store }).ImplementationStore()
}
