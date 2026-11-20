package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/registry"
	"github.com/labstack/echo/v5"
)

func TestAgentModelAddNativeNoDraftExactPasswordJourney(t *testing.T) {
	s := apiService(t)
	configureAPIFleet(t, s)
	ctx := context.Background()
	sub, err := s.AuthenticateUnixPeer(ctx, uint32(os.Getuid()))
	if err != nil {
		t.Fatal(err)
	}
	c := core.Charter{SchemaVersion: core.SchemaVersion, AgentID: "silas", Name: "Silas fixture", Revision: 1, Runtime: core.RuntimeConstraint{Adapter: "hermes", Runtime: "hermes-agent", VersionConstraint: ">=0.18.0", Target: "aegis-owned-ephemeral"}, CreatedBy: sub.PrincipalID, CreatedAt: s.Now(), Stanzas: []core.TrustStanza{{ID: "principal", Name: "Principal", Enabled: true, Authentication: core.AuthenticationPolicy{Methods: []string{"local-os"}, Selectors: []core.IdentitySelector{{PrincipalIDs: []string{sub.PrincipalID}, Issuers: []string{"linux-so-peercred"}, Environments: []string{"local"}}}}, Grant: core.Grant{}, Scopes: core.Scopes{}, Session: core.SessionPolicy{MaximumLifetimeSec: 60}, Approval: core.ApprovalPolicy{MaximumLifetimeSec: 60, SingleUse: true}, InformationFlow: core.InformationFlowPolicy{CrossStanza: "deny"}, Hermes: core.HermesConfig{Model: "unchanged-model", Provider: "codex", ProviderAuthentication: &core.ProviderAuthentication{Mode: core.ControllerCodexAccessTokenV1}}}}}
	raw, _ := json.Marshal(c)
	ch, err := s.ImportCharterAs(ctx, sub, raw)
	if err != nil {
		t.Fatal(err)
	}
	fixture, _ := json.Marshal(registry.CurrentFleetFixture{SchemaVersion: registry.CurrentFleetFixtureSchemaVersion, FleetID: "model-fleet", Agents: []registry.CurrentFleetAgent{{SourceID: "model-source", AgentID: c.AgentID, Runtime: registry.RuntimeBinding{Adapter: c.Runtime.Adapter, Runtime: c.Runtime.Runtime, Target: c.Runtime.Target}, Ownership: registry.Ownership{OwnerID: sub.PrincipalID, AccountabilityID: sub.PrincipalID}, Lifecycle: registry.LifecycleEnabled, Charter: app.RevisionReference(c.AgentID, 1, ch.Digest)}}})
	if _, _, err = s.RegisterFleetAgentAs(ctx, sub, app.NewRegisterFleetAgentInput(fixture, "model-fleet", "model-source")); err != nil {
		t.Fatal(err)
	}
	manager, _, _ := consoleQueueRouteFixture(t, s)
	s.DashboardActionAuthentication = manager.ValidateActionSubject
	e := echo.New()
	registerDoerProtectedSetupRoutes(e, s, manager)
	var cookie *http.Cookie
	csrf := ""
	call := func(path string, body any, peer bool, origin, token string) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(body)
		content := "application/json"
		if path == "login" {
			raw = []byte(body.(string))
			content = "application/octet-stream"
		}
		req := httptest.NewRequest("POST", "http://127.0.0.1/console/loops/doer/setup-native/"+path, strings.NewReader(string(raw)))
		if peer {
			req = req.WithContext(context.WithValue(req.Context(), peerUIDKey{}, uint32(os.Getuid())))
		}
		req.Header.Set("Origin", origin)
		req.Header.Set("Content-Type", content)
		req.Header.Set("X-CSRF-Token", token)
		if cookie != nil {
			req.AddCookie(cookie)
		}
		rr := httptest.NewRecorder()
		e.ServeHTTP(rr, req)
		return rr
	}
	in := app.AgentModelAddInput{AgentID: "silas", Provider: "openai-codex", Model: "gpt-6.2-sol"}
	if r := call("model-review", in, true, "http://127.0.0.1", ""); r.Code < 400 {
		t.Fatal("peer-only review accepted")
	}
	if r := call("login", "wrong-password", true, "http://127.0.0.1", ""); r.Code < 400 {
		t.Fatal("bad password accepted")
	}
	login := call("login", "test-principal-password", true, "http://127.0.0.1", "")
	var auth struct {
		CSRF string `json:"csrf"`
	}
	if login.Code != 200 || json.Unmarshal(login.Body.Bytes(), &auth) != nil || len(login.Result().Cookies()) != 1 {
		t.Fatal("login", login.Code)
	}
	cookie = login.Result().Cookies()[0]
	csrf = auth.CSRF
	for _, bad := range []struct {
		peer         bool
		origin, csrf string
	}{{false, "http://127.0.0.1", csrf}, {true, "https://foreign.example", csrf}, {true, "http://127.0.0.1", "wrong"}} {
		if r := call("model-review", in, bad.peer, bad.origin, bad.csrf); r.Code < 400 {
			t.Fatal("invalid admission")
		}
	}
	review := func() app.AgentModelAddReview {
		t.Helper()
		r := call("model-review", in, true, "http://127.0.0.1", csrf)
		var out app.AgentModelAddReview
		if r.Code != 200 || json.Unmarshal(r.Body.Bytes(), &out) != nil || out.Receipt == "" {
			t.Fatal("review", r.Code, r.Body.String())
		}
		return out
	}
	first := review()
	decision := func(receipt, action string) *httptest.ResponseRecorder {
		return call("model-decision", app.DoerProtectedSetupDecision{Receipt: receipt, Decision: action}, true, "http://127.0.0.1", csrf)
	}
	if r := call("model-decision", map[string]any{"receipt": first.Receipt, "decision": "approve", "model": "different"}, true, "http://127.0.0.1", csrf); r.Code != 400 {
		t.Fatal("override not rejected", r.Code)
	}
	if r := decision(first.Receipt, "reject"); r.Code != 200 {
		t.Fatal("reject", r.Code)
	}
	if r := decision(first.Receipt, "approve"); r.Code < 400 {
		t.Fatal("rejected receipt reused")
	}
	second := review()
	r := decision(second.Receipt, "approve")
	var result app.AgentModelAddResult
	if r.Code != 200 || json.Unmarshal(r.Body.Bytes(), &result) != nil {
		t.Fatal("approve", r.Code, r.Body.String())
	}
	agent := result.Agent
	got, err := s.GetFleetAgentAs(ctx, sub, "silas", 0)
	if err != nil || got.Revision.Digest != agent.Revision.Digest || got.Revision.Charter.Digest != second.Proposed.Digest {
		t.Fatal("readback", err)
	}
	charter, err := s.GetCharter("silas", got.Revision.Charter.Revision)
	if err != nil {
		t.Fatal(err)
	}
	h := charter.Charter.Stanzas[0].Hermes
	if h.Provider != "codex" || h.Model != "unchanged-model" || len(h.AllowedLanes) != 2 || h.AllowedLanes[1].Provider != in.Provider || h.AllowedLanes[1].Model != in.Model {
		t.Fatal("wrong permission")
	}
	if decision(second.Receipt, "approve").Code < 400 {
		t.Fatal("single-use receipt replay")
	}
	loops, err := s.ListLoopsAs(ctx, sub)
	if err != nil || len(loops) != 0 {
		t.Fatal("model addition created Loop")
	}
	items, err := s.FleetRepository.ListQueueItems(ctx)
	if err != nil || len(items) != 0 {
		t.Fatal("model addition executed")
	}
	t.Log("PASS: Silas exact openai-codex/gpt-6.2-sol addition via enrolled-password native review/decision; no Loop draft/publication or Queue work; default preserved")
}
