package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/berryhill/aegis/internal/app"

	"github.com/berryhill/aegis/internal/graph"
	"github.com/berryhill/aegis/internal/queue"
	"github.com/berryhill/aegis/internal/reference"
	consoleweb "github.com/berryhill/aegis/web/console"
	"github.com/labstack/echo/v5"
)

func TestDirectLoopJourneyQueueNormalizedInputReadback(t *testing.T) {
	inputs := []graph.NormalizedInput{
		{PortID: "flag", Type: graph.TypeBoolean, Value: json.RawMessage(`false`)},
		{PortID: "zero", Type: graph.TypeInteger, Value: json.RawMessage(`0`)},
		{PortID: "object", Type: graph.TypeObject, Value: json.RawMessage(`{"a":1}`)},
		{PortID: "array", Type: graph.TypeArray, Value: json.RawMessage(`[1,"two"]`)},
		{PortID: "text", Type: graph.TypeString, Value: json.RawMessage(`"<script>\"hi\"</script>"`)},
	}
	ref := reference.RevisionRef{ID: "loop", Revision: 1, Digest: "sha256:exact"}
	for _, source := range []graph.SourceKind{graph.SourceDirectLoop, graph.SourcePublishedGraph} {
		t.Run(string(source), func(t *testing.T) {
			view := app.QueueExecutionView{SourceKind: source, Source: ref, Inputs: inputs, Item: queue.Item{ItemID: "item-1"}}
			var graphs []app.GraphView
			if source == graph.SourcePublishedGraph {
				view.Item.Snapshot = reference.DigestRef{ID: "snapshot", Digest: "sha256:snapshot"}
				revision := graph.GraphRevision{GraphID: ref.ID, Revision: ref.Revision, Digest: ref.Digest, Inputs: []graph.Port{{ID: "flag", Type: graph.TypeBoolean}, {ID: "zero", Type: graph.TypeInteger}, {ID: "object", Type: graph.TypeObject}, {ID: "array", Type: graph.TypeArray}, {ID: "text", Type: graph.TypeString}, {ID: "absent", Type: graph.TypeString}}}
				graphs = []app.GraphView{{Revision: revision, Runs: []app.AcceptedGraphRunView{{Snapshot: graph.GraphRunSnapshot{SnapshotID: "snapshot", Digest: "sha256:snapshot", Graph: ref}, QueueItem: view.Item}}}}
			}
			record := consoleQueueRecordWithLoops(view, nil, graphs)
			if record.Queue == nil || len(record.Queue.Inputs) != len(inputs)+map[graph.SourceKind]int{graph.SourceDirectLoop: 0, graph.SourcePublishedGraph: 1}[source] {
				t.Fatalf("normalized inputs missing from %s detail: %+v", source, record.Queue)
			}
			for i, input := range inputs {
				got := record.Queue.Inputs[i]
				if got.PortID != input.PortID || got.Type != string(input.Type) || got.Value != string(input.Value) || got.Source != "normalized run snapshot" || got.Status != "applied" {
					t.Fatalf("%s input %d: %+v", source, i, got)
				}
			}
			if source == graph.SourcePublishedGraph {
				absent := record.Queue.Inputs[len(inputs)]
				if absent.PortID != "absent" || absent.Value != "" || absent.Source == "default" || absent.Status != "missing" {
					t.Fatalf("invented default value: %+v", absent)
				}
			}
			var rendered strings.Builder
			if err := consoleweb.QueueWorkspace(consoleweb.SurfaceModel{Domain: consoleweb.DomainQueue}, &record, consoleweb.QueuePinnedTopology{}).Render(context.Background(), &rendered); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(rendered.String(), "No normalized inputs recorded") || strings.Contains(rendered.String(), "<script>") || !strings.Contains(rendered.String(), "&lt;script&gt;") {
				t.Fatal("queue renderer omitted normalized values or failed HTML escaping")
			}
		})
	}

}

func TestDirectLoopJourneyNewRunCanonicalizesOnce(t *testing.T) {
	svc := apiService(t)
	manager, authCookie, _ := consoleQueueRouteFixture(t, svc)
	record := &consoleweb.RecordModel{Key: "loop:1", Label: "loop", Revision: "r1", Digest: "sha256:exact", Loop: &consoleweb.LoopDetailModel{DoerV4: true}}
	e := echo.New()
	e.GET("/console/loops", func(c *echo.Context) error {
		subject, err := manager.Authenticate(c.Request())
		if err != nil {
			return c.NoContent(http.StatusUnauthorized)
		}
		key, redirected, err := consoleLoopRunPageIdentity(c, subject, record)
		if err != nil {
			return err
		}
		if redirected {
			return nil
		}
		return c.String(http.StatusOK, key)
	})
	request := func(target string, cookie *http.Cookie) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1"+target, nil)
		req.AddCookie(authCookie)
		if cookie != nil {
			req.AddCookie(cookie)
		}
		res := httptest.NewRecorder()
		e.ServeHTTP(res, req)
		return res
	}
	clean := "/console/loops?record_key=" + url.QueryEscape(record.Key)
	unauthenticated := httptest.NewRecorder()
	e.ServeHTTP(unauthenticated, httptest.NewRequest(http.MethodGet, "http://127.0.0.1"+clean+"&new_run=1", nil))
	if unauthenticated.Code != http.StatusUnauthorized || len(unauthenticated.Result().Cookies()) != 0 {
		t.Fatal("unauthenticated rotation admitted")
	}
	first := request(clean, nil)
	original := first.Body.String()
	if first.Code != http.StatusOK || original == "" || len(first.Result().Cookies()) != 1 {
		t.Fatalf("initial run identity missing: status=%d cookies=%d", first.Code, len(first.Result().Cookies()))
	}
	rotated := request(clean+"&new_run=1", first.Result().Cookies()[0])
	if rotated.Code != http.StatusSeeOther || rotated.Header().Get("Location") != clean || len(rotated.Result().Cookies()) != 1 {
		t.Fatalf("rotation failed to canonicalize: code=%d location=%q", rotated.Code, rotated.Header().Get("Location"))
	}
	second := request(rotated.Header().Get("Location"), rotated.Result().Cookies()[0])
	third := request(clean, rotated.Result().Cookies()[0])
	if original == "" || second.Body.String() == original || second.Body.String() != third.Body.String() || len(second.Result().Cookies()) != 0 || len(third.Result().Cookies()) != 0 {
		t.Fatalf("run key changed on canonical reload or never rotated: %q %q %q", original, second.Body.String(), third.Body.String())
	}
	// A different exact revision/digest cannot inherit this cookie's lineage.
	record.Revision, record.Digest = "r2", "sha256:other"
	other := request(clean, rotated.Result().Cookies()[0])
	if other.Body.String() == second.Body.String() || len(other.Result().Cookies()) != 1 {
		t.Fatal("run identity was not bound to exact revision/digest")
	}
}
