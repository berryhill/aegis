package api

import (
	"context"
	"errors"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/execution"
	"github.com/berryhill/aegis/internal/graph"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	"github.com/berryhill/aegis/internal/queue"
	"github.com/berryhill/aegis/internal/reference"
	consoleweb "github.com/berryhill/aegis/web/console"
)

type queuePresentationCountRepository struct {
	fleet.Repository
	fleet.QueuePresentationRepository
	lists         atomic.Int64
	corrupt       bool
	archiveDenied bool
	revision      uint64
}

func (r *queuePresentationCountRepository) ListQueueItems(ctx context.Context) ([]queue.Item, error) {
	r.lists.Add(1)
	if r.corrupt {
		return nil, fleet.ErrCorrupt
	}
	return r.Repository.ListQueueItems(ctx)
}
func (r *queuePresentationCountRepository) GetQueueArchive(ctx context.Context, principal string, ref reference.DigestRef) (queue.Archive, error) {
	if r.archiveDenied {
		return queue.Archive{}, fleet.ErrPresentationDenied
	}
	v, err := r.QueuePresentationRepository.GetQueueArchive(ctx, principal, ref)
	if err == nil || errors.Is(err, fleet.ErrNotFound) {
		v.Revision = r.revision
		return v, nil
	}
	return v, err
}

