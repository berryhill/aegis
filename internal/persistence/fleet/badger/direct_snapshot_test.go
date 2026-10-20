package badger

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/execution"
	"github.com/berryhill/aegis/internal/graph"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	"github.com/berryhill/aegis/internal/queue"
	"github.com/berryhill/aegis/internal/reference"
	badgerdb "github.com/dgraph-io/badger/v4"
)

func TestDirectSnapshotAtomicDurableWithoutGraph(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), schemaVersion)
	store, err := Open(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if store != nil {
			store.Close()
		}
	}()
	registration, agent := agentFixture(t)
	if _, err = store.RegisterAgent(ctx, registration, agent, audit("agent.registered", agent.AgentID)); err != nil {
		t.Fatal(err)
	}
	lr, lv := loopFixture(t)
	if _, err = store.PublishLoop(ctx, loop.PublishRequest{Revision: lr, Validation: lv, Provenance: loopPublicationProvenance(t, lr, lv), IdempotencyKey: "direct-loop"}, audit("loop.published", lr.LoopID)); err != nil {
		t.Fatal(err)
	}
	node := graph.Node{ID: "direct", Participant: registration.InitialRevision, Loop: reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: lr.LoopID, Revision: lr.Revision, Digest: lr.Digest}, Inputs: []graph.Port{{ID: "value", Type: graph.TypeString, Required: true}}, Outputs: []graph.Port{{ID: "result", Type: graph.TypeString, Required: true}}}
	nested, err := graph.NewDirectLoopSnapshot("direct-snapshot", node, []graph.NormalizedInput{{PortID: "value", Type: graph.TypeString, Value: []byte(`"hello"`)}})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := graph.NewDirectRunSnapshot(nested)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	authority := lifecycleDigestRef("authority", "sha256:"+strings.Repeat("f", 64))
	submission, err := queue.NewSubmission(queue.Submission{SubmissionID: "direct-submission", IdempotencyKey: "direct-request", Snapshot: lifecycleDigestRef(snapshot.SnapshotID, snapshot.Digest), Authority: authority, MandateID: "mandate", Runtime: "hermes-agent", SubmittedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	item, err := queue.NewItem(queue.Item{ItemID: "direct-item", Submission: lifecycleDigestRef(submission.SubmissionID, submission.Digest), Snapshot: submission.Snapshot, Authority: authority, GraphRunID: "direct-run", MaxAttempts: 2, EnqueuedAt: now, AvailableAt: now})
	if err != nil {
		t.Fatal(err)
	}
	run, err := execution.NewGraphRun(execution.GraphRun{GraphRunID: item.GraphRunID, QueueItem: lifecycleDigestRef(item.ItemID, item.Digest), Snapshot: submission.Snapshot, Authority: authority, CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	transition, err := queue.NewTransition(queue.QueueTransition{TransitionID: "direct-queued", QueueItemID: item.ItemID, To: queue.StateQueued, Reason: "fixture acceptance", OccurredAt: now})
	if err != nil {
		t.Fatal(err)
	}
	accepted := fleet.AcceptedSubmission{Snapshot: snapshot, Submission: submission, QueueItem: item, GraphRun: run, InitialTransition: transition}
	fact := audit("submission.accepted", submission.SubmissionID)
	// Valid sealed records with inconsistent cross-record identities must not
	// leave even a snapshot, request binding or audit behind.
	broken := accepted
	broken.GraphRun, err = execution.NewGraphRun(execution.GraphRun{GraphRunID: "other-run", QueueItem: run.QueueItem, Snapshot: run.Snapshot, Authority: authority, CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.AcceptSubmission(ctx, broken, fact); !errors.Is(err, fleet.ErrConflict) {
		t.Fatalf("cross-record mismatch: %v", err)
	}
	assertSnapshotFailureAtomic(t, store, snapshot.SnapshotID, 2)
	if created, err := store.AcceptSubmission(ctx, accepted, fact); err != nil || !created {
		t.Fatalf("accept %v %v", created, err)
	}
	original, _ := graph.MarshalRunSnapshot(snapshot)
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	store = nil
	store, err = Open(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	got, err := store.GetGraphRunSnapshot(ctx, snapshot.SnapshotID)
	if err != nil {
		t.Fatal(err)
	}
	wire, err := graph.MarshalRunSnapshot(got)
	if err != nil || !bytes.Equal(wire, original) {
		t.Fatalf("durable bytes: %v", err)
	}
	if _, err = graph.ResolveRunTopology(got, nil, &node); err != nil {
		t.Fatal(err)
	}
	assertAcceptedReadback(t, store, accepted)
	if created, err := store.AcceptSubmission(ctx, accepted, fact); err != nil || created {
		t.Fatalf("replay %v %v", created, err)
	}
	changed := fact
	changed.Event.Reason = "different request"
	if _, err := store.AcceptSubmission(ctx, accepted, changed); !errors.Is(err, fleet.ErrConflict) {
		t.Fatalf("conflict %v", err)
	}
	if err = store.view(ctx, func(txn *badgerdb.Txn) error {
		return scan(txn, familyGraphRevision, func(_, _ []byte) error { t.Fatal("direct persistence created Graph library entry"); return nil })
	}); err != nil {
		t.Fatal(err)
	}
	// A fully re-sealed but false interface must fail authoritative Loop readback.
	wrong := node
	wrong.Outputs = []graph.Port{{ID: "forged", Type: graph.TypeString, Required: true}}
	fake, _ := graph.NewDirectLoopSnapshot("wrong-interface", wrong, nested.Inputs)
	envelope, _ := graph.NewDirectRunSnapshot(fake)
	if _, err = store.CreateGraphRunSnapshot(ctx, envelope, audit("snapshot.created", "wrong-interface")); !errors.Is(err, fleet.ErrConflict) {
		t.Fatalf("interface substitution %v", err)
	}
	assertSnapshotFailureAtomic(t, store, "wrong-interface", 3)
	for _, kind := range []string{"participant", "loop"} {
		wrong := node
		if kind == "participant" {
			wrong.Participant.Digest = "sha256:" + strings.Repeat("e", 64)
		} else {
			wrong.Loop.Digest = "sha256:" + strings.Repeat("e", 64)
		}
		fake, err := graph.NewDirectLoopSnapshot("wrong-"+kind, wrong, nested.Inputs)
		if err != nil {
			t.Fatal(err)
		}
		envelope, err := graph.NewDirectRunSnapshot(fake)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = store.CreateGraphRunSnapshot(ctx, envelope, audit("snapshot.created", envelope.SnapshotID)); !errors.Is(err, fleet.ErrConflict) {
			t.Fatalf("%s substitution: %v", kind, err)
		}
		assertSnapshotFailureAtomic(t, store, envelope.SnapshotID, 3)
	}
	// Actual persisted-byte corruption is detected at readback, not reclassified.
	setRawRecord(t, store, key(familySnapshot, snapshot.SnapshotID), bytes.Replace(original, []byte(`"hello"`), []byte(`"tamper"`), 1))
	if _, err = store.GetGraphRunSnapshot(ctx, snapshot.SnapshotID); !errors.Is(err, fleet.ErrCorrupt) {
		t.Fatalf("tamper %v", err)
	}
}
