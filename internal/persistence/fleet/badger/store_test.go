package badger

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/graph"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	"github.com/berryhill/aegis/internal/reference"
	"github.com/berryhill/aegis/internal/registry"
	badgerdb "github.com/dgraph-io/badger/v4"
)

func TestStorePersistsFleetFactsAtomicallyAndDurably(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), schemaVersion)
	store, err := Open(ctx, root)
	if err != nil {
		t.Fatal(err)
	}

	registration, initial := agentFixture(t)
	created, err := store.RegisterAgent(ctx, registration, initial, audit("agent.registered", initial.AgentID))
	if err != nil || !created {
		t.Fatalf("register agent: created=%v err=%v", created, err)
	}
	created, err = store.RegisterAgent(ctx, registration, initial, audit("ignored.on.replay", initial.AgentID))
	if err != nil || created {
		t.Fatalf("exact registration replay: created=%v err=%v", created, err)
	}
	bySource, err := store.GetAgentRegistrationBySource(ctx, registration.Source)
	if err != nil || bySource != registration {
		t.Fatalf("source readback: got=%+v err=%v", bySource, err)
	}

	second := initial
	second.Revision = 2
	second.Lifecycle = registry.LifecycleDisabled
	second, err = registry.SealRevision(second)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.PublishAgentRevision(ctx, second, audit("agent.revision.published", second.AgentID)); err != nil {
		t.Fatal(err)
	}
	latest, err := store.LatestAgentRevision(ctx, initial.AgentID)
	if err != nil || latest.Digest != second.Digest {
		t.Fatalf("latest revision: got=%+v err=%v", latest, err)
	}
	history, err := store.ListAgentRevisions(ctx, initial.AgentID)
	if err != nil || len(history) != 2 || history[0].Digest != initial.Digest || history[1].Digest != second.Digest {
		t.Fatalf("ordered immutable Agent history: got=%+v err=%v", history, err)
	}
	if _, err = store.ListAgentRevisions(ctx, "missing-agent"); !errors.Is(err, fleet.ErrNotFound) {
		t.Fatalf("missing Agent history did not fail closed: %v", err)
	}

	loopRevision, loopValidation := loopFixture(t)
	loopRequest := loop.PublishRequest{Revision: loopRevision, Validation: loopValidation, IdempotencyKey: "loop-publish-1"}
	loopAudit := audit("loop.published", loopRevision.LoopID)
	decision, err := store.PublishLoop(ctx, loopRequest, loopAudit)
	if err != nil || decision.Idempotent {
		t.Fatalf("publish Loop: decision=%+v err=%v", decision, err)
	}
	decision, err = store.PublishLoop(ctx, loopRequest, loopAudit)
	if err != nil || !decision.Idempotent {
		t.Fatalf("replay Loop publication: decision=%+v err=%v", decision, err)
	}
	storedLoopValidation, err := store.GetLoopValidation(ctx, loopRevision.LoopID, 1, loopValidation.Digest)
	if err != nil || storedLoopValidation.RevisionDigest != loopRevision.Digest {
		t.Fatalf("Loop validation readback: got=%+v err=%v", storedLoopValidation, err)
	}
	loopRevisions, err := store.ListLoopRevisions(ctx)
	if err != nil || len(loopRevisions) != 1 || loopRevisions[0].Digest != loopRevision.Digest {
		t.Fatalf("Loop collection readback: got=%+v err=%v", loopRevisions, err)
	}
	loopValidations, err := store.ListLoopValidations(ctx)
	if err != nil || len(loopValidations) != 1 || loopValidations[0].Digest != loopValidation.Digest {
		t.Fatalf("Loop validation collection readback: got=%+v err=%v", loopValidations, err)
	}

	graphRevision, graphValidation := graphFixture(t, initial, loopRevision)
	graphRequest := graph.PublishRequest{Revision: graphRevision, Validation: graphValidation, IdempotencyKey: "graph-publish-1"}
	graphAudit := audit("graph.published", graphRevision.GraphID)
	decisionGraph, err := store.PublishGraph(ctx, graphRequest, graphAudit)
	if err != nil || decisionGraph.Idempotent {
		t.Fatalf("publish Graph: decision=%+v err=%v", decisionGraph, err)
	}
	decisionGraph, err = store.PublishGraph(ctx, graphRequest, graphAudit)
	if err != nil || !decisionGraph.Idempotent {
		t.Fatalf("replay Graph publication: decision=%+v err=%v", decisionGraph, err)
	}
	storedGraph, err := store.GetGraphRevision(ctx, graphRevision.GraphID, 1)
	if err != nil || storedGraph.Digest != graphRevision.Digest {
		t.Fatalf("Graph readback: got=%+v err=%v", storedGraph, err)
	}
	graphRevisions, err := store.ListGraphRevisions(ctx)
	if err != nil || len(graphRevisions) != 1 || graphRevisions[0].Digest != graphRevision.Digest {
		t.Fatalf("Graph collection readback: got=%+v err=%v", graphRevisions, err)
	}
	graphValidations, err := store.ListGraphValidations(ctx)
	if err != nil || len(graphValidations) != 1 || graphValidations[0].Digest != graphValidation.Digest {
		t.Fatalf("Graph validation collection readback: got=%+v err=%v", graphValidations, err)
	}

	snapshot, err := graph.NewRunSnapshot("snapshot-1", graphRevision, []graph.NormalizedInput{{PortID: "value", Type: graph.TypeString, Value: json.RawMessage(`"hello"`)}})
	if err != nil {
		t.Fatal(err)
	}
	snapshotAudit := audit("graph.snapshot.created", snapshot.SnapshotID)
	created, err = store.CreateGraphRunSnapshot(ctx, snapshot, snapshotAudit)
	if err != nil || !created {
		t.Fatalf("create snapshot: created=%v err=%v", created, err)
	}
	created, err = store.CreateGraphRunSnapshot(ctx, snapshot, snapshotAudit)
	if err != nil || created {
		t.Fatalf("replay snapshot: created=%v err=%v", created, err)
	}
	changedSnapshotAudit := snapshotAudit
	changedSnapshotAudit.Event.Reason = "different snapshot semantic"
	if _, err = store.CreateGraphRunSnapshot(ctx, snapshot, changedSnapshotAudit); !errors.Is(err, fleet.ErrConflict) {
		t.Fatalf("changed snapshot replay semantics did not conflict: %v", err)
	}

	events, err := store.AuditEvents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 5 {
		t.Fatalf("expected one audit fact per mutation and none for replay, got %d", len(events))
	}
	for index, event := range events {
		if event.ID == "" || event.OccurredAt.IsZero() || event.EventDigest == "" {
			t.Fatalf("repository did not assign audit chain fields: %+v", event)
		}
		if index > 0 && event.PreviousDigest != events[index-1].EventDigest {
			t.Fatalf("audit chain broken at %d", index)
		}
	}

	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(ctx, root)
	if err != nil {
		t.Fatalf("reopen clean durable store: %v", err)
	}
	defer store.Close()
	storedSnapshot, err := store.GetGraphRunSnapshot(ctx, snapshot.SnapshotID)
	if err != nil || storedSnapshot.Digest != snapshot.Digest {
		t.Fatalf("durable snapshot readback: got=%+v err=%v", storedSnapshot, err)
	}
	events, err = store.AuditEvents(ctx)
	if err != nil || len(events) != 5 {
		t.Fatalf("durable audit readback: count=%d err=%v", len(events), err)
	}
}

