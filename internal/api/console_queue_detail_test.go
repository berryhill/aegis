package api

import (
	"context"
	"net/url"
	"os"
	"testing"

	"github.com/berryhill/aegis/internal/queue"

	consoleweb "github.com/berryhill/aegis/web/console"
)

func TestConsoleQueueInitialUnarchivedSucceeded(t *testing.T) {
	svc, _ := queueScopeFixture(t)
	ctx := context.Background()
	sub, err := svc.AuthenticateUnixPeer(ctx, uint32(os.Getuid()))
	if err != nil {
		t.Fatal(err)
	}
	prefs, err := svc.GetQueuePreferencesAs(ctx, sub)
	if err != nil || prefs.Revision != 0 || prefs.Selection != queue.SelectionSucceeded || prefs.Visibility != queue.VisibilityUnarchived {
		t.Fatalf("initial Queue preferences: %+v %v", prefs, err)
	}
	model := consoleweb.SurfaceModel{Domain: "queue"}
	if err := filterConsoleQueuePresentation(ctx, svc, sub, &model, url.Values{}); err != nil {
		t.Fatal(err)
	}
	if model.QueueState != "succeeded" || model.QueueVisibility != "unarchived" {
		t.Fatalf("initial Console filter = %s/%s", model.QueueState, model.QueueVisibility)
	}
}

func TestConsoleQueueExactDetailPreservesCollectionPagination(t *testing.T) {
	for _, empty := range []bool{false, true} {
		records := []consoleweb.RecordModel{{Key: "first"}, {Key: "second"}}
		page := "2"
		if empty {
			records = nil
			page = "1"
		}
		model := consoleweb.SurfaceModel{Domain: "queue", Records: records, Inspector: &consoleweb.RecordModel{Key: "historical"}, InspectorOpen: true}
		query := url.Values{"page": {page}, "state": {"succeeded"}, "visibility": {"archived"}, "record_key": {"historical"}}
		if err := paginateConsoleCollection(&model, page, "historical", 1, query); err != nil {
			t.Fatal(err)
		}
		if err := selectConsoleRecord(&model, "historical"); err != nil {
			t.Fatal(err)
		}
		if model.Inspector.Key != "historical" || !model.InspectorOpen {
			t.Fatal("lost exact history")
		}
		if !empty && (len(model.Records) != 1 || model.Records[0].Key != "second") {
			t.Fatal("detail changed collection page")
		}
		parsed, err := url.Parse(model.CollectionURL)
		if err != nil {
			t.Fatal(err)
		}
		if parsed.Query().Get("state") != "succeeded" || parsed.Query().Get("visibility") != "archived" || parsed.Query().Get("record_key") != "" {
			t.Fatal("lost collection context", model.CollectionURL)
		}
		if err := selectConsoleRecord(&model, "missing"); err == nil {
			t.Fatal("unknown detail admitted")
		}
	}
}
