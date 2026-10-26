package orchestration

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
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
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// Controlled RAW adapter; outputs derive from admitted inputs, never a fixed hello.
type rawChainAdapter struct {
	calls []string
	bad   bool
}

func (a *rawChainAdapter) Execute(ctx context.Context, r RuntimeRequest) (RuntimeResult, error) {
	a.calls = append(a.calls, r.GraphNodeID)
	var out []byte
	if r.GraphNodeID == "producer" {
		var s string
		if len(r.Inputs) != 1 {
			return RuntimeResult{}, fmt.Errorf("missing input")
		}
		if e := json.Unmarshal(r.Inputs[0].Value, &s); e != nil {
			return RuntimeResult{}, e
		}
		out = []byte(s)
		if a.bad {
			out = append(out, '\n')
		}
	} else {
		if len(r.ArtifactInputs) != 1 {
			return RuntimeResult{}, fmt.Errorf("missing verified input")
		}
		p := r.ArtifactInputs[0].Provenance()
		if p.ProducerNodeID != "producer" || p.ConsumerNodeID != "consumer" || p.GraphRunID != r.GraphRunID {
			return RuntimeResult{}, fmt.Errorf("lineage")
		}
		out = append([]byte("confirmed:"), r.ArtifactInputs[0].Content()...)
	}
	return RuntimeResult{Output: out, MediaType: "text/plain"}, nil
}

// Memory service fixture tests real Process routing; durable protocol separately
// exercises the real Badger implementation in TestGraphNodeProtocolDurableChain.
type rawChainRepository struct {
	*fleetServiceRepository
	loops       map[string]loop.LoopRevision
	admissions  map[string]fleet.GraphNodeAdmission
	checkpoints map[string]fleet.GraphNodeCheckpoint
}

func (r *rawChainRepository) GetLoopRevision(_ context.Context, id string, _ uint64) (loop.LoopRevision, error) {
	return r.loops[id], nil
}
func (r *rawChainRepository) ListLoopLifecycleEvents(context.Context) ([]loop.LifecycleEvent, error) {
	var out []loop.LifecycleEvent
	for _, l := range r.loops {
		out = append(out, loop.LifecycleEvent{LoopID: l.LoopID, State: loop.LifecycleActive, Revision: loop.NewProvenanceRevision(l.LoopID, l.Revision, l.Digest), Digest: "current"})
	}
	return out, nil
}
func (r *rawChainRepository) GetGraphRun(context.Context, string) (execution.GraphRun, error) {
	return r.accepted.GraphRun, nil
}
func (r *rawChainRepository) GetLoopExecution(_ context.Context, id string) (execution.LoopExecution, error) {
	for _, a := range r.admissions {
		if a.Execution.LoopExecutionID == id {
			return a.Execution, nil
		}
	}
	return execution.LoopExecution{}, fleet.ErrNotFound
}
func (r *rawChainRepository) GetRuntimeArtifact(_ context.Context, id string) (evidence.RuntimeArtifact, error) {
	for _, c := range r.checkpoints {
		if c.Completion.Artifact != nil && c.Completion.Artifact.ID == id {
			return *c.Completion.Artifact, nil
		}
	}
	return evidence.RuntimeArtifact{}, fleet.ErrNotFound
}
func (r *rawChainRepository) GetVerificationReceipt(_ context.Context, id string) (evidence.VerificationReceipt, error) {
	for _, c := range r.checkpoints {
		for _, v := range c.Completion.Receipts {
			if v.ID == id {
				return v, nil
			}
		}
	}
	return evidence.VerificationReceipt{}, fleet.ErrNotFound
}
func (r *rawChainRepository) AdmitGraphNode(_ context.Context, a fleet.GraphNodeAdmission, _ fleet.AuditFact) error {
	if _, ok := r.admissions[a.Execution.GraphNodeID]; ok {
		return fleet.ErrConflict
	}
	r.admissions[a.Execution.GraphNodeID] = a
	return nil
}
func (r *rawChainRepository) GetGraphNode(_ context.Context, _ string, node string) (fleet.GraphNodeAdmission, error) {
	a, ok := r.admissions[node]
	if !ok {
		return a, fleet.ErrNotFound
	}
	return a, nil
}
func (r *rawChainRepository) GetGraphNodeCheckpoint(_ context.Context, _ string, node string) (fleet.GraphNodeCheckpoint, error) {
	c, ok := r.checkpoints[node]
	if !ok {
		return c, fleet.ErrNotFound
	}
	return c, nil
}
func (r *rawChainRepository) CheckpointGraphNode(_ context.Context, c fleet.GraphNodeCheckpoint, _ fleet.AuditFact, _ fleet.EvidenceReader) error {
	r.checkpoints[c.Admission.Execution.GraphNodeID] = c
	return nil
}
func (r *rawChainRepository) CompleteGraph(ctx context.Context, c fleet.Completion, f fleet.AuditFact, b fleet.EvidenceReader) error {
	if len(r.checkpoints) != 2 {
		return fleet.ErrConflict
	}
	r.projection.State = queue.StateSucceeded
	return r.CompleteQueueItem(ctx, c, f, b)
}