func TestStoreDeniesConflictsAndRollsBackMutationWithoutAuthoritativeAudit(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), schemaVersion))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	registration, initial := agentFixture(t)
	if _, err = store.RegisterAgent(ctx, registration, initial, audit("agent.registered", initial.AgentID)); err != nil {
		t.Fatal(err)
	}

	conflictingRegistration := registration
	conflictingRegistration.Source.SourceID = "source-other"
	conflictingInitial := initial
	conflictingInitial.Source = conflictingRegistration.Source
	conflictingInitial, err = registry.SealRevision(conflictingInitial)
	if err != nil {
		t.Fatal(err)
	}
	conflictingRegistration.InitialRevision.Digest = conflictingInitial.Digest
	if _, err = store.RegisterAgent(ctx, conflictingRegistration, conflictingInitial, audit("agent.registered", initial.AgentID)); !errors.Is(err, fleet.ErrConflict) {
		t.Fatalf("agent identity rebinding was not denied: %v", err)
	}

	gap := initial
	gap.Revision = 3
	gap, err = registry.SealRevision(gap)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.PublishAgentRevision(ctx, gap, audit("agent.revision.published", gap.AgentID)); !errors.Is(err, fleet.ErrConflict) {
		t.Fatalf("revision gap was not denied: %v", err)
	}

	second := initial
	second.Revision = 2
	second.Lifecycle = registry.LifecycleDisabled
	second, err = registry.SealRevision(second)
	if err != nil {
		t.Fatal(err)
	}
	badAudit := audit("agent.revision.published", second.AgentID)
	badAudit.Event.ID = "caller-assigned"
	if err = store.PublishAgentRevision(ctx, second, badAudit); err == nil {
		t.Fatal("caller-assigned authoritative audit fields were accepted")
	}
	if _, err = store.GetAgentRevision(ctx, second.AgentID, second.Revision); !errors.Is(err, fleet.ErrNotFound) {
		t.Fatalf("mutation was not rolled back with rejected audit fact: %v", err)
	}
	events, err := store.AuditEvents(ctx)
	if err != nil || len(events) != 1 {
		t.Fatalf("failed mutations changed authoritative audit: count=%d err=%v", len(events), err)
	}

	if _, err = store.GetGraphRevision(ctx, "graph-missing", 1); !errors.Is(err, fleet.ErrNotFound) {
		t.Fatalf("missing exact read did not fail closed: %v", err)
	}
}