func TestConsoleQueuePresentationBatchCanonicalReadAndDenials(t *testing.T) {
	svc := apiService(t)
	store := configureAPIFleet(t, svc)
	ctx := context.Background()
	sub, err := svc.AuthenticateUnixPeer(ctx, uint32(os.Getuid()))
	if err != nil {
		t.Fatal(err)
	}
	agent := registerDoerWorkspaceAgent(t, svc, sub, "batch-agent", sub.PrincipalID).Revision
	storeAgentExecutionHistory(t, svc, store, agent)
	items, err := svc.FleetRepository.ListQueueItems(ctx)
	if err != nil || len(items) == 0 {
		t.Fatalf("seed: %v", err)
	}
	seed := items[0]
	old, err := svc.FleetRepository.GetGraphRunSnapshot(ctx, seed.Snapshot.ID)
	if err != nil {
		t.Fatal(err)
	}
	gr, err := svc.FleetRepository.GetGraphRevision(ctx, old.Graph.ID, old.Graph.Revision)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := graph.NewRunSnapshot("batch-snapshot", gr, old.Inputs)
	if err != nil {
		t.Fatal(err)
	}
	ref := func(id, digest string) reference.DigestRef {
		return reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: id, Digest: digest}
	}
	now := svc.Now()
	submission, err := queue.NewSubmission(queue.Submission{SubmissionID: "batch-submission", IdempotencyKey: "batch-intent", Snapshot: ref(snapshot.SnapshotID, snapshot.Digest), Authority: seed.Authority, MandateID: "batch-mandate", Runtime: "hermes", SubmittedAt: now, OwnerID: sub.PrincipalID, OwnerAgentID: agent.AgentID})
	if err != nil {
		t.Fatal(err)
	}
	item, err := queue.NewItem(queue.Item{ItemID: "batch-owned", Submission: ref(submission.SubmissionID, submission.Digest), Snapshot: submission.Snapshot, Authority: submission.Authority, GraphRunID: "batch-run", MaxAttempts: 1, EnqueuedAt: now, AvailableAt: now})
	if err != nil {
		t.Fatal(err)
	}
	run, err := execution.NewGraphRun(execution.GraphRun{GraphRunID: item.GraphRunID, QueueItem: ref(item.ItemID, item.Digest), Snapshot: item.Snapshot, Authority: item.Authority, CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	transition, err := queue.NewTransition(queue.QueueTransition{TransitionID: "batch-queued", QueueItemID: item.ItemID, To: queue.StateQueued, Reason: "batch fixture", OccurredAt: now})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.FleetRepository.AcceptSubmission(ctx, fleet.AcceptedSubmission{Snapshot: snapshot, Submission: submission, QueueItem: item, GraphRun: run, InitialTransition: transition}, fleet.AuditFact{Event: core.AuditEvent{Type: "submission.accepted", SubjectID: sub.ID, PrincipalID: sub.PrincipalID, Outcome: "succeeded", Reason: "batch fixture"}})
	if err != nil {
		t.Fatal(err)
	}
	repo := &queuePresentationCountRepository{Repository: svc.FleetRepository, QueuePresentationRepository: svc.FleetRepository.(fleet.QueuePresentationRepository)}
	svc.FleetRepository = repo
	// Distinct queued controls in every collection, plus duplicate inspector.
	model := consoleweb.SurfaceModel{
		Records:       []consoleweb.RecordModel{{Key: "batch-owned"}},
		ActiveRecords: []consoleweb.RecordModel{{Key: seed.ItemID}},
		FailedRecords: []consoleweb.RecordModel{{Key: "scope-claimable-1"}},
		Inspector:     &consoleweb.RecordModel{Key: "batch-owned"},
	}
	if _, err := svc.ConsoleQueuePresentationAs(ctx, sub, "batch-owned", ""); err != nil {
		t.Fatalf("owned exact presentation: %v", err)
	}
	repo.lists.Store(0)
	applyConsoleQueuePresentation(ctx, svc, sub, &model)
	if got := repo.lists.Load(); got != 1 {
		t.Fatalf("canonical reconstructions = %d, want 1 for all controls", got)
	}
	for _, record := range []consoleweb.RecordModel{model.ActiveRecords[0], model.FailedRecords[0]} {
		if record.QueueActions == nil || record.QueueActions.Digest != "" {
			t.Fatal("foreign-owned controls disclosed")
		}
	}
	for _, record := range []consoleweb.RecordModel{model.Records[0], *model.Inspector} {
		if record.QueueActions == nil || record.QueueActions.Digest == "" {
			t.Fatalf("missing canonical controls: %+v", record)
		}
	}
	id := "batch-owned"
	exact, err := svc.ConsoleQueuePresentationAs(ctx, sub, id, "")
	if err != nil {
		t.Fatal(err)
	}
	selectors := map[string]string{id: exact.Digest, "missing": ""}
	batch, err := svc.ConsoleQueuePresentationsAs(ctx, sub, selectors)
	if err != nil || len(batch) != 1 || batch[id] != exact {
		t.Fatalf("exact parity: %+v %v", batch, err)
	}
	selectors[id] = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	batch, err = svc.ConsoleQueuePresentationsAs(ctx, sub, selectors)
	if err != nil || len(batch) != 0 {
		t.Fatalf("digest denial: %+v %v", batch, err)
	}
	selectors[id] = exact.Digest
	for _, bad := range []core.Subject{{}, {ID: sub.ID, PrincipalID: "foreign", AuthenticatedAt: sub.AuthenticatedAt, ExpiresAt: sub.ExpiresAt}, {ID: sub.ID, PrincipalID: sub.PrincipalID, AuthenticatedAt: sub.AuthenticatedAt, ExpiresAt: svc.Now().Add(-time.Second)}} {
		if got, err := svc.ConsoleQueuePresentationsAs(ctx, bad, selectors); err == nil || len(got) != 0 {
			t.Fatalf("identity denial: %+v %v", got, err)
		}
	}
	// Fresh archive reads on each invocation, never retained in a shared cache.
	repo.revision = 7
	batch, err = svc.ConsoleQueuePresentationsAs(ctx, sub, selectors)
	if err != nil || batch[id].Revision != 7 {
		t.Fatalf("fresh archive: %+v %v", batch, err)
	}
	repo.archiveDenied = true
	batch, err = svc.ConsoleQueuePresentationsAs(ctx, sub, selectors)
	if err != nil || len(batch) != 0 {
		t.Fatalf("archive ownership denial: %+v %v", batch, err)
	}
	repo.archiveDenied = false
	repo.corrupt = true
	batch, err = svc.ConsoleQueuePresentationsAs(ctx, sub, selectors)
	if !errors.Is(err, fleet.ErrCorrupt) || len(batch) != 0 {
		t.Fatalf("corruption fail closed: %+v %v", batch, err)
	}
	applyConsoleQueuePresentation(ctx, svc, sub, &model)
	if model.Records[0].QueueActions.Digest != "" {
		t.Fatal("corruption retained old controls")
	}
	if _, err := svc.ConsoleQueuePresentationAs(ctx, sub, id, exact.Digest); !errors.Is(err, fleet.ErrCorrupt) {
		t.Fatalf("exact read corruption: %v", err)
	}
}
