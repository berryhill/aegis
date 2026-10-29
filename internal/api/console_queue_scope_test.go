package api

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/disposition"
	"github.com/berryhill/aegis/internal/execution"
	"github.com/berryhill/aegis/internal/graph"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	"github.com/berryhill/aegis/internal/queue"
	"github.com/berryhill/aegis/internal/reference"
	consoleweb "github.com/berryhill/aegis/web/console"
	"golang.org/x/net/html"
)

var queueScopeStates = []string{"all", "queued", "claimable", "active", "retrying", "cancelled", "expired", "denied", "failed", "exhausted", "succeeded"}

// Synthetic read-boundary lifecycle fixtures atop canonical persisted admissions.
// The real principal login, FleetSurface join, routed render and exact detail
// selection are exercised. This is not runtime/provider execution evidence.
type queueScopeRepository struct {
	fleet.Repository
	fleet.LoopAvailabilityRepository
	fleet.QueuePresentationRepository
	projections  map[string]queue.Projection
	dispositions map[string]disposition.Record
	retries      map[string][]queue.Retry
}

func (r *queueScopeRepository) GetQueueProjection(ctx context.Context, id string) (queue.Projection, error) {
	if p, ok := r.projections[id]; ok {
		return p, nil
	}
	return r.Repository.GetQueueProjection(ctx, id)
}
func (r *queueScopeRepository) GetDispositionByGraphRun(ctx context.Context, id string) (disposition.Record, error) {
	if d, ok := r.dispositions[id]; ok {
		return d, nil
	}
	return r.Repository.GetDispositionByGraphRun(ctx, id)
}
func (r *queueScopeRepository) ListQueueRetries(ctx context.Context, id string) ([]queue.Retry, error) {
	if retries, ok := r.retries[id]; ok {
		return retries, nil
	}
	return r.Repository.ListQueueRetries(ctx, id)
}