func TestStoreBindsExactReplayRequestAndAuditSemantics(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), schemaVersion))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	loopRevision, loopValidation := loopFixture(t)
	loopRequest := loop.PublishRequest{Revision: loopRevision, Validation: loopValidation, IdempotencyKey: "loop-binding"}
	loopAudit := audit("loop.published", loopRevision.LoopID)
	if _, err = store.PublishLoop(ctx, loopRequest, loopAudit); err != nil {
		t.Fatal(err)
	}
	changedValidation := loopRequest
	changedValidation.Validation.Digest = "sha256:" + strings.Repeat("b", 64)
	if _, err = store.PublishLoop(ctx, changedValidation, loopAudit); err == nil {
		t.Fatal("same-key changed Loop validation was accepted")
	}
	changedAudit := loopAudit
	changedAudit.Event.Reason = "different authorized semantic"
	if _, err = store.PublishLoop(ctx, loopRequest, changedAudit); !errors.Is(err, fleet.ErrConflict) {
		t.Fatalf("same-key changed Loop audit semantics did not conflict: %v", err)
	}

	registration, agent := agentFixture(t)
	if _, err = store.RegisterAgent(ctx, registration, agent, audit("agent.registered", agent.AgentID)); err != nil {
		t.Fatal(err)
	}
	graphRevision, graphValidation := graphFixture(t, agent, loopRevision)
	graphRequest := graph.PublishRequest{Revision: graphRevision, Validation: graphValidation, IdempotencyKey: "graph-binding"}
	graphAudit := audit("graph.published", graphRevision.GraphID)
	if _, err = store.PublishGraph(ctx, graphRequest, graphAudit); err != nil {
		t.Fatal(err)
	}
	changedGraphValidation := graphRequest
	changedGraphValidation.Validation.Digest = "sha256:" + strings.Repeat("c", 64)
	if _, err = store.PublishGraph(ctx, changedGraphValidation, graphAudit); err == nil {
		t.Fatal("same-key changed Graph validation was accepted")
	}
	changedGraphAudit := graphAudit
	changedGraphAudit.Event.Metadata = map[string]string{"authority_context": "different"}
	if _, err = store.PublishGraph(ctx, graphRequest, changedGraphAudit); !errors.Is(err, fleet.ErrConflict) {
		t.Fatalf("same-key changed Graph audit semantics did not conflict: %v", err)
	}

	events, err := store.AuditEvents(ctx)
	if err != nil || len(events) != 3 {
		t.Fatalf("conflicting replays changed authoritative audit: count=%d err=%v", len(events), err)
	}
}

