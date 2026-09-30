package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/reference"
	"github.com/berryhill/aegis/internal/registry"
	consoleweb "github.com/berryhill/aegis/web/console"
	"github.com/labstack/echo/v5"
)

func TestConsoleLoopRunKeySurvivesLostResponseUntilExplicitNewRun(t *testing.T) {
	identity := core.Subject{PrincipalID: "principal"}
	record := &consoleweb.RecordModel{Label: "selected", Revision: "r1", Digest: "sha256:exact"}
	request := func(path string, cookie *http.Cookie) (string, *http.Cookie) {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		if cookie != nil {
			req.AddCookie(cookie)
		}
		response := httptest.NewRecorder()
		key, err := consoleLoopRunKey(echo.New().NewContext(req, response), identity, record)
		if err != nil {
			t.Fatal(err)
		}
		cookies := response.Result().Cookies()
		if len(cookies) == 0 {
			return key, cookie
		}
		if !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode {
			t.Fatalf("unsafe request identity cookie: %+v", cookies[0])
		}
		return key, cookies[0]
	}
	first, cookie := request("http://127.0.0.1/console/loops?record_key=selected:1", nil)
	reloaded, _ := request("http://127.0.0.1/console/loops?record_key=selected:1", cookie)
	rotated, _ := request("http://127.0.0.1/console/loops?record_key=selected:1&new_run=1", cookie)
	if first == "" || first != reloaded || first == rotated {
		t.Fatalf("lost-response retry changed identity or explicit new run did not: %q %q %q", first, reloaded, rotated)
	}
}

func TestConsoleLoopRunRouteBlockedIntentAndAuthentication(t *testing.T) {
	svc := apiService(t)
	configureAPIFleet(t, svc)
	ctx := context.Background()
	subject, err := svc.AuthenticateUnixPeer(ctx, uint32(os.Getuid()))
	if err != nil {
		t.Fatal(err)
	}
	charter := core.Charter{SchemaVersion: core.SchemaVersion, AgentID: "console-run-agent", Name: "Console runner", Revision: 1,
		Runtime: core.RuntimeConstraint{Adapter: "hermes", Runtime: "hermes-agent", VersionConstraint: ">=0.18.0", Target: "aegis-owned-ephemeral"},
		Stanzas: []core.TrustStanza{{ID: "principal", Name: "Principal", Enabled: true,
			Authentication: core.AuthenticationPolicy{Methods: []string{"local-os"}, Selectors: []core.IdentitySelector{{SubjectIDs: []string{"local-uid:" + strconv.Itoa(os.Getuid())}, PrincipalIDs: []string{svc.Config.Principal.ID}, Issuers: []string{"linux-so-peercred"}, Environments: []string{"local"}}}, RequireFresh: true, MaxAuthAgeSec: 60},
			Grant:          core.Grant{}, Scopes: core.Scopes{}, Session: core.SessionPolicy{MaximumLifetimeSec: 60, RequireReauth: true}, Approval: core.ApprovalPolicy{RequiredOperations: []string{"provision"}, MaximumLifetimeSec: 60, SingleUse: true}, InformationFlow: core.InformationFlowPolicy{CrossStanza: "deny"}, Hermes: core.HermesConfig{Model: "none", Provider: "none"}}}, CreatedBy: svc.Config.Principal.ID, CreatedAt: svc.Now()}
	wire, err := json.Marshal(charter)
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := svc.ImportCharterAs(ctx, subject, wire)
	if err != nil {
		t.Fatal(err)
	}
	fixture, err := json.Marshal(registry.CurrentFleetFixture{SchemaVersion: registry.CurrentFleetFixtureSchemaVersion, FleetID: "console-run-fleet", Agents: []registry.CurrentFleetAgent{{SourceID: "console-run-source", AgentID: charter.AgentID,
		Runtime:   registry.RuntimeBinding{Adapter: charter.Runtime.Adapter, Runtime: charter.Runtime.Runtime, Target: charter.Runtime.Target},
		Ownership: registry.Ownership{OwnerID: "owner", AccountabilityID: "owner"}, Lifecycle: registry.LifecycleEnabled,
		Charter: reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: charter.AgentID, Revision: 1, Digest: canonical.Digest}}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = svc.RegisterFleetAgentAs(ctx, subject, app.NewRegisterFleetAgentInput(fixture, "console-run-fleet", "console-run-source")); err != nil {
		t.Fatal(err)
	}
	revision, _, err := loop.NewDoerRevision("console-run-loop", 1, "", loop.DoerContract{Task: "Create selected file", Workspace: t.TempDir(), WritableFiles: []string{"selected.txt"}, VerifyFile: "selected.txt", MaxAttempts: 1})
	if err != nil {
		t.Fatal(err)
	}
	published, err := svc.PublishLoopAs(ctx, subject, app.PublishLoopInput{AgentID: charter.AgentID, Revision: revision, IdempotencyKey: "console-run-publish"})
	if err != nil {
		t.Fatal(err)
	}
	manager, cookie, csrf := consoleQueueRouteFixture(t, svc)
	e := echo.New()
	e.HTTPErrorHandler = func(c *echo.Context, err error) {
		status, code, message := classifyError(err)
		_ = c.JSON(status, envelope{Code: code, Message: message, RequestID: "test-request"})
	}
	e.POST("/console/loops/:loop/run", consoleLoopRunHandler(svc, manager))
	invoke := func(key, token, digest string, authenticated bool) *httptest.ResponseRecorder {
		form := url.Values{"csrf": {token}, "revision": {"1"}, "digest": {digest}, "idempotency_key": {key}}
		request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1/console/loops/console-run-loop/run", strings.NewReader(form.Encode()))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		request.Header.Set("Origin", "http://127.0.0.1")
		if authenticated {
			request.AddCookie(cookie)
		}
		response := httptest.NewRecorder()
		e.ServeHTTP(response, request)
		return response
	}
	for _, response := range []*httptest.ResponseRecorder{invoke("key", csrf, published.Revision.Digest, false), invoke("key", "wrong", published.Revision.Digest, true), invoke("key", csrf, "sha256:"+strings.Repeat("a", 64), true)} {
		if response.Code == http.StatusOK {
			t.Fatalf("untrusted form admitted: %s", response.Body.String())
		}
	}
	first := invoke("stable-key", csrf, published.Revision.Digest, true)
	if first.Code != http.StatusOK || !strings.Contains(first.Body.String(), "session_selection_zero_authorized_matches") || !strings.Contains(first.Body.String(), "review_charter_successor_with_matching_authentication") || !strings.Contains(first.Body.String(), "required_charter:") || !strings.Contains(first.Body.String(), "Request key:") || !strings.Contains(first.Body.String(), "stable-key") || !strings.Contains(first.Body.String(), "Read back or resume this request") {
		t.Fatalf("blocked request status=%d stanza_denial=%t", first.Code, strings.Contains(first.Body.String(), "session_selection_zero_authorized_matches"))
	}
	second := invoke("stable-key", csrf, published.Revision.Digest, true)
	if second.Code != http.StatusOK || !strings.Contains(second.Body.String(), "session_selection_zero_authorized_matches") {
		t.Fatalf("blocked intent replay status=%d", second.Code)
	}
	view, err := svc.GetLoopViewAs(ctx, subject, revision.LoopID, revision.Revision)
	if err != nil || view.Lifecycle.State != loop.LifecycleDraft || len(view.History) != 0 {
		t.Fatalf("blocked request mutated Loop: %+v, %v", view.Lifecycle, err)
	}
}
