package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/console"
	"github.com/labstack/echo/v5"
)

func TestPresentationFailureUsesSafeRecoveryNavigation(t *testing.T) {
	manager, err := console.New(console.Config{Origin: "http://127.0.0.1", SessionTTL: time.Minute, MaxPageSize: 100}, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	e := echo.New()
	registerPresentationRecovery(e, nil, manager)
	for _, tc := range []struct {
		name   string
		err    error
		status int
		title  string
	}{
		{"identity", app.ErrUnauthenticated, 401, "Sign-in required"},
		{"freshness", console.ErrReauthenticationRequired, 401, "Sign-in required"},
		{"ownership", app.ErrDenied, 403, "Action denied"},
		{"CAS", app.ErrConflict, 409, "The record or form changed"},
		{"invalid", console.ErrInvalidInput, 400, "Invalid action request"},
		{"unavailable", app.ErrFleetUnavailable, 503, "Service unavailable or completion unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1/console/queue/item/archive", strings.NewReader("invalid"))
			request.Header.Set("Accept", "text/html")
			redirect := httptest.NewRecorder()
			c := e.NewContext(request, redirect)
			if err := presentationFailure(c, manager, tc.err, "/console/queue"); err != nil {
				t.Fatal(err)
			}
			location := redirect.Header().Get("Location")
			if redirect.Code != 303 || !strings.HasPrefix(location, "/console/presentation-error?") {
				t.Fatalf("raw action stranded: %d %s", redirect.Code, location)
			}
			view := httptest.NewRecorder()
			e.ServeHTTP(view, httptest.NewRequest(http.MethodGet, "http://127.0.0.1"+location, nil))
			if view.Code != tc.status || !strings.Contains(view.Body.String(), tc.title) || !strings.Contains(view.Body.String(), `href="/console/queue"`) {
				t.Fatalf("wrong recovery: %d %s", view.Code, view.Body.String())
			}
			if tc.status >= 500 && (!strings.Contains(view.Body.String(), "unknown") || strings.Contains(view.Body.String(), "Nothing was applied")) {
				t.Fatal("uncertain completion relabelled as no effect")
			}
		})
	}
	called := false
	handler := presentationHandler(nil, manager, "/console/loops", func(*echo.Context) error { called = true; return nil })
	request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1/console/loops/availability", strings.NewReader("{}"))
	request.Header.Set("Content-Type", "application/json")
	err = handler(e.NewContext(request, httptest.NewRecorder()))
	status, _, _ := classifyError(err)
	if called || status != 400 {
		t.Fatal("invalid media reached effects", called, status, err)
	}
}
