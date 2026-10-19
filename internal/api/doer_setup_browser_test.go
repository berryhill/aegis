package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/berryhill/aegis/internal/app"
	"github.com/labstack/echo/v5"
)

func TestDoerSetupBrowserHeadlessExactReadback(t *testing.T) {
	t.Setenv("DISPLAY", "")
	t.Setenv("WAYLAND_DISPLAY", "")
	s, sub, _, _, task := nativeSetupFixture(t)
	e := echo.New()
	who := sub
	g := e.Group("/v1", func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error { c.Set("subject", who); return next(c) }
	})
	registerDoerSetupServiceRoutes(g, s)
	call := func(in app.DoerSetupReviewInput) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(in)
		req := httptest.NewRequest(http.MethodPost, "/v1/loops/doer/drafts/"+task.ID+"/setup-browser", strings.NewReader(string(raw)))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		e.ServeHTTP(rr, req)
		return rr
	}
	input := app.DoerSetupReviewInput{ID: task.ID, ExpectedVersion: task.Version}
	for i := 0; i < 2; i++ {
		rr := call(input)
		var got struct {
			Task            app.DoerDraft `json:"task"`
			ConfirmationURL string        `json:"confirmation_url"`
			Status          string        `json:"status"`
		}
		if rr.Code != 200 || json.Unmarshal(rr.Body.Bytes(), &got) != nil || !reflect.DeepEqual(got.Task, task) || got.Status != "independent_setup_required" || got.ConfirmationURL != "/console/loops/doer/setup?draft_id="+task.ID {
			t.Fatalf("handoff: %d %s", rr.Code, rr.Body.String())
		}
	}
	stale := input
	stale.ExpectedVersion++
	if rr := call(stale); rr.Code < 400 {
		t.Fatal("stale version accepted")
	}
	who.PrincipalID = "foreign"
	if rr := call(input); rr.Code < 400 {
		t.Fatal("foreign principal accepted")
	}
	got, err := s.ReadDoerDraftAs(context.Background(), sub, task.ID)
	if err != nil || !reflect.DeepEqual(got, task) {
		t.Fatal("handoff changed retained task", err)
	}
	approvals, _ := s.ListApprovals()
	receipts, _ := s.ListReceipts()
	items, _ := s.FleetRepository.ListQueueItems(context.Background())
	if len(approvals) != 0 || len(receipts) != 0 || len(items) != 0 {
		t.Fatal("read-only handoff applied effects")
	}
}
