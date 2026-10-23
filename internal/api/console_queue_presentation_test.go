package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/disposition"
	"github.com/berryhill/aegis/internal/execution"
	"github.com/berryhill/aegis/internal/graph"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	"github.com/berryhill/aegis/internal/queue"
	"github.com/berryhill/aegis/internal/reference"
	consoleweb "github.com/berryhill/aegis/web/console"
	"github.com/labstack/echo/v5"
)

func TestConsoleQueuePresentationActions(t *testing.T) {
	ctx := context.Background()
	svc := apiService(t)
	store := configureAPIFleet(t, svc)
	sub, err := svc.AuthenticateUnixPeer(ctx, uint32(os.Getuid()))
	if err != nil {
		t.Fatal(err)
	}
	agent := registerDoerWorkspaceAgent(t, svc, sub, "actions-agent", sub.PrincipalID).Revision
	// Reuse the real publication fixture, then admit separate owned canonical work.
	history := storeAgentExecutionHistory(t, svc, store, agent)
	snapshot := history.Snapshot
	snapshot.SnapshotID = "actions-snapshot"
	// Reconstruct, rather than altering sealed snapshot data.
	gr, err := store.GetGraphRevision(ctx, history.Snapshot.Graph.ID, history.Snapshot.Graph.Revision)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err = graph.NewRunSnapshot("actions-snapshot", gr, history.Snapshot.Inputs)
	if err != nil {
		t.Fatal(err)
	}
	ref := func(id, digest string) reference.DigestRef {
		return reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: id, Digest: digest}
	}
	now := svc.Now()
	submission, err := queue.NewSubmission(queue.Submission{SubmissionID: "actions-submission", IdempotencyKey: "actions-intent", Snapshot: ref(snapshot.SnapshotID, snapshot.Digest), Authority: history.Submission.Authority, MandateID: history.Submission.MandateID, Runtime: history.Submission.Runtime, SubmittedAt: now, OwnerID: sub.PrincipalID, OwnerAgentID: agent.AgentID})
	if err != nil {
		t.Fatal(err)
	}
	item, err := queue.NewItem(queue.Item{ItemID: "actions-item", Submission: ref(submission.SubmissionID, submission.Digest), Snapshot: submission.Snapshot, Authority: submission.Authority, GraphRunID: "actions-run", MaxAttempts: 1, EnqueuedAt: now, AvailableAt: now})
	if err != nil {
		t.Fatal(err)
	}
	run, err := execution.NewGraphRun(execution.GraphRun{GraphRunID: item.GraphRunID, QueueItem: ref(item.ItemID, item.Digest), Snapshot: item.Snapshot, Authority: item.Authority, CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	transition, err := queue.NewTransition(queue.QueueTransition{TransitionID: "actions-queued", QueueItemID: item.ItemID, To: queue.StateQueued, Reason: "synthetic fixture", OccurredAt: now})
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.AcceptSubmission(ctx, fleet.AcceptedSubmission{Snapshot: snapshot, Submission: submission, QueueItem: item, GraphRun: run, InitialTransition: transition}, fleet.AuditFact{Event: core.AuditEvent{Type: "submission.accepted", SubjectID: sub.ID, PrincipalID: sub.PrincipalID, Outcome: "succeeded", Reason: "synthetic console actions fixture accepted"}})
	if err != nil {
		t.Fatal(err)
	}
	manager, cookie, csrf := consoleQueueRouteFixture(t, svc)
	e := echo.New()
	e.HTTPErrorHandler = func(c *echo.Context, err error) { status, _, _ := classifyError(err); _ = c.NoContent(status) }
	registerConsoleQueuePresentation(e, svc, manager)
	post := func(action string, values url.Values, session bool, origin string) int {
		req := httptest.NewRequest("POST", "http://127.0.0.1/console/queue/"+item.ItemID+"/"+action, strings.NewReader(values.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Origin", origin)
		if session {
			req.AddCookie(cookie)
		}
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		if rec.Code == http.StatusSeeOther {
			want := consoleRecordURL(consoleQueue, item.ItemID)
			if action == "archive" {
				want = "/console/queue"
			}
			if got := rec.Header().Get("Location"); got != want {
				t.Errorf("%s Location = %q, want %q", action, got, want)
			}
		}
		return rec.Code
	}
	values := func(revision string) url.Values {
		return url.Values{"csrf": {csrf}, "digest": {item.Digest}, "expected_revision": {revision}}
	}
	if got := post("archive", values("0"), true, "http://127.0.0.1"); got < 400 {
		t.Fatalf("nonterminal admitted %d", got)
	}
	// This is historical synthetic work, not a live authority-bound session.
	// Seed a complete canonical cancellation instead of attempting runtime admission.
	cancellation, err := queue.NewCancellation(queue.Cancellation{CancellationID: "actions-cancel", QueueItem: ref(item.ItemID, item.Digest), Reason: "operator_cancelled", OccurredAt: now})
	if err != nil {
		t.Fatal(err)
	}
	cancelled, err := queue.NewTransition(queue.QueueTransition{TransitionID: "actions-cancelled", QueueItemID: item.ItemID, From: queue.StateQueued, To: queue.StateCancelled, Reason: "operator_cancelled", OccurredAt: now})
	if err != nil {
		t.Fatal(err)
	}
	terminal, err := disposition.New(disposition.Record{DispositionID: "actions-disposition", GraphRunID: item.GraphRunID, QueueItem: ref(item.ItemID, item.Digest), Authority: item.Authority, State: execution.StateCancelled, ReasonCode: "operator_cancelled", OccurredAt: now})
	if err != nil {
		t.Fatal(err)
	}
	err = store.CancelQueueItem(ctx, fleet.CancellationMutation{Cancellation: cancellation, Transition: cancelled, Disposition: terminal}, fleet.AuditFact{Event: core.AuditEvent{Type: "queue.cancelled", SubjectID: sub.ID, PrincipalID: sub.PrincipalID, Outcome: "succeeded", Reason: "synthetic historical cancellation"}})
	if err != nil {
		t.Fatal(err)
	}
	before, err := svc.GetQueueItemAs(ctx, sub, item.ItemID)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name    string
		mutate  func(url.Values)
		session bool
		origin  string
	}{
		{"session", func(v url.Values) {}, false, "http://127.0.0.1"},
		{"origin", func(v url.Values) {}, true, "http://foreign.invalid"},
		{"csrf", func(v url.Values) { v.Set("csrf", "forged") }, true, "http://127.0.0.1"},
		{"identity", func(v url.Values) { v.Set("subject", "forged") }, true, "http://127.0.0.1"},
		{"duplicate", func(v url.Values) { v.Add("digest", item.Digest) }, true, "http://127.0.0.1"},
		{"digest", func(v url.Values) { v.Set("digest", "sha256:"+strings.Repeat("a", 64)) }, true, "http://127.0.0.1"},
		{"cas", func(v url.Values) { v.Set("expected_revision", "7") }, true, "http://127.0.0.1"},
	} {
		t.Run(test.name, func(t *testing.T) {
			v := values("0")
			test.mutate(v)
			if got := post("archive", v, test.session, test.origin); got < 400 {
				t.Fatalf("denial admitted %d", got)
			}
		})
	}
	for i, action := range []string{"archive", "unarchive"} {
		revision := "0"
		if i == 1 {
			revision = "1"
		}
		if got := post(action, values(revision), true, "http://127.0.0.1"); got != http.StatusSeeOther {
			t.Fatalf("%s status %d", action, got)
		}
		a, err := svc.GetQueueArchiveAs(ctx, sub, ref(item.ItemID, item.Digest))
		if err != nil || a.Archived != (action == "archive") {
			t.Fatalf("readback %+v %v", a, err)
		}
	}
	if got := post("requeue", values("2"), true, "http://127.0.0.1"); got < 400 {
		t.Fatalf("unsupported requeue admitted %d", got)
	}
	after, err := svc.GetQueueItemAs(ctx, sub, item.ItemID)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("history changed", err)
	}
	model := consoleweb.SurfaceModel{Domain: "queue", CSRF: csrf, Records: []consoleweb.RecordModel{consoleQueueRecord(after)}}
	applyConsoleQueuePresentation(ctx, svc, sub, &model)
	record := model.Records[0]
	for name, component := range map[string]func() ([]byte, error){"row": func() ([]byte, error) { return renderConsole(ctx, consoleweb.QueueRecordPanel(model, model.Records)) }, "detail": func() ([]byte, error) {
		return renderConsole(ctx, consoleweb.QueueWorkspace(model, &record, consoleweb.QueuePinnedTopology{}))
	}} {
		body, err := component()
		if err != nil {
			t.Fatal(err)
		}
		text := string(body)
		if !strings.Contains(text, "/actions-item/archive") || !strings.Contains(text, `name="expected_revision" value="2"`) || strings.Contains(text, "/actions-item/requeue") {
			t.Fatalf("%s markup %s", name, text)
		}
		if name == "row" && strings.Index(text, "</a>") > strings.Index(text, "<form") {
			t.Fatal("form nested in navigation anchor")
		}
	}
}
