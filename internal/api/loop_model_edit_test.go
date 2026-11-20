package api

import (
	"context"
	"encoding/json"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"errors"
	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	"github.com/labstack/echo/v5"
)

func TestOwnedLoopModelEditConsoleDecisionAndResume(t *testing.T) {
	svc, sub, charter, _, draft := nativeSetupFixture(t)
	ctx := context.Background()
	// Fixture-only authority: exact controller-Codex provider, no Agent credentials.
	charter.Revision++
	charter.Stanzas[0].Scopes = core.Scopes{}
	charter.Stanzas[0].Hermes.Provider = "openai-codex"
	charter.Stanzas[0].Hermes.ProviderAuthentication = &core.ProviderAuthentication{Mode: core.ControllerCodexAccessTokenV1}
	wire, _ := json.Marshal(charter)
	canonical, err := svc.ImportCharterAs(ctx, sub, wire)
	if err != nil {
		t.Fatal(err)
	}
	approved, err := svc.ApproveAgentCharterAs(ctx, sub, draft.Agent.ID, app.ApproveAgentCharterInput{Expected: draft.Agent, Charter: app.RevisionReference(draft.Agent.ID, charter.Revision, canonical.Digest)})
	if err != nil {
		t.Fatal(err)
	}
	draft.Agent = app.RevisionReference(approved.Revision.AgentID, approved.Revision.Revision, approved.Revision.Digest)
	original, _, err := loop.NewDoerReusableRevision("model-review", 1, "", loop.DoerReusableContract{MaxAttempts: 2})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.PublishLoopAs(ctx, sub, app.PublishLoopInput{AgentID: draft.Agent.ID, Revision: original, IdempotencyKey: "original-model-review"}); err != nil {
		t.Fatal(err)
	}
	manager, cookie, csrf := consoleQueueRouteFixture(t, svc)
	svc.DashboardActionAuthentication = manager.ValidateActionSubject
	e := echo.New()
	e.HTTPErrorHandler = apiHTTPErrorHandler(svc, manager)
	g := e.Group("/v1", func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error { c.Set("subject", sub); return next(c) }
	})
	registerLoopModelEditRoutes(g, svc)
	registerLoopModelEditConsole(e, svc, manager)
	api := func(method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		e.ServeHTTP(rr, req)
		return rr
	}
	bad := api("POST", "/v1/loops/model-review/1/model-edit", `{"agent_id":"native-doer","provider":"openai-codex","model":"gpt-6.2-sol","steps":["unknown-step"]}`)
	if bad.Code != http.StatusBadRequest || !strings.Contains(bad.Body.String(), `"code":"invalid_input"`) {
		t.Errorf("unknown selector must be deterministic input error: %d %s", bad.Code, bad.Body.String())
	}
	if _, err = svc.GetLoopAs(ctx, sub, original.LoopID, 2); !errors.Is(err, fleet.ErrNotFound) {
		t.Fatal("invalid selector mutated publication")
	}
	result := api("POST", "/v1/loops/model-review/1/model-edit", `{"agent_id":"native-doer","provider":"openai-codex","model":"gpt-6.2-sol"}`)
	var pending app.PublishedLoop
	if result.Code != http.StatusAccepted || json.Unmarshal(result.Body.Bytes(), &pending) != nil || pending.PendingEdit == nil {
		t.Fatalf("retain: %d %s", result.Code, result.Body.String())
	}
	edit := *pending.PendingEdit
	if edit.MissingModel.Model != "gpt-6.2-sol" {
		t.Fatal("missing model not surfaced")
	}
	// Same exact file-free command reconciles to the same retained candidate.
	replay := api("POST", "/v1/loops/model-review/1/model-edit", `{"agent_id":"native-doer","provider":"openai-codex","model":"gpt-6.2-sol"}`)
	var repeated app.PublishedLoop
	if replay.Code != 202 || json.Unmarshal(replay.Body.Bytes(), &repeated) != nil || !reflect.DeepEqual(repeated.PendingEdit, &edit) {
		t.Fatal("file-free replay drift")
	}
	read := api("GET", "/v1/loops/edits/"+edit.ID, "")
	if read.Code != 200 {
		t.Fatalf("pending read: %d", read.Code)
	}
	if rr := api("POST", "/v1/loops/edits/"+edit.ID+"/publish", ""); rr.Code != 403 {
		t.Fatalf("unapproved publish: %d %s", rr.Code, rr.Body.String())
	}
	post := func(action, receipt, origin, token string) *httptest.ResponseRecorder {
		values := url.Values{"csrf": {token}, "edit_id": {edit.ID}, "action": {action}}
		if receipt != "" {
			values.Set("receipt", receipt)
		}
		req := httptest.NewRequest("POST", "http://127.0.0.1/console/loops/model-edit", strings.NewReader(values.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Origin", origin)
		req.AddCookie(cookie)
		rr := httptest.NewRecorder()
		e.ServeHTTP(rr, req)
		return rr
	}
	token := func(rr *httptest.ResponseRecorder) string {
		t.Helper()
		m := regexp.MustCompile(`name="receipt" value="([^"]+)"`).FindStringSubmatch(rr.Body.String())
		if rr.Code != 200 || len(m) != 2 {
			t.Fatalf("review: %d %s", rr.Code, rr.Body.String())
		}
		return html.UnescapeString(m[1])
	}
	request := httptest.NewRequest("GET", "http://127.0.0.1"+edit.ConfirmationURL, nil)
	request.AddCookie(cookie)
	rr := httptest.NewRecorder()
	e.ServeHTTP(rr, request)
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), "gpt-6.2-sol") || !strings.Contains(rr.Body.String(), original.LoopID) {
		t.Fatal("product review link unusable")
	}
	if rr.Header().Get("Content-Security-Policy") == "" {
		t.Fatal("missing Console CSP")
	}
	receipt := token(post("review", "", "http://127.0.0.1", csrf))
	if rr = post("confirm", receipt, "https://foreign.example", csrf); rr.Code != 403 {
		t.Fatal("foreign-origin approval")
	}
	if rr = post("confirm", receipt, "http://127.0.0.1", "wrong"); rr.Code != 403 {
		t.Fatal("missing-CSRF approval")
	}
	if rr = post("confirm", "forged", "http://127.0.0.1", csrf); rr.Code != 403 {
		t.Fatal("forged receipt approval")
	}
	if rr = post("reject", receipt, "http://127.0.0.1", csrf); rr.Code != 200 {
		t.Fatal("reject failed")
	}
	if rr = post("confirm", receipt, "http://127.0.0.1", csrf); rr.Code != 403 {
		t.Fatal("rejected receipt reused")
	}
	agent, err := svc.GetFleetAgentAs(ctx, sub, draft.Agent.ID, 0)
	if err != nil || agent.Revision.Revision != draft.Agent.Revision {
		t.Fatal("review/reject changed Agent")
	}
	receipt = token(post("review", "", "http://127.0.0.1", csrf))
	rr = post("confirm", receipt, "http://127.0.0.1", csrf)
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), "Exact model permission approved") {
		t.Fatalf("confirm: %d %s", rr.Code, rr.Body.String())
	}
	if rr = post("confirm", receipt, "http://127.0.0.1", csrf); rr.Code != 403 {
		t.Fatal("consumed receipt reused")
	}
	if _, err = svc.GetLoopAs(ctx, sub, original.LoopID, 2); !errors.Is(err, fleet.ErrNotFound) {
		t.Fatal("approval published candidate")
	}
	result = api("POST", "/v1/loops/edits/"+edit.ID+"/publish", "")
	var published app.PublishedLoop
	if result.Code != 201 || json.Unmarshal(result.Body.Bytes(), &published) != nil || published.Revision.Digest != edit.Input.Revision.Digest {
		t.Fatalf("resume: %d %s", result.Code, result.Body.String())
	}
	if rr = api("POST", "/v1/loops/edits/"+edit.ID+"/publish", ""); rr.Code != 200 {
		t.Fatalf("resume replay: %d %s", rr.Code, rr.Body.String())
	}
	rr = api("POST", "/v1/loops/model-review/1/model-edit", `{"agent_id":"native-doer","provider":"openai-codex","model":"gpt-6.2-sol"}`)
	if rr.Code != 200 {
		t.Errorf("published update replay: %d %s", rr.Code, rr.Body.String())
	}
	read = api("GET", "/v1/loops/edits/"+edit.ID, "")
	if read.Code != 200 || !strings.Contains(read.Body.String(), `"status":"published"`) {
		t.Errorf("published edit-show: %d %s", read.Code, read.Body.String())
	}
	rr = httptest.NewRecorder()
	e.ServeHTTP(rr, request)
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), "Already published") || strings.Contains(rr.Body.String(), `value="review"`) || strings.Contains(rr.Body.String(), `value="confirm"`) {
		t.Errorf("published Console offered authority review: %d", rr.Code)
	}
	if rr = post("review", "", "http://127.0.0.1", csrf); rr.Code != 409 {
		t.Errorf("published Console issued another review: %d", rr.Code)
	}
	preserved, err := svc.GetLoopAs(ctx, sub, original.LoopID, 1)
	if err != nil || !reflect.DeepEqual(preserved, original) {
		t.Fatal("prior definition changed")
	}
	// Fresh Console login after edit expiry and a later Agent revision still
	// reads the original publication; neither old nor new receipts authorize it.
	current, err := svc.GetFleetAgentAs(ctx, sub, draft.Agent.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.SetAgentLifecycleAs(ctx, sub, draft.Agent.ID, app.SetAgentLifecycleInput{Expected: app.RevisionReference(current.Revision.AgentID, current.Revision.Revision, current.Revision.Digest), Lifecycle: "disabled"}); err != nil {
		t.Fatal(err)
	}
	now := edit.ExpiresAt
	svc.Now = func() time.Time { return now }
	sub.AuthenticatedAt, sub.ExpiresAt = now, now.Add(time.Minute)
	manager, cookie, csrf = consoleQueueRouteFixture(t, svc)
	svc.DashboardActionAuthentication = manager.ValidateActionSubject
	e = echo.New()
	e.HTTPErrorHandler = apiHTTPErrorHandler(svc, manager)
	g = e.Group("/v1", func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error { c.Set("subject", sub); return next(c) }
	})
	registerLoopModelEditRoutes(g, svc)
	registerLoopModelEditConsole(e, svc, manager)
	request = httptest.NewRequest("GET", "http://127.0.0.1"+edit.ConfirmationURL, nil)
	request.AddCookie(cookie)
	rr = httptest.NewRecorder()
	e.ServeHTTP(rr, request)
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), "Already published") || strings.Contains(rr.Body.String(), `value="review"`) {
		t.Fatal("historical Console lost publication")
	}
	if rr = post("review", "", "http://127.0.0.1", csrf); rr.Code != 409 {
		t.Fatal("historical Console reissued review")
	}
	if rr = post("confirm", receipt, "http://127.0.0.1", csrf); rr.Code != 403 {
		t.Fatal("old receipt survived new session")
	}
	if rr = api("POST", "/v1/loops/model-review/1/model-edit", `{"agent_id":"native-doer","provider":"openai-codex","model":"gpt-6.2-sol"}`); rr.Code != 200 {
		t.Fatalf("historical update: %d %s", rr.Code, rr.Body.String())
	}
	if rr = api("POST", "/v1/loops/edits/"+edit.ID+"/publish", ""); rr.Code != 200 {
		t.Fatalf("historical publish: %d %s", rr.Code, rr.Body.String())
	}
	read = api("GET", "/v1/loops/edits/"+edit.ID, "")
	if read.Code != 200 || !strings.Contains(read.Body.String(), `"status":"published"`) {
		t.Fatal("historical edit-show")
	}
	items, err := svc.FleetRepository.ListQueueItems(ctx)
	if err != nil || len(items) != 0 {
		t.Fatal("model approval started execution")
	}
}