func TestQueueWorkerProcessRawProducerConsumer(t *testing.T)       { testQueueWorkerRawChain(t, false) }
func TestQueueWorkerProcessRawProducerConsumerBadger(t *testing.T) { testQueueWorkerRawChain(t, true) }
func TestQueueWorkerManagedProducerConsumerSynthetic(t *testing.T) {
	testQueueWorkerRawChain(t, true, true)
}
func testQueueWorkerRawChain(t *testing.T, durable bool, managedMode ...bool) {
	managed := len(managedMode) == 1 && managedMode[0]
	testQueueWorkerChainScenario(t, durable, managed, "")
}
func testQueueWorkerChainScenario(t *testing.T, durable, managed bool, scenario string) {
	for _, tc := range []struct {
		value string
		bad   bool
	}{{"hello", false}, {"event-317", false}, {"hello", true}} {
		if scenario != "" && (tc.value != "hello" || tc.bad) {
			continue
		}
		t.Run(fmt.Sprintf("%s-bad%t", tc.value, tc.bad), func(t *testing.T) {
			service, base, auth, subject, authority, _ := fleetServiceFixture(t)
			now := time.Now().UTC()
			ar := service.authority.(fleetAuthorityRepository)
			subject.AuthenticatedAt = now.Add(-time.Minute)
			subject.ExpiresAt = now.Add(time.Hour)
			ar.mandate.Subject = subject
			ar.mandate.IssuedAt = now.Add(-time.Minute)
			ar.mandate.ExpiresAt = now.Add(time.Hour)
			if managed {
				ar.mandate.Hermes = core.HermesConfig{Model: "proof-no-key", Provider: "none"}
				auth.Authority.Hermes = ar.mandate.Hermes
			}
			auth.IssuedAt = ar.mandate.IssuedAt
			auth.ExpiresAt = ar.mandate.ExpiresAt
			auth.Digest = core.AuthorityContextDigest(auth)
			ar.authority = auth
			service.authority = ar
			service.authorityCommands = fleetAuthorityCommands{authority: auth, admitted: true}
			authority = digestRef(auth.ID, auth.Digest)
			service.now = func() time.Time { return now }
			r := &rawChainRepository{fleetServiceRepository: base, loops: map[string]loop.LoopRevision{}, admissions: map[string]fleet.GraphNodeAdmission{}, checkpoints: map[string]fleet.GraphNodeCheckpoint{}}
			service.repository = r
			makeLoop := func(id string, producer bool, expected string) loop.LoopRevision {
				if managed {
					sourceType := loop.TypeArtifact
					task := "Write confirmed: followed by the actual source bytes, no newline"
					if producer {
						sourceType = loop.TypeString
						task = "Write the actual source string bytes without a newline"
					}
					text := expected
					c := loop.DoerContract{Task: task, Workspace: t.TempDir(), WritableFiles: []string{"result.txt"}, VerifyFile: "result.txt", ExpectedText: &text, ExactBytes: true, MaxAttempts: 2}
					if !producer {
						c.MaxAttempts = 1
					}
					l, v, e := loop.NewManagedDoerRevision(id, 1, "", loop.ManagedDoerContract{Doer: c, SourceType: sourceType, ExpectedDigest: fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(expected)))})
					if e != nil {
						t.Fatalf("managed revision: %v %+v", e, v)
					}
					r.loops[id] = l
					return l
				}
				l := base.loop
				l.LoopID = id
				l.Inputs = []loop.Port{{ID: "value", Type: loop.TypeArtifact, Required: true}}
				l.Outputs = []loop.Port{{ID: "result", Type: loop.TypeString, Required: true}}
				if producer {
					l.Inputs[0].Type = loop.TypeString
					l.Outputs[0].Type = loop.TypeArtifact
				}
				l.Steps = append([]loop.Step(nil), l.Steps...)
				if l.Steps[0].ID != "work" {
					l.Steps[0], l.Steps[1] = l.Steps[1], l.Steps[0]
				}
				l.Steps[0].EvidenceClaims = append([]loop.EvidenceClaim(nil), l.Steps[0].EvidenceClaims...)
				l.Steps[0].EvidenceClaims[0].ExpectedDigest = fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(expected)))
				l.Steps[0].EvidenceClaims[0].MediaType = "text/plain"
				// Exact Loop interface ports must be connected to its action.
				l.Steps[0].InputPorts = l.Inputs
				l.Steps[0].OutputPorts = l.Outputs
				terminal := *l.Steps[1].Terminal
				terminal.OutputMappings = []loop.PortMapping{{SourcePort: "result", TargetPort: "result"}}
				l.Steps[1].Terminal = &terminal
				l.Steps[1].OutputPorts = l.Outputs
				l, v, e := loop.NewRevision(l)
				if e != nil {
					t.Fatalf("loop: %v %+v", e, v)
				}
				r.loops[id] = l
				return l
			}
			p := makeLoop("producer-loop", true, tc.value)
			c := makeLoop("consumer-loop", false, "confirmed:"+tc.value)
			port := func(ps []loop.Port) []graph.Port {
				out := []graph.Port{}
				for _, p := range ps {
					out = append(out, graph.Port{ID: p.ID, Type: graph.ValueType(p.Type), Required: p.Required})
				}
				return out
			}
			pn := base.graph.Nodes[0]
			pn.ID = "producer"
			pn.Loop = revisionRef(p.LoopID, 1, p.Digest)
			pn.Inputs = port(p.Inputs)
			pn.Outputs = port(p.Outputs)
			cn := pn
			cn.ID = "consumer"
			cn.Loop = revisionRef(c.LoopID, 1, c.Digest)
			cn.Inputs = port(c.Inputs)
			cn.Outputs = port(c.Outputs)
			inputPort := "value"
			outputType := graph.TypeString
			if managed {
				inputPort = "source"
				outputType = graph.TypeArtifact
			}
			g, v, e := graph.NewRevision(graph.GraphRevision{GraphID: "raw-chain", Revision: 1, Inputs: []graph.Port{{ID: "value", Type: graph.TypeString, Required: true}}, Outputs: []graph.Port{{ID: "result", Type: outputType, Required: true}}, Nodes: []graph.Node{cn, pn}, InputMappings: []graph.InputMapping{{GraphInput: "value", ToNodeID: pn.ID, ToPort: inputPort}}, OutputMappings: []graph.OutputMapping{{FromNodeID: cn.ID, FromPort: "result", GraphOutput: "result"}}, Dependencies: []graph.Dependency{{ID: "handoff", FromNodeID: pn.ID, ToNodeID: cn.ID, Mappings: []graph.PortMapping{{FromPort: "result", ToPort: inputPort}}}}})
			if e != nil {
				t.Fatalf("graph: %v %+v", e, v)
			}
			if durable {
				agent := base.agent
				agent.SchemaVersion = registry.AgentRevisionSchemaVersion
				agent.Source = registry.FleetSource{FleetID: "fleet", Kind: "current-fleet", SourceID: agent.AgentID}
				agent.Ownership.OwnerID = subject.PrincipalID
				agent.Digest = ""
				agent, e = registry.SealRevision(agent)
				if e != nil {
					t.Fatal(e)
				}
				base.agent = agent
				pn.Participant = revisionRef(agent.AgentID, agent.Revision, agent.Digest)
				cn.Participant = pn.Participant
				g.Nodes = []graph.Node{cn, pn}
				g, v, e = graph.NewRevision(g)
				if e != nil {
					t.Fatal(e)
				}
			}
			base.graph = g
			var repository fleet.Repository = r
			var nodes fleet.GraphNodeRepository = r
			var disk *fleetbadger.Store
			var diskRoot string
			if durable {
				diskRoot = filepath.Join(t.TempDir(), "fleet-v1")
				disk, e = fleetbadger.Open(context.Background(), diskRoot)
				if e != nil {
					t.Fatal(e)
				}
				t.Cleanup(func() {
					if disk != nil {
						_ = disk.Close()
					}
				})
				repository, nodes = disk, disk
				service.repository = disk
				fact := fleet.AuditFact{Event: core.AuditEvent{Type: "fixture.created", SubjectID: subject.ID, PrincipalID: subject.PrincipalID, Outcome: "succeeded", Reason: "controlled raw adapter"}}
				agent := base.agent
				_, e = disk.RegisterAgent(context.Background(), registry.AgentRegistration{SchemaVersion: registry.AgentRegistrationSchemaVersion, AgentID: agent.AgentID, Source: agent.Source, InitialRevision: revisionRef(agent.AgentID, agent.Revision, agent.Digest)}, agent, fact)
				if e != nil {
					t.Fatal(e)
				}
				for _, lr := range []loop.LoopRevision{p, c} {
					_, validation, err := loop.NewRevision(lr)
					if err != nil {
						t.Fatal(err)
					}
					_, err = service.PublishLoop(context.Background(), PublishLoopRequest{Subject: subject, Authority: authority, Publisher: pn.Participant, Publication: loop.PublishRequest{Revision: lr, Validation: validation, IdempotencyKey: lr.LoopID}})
					if err != nil {
						t.Fatal(err)
					}
					_, _, err = service.SetLoopLifecycle(context.Background(), SetLoopLifecycleRequest{Subject: subject, Authority: authority, Publisher: pn.Participant, Loop: revisionRef(lr.LoopID, lr.Revision, lr.Digest), State: loop.LifecycleActive, EventID: lr.LoopID + "-active"})
					if err != nil {
						t.Fatal(err)
					}
				}
				_, e = service.PublishGraph(context.Background(), PublishGraphRequest{Subject: subject, Authority: authority, Publication: graph.PublishRequest{Revision: g, Validation: v, IdempotencyKey: "publish-graph"}})
				if e != nil {
					t.Fatal(e)
				}
				published, err := disk.GetGraphRevision(context.Background(), g.GraphID, g.Revision)
				if err != nil || published.Digest != g.Digest {
					t.Fatalf("publication readback: %v", err)
				}
			}
			wire, _ := json.Marshal(tc.value)
			d, e := service.PrepareGraphRun(context.Background(), SubmitGraphRequest{Subject: subject, Authority: authority, Graph: revisionRef(g.GraphID, 1, g.Digest), Inputs: []graph.NormalizedInput{{PortID: "value", Type: graph.TypeString, Value: wire}}, SubmissionID: "submission", IdempotencyKey: "intent", SnapshotID: "snapshot", QueueItemID: "queue", GraphRunID: "run", TransitionID: "queued", RejectionID: "rejected", MaxAttempts: 1})
			if e != nil || d.Accepted == nil {
				t.Fatalf("submit: %+v %v", d, e)
			}
			blobs, e := store.Open(t.TempDir())
			if e != nil {
				t.Fatal(e)
			}
			verifier, _ := evidence.NewBlobVerifier(blobs)
			adapter := &rawChainAdapter{bad: tc.bad}
			worker, _ := NewQueueWorker(repository, service, blobs, verifier, adapter, service.now)
			if managed {
				configureSyntheticManagedChain(t, worker, p, c, tc.bad, adapter)
			}
			runCtx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if scenario == "deadline" {
				var stop context.CancelFunc
				runCtx, stop = context.WithTimeout(runCtx, time.Second)
				defer stop()
			}
			var req WorkRequest
			duplicateDenied := false
			boundary := &managedBoundaryRepository{Repository: repository, GraphNodeRepository: nodes, scenario: scenario}
			if scenario != "" {
				worker.repository = boundary
				boundary.afterProducer = func() {
					switch scenario {
					case "cancelled":
						cancel()
					case "deadline":
						<-runCtx.Done()
					case "duplicate-concurrent":
						before := len(adapter.calls)
						done := make(chan error, 1)
						go func() { _, err := worker.Process(context.Background(), req); done <- err }()
						duplicateDenied = <-done != nil && len(adapter.calls) == before
					case "authority-expired":
						now = auth.ExpiresAt.Add(time.Second)
					case "authority-revoked":
						service.authorityCommands = fleetAuthorityCommands{authority: auth, admitted: false}
					}
				}
				if scenario == "host-denied" {
					worker.implementation.config.AuthorizedContracts = nil
					worker.doerHostApproval = func(_ context.Context, contract loop.DoerContract, _ registry.AgentRevision) error {
						if boundary.produced {
							return &implementation.Halt{State: "denied"}
						}
						return nil
					}
				}
				if scenario == "pass-cap-denied" || scenario == "lease-expired" {
					worker.doerPassCap = func(_ context.Context, contract loop.DoerContract, _ registry.AgentRevision) (uint16, error) {
						if boundary.produced {
							if scenario == "lease-expired" {
								now = now.Add(2 * time.Minute)
								return contract.MaxAttempts, nil
							}
							return 0, nil
						}
						return contract.MaxAttempts, nil
					}
				}
				worker.blobs = &managedBoundaryBlobs{BlobStore: blobs, boundary: boundary}
			}
			req = WorkRequest{Subject: subject, Authority: authority, QueueItemID: "queue", WorkerID: "worker", LoopExecutionID: "child", ClaimID: "claim", AttemptID: "attempt", ClaimTransitionID: "claimed", TerminalTransitionID: "terminal", DispositionID: "disposition", ArtifactID: "artifact", LeaseDuration: time.Hour}
			if scenario == "lease-expired" {
				req.LeaseDuration = time.Minute
			}
			result, e := worker.Process(runCtx, req)
			if scenario == "duplicate-concurrent" && !duplicateDenied {
				t.Fatal("concurrent duplicate did not deny without effects")
			}
			if scenario != "" && scenario != "duplicate-concurrent" {
				if !boundary.produced || len(adapter.calls) == 0 {
					t.Fatalf("negative never reached producer: %v", e)
				}
				if e == nil || result.Disposition.State == execution.StateSucceeded {
					t.Fatalf("negative escaped %s: %+v %v", scenario, result, e)
				}
				if _, err := os.Stat(filepath.Join(c.ManagedDoer.Doer.Workspace, "result.txt")); scenario != "corrupt-post-write" && scenario != "corrupt-evidence" && !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("consumer wrote after %s: %v", scenario, err)
				}
				if scenario == "authority-expired" || scenario == "authority-revoked" || scenario == "cancelled" || scenario == "deadline" {
					if _, err := disk.GetGraphNode(context.Background(), "run", "consumer"); !errors.Is(err, fleet.ErrNotFound) {
						t.Fatalf("consumer admitted after authority/context loss: %v", err)
					}
				}
				if scenario == "corrupt-post-write" || scenario == "corrupt-evidence" {
					if _, err := os.Stat(filepath.Join(c.ManagedDoer.Doer.Workspace, "result.txt")); err != nil {
						t.Fatalf("late mutation did not reach controller write: %v", err)
					}
					if _, err := disk.GetGraphNodeCheckpoint(context.Background(), "run", "consumer"); err != nil {
						t.Fatalf("late mutation lost failed consumer checkpoint: %v", err)
					}
				}
				if strings.HasPrefix(scenario, "wrong-") || strings.HasPrefix(scenario, "corrupt-") {
					if !boundary.fired {
						t.Fatal("boundary mutation not exercised")
					}
				}
				if scenario == "lease-expired" {
					child, err := disk.GetGraphNodeCheckpoint(context.Background(), "run", "consumer")
					if err != nil || child.Completion.Disposition.State != execution.StateExpired || result.Disposition.State != execution.StateExpired {
						t.Fatalf("lease expiry lost: %+v %+v %v", child, result, err)
					}
				}
				before := len(adapter.calls)
				_, replayErr := worker.Process(context.Background(), req)
				if replayErr == nil || len(adapter.calls) != before {
					t.Fatal("negative replay caused effects")
				}
				return
			}
			if tc.bad {
				failed, err := nodes.GetGraphNodeCheckpoint(context.Background(), "run", "producer")
				if err != nil || failed.Completion.Disposition.State != execution.StateFailed || failed.Completion.Artifact != nil || len(failed.Completion.Receipts) != 0 {
					t.Fatalf("failed child not durable: %+v %v", failed, err)
				}
				expectedCalls := 1
				if managed {
					expectedCalls = int(p.ManagedDoer.Doer.MaxAttempts)
				}
				if e == nil || len(adapter.calls) != expectedCalls {
					t.Fatalf("bad producer escaped: %v %+v", e, adapter.calls)
				}
				if durable {
					terminal, err := disk.GetDispositionByGraphRun(context.Background(), "run")
					if err != nil || terminal.State != execution.StateFailed {
						t.Fatalf("producer failure root: %+v %v", terminal, err)
					}
					if _, err = disk.GetGraphNode(context.Background(), "run", "consumer"); !errors.Is(err, fleet.ErrNotFound) {
						t.Fatalf("failed producer admitted consumer: %v", err)
					}
				}
				if durable {
					if err := disk.Close(); err != nil {
						t.Fatal(err)
					}
					disk, err = fleetbadger.Open(context.Background(), diskRoot)
					if err != nil {
						t.Fatal(err)
					}
					service.repository = disk
					reopened, err := disk.GetGraphNodeCheckpoint(context.Background(), "run", "producer")
					if err != nil || !reflect.DeepEqual(reopened.Completion.Disposition, failed.Completion.Disposition) {
						t.Fatalf("failed child reopen: %+v %v", reopened, err)
					}
					projection, err := disk.GetQueueProjection(context.Background(), "queue")
					if err != nil || projection.Attempts != 1 {
						t.Fatalf("outer retry budget changed: %+v %v", projection, err)
					}
					worker.repository = disk
					before := len(adapter.calls)
					if _, err = worker.Process(context.Background(), req); err == nil || len(adapter.calls) != before {
						t.Fatal("failed replay reset budget")
					}
				}
				return
			}
			if e != nil {
				t.Fatal(e)
			}
			if len(adapter.calls) != 2 || result.Disposition.State != execution.StateSucceeded {
				t.Fatalf("incomplete: %+v calls=%v", result, adapter.calls)
			}
			if durable {
				if err := disk.Close(); err != nil {
					t.Fatal(err)
				}
				disk = nil
				disk, e = fleetbadger.Open(context.Background(), diskRoot)
				if e != nil {
					t.Fatal(e)
				}
				service.repository = disk
				nodes = disk
				worker, e = NewQueueWorker(disk, service, blobs, verifier, adapter, service.now)
				if e != nil {
					t.Fatal(e)
				}
				root, err := disk.GetDispositionByGraphRun(context.Background(), "run")
				if err != nil || root.DispositionID != result.Disposition.DispositionID {
					t.Fatalf("root after restart: %+v %v", root, err)
				}
			}
			producer, err := nodes.GetGraphNodeCheckpoint(context.Background(), "run", "producer")
			if err != nil {
				t.Fatal(err)
			}
			consumer, err := nodes.GetGraphNodeCheckpoint(context.Background(), "run", "consumer")
			if err != nil {
				t.Fatal(err)
			}
			if managed {
				if producer.Admission.MaxImplementationPasses != 2 || consumer.Admission.MaxImplementationPasses != 1 {
					t.Fatal("managed pass budget not pinned")
				}
				for _, lr := range []loop.LoopRevision{p, c} {
					data, err := os.ReadFile(filepath.Join(lr.ManagedDoer.Doer.Workspace, "result.txt"))
					if err != nil || string(data) != *lr.ManagedDoer.Doer.ExpectedText {
						t.Fatalf("managed file bytes: %q %v", data, err)
					}
				}
			}
			input := consumer.Admission.Input
			if input == nil || input.ArtifactID != producer.Completion.Artifact.ID || input.AttemptID != producer.Admission.Attempt.AttemptID || input.EdgeID != "handoff" || consumer.Admission.Attempt.AttemptID == producer.Admission.Attempt.AttemptID {
				t.Fatal("lost exact durable handoff")
			}
			got, e := blobs.GetBlob(result.Artifact.ContentRef)
			_, replayErr := worker.Process(context.Background(), WorkRequest{Subject: subject, Authority: authority, QueueItemID: "queue", WorkerID: "worker", LeaseDuration: time.Hour})
			if replayErr == nil || len(adapter.calls) != 2 {
				t.Fatal("terminal replay caused effects")
			}
			if e != nil || string(got) != "confirmed:"+tc.value {
				t.Fatalf("actual bytes %q %v", got, e)
			}
		})
	}
}
