package badger

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/berryhill/aegis/internal/disposition"
	"github.com/berryhill/aegis/internal/evidence"
	"github.com/berryhill/aegis/internal/execution"
	"github.com/berryhill/aegis/internal/graph"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	"github.com/berryhill/aegis/internal/queue"
	badgerdb "github.com/dgraph-io/badger/v4"
	"path/filepath"
	"testing"
)

// Raw v2 controlled repository fixture: not managed Doer or live acceptance.
func TestGraphNodeProtocolDurableChain(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "fleet-v1")
	s, accepted := lifecycleFixture(t, ctx, root, "two-node")
	t.Cleanup(func() {
		if s != nil {
			_ = s.Close()
		}
	})
	fail := func(e error) {
		t.Helper()
		if e != nil {
			t.Fatal(e)
		}
	}
	original, _ := s.GetGraphRevision(ctx, accepted.Snapshot.Graph.ID, accepted.Snapshot.Graph.Revision)
	base, _ := loopFixture(t)
	producer := base
	producer.LoopID = "loop.producer"
	producer.Outputs = []loop.Port{{ID: "result", Type: loop.TypeArtifact, Required: true}}
	producer.Steps[0].OutputPorts = []loop.Port{{ID: "final", Type: loop.TypeArtifact, Required: true}}
	producer, pv, e := loop.NewRevision(producer)
	if e != nil {
		t.Fatalf("producer: %v %+v", e, pv)
	}
	_, e = s.PublishLoop(ctx, loop.PublishRequest{Revision: producer, Validation: pv, Provenance: loopPublicationProvenance(t, producer, pv), IdempotencyKey: "producer"}, audit("loop.published", producer.LoopID))
	fail(e)
	base, _ = loopFixture(t)
	consumer := base
	consumer.LoopID = "loop.consumer"
	consumer.Inputs = []loop.Port{{ID: "value", Type: loop.TypeArtifact, Required: true}}
	consumer.Steps[1].InputPorts = consumer.Inputs
	consumer, cv, e := loop.NewRevision(consumer)
	fail(e)
	_, e = s.PublishLoop(ctx, loop.PublishRequest{Revision: consumer, Validation: cv, Provenance: loopPublicationProvenance(t, consumer, cv), IdempotencyKey: "consumer"}, audit("loop.published", consumer.LoopID))
	fail(e)
	pn := original.Nodes[0]
	pn.ID = "producer"
	pn.Loop.ID = producer.LoopID
	pn.Loop.Digest = producer.Digest
	pn.Outputs = []graph.Port{{ID: "result", Type: graph.TypeArtifact, Required: true}}
	cn := original.Nodes[0]
	cn.ID = "consumer"
	cn.Loop.ID = consumer.LoopID
	cn.Loop.Digest = consumer.Digest
	cn.Inputs = []graph.Port{{ID: "value", Type: graph.TypeArtifact, Required: true}}
	cn.Outputs = []graph.Port{{ID: "result", Type: graph.TypeString, Required: true}}
	g := original
	g.GraphID = "graph.chain"
	g.Nodes = []graph.Node{pn, cn}
	g.InputMappings = []graph.InputMapping{{GraphInput: "value", ToNodeID: pn.ID, ToPort: "value"}}
	g.OutputMappings = []graph.OutputMapping{{FromNodeID: cn.ID, FromPort: "result", GraphOutput: "result"}}
	g.Dependencies = []graph.Dependency{{ID: "handoff", FromNodeID: pn.ID, ToNodeID: cn.ID, Mappings: []graph.PortMapping{{FromPort: "result", ToPort: "value"}}}}
	g, gv, e := graph.NewRevision(g)
	fail(e)
	_, e = s.PublishGraph(ctx, graph.PublishRequest{Revision: g, Validation: gv, IdempotencyKey: "chain"}, audit("graph.published", g.GraphID))
	fail(e)
	accepted.Snapshot, e = graph.NewRunSnapshot("snapshot-chain", g, accepted.Snapshot.Inputs)
	fail(e)
	accepted.Submission.Snapshot = lifecycleDigestRef(accepted.Snapshot.SnapshotID, accepted.Snapshot.Digest)
	accepted.Submission, e = queue.NewSubmission(accepted.Submission)
	fail(e)
	accepted.QueueItem.Submission = lifecycleDigestRef(accepted.Submission.SubmissionID, accepted.Submission.Digest)
	accepted.QueueItem.Snapshot = accepted.Submission.Snapshot
	accepted.QueueItem.MaxAttempts = 1
	accepted.QueueItem, e = queue.NewItem(accepted.QueueItem)
	fail(e)
	accepted.GraphRun.QueueItem = lifecycleDigestRef(accepted.QueueItem.ItemID, accepted.QueueItem.Digest)
	accepted.GraphRun.Snapshot = accepted.Submission.Snapshot
	accepted.GraphRun, e = execution.NewGraphRun(accepted.GraphRun)
	fail(e)
	_, e = s.AcceptSubmission(ctx, accepted, audit("submission.accepted", accepted.Submission.SubmissionID))
	fail(e)
	child, e := execution.NewLoopExecution(execution.LoopExecution{LoopExecutionID: "producer-exec", GraphRunID: accepted.GraphRun.GraphRunID, GraphNodeID: pn.ID, Loop: pn.Loop, Participant: pn.Participant, CreatedAt: accepted.Submission.SubmittedAt})
	fail(e)
	_, e = s.CreateLoopExecution(ctx, child, audit("loop-execution.created", child.LoopExecutionID))
	fail(e)
	claim, attempt, tr := claimFixture(t, accepted, child, "outer-claim", "producer-attempt")
	fail(s.ClaimQueueItem(ctx, claim, attempt, tr, audit("queue.claimed", claim.ClaimID)))
	// Obtain real blob-verifier seals from the existing isolated evidence fixture.
	other, c, blobs := completionFixture(t, ctx, filepath.Join(t.TempDir(), "fleet-v1"))
	defer other.Close()
	rebind := func(a fleet.GraphNodeAdmission, id string) fleet.Completion {
		out := c
		out.Claim = claim
		art := *c.Artifact
		art.ID = id + "-artifact"
		art.AttemptID = a.Attempt.AttemptID
		art.RunID = a.Execution.LoopExecutionID
		out.Artifact = &art
		r := c.Receipts[0]
		r.ID = id + "-receipt"
		r.AttemptID = art.AttemptID
		r.ArtifactID = art.ID
		r.RunID = art.RunID
		persistReceiptInto(t, blobs, &r)
		out.Receipts = []evidence.VerificationReceipt{r}
		out.Disposition, e = disposition.New(disposition.Record{DispositionID: id + "-disposition", GraphRunID: a.Execution.GraphRunID, LoopExecutionID: art.RunID, AttemptID: art.AttemptID, QueueItem: claim.QueueItem, Authority: claim.Authority, State: execution.StateSucceeded, ReasonCode: "evidence_satisfied", ArtifactIDs: []string{art.ID}, ReceiptIDs: []string{r.ID}, OccurredAt: claim.ClaimedAt})
		fail(e)
		out.Transition.QueueItemID = claim.QueueItem.ID
		out.Transition.ClaimID = claim.ClaimID
		out.Transition.TransitionID = id + "-transition"
		out.Transition, e = queue.NewTransition(out.Transition)
		fail(e)
		sealCompletion(t, &out, blobs)
		return out
	}
	pa := fleet.GraphNodeAdmission{SchemaVersion: fleet.GraphNodeSchemaVersion, Claim: claim, Execution: child, Attempt: attempt, MaxImplementationPasses: 2, AdmittedAt: claim.ClaimedAt}
	fail(s.AdmitGraphNode(ctx, pa, audit("fleet.graph.node.admitted", pn.ID)))
	ce := child
	ce.LoopExecutionID = "consumer-exec"
	ce.GraphNodeID = cn.ID
	ce.Loop = cn.Loop
	ce, e = execution.NewLoopExecution(ce)
	fail(e)
	ca := pa
	ca.Execution = ce
	ca.Attempt.AttemptID = "consumer-attempt"
	ca.Attempt.LoopExecutionID = ce.LoopExecutionID
	ca.Attempt, e = execution.NewAttempt(ca.Attempt)
	fail(e)
	if e = s.AdmitGraphNode(ctx, ca, audit("fleet.graph.node.admitted", cn.ID)); !errors.Is(e, fleet.ErrConflict) {
		t.Fatalf("early consumer: %v", e)
	}
	pc := fleet.GraphNodeCheckpoint{Admission: pa, Completion: rebind(pa, "producer")}
	badResult := pc
	badArtifact := *pc.Completion.Artifact
	badArtifact.Digest = "sha256:0000000000000000000000000000000000000000000000000000000000000000"
	badResult.Completion.Artifact = &badArtifact
	if e = s.CheckpointGraphNode(ctx, badResult, audit("fleet.graph.node.checkpointed", pn.ID), blobs); !errors.Is(e, fleet.ErrConflict) {
		t.Fatalf("wrong bytes: %v", e)
	}
	expired := ca
	expired.AdmittedAt = claim.ExpiresAt
	expired.Attempt.CreatedAt = claim.ExpiresAt
	expired.Attempt, e = execution.NewAttempt(expired.Attempt)
	fail(e)
	if e = s.AdmitGraphNode(ctx, expired, audit("fleet.graph.node.admitted", cn.ID)); !errors.Is(e, fleet.ErrConflict) {
		t.Fatalf("expired lease: %v", e)
	}
	fail(s.CheckpointGraphNode(ctx, pc, audit("fleet.graph.node.checkpointed", pn.ID), blobs))
	p, e := s.GetQueueProjection(ctx, claim.QueueItem.ID)
	fail(e)
	if p.State != queue.StateClaimed || p.Attempts != 1 {
		t.Fatalf("producer terminalized/reset queue: %+v", p)
	}
	fail(s.Close())
	s, e = Open(ctx, root)
	fail(e)
	got, e := s.GetGraphNodeCheckpoint(ctx, attempt.GraphRunID, pn.ID)
	fail(e)
	if got.Completion.Artifact.ID != pc.Completion.Artifact.ID {
		t.Fatal("lost checkpoint")
	}
	plan, e := graph.ResolveProducerConsumerPlan(accepted.Snapshot, g)
	fail(e)
	input := exactHandoff(plan, pc)
	ca.Input = &input
	bad := input
	bad.AttemptID = "same-bytes-other-attempt"
	ca.Input = &bad
	if e = s.AdmitGraphNode(ctx, ca, audit("fleet.graph.node.admitted", cn.ID)); !errors.Is(e, fleet.ErrConflict) {
		t.Fatalf("wrong lineage: %v", e)
	}
	ca.Input = &input
	fail(s.AdmitGraphNode(ctx, ca, audit("fleet.graph.node.admitted", cn.ID)))
	cc := fleet.GraphNodeCheckpoint{Admission: ca, Completion: rebind(ca, "consumer")}
	if e = s.CompleteGraph(ctx, cc.Completion, completionAudit(cc.Completion), blobs); !errors.Is(e, fleet.ErrConflict) {
		t.Fatalf("early aggregate: %v", e)
	}
	fail(s.CheckpointGraphNode(ctx, cc, audit("fleet.graph.node.checkpointed", cn.ID), blobs))
	// Corrupt canonical family bytes in isolated store only. Each mutation must
	// deny readback and aggregate, then restore the original immutable bytes.
	for _, family := range []byte{familyGraphNode, familyGraphNodeCheckpoint} {
		k := key(family, ca.Execution.GraphRunID, cn.ID)
		var original []byte
		fail(s.view(ctx, func(txn *badgerdb.Txn) error { var err error; original, err = get(txn, k); return err }))
		mutations := map[string]func(map[string]any){
			"unknown":    func(m map[string]any) { m["unreviewed"] = true },
			"bad-schema": func(m map[string]any) { m["schema_version"] = "unknown" },
			"maxpass":    func(m map[string]any) { m["max_implementation_passes"] = float64(3) },
			"claim":      func(m map[string]any) { m["claim"].(map[string]any)["worker_id"] = "other-worker" },
			"handoff":    func(m map[string]any) { m["input"].(map[string]any)["artifact_id"] = "same-bytes-wrong-artifact" },
		}
		for name, mutate := range mutations {
			t.Run(fmt.Sprintf("family-%x-%s", family, name), func(t *testing.T) {
				var envelope map[string]any
				fail(json.Unmarshal(original, &envelope))
				payload := envelope["payload"].(map[string]any)
				admission := payload
				if family == familyGraphNodeCheckpoint {
					admission = payload["admission"].(map[string]any)
				}
				mutate(admission)
				corruptWire, err := json.Marshal(envelope)
				fail(err)
				fail(s.update(ctx, func(txn *badgerdb.Txn) error { return txn.Set(k, corruptWire) }))
				if err = s.view(ctx, validateGraphNodeFamilies); !errors.Is(err, fleet.ErrCorrupt) {
					t.Fatalf("family qualification accepted mutation: %v", err)
				}
				if family == familyGraphNode {
					if _, err = s.GetGraphNode(ctx, ca.Execution.GraphRunID, cn.ID); !errors.Is(err, fleet.ErrCorrupt) {
						t.Fatalf("admission mutation escaped: %v", err)
					}
				}
				if _, err = s.GetGraphNodeCheckpoint(ctx, ca.Execution.GraphRunID, cn.ID); !errors.Is(err, fleet.ErrCorrupt) {
					t.Fatalf("checkpoint mutation escaped: %v", err)
				}
				if err = s.CompleteGraph(ctx, cc.Completion, completionAudit(cc.Completion), blobs); !errors.Is(err, fleet.ErrConflict) {
					t.Fatalf("aggregate mutation escaped: %v", err)
				}
				fail(s.update(ctx, func(txn *badgerdb.Txn) error { return txn.Set(k, original) }))
			})
		}
		// Recompute the envelope digest: semantic validation must reject
		// forged but canonically encoded facts too, not just digest drift.
		if family == familyGraphNode {
			for _, name := range []string{"valid-range-maxpass", "claim", "handoff"} {
				forged := ca
				switch name {
				case "valid-range-maxpass":
					forged.MaxImplementationPasses = 1
				case "claim":
					forged.Claim.WorkerID = "different-worker"
				case "handoff":
					v := *forged.Input
					v.AttemptID = "same-bytes-other-attempt"
					forged.Input = &v
				}
				wire, err := marshalGraphNodeRecord(fleet.GraphNodeSchemaVersion, forged)
				fail(err)
				fail(s.update(ctx, func(txn *badgerdb.Txn) error { return txn.Set(k, wire) }))
				if err = s.view(ctx, validateGraphNodeFamilies); err == nil {
					t.Fatalf("canonical %s accepted", name)
				}
				fail(s.update(ctx, func(txn *badgerdb.Txn) error { return txn.Set(k, original) }))
			}
			fail(s.update(ctx, func(txn *badgerdb.Txn) error { return txn.Delete(k) }))
			if err := s.view(ctx, validateGraphNodeFamilies); err == nil {
				t.Fatal("orphan checkpoint accepted")
			}
			fail(s.update(ctx, func(txn *badgerdb.Txn) error { return txn.Set(k, original) }))
		}
		if family == familyGraphNodeCheckpoint {
			// Even an internally canonical/digest-matching checkpoint cannot
			// substitute an admission independently stored under the same key.
			mismatch := cc
			input := *cc.Admission.Input
			input.ArtifactID = "same-bytes-other-admission"
			mismatch.Admission.Input = &input
			wire, err := marshalGraphNodeRecord(graphNodeCheckpointSchema, mismatch)
			fail(err)
			fail(s.update(ctx, func(txn *badgerdb.Txn) error { return txn.Set(k, wire) }))
			if _, err = s.GetGraphNodeCheckpoint(ctx, ca.Execution.GraphRunID, cn.ID); !errors.Is(err, fleet.ErrCorrupt) {
				t.Fatalf("mismatched independent admission escaped: %v", err)
			}
			if err = s.CompleteGraph(ctx, cc.Completion, completionAudit(cc.Completion), blobs); !errors.Is(err, fleet.ErrConflict) {
				t.Fatalf("mismatched aggregate escaped: %v", err)
			}
			fail(s.update(ctx, func(txn *badgerdb.Txn) error { return txn.Set(k, original) }))
		}
	}
	fail(s.CompleteGraph(ctx, cc.Completion, completionAudit(cc.Completion), blobs))
	if e = s.CompleteGraph(ctx, cc.Completion, completionAudit(cc.Completion), blobs); !errors.Is(e, fleet.ErrConflict) {
		t.Fatalf("aggregate replay: %v", e)
	}
	fail(s.Close())
	s, e = Open(ctx, root)
	fail(e)
	p, e = s.GetQueueProjection(ctx, claim.QueueItem.ID)
	fail(e)
	if p.State != queue.StateSucceeded || p.Attempts != 1 {
		t.Fatalf("durable aggregate: %+v", p)
	}
	// Physical Badger export/restore fixture: prove both canonical namespaces
	// survive alongside the root, rather than a projection-only reconstruction.
	var snapshot bytes.Buffer
	_, e = s.db.Backup(&snapshot, 0)
	fail(e)
	restoreRoot := filepath.Join(t.TempDir(), "fleet-v1")
	restored, e := Open(ctx, restoreRoot)
	fail(e)
	t.Cleanup(func() {
		if restored != nil {
			_ = restored.Close()
		}
	})
	fail(restored.db.Load(bytes.NewReader(snapshot.Bytes()), 2))
	fail(restored.Close())
	restored, e = Open(ctx, restoreRoot)
	fail(e)
	for _, node := range []string{pn.ID, cn.ID} {
		_, e = restored.GetGraphNode(ctx, attempt.GraphRunID, node)
		fail(e)
		_, e = restored.GetGraphNodeCheckpoint(ctx, attempt.GraphRunID, node)
		fail(e)
	}
	terminal, e := restored.GetDispositionByGraphRun(ctx, attempt.GraphRunID)
	fail(e)
	if terminal.DispositionID != cc.Completion.Disposition.DispositionID {
		t.Fatal("snapshot lost authoritative root")
	}
	// Actual startup, not only direct invocation of the family validator. Each
	// corrupt copy is an independently restored synthetic fixture.
	for _, mutation := range []string{"unknown-envelope", "schema", "maxpass", "claim", "handoff", "missing-admission", "missing-checkpoint", "checkpoint-admission"} {
		t.Run("startup-"+mutation, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "fleet-v1")
			copy, err := Open(ctx, path)
			if err != nil {
				t.Fatal(err)
			}
			if err = copy.db.Load(bytes.NewReader(snapshot.Bytes()), 2); err != nil {
				copy.Close()
				t.Fatal(err)
			}
			err = copy.update(ctx, func(txn *badgerdb.Txn) error {
				admissionKey := key(familyGraphNode, ca.Execution.GraphRunID, cn.ID)
				checkpointKey := key(familyGraphNodeCheckpoint, ca.Execution.GraphRunID, cn.ID)
				switch mutation {
				case "missing-admission":
					return txn.Delete(admissionKey)
				case "missing-checkpoint":
					return txn.Delete(checkpointKey)
				case "checkpoint-admission":
					forged := cc
					input := *forged.Admission.Input
					input.ArtifactID = "wrong-admission"
					forged.Admission.Input = &input
					wire, e := marshalGraphNodeRecord(graphNodeCheckpointSchema, forged)
					if e != nil {
						return e
					}
					return txn.Set(checkpointKey, wire)
				}
				forged := ca
				switch mutation {
				case "maxpass":
					forged.MaxImplementationPasses = 1
				case "claim":
					forged.Claim.WorkerID = "wrong-worker"
				case "handoff":
					input := *forged.Input
					input.ExecutionID = "same-bytes-other-run"
					forged.Input = &input
				}
				schema := fleet.GraphNodeSchemaVersion
				if mutation == "schema" {
					schema = "unknown"
				}
				wire, e := marshalGraphNodeRecord(schema, forged)
				if e != nil {
					return e
				}
				if mutation == "unknown-envelope" {
					wire = append([]byte(`{"unreviewed":true,`), wire[1:]...)
				}
				return txn.Set(admissionKey, wire)
			})
			if err != nil {
				copy.Close()
				t.Fatal(err)
			}
			if err = copy.Close(); err != nil {
				t.Fatal(err)
			}
			reopened, err := Open(ctx, path)
			if reopened != nil {
				reopened.Close()
			}
			if err == nil {
				t.Fatalf("startup accepted %s", mutation)
			}
		})
	}
}
