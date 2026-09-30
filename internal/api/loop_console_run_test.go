package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/loop"
	consoleweb "github.com/berryhill/aegis/web/console"
)

func TestConsoleLoopRunFormIsBoundedAndClosed(t *testing.T) {
	good := url.Values{"csrf": {"token"}, "revision": {"1"}, "digest": {"sha256:" + strings.Repeat("a", 64)}, "idempotency_key": {"stable-key"}}
	request := func(v url.Values) *http.Request {
		r := httptest.NewRequest(http.MethodPost, "/console/loops/doer/run", strings.NewReader(v.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		return r
	}
	if _, err := decodeConsoleLoopRunForm(request(good)); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"authority", "stanza", "workspace", "graph_id", "queue_item_id", "agent_id", "activate", "inputs"} {
		v := url.Values{}
		for k, values := range good {
			v[k] = append([]string(nil), values...)
		}
		v.Set(field, "forged")
		if _, err := decodeConsoleLoopRunForm(request(v)); err == nil {
			t.Errorf("accepted %s", field)
		}
	}
	for _, field := range []string{"csrf", "revision", "digest", "idempotency_key"} {
		v := url.Values{}
		for k, values := range good {
			v[k] = append([]string(nil), values...)
		}
		v.Add(field, "duplicate")
		if _, err := decodeConsoleLoopRunForm(request(v)); err == nil {
			t.Errorf("accepted duplicate %s", field)
		}
	}
}

func TestConsoleDoerRunAffordanceOnlyOnV4(t *testing.T) {
	revision, _, err := loop.NewDoerRevision("team/doer-console-run", 1, "", loop.DoerContract{Task: "Write selected", Workspace: t.TempDir(), WritableFiles: []string{"result.txt"}, VerifyFile: "result.txt", MaxAttempts: 1})
	if err != nil {
		t.Fatal(err)
	}
	record := consoleLoopRecord(app.LoopView{Revision: revision, Lifecycle: loop.Lifecycle{LoopID: revision.LoopID, State: loop.LifecycleDraft}})
	body, err := renderConsole(context.Background(), consoleweb.LoopWorkspace(consoleweb.SurfaceModel{Domain: "loops", CSRF: "csrf-token"}, &record, consoleweb.LoopTopology{}))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`action="/console/loops/run?loop_id=team%2Fdoer-console-run"`, `name="idempotency_key"`, `name="csrf"`, `name="revision"`, `name="digest"`} {
		if !strings.Contains(string(body), want) {
			t.Errorf("missing %s", want)
		}
	}
	record.Loop.DoerV4 = false
	record.Loop.DoerV5 = true
	body, err = renderConsole(context.Background(), consoleweb.LoopWorkspace(consoleweb.SurfaceModel{Domain: "loops", CSRF: "csrf-token"}, &record, consoleweb.LoopTopology{}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), `/run"`) {
		t.Fatal("v5 exposed run action")
	}
}