func TestGraphRunSnapshotResolvesAuthoritativeReferencesAtomically(t *testing.T) {
	ctx := context.Background()
	t.Run("missing Graph denies without mutation", func(t *testing.T) {
		store, err := Open(ctx, filepath.Join(t.TempDir(), schemaVersion))
		if err != nil {
			t.Fatal(err)
		}
		defer store.Close()
		_, agent := agentFixture(t)
		loopRevision, _ := loopFixture(t)
		graphRevision, _ := graphFixture(t, agent, loopRevision)
		snapshot, err := graph.NewRunSnapshot("missing-graph", graphRevision, []graph.NormalizedInput{{PortID: "value", Type: graph.TypeString, Value: json.RawMessage(`"hello"`)}})
		if err != nil {
			t.Fatal(err)
		}
		assertSnapshotDenied(t, store, snapshot, fleet.ErrNotFound, 0)
	})

	t.Run("missing participant denies without mutation", func(t *testing.T) {
		store, snapshot := persistedSnapshotFixture(t, ctx, registry.LifecycleEnabled)
		defer store.Close()
		participant := snapshot.Participants[0]
		deleteRawRecord(t, store, key(familyAgentRevision, participant.ID, revisionPart(participant.Revision)))
		assertSnapshotDenied(t, store, snapshot, fleet.ErrNotFound, 3)
	})

	t.Run("missing Loop denies without mutation", func(t *testing.T) {
		store, snapshot := persistedSnapshotFixture(t, ctx, registry.LifecycleEnabled)
		defer store.Close()
		loopRef := snapshot.Loops[0]
		deleteRawRecord(t, store, key(familyLoopRevision, loopRef.ID, revisionPart(loopRef.Revision)))
		assertSnapshotDenied(t, store, snapshot, fleet.ErrNotFound, 3)
	})

	t.Run("missing Graph validation denies without mutation", func(t *testing.T) {
		store, snapshot := persistedSnapshotFixture(t, ctx, registry.LifecycleEnabled)
		defer store.Close()
		deleteRawPrefix(t, store, key(familyGraphValidation, snapshot.Graph.ID, revisionPart(snapshot.Graph.Revision)))
		assertSnapshotDenied(t, store, snapshot, fleet.ErrNotFound, 3)
	})

	t.Run("missing Loop validation denies without mutation", func(t *testing.T) {
		store, snapshot := persistedSnapshotFixture(t, ctx, registry.LifecycleEnabled)
		defer store.Close()
		loopRef := snapshot.Loops[0]
		deleteRawPrefix(t, store, key(familyLoopValidation, loopRef.ID, revisionPart(loopRef.Revision)))
		assertSnapshotDenied(t, store, snapshot, fleet.ErrNotFound, 3)
	})

	t.Run("Graph digest substitution denies without mutation", func(t *testing.T) {
		store, snapshot := persistedSnapshotFixture(t, ctx, registry.LifecycleEnabled)
		defer store.Close()
		stored, err := store.GetGraphRevision(ctx, snapshot.Graph.ID, snapshot.Graph.Revision)
		if err != nil {
			t.Fatal(err)
		}
		stored.Inputs[0].Required = false
		stored.Digest = ""
		substitute, _, err := graph.NewRevision(stored)
		if err != nil {
			t.Fatal(err)
		}
		replaceGraphRevision(t, store, substitute)
		assertSnapshotDenied(t, store, snapshot, fleet.ErrConflict, 3)
	})

	t.Run("participant digest substitution denies without mutation", func(t *testing.T) {
		store, snapshot := persistedSnapshotFixture(t, ctx, registry.LifecycleEnabled)
		defer store.Close()
		participant := snapshot.Participants[0]
		stored, err := store.GetAgentRevision(ctx, participant.ID, participant.Revision)
		if err != nil {
			t.Fatal(err)
		}
		stored.Charter.Digest = "sha256:" + strings.Repeat("d", 64)
		stored.Digest = ""
		substitute, err := registry.SealRevision(stored)
		if err != nil {
			t.Fatal(err)
		}
		replaceAgentRevision(t, store, substitute)
		assertSnapshotDenied(t, store, snapshot, fleet.ErrConflict, 3)
	})

	t.Run("Loop digest substitution denies without mutation", func(t *testing.T) {
		store, snapshot := persistedSnapshotFixture(t, ctx, registry.LifecycleEnabled)
		defer store.Close()
		loopRef := snapshot.Loops[0]
		stored, err := store.GetLoopRevision(ctx, loopRef.ID, loopRef.Revision)
		if err != nil {
			t.Fatal(err)
		}
		for index := range stored.Steps {
			if stored.Steps[index].Kind == loop.StepAction {
				stored.Steps[index].Retry.MaxAttempts = 2
				break
			}
		}
		stored.Digest = ""
		substitute, _, err := loop.NewRevision(stored)
		if err != nil {
			t.Fatal(err)
		}
		replaceLoopRevision(t, store, substitute)
		assertSnapshotDenied(t, store, snapshot, fleet.ErrConflict, 3)
	})

	t.Run("mismatched Graph validation identity denies without mutation", func(t *testing.T) {
		store, snapshot := persistedSnapshotFixture(t, ctx, registry.LifecycleEnabled)
		defer store.Close()
		stored, err := store.GetGraphRevision(ctx, snapshot.Graph.ID, snapshot.Graph.Revision)
		if err != nil {
			t.Fatal(err)
		}
		stored.Inputs[0].Required = false
		stored.Digest = ""
		_, substitute, err := graph.NewRevision(stored)
		if err != nil {
			t.Fatal(err)
		}
		deleteRawPrefix(t, store, key(familyGraphValidation, snapshot.Graph.ID, revisionPart(snapshot.Graph.Revision)))
		wire, err := graph.MarshalValidationResult(substitute)
		if err != nil {
			t.Fatal(err)
		}
		setRawRecord(t, store, key(familyGraphValidation, snapshot.Graph.ID, revisionPart(snapshot.Graph.Revision), substitute.Digest), wire)
		assertSnapshotDenied(t, store, snapshot, fleet.ErrNotFound, 3)
	})

	t.Run("mismatched Loop validation identity denies without mutation", func(t *testing.T) {
		store, snapshot := persistedSnapshotFixture(t, ctx, registry.LifecycleEnabled)
		defer store.Close()
		loopRef := snapshot.Loops[0]
		stored, err := store.GetLoopRevision(ctx, loopRef.ID, loopRef.Revision)
		if err != nil {
			t.Fatal(err)
		}
		for index := range stored.Steps {
			if stored.Steps[index].Kind == loop.StepAction {
				stored.Steps[index].Retry.MaxAttempts = 2
				break
			}
		}
		stored.Digest = ""
		_, substitute, err := loop.NewRevision(stored)
		if err != nil {
			t.Fatal(err)
		}
		deleteRawPrefix(t, store, key(familyLoopValidation, loopRef.ID, revisionPart(loopRef.Revision)))
		wire, err := loop.MarshalLoopValidationResult(substitute)
		if err != nil {
			t.Fatal(err)
		}
		setRawRecord(t, store, key(familyLoopValidation, loopRef.ID, revisionPart(loopRef.Revision), substitute.Digest), wire)
		assertSnapshotDenied(t, store, snapshot, fleet.ErrNotFound, 3)
	})

	t.Run("corrupt validation denies without mutation", func(t *testing.T) {
		store, snapshot := persistedSnapshotFixture(t, ctx, registry.LifecycleEnabled)
		defer store.Close()
		deleteRawPrefix(t, store, key(familyGraphValidation, snapshot.Graph.ID, revisionPart(snapshot.Graph.Revision)))
		setRawRecord(t, store, key(familyGraphValidation, snapshot.Graph.ID, revisionPart(snapshot.Graph.Revision), "corrupt"), []byte("not-json"))
		assertSnapshotDenied(t, store, snapshot, fleet.ErrCorrupt, 3)
	})

	for _, lifecycle := range []registry.Lifecycle{registry.LifecycleDisabled, registry.LifecycleRetired} {
		t.Run(string(lifecycle)+" participant denies without mutation", func(t *testing.T) {
			store, snapshot := persistedSnapshotFixture(t, ctx, lifecycle)
			defer store.Close()
			assertSnapshotDenied(t, store, snapshot, fleet.ErrConflict, 3)
		})
	}
}

