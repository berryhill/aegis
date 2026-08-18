package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/config"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/execution"
	"github.com/berryhill/aegis/internal/graph"
	"github.com/berryhill/aegis/internal/orchestration"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	queue "github.com/berryhill/aegis/internal/queue"
	"github.com/berryhill/aegis/internal/reference"
)

type graphReadRepository struct {
	fleet.Repository
	lifecycle   graph.Lifecycle
	revisions   []graph.GraphRevision
	validations []graph.GraphValidationResult
	lifecycles  []graph.Lifecycle
	submissions []queue.Submission
	items       []queue.Item
	runs        []execution.GraphRun
	snapshots   map[string]graph.GraphRunSnapshot
	rejections  []queue.Rejection
	err         error
}

func (r *graphReadRepository) GetGraphLifecycle(context.Context, string) (graph.Lifecycle, error) {
	if r.err != nil {
		return graph.Lifecycle{}, r.err
	}
	return r.lifecycle, nil
}
func (r *graphReadRepository) ListGraphRevisions(context.Context) ([]graph.GraphRevision, error) {
	if r.err != nil {
		return nil, r.err
	}
	return r.revisions, nil
}
func (r *graphReadRepository) ListGraphValidations(context.Context) ([]graph.GraphValidationResult, error) {
	return r.validations, nil
}
func (r *graphReadRepository) ListGraphLifecycles(context.Context) ([]graph.Lifecycle, error) {
	return r.lifecycles, nil
}
func (r *graphReadRepository) ListSubmissions(context.Context) ([]queue.Submission, error) {
	if r.err != nil {
		return nil, r.err
	}
	return r.submissions, nil
}
func (r *graphReadRepository) ListQueueItems(context.Context) ([]queue.Item, error) {
	return r.items, nil
}
func (r *graphReadRepository) ListGraphRuns(context.Context) ([]execution.GraphRun, error) {
	return r.runs, nil
}
func (r *graphReadRepository) GetGraphRunSnapshot(_ context.Context, id string) (graph.GraphRunSnapshot, error) {
	value, ok := r.snapshots[id]
	if !ok {
		return graph.GraphRunSnapshot{}, fleet.ErrNotFound
	}
	return value, nil
}
func (r *graphReadRepository) ListRejections(context.Context) ([]queue.Rejection, error) {
	return r.rejections, nil
}

func graphReadService(repo fleet.Repository, now time.Time) (*Service, core.Subject) {
	cfg := config.Defaults()
	cfg.Principal.ID = "principal-1"
	return &Service{
		Config: cfg, Now: func() time.Time { return now }, FleetRepository: repo,
		Fleet: &orchestration.FleetService{}, QueueWorker: &orchestration.QueueWorker{},
	}, core.Subject{ID: "operator", PrincipalID: "principal-1", ExpiresAt: now.Add(time.Minute)}
}

func graphHistoryFixture() (*graphReadRepository, SubmissionHistory) {
	digest := func(marker string) string { return "sha256:" + strings.Repeat(marker, 64) }
	snapshot := graph.GraphRunSnapshot{SnapshotID: "snapshot-1", Digest: digest("a"), Graph: reference.RevisionRef{ID: "graph-1", Revision: 3, Digest: digest("b")}}
	submission := queue.Submission{SubmissionID: "submission-1", Digest: digest("c"), Snapshot: reference.DigestRef{ID: snapshot.SnapshotID, Digest: snapshot.Digest}}
	item := queue.Item{ItemID: "item-1", Digest: digest("d"), GraphRunID: "run-1", Submission: reference.DigestRef{ID: submission.SubmissionID, Digest: submission.Digest}}
	run := execution.GraphRun{GraphRunID: item.GraphRunID, QueueItem: reference.DigestRef{ID: item.ItemID, Digest: item.Digest}}
	rejection := queue.Rejection{RejectionID: "rejection-1", SubmissionID: "rejected-1", ReasonCode: "invalid_input"}
	repo := &graphReadRepository{submissions: []queue.Submission{submission}, items: []queue.Item{item}, runs: []execution.GraphRun{run}, snapshots: map[string]graph.GraphRunSnapshot{snapshot.SnapshotID: snapshot}, rejections: []queue.Rejection{rejection}}
	return repo, SubmissionHistory{Accepted: []AcceptedGraphRunView{{Snapshot: snapshot, Submission: submission, QueueItem: item, GraphRun: run}}, Rejected: []queue.Rejection{rejection}}
}

