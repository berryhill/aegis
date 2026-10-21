package api

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/berryhill/aegis/internal/app"
	"github.com/labstack/echo/v5"
)

// Extracting the production renderer must not turn genuine failures into
// authentication denials or expose internal error text to the browser.
func TestAPIHTTPErrorHandlerPreservesErrorMapping(t *testing.T) {
	svc := &app.Service{Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	for _, tc := range []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"denied", fmt.Errorf("%w: private detail", app.ErrDenied), http.StatusForbidden, "denied"},
		{"unauthenticated", app.ErrUnauthenticated, http.StatusUnauthorized, "unauthenticated"},
		{"unexpected", errors.New("private detail"), http.StatusInternalServerError, "internal_error"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := echo.New()
			e.HTTPErrorHandler = apiHTTPErrorHandler(svc, nil)
			e.GET("/error", func(c *echo.Context) error { return tc.err })
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/error", nil))
			if rec.Code != tc.status || !strings.Contains(rec.Body.String(), `"code":"`+tc.code+`"`) || strings.Contains(rec.Body.String(), "private detail") {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
		})
	}
}
