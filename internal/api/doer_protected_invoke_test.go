package api

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/berryhill/aegis/internal/app"
	"github.com/labstack/echo/v5"
)

// The launcher is inert. These tests never simulate a human decision into a
// real approval service; a forged helper success must fail canonical readback.
func TestDoerProtectedInvokeCannotSupplyHumanDecision(t *testing.T) {
	s, sub, _, _, draft := nativeSetupFixture(t)
	e := echo.New()
	g := e.Group("/v1")
	g.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error { c.Set("subject", sub); return next(c) }
	})
	calls := 0
	status := "cancelled"
	registerDoerProtectedInvokeRoutes(g, s, func(ctx context.Context, in app.DoerProtectedSetupInput) string {
		calls++
		if in.ID != draft.ID || in.ExpectedVersion != draft.Version || in.Action != "successor" {
			t.Error("invocation drift")
		}
		return status
	})
	call := func(body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/v1/loops/doer/drafts/"+draft.ID+"/setup-protected", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		e.ServeHTTP(w, r)
		return w
	}
	raw, _ := json.Marshal(app.DoerProtectedSetupInput{ID: draft.ID, ExpectedVersion: draft.Version, Action: "successor"})
	for _, field := range []string{`"password":"synthetic-password"`, `"decision":"approve"`, `"receipt":"synthetic-receipt"`, `"proposal":{}`} {
		body := strings.TrimSuffix(string(raw), "}") + "," + field + "}"
		if w := call(body); w.Code < 400 {
			t.Fatal("human material accepted", field, w.Code)
		}
	}
	if calls != 0 {
		t.Fatal("invalid input launched helper")
	}
	w := call(string(raw))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"status":"cancelled"`) || calls != 1 {
		t.Fatal("cancellation lost", w.Code, w.Body.String())
	}
	status = "approved"
	if w := call(string(raw)); w.Code < 400 {
		t.Fatal("helper exit text promoted to approval")
	}
	got, err := s.ReadDoerDraftAs(context.Background(), sub, draft.ID)
	if err != nil || !reflect.DeepEqual(got, draft) {
		t.Fatal("inert launch changed task", err)
	}
	approvals, _ := s.ListApprovals()
	receipts, _ := s.ListReceipts()
	if len(approvals) != 0 || len(receipts) != 0 {
		t.Fatal("invocation manufactured approval")
	}
	wrong := app.DoerProtectedSetupInput{ID: draft.ID, ExpectedVersion: draft.Version + 1, Action: "successor"}
	raw, _ = json.Marshal(wrong)
	prior := calls
	if w := call(string(raw)); w.Code < 400 || calls != prior {
		t.Fatal("stale draft launched")
	}
}

func TestDoerProtectedInvokeRequiresAuthenticatedPrincipal(t *testing.T) {
	s, _, _, _, draft := nativeSetupFixture(t)
	e := echo.New()
	calls := 0
	registerDoerProtectedInvokeRoutes(e.Group("/v1"), s, func(context.Context, app.DoerProtectedSetupInput) string { calls++; return "approved" })
	raw, _ := json.Marshal(app.DoerProtectedSetupInput{ID: draft.ID, ExpectedVersion: draft.Version, Action: "host"})
	r := httptest.NewRequest("POST", "/v1/loops/doer/drafts/"+draft.ID+"/setup-protected", strings.NewReader(string(raw)))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	e.ServeHTTP(w, r)
	if w.Code < 400 || calls != 0 {
		t.Fatal("unauthenticated caller launched protected approval")
	}
}
