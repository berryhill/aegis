package api

import (
	"context"
	"encoding/json"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/reference"
	"github.com/berryhill/aegis/internal/registry"
	"github.com/labstack/echo/v5"
)

func nativeSetupFixture(t *testing.T) (*app.Service, core.Subject, core.Charter, core.CanonicalCharter, app.DoerDraft) {
	t.Helper()
	s := apiService(t)
	configureAPIFleet(t, s)
	ctx := context.Background()
	sub, err := s.AuthenticateUnixPeer(ctx, uint32(os.Getuid()))
	if err != nil {
		t.Fatal(err)
	}
	c := core.Charter{SchemaVersion: core.SchemaVersion, AgentID: "native-doer", Name: "Native Doer", Revision: 1, Runtime: core.RuntimeConstraint{Adapter: "hermes", Runtime: "hermes-agent", VersionConstraint: ">=0.18.0", Target: "aegis-owned-ephemeral"}, CreatedBy: sub.PrincipalID, CreatedAt: s.Now(), Stanzas: []core.TrustStanza{{ID: "principal", Name: "Principal", Enabled: true, Authentication: core.AuthenticationPolicy{Methods: []string{"local-os"}, Selectors: []core.IdentitySelector{{PrincipalIDs: []string{sub.PrincipalID}, Issuers: []string{"linux-so-peercred"}, Environments: []string{"local"}}}}, Grant: core.Grant{}, Scopes: core.Scopes{Credentials: []string{"provider:codex"}}, Session: core.SessionPolicy{MaximumLifetimeSec: 60}, Approval: core.ApprovalPolicy{MaximumLifetimeSec: 60, SingleUse: true}, InformationFlow: core.InformationFlowPolicy{CrossStanza: "deny"}, Hermes: core.HermesConfig{Model: "unchanged-model", Provider: "codex"}}}}
	wire, _ := json.Marshal(c)
	ch, err := s.ImportCharterAs(ctx, sub, wire)
	if err != nil {
		t.Fatal(err)
	}
	fixture, _ := json.Marshal(registry.CurrentFleetFixture{SchemaVersion: registry.CurrentFleetFixtureSchemaVersion, FleetID: "native-fleet", Agents: []registry.CurrentFleetAgent{{SourceID: "native-source", AgentID: c.AgentID, Runtime: registry.RuntimeBinding{Adapter: c.Runtime.Adapter, Runtime: c.Runtime.Runtime, Target: c.Runtime.Target}, Ownership: registry.Ownership{OwnerID: sub.PrincipalID, AccountabilityID: sub.PrincipalID}, Lifecycle: registry.LifecycleEnabled, Charter: reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: c.AgentID, Revision: 1, Digest: ch.Digest}}}})
	agent, _, err := s.RegisterFleetAgentAs(ctx, sub, app.NewRegisterFleetAgentInput(fixture, "native-fleet", "native-source"))
	if err != nil {
		t.Fatal(err)
	}
	text := "retained exact text"
	draft, err := s.SaveDoerDraftAs(ctx, sub, app.DoerDraftInput{Agent: reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: c.AgentID, Revision: agent.Revision.Revision, Digest: agent.Revision.Digest}, LoopID: "native-loop", Revision: 1, Contract: loop.DoerContract{Task: "Retain exact task", Workspace: t.TempDir(), WritableFiles: []string{"result.txt"}, VerifyFile: "result.txt", ExpectedText: &text, MaxAttempts: 2}})
	if err != nil {
		t.Fatal(err)
	}
	return s, sub, c, ch, draft
}