func queueScopeFixture(t *testing.T) (*app.Service, string) {
	t.Helper()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	state, err := os.MkdirTemp(root, ".issue327-state-")
	if err != nil {
		t.Fatal(err)
	}
	// Retain isolated evidence/state for parent inspection; never touch ~/.aegis.
	svc := apiServiceAt(t, state)
	store := configureAPIFleet(t, svc)
	ctx := context.Background()
	subject, err := svc.AuthenticateUnixPeer(ctx, uint32(os.Getuid()))
	if err != nil {
		t.Fatal(err)
	}
	agent := registerDoerWorkspaceAgent(t, svc, subject, "scope-agent", "scope-owner")
	seed := storeAgentExecutionHistory(t, svc, store, agent.Revision)
	gr, err := store.GetGraphRevision(ctx, seed.Snapshot.Graph.ID, seed.Snapshot.Graph.Revision)
	if err != nil {
		t.Fatal(err)
	}
	repo := &queueScopeRepository{Repository: store, LoopAvailabilityRepository: store, QueuePresentationRepository: store, projections: map[string]queue.Projection{}, dispositions: map[string]disposition.Record{}, retries: map[string][]queue.Retry{}}
	ref := func(id, digest string) reference.DigestRef {
		return reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: id, Digest: digest}
	}
	// Seed's queued record is also claimable. Two records per other lifecycle
	// force independent bands to contain records outside a one-row bottom page.
	for _, state := range queueScopeStates[1:] {
		for index := 1; index <= 2; index++ {
			id := fmt.Sprintf("scope-%s-%d", state, index)
			snapshot, err := graph.NewRunSnapshot(id+"-snapshot", gr, seed.Snapshot.Inputs)
			if err != nil {
				t.Fatal(err)
			}
			now := svc.Now()
			available := now.Add(time.Hour)
			if state == "claimable" {
				available = now
			}
			sub, err := queue.NewSubmission(queue.Submission{SubmissionID: id + "-submission", IdempotencyKey: id, Snapshot: ref(snapshot.SnapshotID, snapshot.Digest), Authority: seed.Submission.Authority, MandateID: seed.Submission.MandateID, Runtime: seed.Submission.Runtime, SubmittedAt: now})
			if err != nil {
				t.Fatal(err)
			}
			item, err := queue.NewItem(queue.Item{ItemID: id, Submission: ref(sub.SubmissionID, sub.Digest), Snapshot: sub.Snapshot, Authority: sub.Authority, GraphRunID: id + "-run", MaxAttempts: 3, EnqueuedAt: now, AvailableAt: available})
			if err != nil {
				t.Fatal(err)
			}
			run, err := execution.NewGraphRun(execution.GraphRun{GraphRunID: item.GraphRunID, QueueItem: ref(id, item.Digest), Snapshot: item.Snapshot, Authority: item.Authority, CreatedAt: now})
			if err != nil {
				t.Fatal(err)
			}
			tr, err := queue.NewTransition(queue.QueueTransition{TransitionID: id + "-queued", QueueItemID: id, To: queue.StateQueued, Reason: "synthetic scope fixture", OccurredAt: now})
			if err != nil {
				t.Fatal(err)
			}
			_, err = store.AcceptSubmission(ctx, fleet.AcceptedSubmission{Snapshot: snapshot, Submission: sub, QueueItem: item, GraphRun: run, InitialTransition: tr}, fleet.AuditFact{Event: core.AuditEvent{Type: "submission.accepted", SubjectID: id, PrincipalID: svc.Config.Principal.ID, Outcome: "succeeded", Reason: "synthetic scope fixture"}})
			if err != nil {
				t.Fatal(err)
			}
			p, err := store.GetQueueProjection(ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			switch state {
			case "active":
				p.State = queue.StateClaimed
			case "retrying":
				repo.retries[id] = []queue.Retry{{RetryID: id + "-retry"}}
			case "queued", "claimable":
			default:
				p.State = queue.State(state)
				reason := "synthetic_" + state
				outcome := execution.State(state)
				if state == "exhausted" {
					p.State = queue.StateFailed
					outcome = execution.StateFailed
					reason = "retry_exhausted"
				}
				repo.dispositions[run.GraphRunID] = disposition.Record{DispositionID: id + "-disposition", GraphRunID: run.GraphRunID, QueueItem: run.QueueItem, State: outcome, ReasonCode: reason, OccurredAt: now}
			}
			repo.projections[id] = p
		}
	}
	svc.FleetRepository = repo
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := probe.Addr().String()
	_ = probe.Close()
	svc.Config.API.Listen = address
	svc.Config.API.Console.Origin = "http://" + address
	serverCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- Serve(serverCtx, svc) }()
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	})
	waitFor(t, "tcp", address)
	return svc, address
}

type queueScopeBand struct {
	IDs   []string
	Count string
	Links []string
}

