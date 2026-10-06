package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/core"
	"github.com/labstack/echo/v5"
)

func TestDoerSetupTypedReviewDeniesAgentApproval(t *testing.T) {
	s, sub, _, _, draft := nativeSetupFixture(t)
	e := echo.New()
	who := sub
	g := e.Group("/v1", func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error { c.Set("subject", who); return next(c) }
	})
	registerDoerServiceRoutes(g, s)
	base := "/v1/loops/doer/drafts/" + draft.ID
	call := func(path string, in any) *httptest.ResponseRecorder {
		t.Helper()
		raw, _ := json.Marshal(in)
		req := httptest.NewRequest(http.MethodPost, base+path, strings.NewReader(string(raw)))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		e.ServeHTTP(rr, req)
		return rr
	}
	read := func() app.DoerSetupReview {
		t.Helper()
		rr := call("/setup-review", app.DoerSetupReviewInput{ID: draft.ID, ExpectedVersion: draft.Version})
		var v app.DoerSetupReview
		if rr.Code != 200 || json.Unmarshal(rr.Body.Bytes(), &v) != nil || v.Receipt == "" || v.ConfirmationURL != "/console/loops/doer/setup?draft_id="+draft.ID {
			t.Fatalf("review: %d %s", rr.Code, rr.Body.String())
		}
		return v
	}
	unchanged := func() {
		t.Helper()
		d, err := s.ReadDoerDraftAs(context.Background(), sub, draft.ID)
		agent, agentErr := s.GetFleetAgentAs(context.Background(), sub, draft.Agent.ID, 0)
		if agentErr != nil || agent.Revision.Revision != draft.Agent.Revision || agent.Revision.Digest != draft.Agent.Digest {
			t.Fatal("review/denial changed Agent", agentErr)
		}
		charters, _ := s.ListCharters(draft.Agent.ID)
		if err != nil || !reflect.DeepEqual(d, draft) || len(charters) != 1 {
			t.Fatal("denial/review mutated state", err)
		}
	}
	review := read()
	unchanged()
	decision := app.DoerSetupDecisionInput{ID: draft.ID, Receipt: review.Receipt, Decision: "approve-successor"}
	for _, token := range []string{"", "tampered"} {
		invalid := decision
		invalid.Receipt = token
		if rr := call("/setup-decision", invalid); rr.Code < 400 {
			t.Fatal("missing/tampered receipt accepted")
		}
		unchanged()
	}
	who.PrincipalID = "foreign"
	if rr := call("/setup-decision", decision); rr.Code < 400 {
		t.Fatal("foreign principal accepted")
	}
	who = sub
	unchanged()
	// Strict decode denies a caller-edited candidate without consuming review.
	if rr := call("/setup-decision", map[string]any{"id": draft.ID, "receipt": review.Receipt, "decision": "approve-successor", "proposal": review.Proposal}); rr.Code != 400 {
		t.Fatal("candidate substitution accepted")
	}
	unchanged()
	// A genuinely fresh transport authentication may have a newer timestamp;
	// identity binding does not pretend the two authentications are one session.
	who.AuthenticatedAt = who.AuthenticatedAt.Add(time.Nanosecond)
	rr := call("/setup-decision", decision)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("agent approval must deny: %d %s", rr.Code, rr.Body.String())
	}
	unchanged()
	// Denied approval does not consume the safe rejection review.
	if rr := call("/setup-decision", app.DoerSetupDecisionInput{ID: draft.ID, Receipt: review.Receipt, Decision: "reject"}); rr.Code != 200 {
		t.Fatalf("reject after denied approval: %d %s", rr.Code, rr.Body.String())
	}
	unchanged()
	if rr := call("/setup-decision", decision); rr.Code < 400 {
		t.Fatal("receipt replay accepted")
	}
	approvals, _ := s.ListApprovals()
	receipts, _ := s.ListReceipts()
	items, _ := s.FleetRepository.ListQueueItems(context.Background())
	if len(approvals) != 0 || len(receipts) != 0 || len(items) != 0 {
		t.Fatal("setup executed runtime effects")
	}
}

func TestDoerSetupTypedExpiryAndReject(t *testing.T) {
	for _, mode := range []string{"expire", "reject"} {
		t.Run(mode, func(t *testing.T) {
			s, sub, _, _, draft := nativeSetupFixture(t)
			e := echo.New()
			g := e.Group("/v1", func(next echo.HandlerFunc) echo.HandlerFunc {
				return func(c *echo.Context) error { c.Set("subject", sub); return next(c) }
			})
			registerDoerServiceRoutes(g, s)
			call := func(suffix string, in any) *httptest.ResponseRecorder {
				raw, _ := json.Marshal(in)
				req := httptest.NewRequest("POST", "/v1/loops/doer/drafts/"+draft.ID+suffix, strings.NewReader(string(raw)))
				req.Header.Set("Content-Type", "application/json")
				rr := httptest.NewRecorder()
				e.ServeHTTP(rr, req)
				return rr
			}
			rr := call("/setup-review", app.DoerSetupReviewInput{ID: draft.ID, ExpectedVersion: draft.Version})
			var v app.DoerSetupReview
			if rr.Code != 200 || json.Unmarshal(rr.Body.Bytes(), &v) != nil {
				t.Fatal(rr.Body.String())
			}
			decision := "reject"
			if mode == "expire" {
				s.Now = func() time.Time { return v.ExpiresAt }
				decision = "approve-successor"
			}
			rr = call("/setup-decision", app.DoerSetupDecisionInput{ID: draft.ID, Receipt: v.Receipt, Decision: decision})
			if mode == "expire" && rr.Code < 400 || mode == "reject" && rr.Code != 200 {
				t.Fatalf("%d %s", rr.Code, rr.Body.String())
			}
			cs, _ := s.ListCharters(draft.Agent.ID)
			if len(cs) != 1 {
				t.Fatal("rejection/expiry imported charter")
			}
		})
	}
}