func TestDoerSetupNativeProposalExactDecision(t *testing.T) {
	s, sub, c, ch, draft := nativeSetupFixture(t)
	ctx := context.Background()
	// Shared typed-service denials are independent of browser receipt handling.
	proposal, err := s.ProposeDoerSetupSuccessorAs(ctx, sub, draft.ID, draft.Version)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"version", "principal", "body", "digest", "expired"} {
		t.Run(name, func(t *testing.T) {
			p, who := proposal, sub
			now := s.Now
			defer func() { s.Now = now }()
			switch name {
			case "version":
				p.DraftVersion++
			case "principal":
				who.PrincipalID = "foreign"
			case "body":
				p.Proposed.Charter.Name = "substitution"
			case "digest":
				p.Proposed.Digest = p.Original.Digest
			case "expired":
				s.Now = func() time.Time { return draft.ExpiresAt }
			}
			if _, err := s.ConfirmDoerSetupSuccessorAs(ctx, who, p); err == nil {
				t.Fatal("invalid decision accepted")
			}
			charters, _ := s.ListCharters(c.AgentID)
			if len(charters) != 1 {
				t.Fatal("denial imported charter")
			}
		})
	}
	// An independently imported conflicting revision must not be overwritten.
	occupied := proposal.Proposed.Charter
	occupied.Name = "independent conflicting draft"
	occupiedWire, _ := json.Marshal(occupied)
	if _, err := s.ImportCharterAs(ctx, sub, occupiedWire); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ConfirmDoerSetupSuccessorAs(ctx, sub, proposal); err == nil {
		t.Fatal("occupied revision accepted")
	}
	// Fresh review moves to the next unused canonical revision, not another
	// task or automatic decision. Same-draft inputs remain untouched.
	manager, cookie, csrf := consoleQueueRouteFixture(t, s)
	e := echo.New()
	registerDoerSetupRoutes(e, s, manager)
	post := func(action, receipt, origin string) *httptest.ResponseRecorder {
		v := url.Values{"csrf": {csrf}, "draft_id": {draft.ID}, "action": {action}}
		if receipt != "" {
			v.Set("receipt", receipt)
		}
		req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1/console/loops/doer/setup", strings.NewReader(v.Encode()))
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
			t.Fatalf("no receipt: %d %s", rr.Code, rr.Body.String())
		}
		return html.UnescapeString(m[1])
	}
	noEffects := func() {
		t.Helper()
		approvals, _ := s.ListApprovals()
		receipts, _ := s.ListReceipts()
		items, _ := s.FleetRepository.ListQueueItems(ctx)
		if len(approvals) != 0 || len(receipts) != 0 || len(items) != 0 {
			t.Fatal("proposal granted effects")
		}
		if _, err := os.Stat(draft.Contract.Workspace + "/result.txt"); !os.IsNotExist(err) {
			t.Fatal("host file effect")
		}
	}
	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/console/loops/doer/setup?draft_id="+draft.ID, nil)
	req.AddCookie(cookie)
	rr := httptest.NewRecorder()
	e.ServeHTTP(rr, req)
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), "propose-review") {
		t.Fatal("missing native proposal entry")
	}
	noEffects()
	charters, _ := s.ListCharters(c.AgentID)
	if len(charters) != 2 {
		t.Fatal("GET imported charter")
	}
	// The actual browser identity uses Aegis's principal mapping; no synthetic
	// prompt-selected identity is supplied to the proposal service.
	review := post("propose-review", "", "http://127.0.0.1")
	receipt := token(review)
	if !strings.Contains(review.Body.String(), "Original exact authority") || !strings.Contains(review.Body.String(), core.ControllerCodexAccessTokenV1) || !strings.Contains(review.Body.String(), "unchanged-model") {
		t.Fatal("incomplete exact proposal")
	}
	noEffects()
	charters, _ = s.ListCharters(c.AgentID)
	if len(charters) != 2 {
		t.Fatal("review imported charter")
	}
	if denied := post("confirm", receipt, "https://wrong.example"); denied.Code != 403 {
		t.Fatal("foreign origin accepted")
	}
	for _, invalid := range []string{"", "tampered"} {
		denied := post("confirm", invalid, "http://127.0.0.1")
		if strings.Contains(denied.Body.String(), "Exact proposed successor and same retained task read back") {
			t.Fatal("missing/tampered receipt accepted")
		}
		fresh, _ := s.ReadDoerDraftAs(ctx, sub, draft.ID)
		if !reflect.DeepEqual(fresh, draft) {
			t.Fatal("receipt denial mutated draft")
		}
	}
	confirmed := post("confirm", receipt, "http://127.0.0.1")
	if !strings.Contains(confirmed.Body.String(), "Exact proposed successor and same retained task read back") {
		t.Fatalf("confirm: %s", confirmed.Body.String())
	}
	got, err := s.ReadDoerDraftAs(ctx, sub, draft.ID)
	if err != nil || got.ID != draft.ID || got.Version != draft.Version+1 || !reflect.DeepEqual(got.Contract, draft.Contract) {
		t.Fatal("lost retained draft", err)
	}
	successor, err := s.GetFleetAgentAs(ctx, sub, c.AgentID, 0)
	if err != nil || successor.Revision.Charter.Revision != 3 {
		t.Fatal("missing successor", err)
	}
	next, err := s.GetCharter(c.AgentID, 3)
	if err != nil {
		t.Fatal(err)
	}
	stanza := next.Charter.Stanzas[0]
	if len(stanza.Grant.Tools) != 0 || len(stanza.Scopes.Credentials) != 0 || len(stanza.Scopes.Memory) != 0 || len(stanza.Hermes.Toolsets) != 0 || stanza.Hermes.Model != c.Stanzas[0].Hermes.Model || stanza.Hermes.Provider != c.Stanzas[0].Hermes.Provider || !reflect.DeepEqual(stanza.Authentication, c.Stanzas[0].Authentication) || next.Charter.Runtime != c.Runtime {
		t.Fatal("incorrect authority transformation")
	}
	noEffects()
	post("confirm", receipt, "http://127.0.0.1")
	latest, _ := s.GetFleetAgentAs(ctx, sub, c.AgentID, 0)
	if latest.Revision.Digest != successor.Revision.Digest {
		t.Fatal("receipt replay changed successor")
	}
	original, _ := s.GetCharter(c.AgentID, 1)
	if original.Digest != ch.Digest {
		t.Fatal("original modified")
	}
}