func assertSnapshotDenied(t *testing.T, store *Store, snapshot graph.GraphRunSnapshot, want error, expectedAudits int) {
	t.Helper()
	if _, err := store.CreateGraphRunSnapshot(context.Background(), snapshot, audit("graph.snapshot.created", snapshot.SnapshotID)); !errors.Is(err, want) {
		t.Fatalf("snapshot denial: got %v, want %v", err, want)
	}
	assertSnapshotFailureAtomic(t, store, snapshot.SnapshotID, expectedAudits)
}

func deleteRawRecord(t *testing.T, store *Store, recordKey []byte) {
	t.Helper()
	if err := store.update(context.Background(), func(txn *badgerdb.Txn) error { return txn.Delete(recordKey) }); err != nil {
		t.Fatal(err)
	}
}

func deleteRawPrefix(t *testing.T, store *Store, prefix []byte) {
	t.Helper()
	if err := store.update(context.Background(), func(txn *badgerdb.Txn) error {
		iterator := txn.NewIterator(badgerdb.DefaultIteratorOptions)
		defer iterator.Close()
		var keys [][]byte
		for iterator.Seek(prefix); iterator.ValidForPrefix(prefix); iterator.Next() {
			keys = append(keys, iterator.Item().KeyCopy(nil))
		}
		for _, recordKey := range keys {
			if err := txn.Delete(recordKey); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func setRawRecord(t *testing.T, store *Store, recordKey, value []byte) {
	t.Helper()
	if err := store.update(context.Background(), func(txn *badgerdb.Txn) error { return txn.Set(recordKey, value) }); err != nil {
		t.Fatal(err)
	}
}

func replaceAgentRevision(t *testing.T, store *Store, revision registry.AgentRevision) {
	t.Helper()
	wire, err := registry.MarshalAgentRevision(revision)
	if err != nil {
		t.Fatal(err)
	}
	setRawRecord(t, store, key(familyAgentRevision, revision.AgentID, revisionPart(revision.Revision)), wire)
}

func replaceLoopRevision(t *testing.T, store *Store, revision loop.LoopRevision) {
	t.Helper()
	wire, err := loop.MarshalRevision(revision)
	if err != nil {
		t.Fatal(err)
	}
	setRawRecord(t, store, key(familyLoopRevision, revision.LoopID, revisionPart(revision.Revision)), wire)
}

func replaceGraphRevision(t *testing.T, store *Store, revision graph.GraphRevision) {
	t.Helper()
	wire, err := graph.MarshalRevision(revision)
	if err != nil {
		t.Fatal(err)
	}
	setRawRecord(t, store, key(familyGraphRevision, revision.GraphID, revisionPart(revision.Revision)), wire)
}

func persistedSnapshotFixture(t *testing.T, ctx context.Context, lifecycle registry.Lifecycle) (*Store, graph.GraphRunSnapshot) {
	t.Helper()
	store, err := Open(ctx, filepath.Join(t.TempDir(), schemaVersion))
	if err != nil {
		t.Fatal(err)
	}
	registration, agent := agentFixture(t)
	if lifecycle != agent.Lifecycle {
		agent.Lifecycle = lifecycle
		agent, err = registry.SealRevision(agent)
		if err != nil {
			t.Fatal(err)
		}
		registration.InitialRevision.Digest = agent.Digest
	}
	if _, err = store.RegisterAgent(ctx, registration, agent, audit("agent.registered", agent.AgentID)); err != nil {
		t.Fatal(err)
	}
	loopRevision, loopValidation := loopFixture(t)
	if _, err = store.PublishLoop(ctx, loop.PublishRequest{Revision: loopRevision, Validation: loopValidation, IdempotencyKey: "loop-snapshot"}, audit("loop.published", loopRevision.LoopID)); err != nil {
		t.Fatal(err)
	}
	graphRevision, graphValidation := graphFixture(t, agent, loopRevision)
	if _, err = store.PublishGraph(ctx, graph.PublishRequest{Revision: graphRevision, Validation: graphValidation, IdempotencyKey: "graph-snapshot"}, audit("graph.published", graphRevision.GraphID)); err != nil {
		t.Fatal(err)
	}
	snapshot, err := graph.NewRunSnapshot("snapshot-reference-check", graphRevision, []graph.NormalizedInput{{PortID: "value", Type: graph.TypeString, Value: json.RawMessage(`"hello"`)}})
	if err != nil {
		t.Fatal(err)
	}
	return store, snapshot
}

func assertSnapshotFailureAtomic(t *testing.T, store *Store, snapshotID string, expectedAudits int) {
	t.Helper()
	if _, err := store.GetGraphRunSnapshot(context.Background(), snapshotID); !errors.Is(err, fleet.ErrNotFound) {
		t.Fatalf("failed snapshot mutation persisted a record: %v", err)
	}
	events, err := store.AuditEvents(context.Background())
	if err != nil || len(events) != expectedAudits {
		t.Fatalf("failed snapshot mutation changed audit: count=%d err=%v", len(events), err)
	}
}

func agentFixture(t *testing.T) (registry.AgentRegistration, registry.AgentRevision) {
	t.Helper()
	digest := "sha256:" + strings.Repeat("a", 64)
	source := registry.FleetSource{FleetID: "fleet-primary", Kind: "current-fleet", SourceID: "source-alpha"}
	revision, err := registry.SealRevision(registry.AgentRevision{
		SchemaVersion:          registry.AgentRevisionSchemaVersion,
		AgentID:                "agent-alpha",
		Revision:               1,
		Source:                 source,
		Runtime:                registry.RuntimeBinding{Adapter: "hermes", Runtime: "hermes-agent", Target: "profile/alpha"},
		Ownership:              registry.Ownership{OwnerID: "operator-primary", AccountabilityID: "team-platform"},
		Lifecycle:              registry.LifecycleEnabled,
		Charter:                reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: "agent-alpha", Revision: 1, Digest: digest},
		CapabilityDeclarations: []string{},
		PolicyRefs:             []reference.DigestRef{},
	})
	if err != nil {
		t.Fatal(err)
	}
	registration := registry.AgentRegistration{
		SchemaVersion:   registry.AgentRegistrationSchemaVersion,
		AgentID:         revision.AgentID,
		Source:          source,
		InitialRevision: reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: revision.AgentID, Revision: 1, Digest: revision.Digest},
	}
	return registration, revision
}

func loopFixture(t *testing.T) (loop.LoopRevision, loop.LoopValidationResult) {
	t.Helper()
	value := loop.Port{ID: "value", Type: loop.TypeString, Required: true}
	revision, validation, err := loop.NewRevision(loop.LoopRevision{
		LoopID: "loop.echo", Revision: 1, Inputs: []loop.Port{value}, Outputs: []loop.Port{{ID: "result", Type: loop.TypeString, Required: true}}, EntryStepID: "echo",
		Steps: []loop.Step{
			{ID: "echo", Kind: loop.StepAction, InputPorts: []loop.Port{value}, OutputPorts: []loop.Port{value}, Retry: loop.RetryPolicy{MaxAttempts: 1}},
			{ID: "done", Kind: loop.StepTerminal, InputPorts: []loop.Port{value}, OutputPorts: []loop.Port{{ID: "final", Type: loop.TypeString, Required: true}}, Retry: loop.RetryPolicy{MaxAttempts: 1}, Terminal: &loop.TerminalDefinition{Outcome: loop.OutcomeSucceeded, OutputMappings: []loop.PortMapping{{SourcePort: "final", TargetPort: "result"}}}},
		},
		Transitions: []loop.Transition{{ID: "echo-done", FromStepID: "echo", ToStepID: "done", Mappings: []loop.PortMapping{{SourcePort: "value", TargetPort: "value"}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return revision, validation
}

func graphFixture(t *testing.T, agent registry.AgentRevision, loopRevision loop.LoopRevision) (graph.GraphRevision, graph.GraphValidationResult) {
	t.Helper()
	value := graph.Port{ID: "value", Type: graph.TypeString, Required: true}
	revision, validation, err := graph.NewRevision(graph.GraphRevision{
		GraphID: "graph.echo", Revision: 1, Inputs: []graph.Port{value}, Outputs: []graph.Port{{ID: "result", Type: graph.TypeString, Required: true}},
		Nodes:          []graph.Node{{ID: "echo", Participant: reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: agent.AgentID, Revision: agent.Revision, Digest: agent.Digest}, Loop: reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: loopRevision.LoopID, Revision: loopRevision.Revision, Digest: loopRevision.Digest}, Inputs: []graph.Port{value}, Outputs: []graph.Port{value}}},
		InputMappings:  []graph.InputMapping{{GraphInput: "value", ToNodeID: "echo", ToPort: "value"}},
		OutputMappings: []graph.OutputMapping{{FromNodeID: "echo", FromPort: "value", GraphOutput: "result"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return revision, validation
}

func audit(eventType, subject string) fleet.AuditFact {
	return fleet.AuditFact{Event: core.AuditEvent{Type: eventType, SubjectID: subject, PrincipalID: "operator-primary", Outcome: "succeeded", Reason: "authorized mutation"}}
}