func TestDoerSetupReusesExactImportedAndPartialImport(t *testing.T) {
	s, sub, _, _, d := nativeSetupFixture(t)
	ctx := context.Background()
	p, err := s.ProposeDoerSetupSuccessorAs(ctx, sub, d.ID, d.Version)
	if err != nil {
		t.Fatal(err)
	}
	c := p.Proposed.Charter
	c.Revision = 4
	c.CreatedAt = c.CreatedAt.Add(time.Second)
	raw, _ := json.Marshal(c)
	imported, err := s.ImportCharterAs(ctx, sub, raw)
	if err != nil {
		t.Fatal(err)
	}
	reused, err := s.ProposeDoerSetupSuccessorAs(ctx, sub, d.ID, d.Version)
	if err != nil || reused.Proposed.Digest != imported.Digest {
		t.Fatal("failed exact reuse", err)
	}
	old, _ := s.ReadDoerDraftAs(ctx, sub, d.ID)
	if !reflect.DeepEqual(old, d) {
		t.Fatal("reuse automatically approved")
	}
	next, err := s.ConfirmDoerSetupSuccessorAs(ctx, sub, reused)
	if err != nil || next.Version != d.Version+1 {
		t.Fatal("partial-import recovery", err)
	}
	cs, _ := s.ListCharters(d.Agent.ID)
	if len(cs) != 2 {
		t.Fatal("created unnecessary successor")
	}
}

func TestDoerSetupRecoversExactApprovedPartialSuccessor(t *testing.T) {
	s, sub, _, _, d := nativeSetupFixture(t)
	ctx := context.Background()
	p, err := s.ProposeDoerSetupSuccessorAs(ctx, sub, d.ID, d.Version)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ImportCharterAs(ctx, sub, p.Proposed.Canonical); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ApproveAgentCharterAs(ctx, sub, d.Agent.ID, app.ApproveAgentCharterInput{Expected: d.Agent, Charter: app.RevisionReference(d.Agent.ID, p.Proposed.Charter.Revision, p.Proposed.Digest)}); err != nil {
		t.Fatal(err)
	}
	// Reconstruct after losing an adapter receipt or failing the final draft CAS.
	recovered, err := s.ProposeDoerSetupSuccessorAs(ctx, sub, d.ID, d.Version)
	if err != nil || recovered.Proposed.Digest != p.Proposed.Digest {
		t.Fatal("changed partial intent", err)
	}
	current, _ := s.ReadDoerDraftAs(ctx, sub, d.ID)
	if !reflect.DeepEqual(current, d) {
		t.Fatal("review rebound draft")
	}
	next, err := s.ConfirmDoerSetupSuccessorAs(ctx, sub, recovered)
	if err != nil || next.Version != d.Version+1 || !reflect.DeepEqual(next.Contract, d.Contract) {
		t.Fatal("failed exact recovery", err)
	}
	cs, _ := s.ListCharters(d.Agent.ID)
	if len(cs) != 2 {
		t.Fatal("recovery imported another successor")
	}
}

func TestDoerSetupNeverErasesUnrelatedAuthority(t *testing.T) {
	for _, field := range []string{"tools", "capabilities", "memory", "credentials", "ambiguous"} {
		t.Run(field, func(t *testing.T) {
			s, sub, c, _, d := nativeSetupFixture(t)
			ctx := context.Background()
			switch field {
			case "tools":
				c.Stanzas[0].Grant.Tools = []string{"terminal"}
				c.Stanzas[0].Hermes.Toolsets = []string{"terminal"}
			case "capabilities":
				c.Stanzas[0].Grant.Capabilities = []string{"design"}
			case "memory":
				c.Stanzas[0].Scopes.Memory = []string{"own"}
			case "credentials":
				c.Stanzas[0].Scopes.Credentials = []string{"provider:codex", "other"}
			case "ambiguous":
				other := c.Stanzas[0]
				other.ID = "other"
				other.Authentication.Selectors = []core.IdentitySelector{{PrincipalIDs: []string{"foreign"}, Issuers: []string{"linux-so-peercred"}, Environments: []string{"local"}}}
				c.Stanzas = append(c.Stanzas, other)
			}
			// Guard the shape through an exact fixture-only successor binding.
			var canonical core.CanonicalCharter
			var err error
			c.Revision = 2
			raw, _ := json.Marshal(c)
			canonical, err = s.ImportCharterAs(ctx, sub, raw)
			if err != nil {
				t.Fatal(err)
			}
			d, err = s.ApproveDoerDraftSuccessorAs(ctx, sub, d.ID, d.Version, app.ApproveAgentCharterInput{Expected: d.Agent, Charter: app.RevisionReference(c.AgentID, 2, canonical.Digest)})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = s.ProposeDoerSetupSuccessorAs(ctx, sub, d.ID, d.Version); err == nil {
				t.Fatal("unrelated authority erased")
			}
		})
	}
}
