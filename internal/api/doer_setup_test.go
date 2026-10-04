package api

import (
	"context"
	"encoding/json"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/reference"
	"github.com/berryhill/aegis/internal/registry"
	"github.com/labstack/echo/v5"
)

func TestDoerSetupIndependentConsentRetainsDraftAndNoAutoEffects(t *testing.T) {
	s := apiService(t)
	configureAPIFleet(t, s)
	// Signed approvals require a canonical deployment binding, not credentials.
	s.Config.Credentials.Authority.DeploymentID = "setup-deployment"
	ctx := context.Background()
	sub, err := s.AuthenticateUnixPeer(ctx, uint32(os.Getuid()))
	if err != nil {
		t.Fatal(err)
	}
	c := core.Charter{SchemaVersion: core.SchemaVersion, AgentID: "setup-doer", Name: "Setup Doer", Revision: 1, Runtime: core.RuntimeConstraint{Adapter: "hermes", Runtime: "hermes-agent", VersionConstraint: ">=0.18.0", Target: "aegis-owned-ephemeral"}, CreatedBy: sub.PrincipalID, CreatedAt: s.Now(), Stanzas: []core.TrustStanza{{ID: "principal", Name: "Principal", Enabled: true, Authentication: core.AuthenticationPolicy{Methods: []string{"local-os"}, Selectors: []core.IdentitySelector{{PrincipalIDs: []string{sub.PrincipalID}, Issuers: []string{"linux-so-peercred"}, Environments: []string{"local"}}}}, Session: core.SessionPolicy{MaximumLifetimeSec: 60}, Approval: core.ApprovalPolicy{MaximumLifetimeSec: 60, SingleUse: true}, InformationFlow: core.InformationFlowPolicy{CrossStanza: "deny"}, Hermes: core.HermesConfig{Model: "proof-no-key", Provider: "none"}}}}
	wire, _ := json.Marshal(c)
	ch, err := s.ImportCharterAs(ctx, sub, wire)
	if err != nil {
		t.Fatal(err)
	}
	fixture, _ := json.Marshal(registry.CurrentFleetFixture{SchemaVersion: registry.CurrentFleetFixtureSchemaVersion, FleetID: "setup-fleet", Agents: []registry.CurrentFleetAgent{{SourceID: "setup-source", AgentID: c.AgentID, Runtime: registry.RuntimeBinding{Adapter: c.Runtime.Adapter, Runtime: c.Runtime.Runtime, Target: c.Runtime.Target}, Ownership: registry.Ownership{OwnerID: sub.PrincipalID, AccountabilityID: sub.PrincipalID}, Lifecycle: registry.LifecycleEnabled, Charter: reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: c.AgentID, Revision: 1, Digest: ch.Digest}}}})
	agent, _, err := s.RegisterFleetAgentAs(ctx, sub, app.NewRegisterFleetAgentInput(fixture, "setup-fleet", "setup-source"))
	if err != nil {
		t.Fatal(err)
	}
	text := "retained exact text"
	draft, err := s.SaveDoerDraftAs(ctx, sub, app.DoerDraftInput{Agent: reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: c.AgentID, Revision: agent.Revision.Revision, Digest: agent.Revision.Digest}, LoopID: "setup-loop", Revision: 1, Contract: loop.DoerContract{Task: "Retain this task", Workspace: t.TempDir(), WritableFiles: []string{"result.txt"}, VerifyFile: "result.txt", ExpectedText: &text, MaxAttempts: 2}})
	if err != nil {
		t.Fatal(err)
	}
	manager, cookie, csrf := consoleQueueRouteFixture(t, s)
	e := echo.New()
	registerDoerSetupRoutes(e, s, manager)
	post := func(values url.Values, origin string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1/console/loops/doer/setup", strings.NewReader(values.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Origin", origin)
		req.AddCookie(cookie)
		rr := httptest.NewRecorder()
		e.ServeHTTP(rr, req)
		return rr
	}
	form := func(action, receipt string) url.Values {
		v := url.Values{"csrf": {csrf}, "draft_id": {draft.ID}, "action": {action}}
		if receipt != "" {
			v.Set("receipt", receipt)
		}
		return v
	}
	token := func(rr *httptest.ResponseRecorder) string {
		t.Helper()
		match := regexp.MustCompile(`name="receipt" value="([^"]+)"`).FindStringSubmatch(rr.Body.String())
		if rr.Code != 200 || len(match) != 2 {
			t.Fatalf("missing review %d: %s", rr.Code, rr.Body.String())
		}
		return html.UnescapeString(match[1])
	}
	if rr := post(form("preview", ""), "https://wrong.example"); rr.Code != 403 {
		t.Fatalf("origin accepted %d", rr.Code)
	}
	v := form("preview", "")
	v.Set("csrf", "wrong")
	if rr := post(v, "http://127.0.0.1"); rr.Code != 403 {
		t.Fatalf("csrf accepted %d", rr.Code)
	}
	rr := post(form("preview", ""), "http://127.0.0.1")
	review := token(rr)
	if !strings.Contains(rr.Body.String(), "Complete per-stanza declarations") || !strings.Contains(rr.Body.String(), ch.Digest) {
		t.Fatal("incomplete plan preview")
	}
	approvals, _ := s.ListApprovals()
	receipts, _ := s.ListReceipts()
	if len(approvals) != 0 || len(receipts) != 0 {
		t.Fatal("preview auto consent/effects")
	}
	rr = post(form("confirm", review), "http://127.0.0.1")
	decision := token(rr)
	approvals, _ = s.ListApprovals()
	if len(approvals) != 1 || approvals[0].Status != "pending" {
		t.Fatalf("not independent pending: %v", approvals)
	}
	post(form("confirm", review), "http://127.0.0.1")
	approvals, _ = s.ListApprovals()
	if len(approvals) != 1 || approvals[0].Status != "pending" {
		t.Fatal("review replay altered approval")
	}
	rr = post(form("confirm", decision), "http://127.0.0.1")
	apply := token(rr)
	approvals, _ = s.ListApprovals()
	receipts, _ = s.ListReceipts()
	if approvals[0].Status != "approved" || len(receipts) != 0 {
		t.Fatal("decision auto applied")
	}
	got, err := s.ReadDoerDraftAs(ctx, sub, draft.ID)
	if err != nil || !reflect.DeepEqual(got, draft) {
		t.Fatal("approval modified retained inputs")
	}
	rr = post(form("confirm", apply), "http://127.0.0.1")
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), "Authoritative provisioning receipt:") {
		t.Fatalf("apply: %d %s", rr.Code, rr.Body.String())
	}
	receipts, _ = s.ListReceipts()
	if len(receipts) != 1 || receipts[0].CharterDigest != ch.Digest {
		t.Fatal("wrong receipt")
	}
	post(form("confirm", apply), "http://127.0.0.1")
	receipts, _ = s.ListReceipts()
	if len(receipts) != 1 {
		t.Fatal("apply replay created effects")
	}
	got, err = s.ReadDoerDraftAs(ctx, sub, draft.ID)
	if err != nil || !reflect.DeepEqual(got, draft) {
		t.Fatal("apply modified draft")
	}
	// Use the actual Manager-derived browser principal, not a fabricated human
	// or local-OS subject, for the independent contextual host decision.
	request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1/console/loops/doer/setup", nil)
	request.AddCookie(cookie)
	request.Header.Set("Origin", "http://127.0.0.1")
	request.Header.Set("X-CSRF-Token", csrf)
	browser, err := manager.AuthorizeMutation(request)
	if err != nil || browser.Kind != "principal" || browser.Method != "password" || browser.Issuer != "aegis-principal-auth" {
		t.Fatal("fixture did not supply the actual browser identity", err)
	}
	rr = post(form("host-review", ""), "http://127.0.0.1")
	hostReview := token(rr)
	if !strings.Contains(rr.Body.String(), "Maximum approval lifetime: 24 hours") || !strings.Contains(rr.Body.String(), draft.Contract.Task) {
		t.Fatal("host review omitted its exact task or independent lifetime")
	}
	if _, err = s.ReadDoerHostWriteApprovalAs(ctx, browser, draft.Agent, draft.Contract); err == nil {
		t.Fatal("host preview silently granted permission")
	}
	rr = post(form("confirm", hostReview), "http://127.0.0.1")
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), "Authoritative signed host-write approval read back:") {
		t.Fatalf("browser host approval failed: %d %s", rr.Code, rr.Body.String())
	}
	if _, err = s.ReadDoerHostWriteApprovalAs(ctx, browser, draft.Agent, draft.Contract); err != nil {
		t.Fatal("exact browser-approved host contract did not reload", err)
	}
	if _, err = os.Stat(filepath.Join(draft.Contract.Workspace, draft.Contract.VerifyFile)); !os.IsNotExist(err) {
		t.Fatal("host approval performed a file effect")
	}
	items, err := s.FleetRepository.ListQueueItems(ctx)
	if err != nil || len(items) != 0 {
		t.Fatal("contextual setup admitted execution")
	}
	got, err = s.ReadDoerDraftAs(ctx, browser, draft.ID)
	if err != nil || !reflect.DeepEqual(got, draft) {
		t.Fatal("host approval changed retained inputs")
	}
}