func TestGraphLifecycleAndSubmissionHistoryRequireFreshPrincipal(t *testing.T) {
	now := time.Now().UTC()
	repo, _ := graphHistoryFixture()
	repo.lifecycle = graph.Lifecycle{GraphID: "graph-1", State: graph.LifecycleActive, ActiveRevision: 3, ActiveDigest: repo.snapshots["snapshot-1"].Graph.Digest}
	svc, principal := graphReadService(repo, now)
	got, err := svc.GetGraphLifecycleAs(context.Background(), principal, "graph-1")
	if err != nil || got != repo.lifecycle {
		t.Fatalf("lifecycle=%+v err=%v", got, err)
	}
	history, err := svc.ListSubmissionHistoryAs(context.Background(), principal)
	if err != nil || len(history.Accepted) != 1 || history.Accepted[0].Snapshot.Digest != repo.snapshots["snapshot-1"].Digest || len(history.Rejected) != 1 {
		t.Fatalf("history=%+v err=%v", history, err)
	}
	principal.ExpiresAt = now
	if _, err = svc.GetGraphLifecycleAs(context.Background(), principal, "graph-1"); !errors.Is(err, ErrDenied) {
		t.Fatalf("expired lifecycle read err=%v", err)
	}
	if _, err = svc.ListSubmissionHistoryAs(context.Background(), principal); !errors.Is(err, ErrDenied) {
		t.Fatalf("expired history read err=%v", err)
	}
}

func TestSubmissionHistoryFailsClosedOnMissingOrSubstitutedJoinedFacts(t *testing.T) {
	now := time.Now().UTC()
	for name, mutate := range map[string]func(*graphReadRepository){
		"missing snapshot": func(r *graphReadRepository) { r.snapshots = nil },
		"substituted snapshot digest": func(r *graphReadRepository) {
			x := r.snapshots["snapshot-1"]
			x.Digest = "sha256:" + strings.Repeat("f", 64)
			r.snapshots["snapshot-1"] = x
		},
		"missing queue item":             func(r *graphReadRepository) { r.items = nil },
		"substituted submission binding": func(r *graphReadRepository) { r.items[0].Submission.Digest = "sha256:" + strings.Repeat("f", 64) },
		"missing graph run":              func(r *graphReadRepository) { r.runs = nil },
		"substituted queue binding":      func(r *graphReadRepository) { r.runs[0].QueueItem.Digest = "sha256:" + strings.Repeat("f", 64) },
	} {
		t.Run(name, func(t *testing.T) {
			repo, _ := graphHistoryFixture()
			mutate(repo)
			svc, principal := graphReadService(repo, now)
			if _, err := svc.ListSubmissionHistoryAs(context.Background(), principal); !errors.Is(err, fleet.ErrCorrupt) {
				t.Fatalf("err=%v", err)
			}
		})
	}
}

func TestGraphReadAggregatesPropagateRepositoryFailure(t *testing.T) {
	now := time.Now().UTC()
	failure := errors.New("repository read failed")
	repo := &graphReadRepository{err: failure}
	svc, principal := graphReadService(repo, now)
	if _, err := svc.GetGraphLifecycleAs(context.Background(), principal, "graph-1"); !errors.Is(err, failure) {
		t.Fatalf("lifecycle err=%v", err)
	}
	if _, err := svc.ListSubmissionHistoryAs(context.Background(), principal); !errors.Is(err, failure) {
		t.Fatalf("history err=%v", err)
	}
}

func TestListGraphsJoinsExactValidationLifecycleAndAcceptedSnapshot(t *testing.T) {
	now := time.Now().UTC()
	repo, _ := graphHistoryFixture()
	revision := graph.GraphRevision{GraphID: "graph-1", Revision: 3, Digest: repo.snapshots["snapshot-1"].Graph.Digest}
	repo.revisions = []graph.GraphRevision{revision}
	repo.validations = []graph.GraphValidationResult{{GraphID: revision.GraphID, Revision: revision.Revision, RevisionDigest: revision.Digest, Outcome: graph.ValidationValid}}
	repo.lifecycles = []graph.Lifecycle{{GraphID: revision.GraphID, State: graph.LifecycleActive, ActiveRevision: revision.Revision, ActiveDigest: revision.Digest}}
	svc, principal := graphReadService(repo, now)
	views, err := svc.ListGraphsAs(context.Background(), principal)
	if err != nil || len(views) != 1 || len(views[0].Validations) != 1 || views[0].Lifecycle.ActiveDigest != revision.Digest || len(views[0].Runs) != 1 {
		t.Fatalf("views=%+v err=%v", views, err)
	}

	for name, mutate := range map[string]func(*graphReadRepository){
		"missing validation": func(r *graphReadRepository) { r.validations = nil },
		"substituted validation digest": func(r *graphReadRepository) {
			r.validations[0].RevisionDigest = "sha256:" + strings.Repeat("f", 64)
		},
		"missing lifecycle": func(r *graphReadRepository) { r.lifecycles = nil },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := *repo
			candidate.validations = append([]graph.GraphValidationResult(nil), repo.validations...)
			candidate.lifecycles = append([]graph.Lifecycle(nil), repo.lifecycles...)
			mutate(&candidate)
			testService, testPrincipal := graphReadService(&candidate, now)
			if _, readErr := testService.ListGraphsAs(context.Background(), testPrincipal); !errors.Is(readErr, fleet.ErrCorrupt) {
				t.Fatalf("err=%v", readErr)
			}
		})
	}
}