func queueScopeBands(t *testing.T, body string) map[string]queueScopeBand {
	t.Helper()
	doc, err := html.Parse(strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	result := map[string]queueScopeBand{}
	attr := func(n *html.Node, key string) string {
		for _, a := range n.Attr {
			if a.Key == key {
				return a.Val
			}
		}
		return ""
	}
	var text func(*html.Node) string
	text = func(n *html.Node) string {
		if n.Type == html.TextNode {
			return n.Data
		}
		var s string
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			s += text(c)
		}
		return s
	}
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "section" {
			key := attr(n, "aria-labelledby")
			if key == "active-executions" || key == "failed-executions" || key == "queue-lifecycle" {
				band := queueScopeBand{}
				var scan func(*html.Node)
				scan = func(v *html.Node) {
					if v.Type == html.ElementNode {
						if v.Data == "a" && strings.HasPrefix(attr(v, "id"), "record-") {
							band.IDs = append(band.IDs, strings.TrimPrefix(attr(v, "id"), "record-"))
							band.Links = append(band.Links, attr(v, "href"))
						}
						if attr(v, "class") == "count" {
							band.Count = text(v)
						}
					}
					for c := v.FirstChild; c != nil; c = c.NextSibling {
						scan(c)
					}
				}
				scan(n)
				result[key] = band
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return result
}
func queueScopeGet(t *testing.T, client *http.Client, base, path string, status int) string {
	t.Helper()
	// Exercise the production limiter rather than disabling it for navigation.
	time.Sleep(250 * time.Millisecond)
	response, err := client.Get(base + path)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != status {
		t.Fatalf("GET %s status=%d want=%d body=%s", path, response.StatusCode, status, body)
	}
	return string(body)
}
func assertQueueScopeBands(t *testing.T, bands map[string]queueScopeBand) {
	t.Helper()
	for key, state := range map[string]string{"active-executions": "active", "failed-executions": "failed"} {
		want := []string{"scope-" + state + "-1", "scope-" + state + "-2"}
		band := bands[key]
		if !reflect.DeepEqual(band.IDs, want) || band.Count != "2 executions" {
			t.Fatalf("%s=%+v want IDs=%v count=2 executions", key, band, want)
		}
	}
}
func TestConsoleQueueScopeAuthenticatedRoutes(t *testing.T) {
	t.Parallel()
	svc, address := queueScopeFixture(t)
	client, _ := loginConsole(t, address)
	base := "http://" + address
	before := agentReadOnlyState(t, svc)
	// Authentication is exercised, not a PageModel.Authenticated flag.
	locked := queueScopeGet(t, http.DefaultClient, base, "/console/queue?state=failed", http.StatusOK)
	if !strings.Contains(locked, "Authentication required") || strings.Contains(locked, "scope-active-1") {
		t.Fatal("unauthenticated Queue disclosure")
	}
	for _, state := range queueScopeStates {
		count := 2
		if state == "all" {
			count = 21
		} else if state == "claimable" {
			count = 3
		}
		for _, page := range []string{"1", "2"} {
			path := "/console/queue?state=" + state + "&limit=1&page=" + page
			t.Run(state+"/page"+page, func(t *testing.T) {
				body := queueScopeGet(t, client, base, path, http.StatusOK)
				bands := queueScopeBands(t, body)
				assertQueueScopeBands(t, bands)
				bottom := bands["queue-lifecycle"]
				if bottom.Count != fmt.Sprintf("%d executions", count) || !strings.Contains(body, fmt.Sprintf("Page %s of %d", page, count)) {
					t.Fatalf("%s bottom count/pagination=%+v want total=%d", path, bottom, count)
				}
				if len(bottom.IDs) != 1 {
					t.Fatalf("bottom=%+v", bottom)
				}
				if state != "all" && !strings.HasPrefix(bottom.IDs[0], "scope-"+state+"-") && !(state == "claimable" && bottom.IDs[0] == "agent-history-item") {
					t.Fatalf("wrong bottom lifecycle: %+v", bottom)
				}
				// Follow actual rendered band links with incompatible state and page.
				for _, key := range []string{"active-executions", "failed-executions"} {
					for i, link := range bands[key].Links {
						parsed, err := url.Parse(link)
						if err != nil {
							t.Fatal(err)
						}
						if parsed.Query().Get("state") != "" || parsed.Query().Get("page") != "" {
							t.Fatalf("band link retains bottom context: %s", link)
						}
						// All selections must produce the same unfiltered link
						// context. Follow each distinct detail once from the
						// incompatible paginated case, not 22 times per link:
						// redundant full renders exhaust the race-shard budget.
						if state == "succeeded" && page == "2" {
							detail := queueScopeGet(t, client, base, link, http.StatusOK)
							// The inspector heading is the pinned Loop's display name,
							// not the queue ID. Assert exact record selection using the
							// separate authoritative queue identity in its detail header.
							if !strings.Contains(detail, "data-queue-workspace") || !strings.Contains(detail, bands[key].IDs[i]+" · pinned snapshot") {
								t.Fatalf("band detail did not resolve exact identity %s", link)
							}
						}
					}
				}
				fragment := queueScopeGet(t, client, base, "/console/fragments/surface?domain=queue&state="+state+"&limit=1&page="+page, http.StatusOK)
				fragmentBands := queueScopeBands(t, fragment)
				assertQueueScopeBands(t, fragmentBands)
				// Fragment links also carry domain=queue; compare the visible
				// lifecycle membership/count rather than unrelated URL context.
				if !reflect.DeepEqual(bottom.IDs, fragmentBands["queue-lifecycle"].IDs) || bottom.Count != fragmentBands["queue-lifecycle"].Count || !strings.Contains(fragment, fmt.Sprintf("Page %s of %d", page, count)) {
					t.Fatalf("fragment differs from full lifecycle dataset/count/pagination: %+v", fragmentBands)
				}
				// URL reload produces exactly the same visible identities/counts.
				again := queueScopeBands(t, queueScopeGet(t, client, base, path, http.StatusOK))
				if !reflect.DeepEqual(bands, again) {
					t.Fatal("URL reload changed collections")
				}
			})
		}
	}
	for _, suffix := range []string{"state=unknown", "state=FAILED", "state=failed&page=0", "state=failed&page=-1", "state=failed&page=no", "state=failed&page=10001", "state=failed&limit=1&page=3", "state=failed&limit=0", "state=failed&limit=101", "state=failed&record_key=scope-active-1", "record_key=missing"} {
		for _, prefix := range []string{"/console/queue?", "/console/fragments/surface?domain=queue&"} {
			queueScopeGet(t, client, base, prefix+suffix, http.StatusBadRequest)
		}
	}
	if after := agentReadOnlyState(t, svc); after != before {
		t.Fatal("read-only lifecycle navigation mutated canonical records")
	}
	// A legitimate empty bottom result must retain both independent bands.
	repo := svc.FleetRepository.(*queueScopeRepository)
	for id, p := range repo.projections {
		if p.State == queue.StateSucceeded {
			p.State = queue.StateQueued
			p.AvailableAt = svc.Now().Add(time.Hour)
			repo.projections[id] = p
		}
	}
	body := queueScopeGet(t, client, base, "/console/queue?state=succeeded&limit=1", http.StatusOK)
	bands := queueScopeBands(t, body)
	assertQueueScopeBands(t, bands)
	if len(bands["queue-lifecycle"].IDs) != 0 || bands["queue-lifecycle"].Count != "0 executions" {
		t.Fatal("expected empty lifecycle result")
	}
	fragmentBands := queueScopeBands(t, queueScopeGet(t, client, base, "/console/fragments/surface?domain=queue&state=succeeded&limit=1", http.StatusOK))
	assertQueueScopeBands(t, fragmentBands)
	if !reflect.DeepEqual(bands, fragmentBands) || agentReadOnlyState(t, svc) != before {
		t.Fatal("empty fragment differs or navigation changed canonical records")
	}
	// Saved preferences apply only to the collection, never exact historical
	// readback. Keep an intentionally empty Archived/Succeeded bottom view.
	ctx := context.Background()
	subject, err := svc.AuthenticateUnixPeer(ctx, uint32(os.Getuid()))
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = svc.SetQueuePreferencesAs(ctx, subject, app.SetQueuePreferencesInput{
		RequestID: "scope-saved-view", Selection: "succeeded", Visibility: queue.VisibilityArchived, ExpectedRevision: 0,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, prefix := range []string{"/console/queue?", "/console/fragments/surface?domain=queue&", "/console/fragments/inspect?domain=queue&"} {
		detail := queueScopeGet(t, client, base, prefix+"record_key=scope-failed-1&limit=1", http.StatusOK)
		if !strings.Contains(detail, "data-queue-workspace") || !strings.Contains(detail, "scope-failed-1") {
			t.Fatalf("saved view hid exact history: %s", prefix)
		}
	}
	prefs, err := svc.GetQueuePreferencesAs(ctx, subject)
	if err != nil || prefs.Revision != 1 || prefs.Selection != "succeeded" || prefs.Visibility != queue.VisibilityArchived {
		t.Fatalf("detail navigation changed saved view: %+v %v", prefs, err)
	}
}

func TestConsoleQueueScopeBrowser(t *testing.T) {
	if os.Getenv("AEGIS_QUEUE_SCOPE_BROWSER_TEST") != "1" {
		t.Skip("set AEGIS_QUEUE_SCOPE_BROWSER_TEST=1 with Python 3 and /usr/bin/google-chrome (native stdlib CDP driver)")
	}
	svc, address := queueScopeFixture(t)
	before := agentReadOnlyState(t, svc)
	python := os.Getenv("AEGIS_GRAPH_BROWSER_PYTHON")
	if python == "" {
		python = "python3"
	}
	for _, empty := range []bool{false, true} {
		if empty {
			// Child Chrome has exited; no concurrent repository reads remain.
			repo := svc.FleetRepository.(*queueScopeRepository)
			for id, p := range repo.projections {
				if p.State == queue.StateSucceeded {
					p.State = queue.StateQueued
					p.AvailableAt = svc.Now().Add(time.Hour)
					repo.projections[id] = p
				}
			}
		}
		cmd := exec.Command(python, "../../scripts/queue_collection_browser_test.py")
		cmd.Env = append(os.Environ(), "AEGIS_CONSOLE_BASE=http://"+address, "AEGIS_CONSOLE_PASSWORD="+principalTestSecret())
		if empty {
			cmd.Env = append(cmd.Env, "AEGIS_QUEUE_SCOPE_EMPTY=1")
		}
		output, err := cmd.CombinedOutput()
		t.Log(string(output))
		if err != nil {
			t.Fatal(err)
		}
	}
	if after := agentReadOnlyState(t, svc); after != before {
		t.Fatal("browser navigation mutated canonical records")
	}
}

// Independent bands originate in the full authorized model, never the bottom
// filtered/page slice. All state semantics continue to be tested separately.
func TestConsoleQueueScopeModel(t *testing.T) {
	surface := app.FleetSurface{Readiness: map[string]app.SurfaceReadiness{"queue": {Authoritative: true, State: "ready", Count: 4}}, Queue: []app.QueueExecutionView{
		{Item: queue.Item{ItemID: "active"}, Projection: queue.Projection{State: queue.StateClaimed}},
		{Item: queue.Item{ItemID: "failed"}, Projection: queue.Projection{State: queue.StateFailed}},
		{Item: queue.Item{ItemID: "succeeded"}, Projection: queue.Projection{State: queue.StateSucceeded}},
		{Item: queue.Item{ItemID: "queued"}, Projection: queue.Projection{State: queue.StateQueued, AvailableAt: time.Now().Add(time.Hour)}},
	}}
	for _, state := range queueScopeStates {
		model, err := consoleSurfaceModel(surface, consoleQueue)
		if err != nil {
			t.Fatal(err)
		}
		active, failed := append([]consoleweb.RecordModel(nil), model.ActiveRecords...), append([]consoleweb.RecordModel(nil), model.FailedRecords...)
		if err := filterConsoleQueue(&model, state); err != nil {
			t.Fatal(err)
		}
		count := len(model.Records)
		if model.TotalCount != count {
			t.Fatalf("%s count=%d want=%d before pagination", state, model.TotalCount, count)
		}
		if err := paginateConsoleCollection(&model, "", "", 1, url.Values{"state": {state}}); err != nil {
			t.Fatal(err)
		}
		if model.TotalCount != count {
			t.Fatalf("%s count=%d want=%d after pagination", state, model.TotalCount, count)
		}
		if !reflect.DeepEqual(model.ActiveRecords, active) || !reflect.DeepEqual(model.FailedRecords, failed) {
			t.Fatalf("%s changed bands", state)
		}
	}
}
