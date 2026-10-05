package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/berryhill/aegis/internal/app"
	"github.com/labstack/echo/v5"
)

func TestDoerServiceReadinessAdapter(t *testing.T) {
	s, subject, input := candidateReadinessFixture(t, "none")
	e := echo.New()
	g := e.Group("/v1", func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error { c.Set("subject", subject); return next(c) }
	})
	registerDoerServiceRoutes(g, s)
	want, err := s.ReadDoerCandidateReadinessAs(context.Background(), subject, input)
	if err != nil {
		t.Fatal(err)
	}
	for _, probe := range []bool{false, true} {
		body, _ := json.Marshal(map[string]any{"agent": input.Agent, "candidate": input.Candidate, "probe": probe})
		req := httptest.NewRequest(http.MethodPost, "/v1/loops/doer/readiness", strings.NewReader(string(body)))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		e.ServeHTTP(rr, req)
		var got app.DoerCandidateReadiness
		if rr.Code != 200 || json.Unmarshal(rr.Body.Bytes(), &got) != nil || got.Reason != want.Reason || got.Agent != want.Agent {
			t.Fatalf("readiness: %d %s", rr.Code, rr.Body.String())
		}
	}
}

func TestDoerServiceStrictInputsAndAuthentication(t *testing.T) {
	s := apiService(t)
	for _, authenticated := range []bool{false, true} {
		e := echo.New()
		g := e.Group("/v1")
		if authenticated {
			subject, err := s.Authenticate(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			g.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
				return func(c *echo.Context) error { c.Set("subject", subject); return next(c) }
			})
		}
		registerDoerServiceRoutes(g, s)
		for _, path := range []string{"/v1/loops/doer/readiness", "/v1/loops/doer/drafts", "/v1/loops/doer/drafts/example/continue"} {
			req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"approval":true}`))
			req.Header.Set("Content-Type", "application/json")
			rr := httptest.NewRecorder()
			e.ServeHTTP(rr, req)
			if authenticated && rr.Code != 400 {
				t.Fatalf("strict decode: %d %s", rr.Code, rr.Body.String())
			}
			if !authenticated && rr.Code == 404 {
				t.Fatalf("route missing: %s", path)
			}
			if !authenticated && rr.Code < 400 {
				t.Fatal("unauthenticated request admitted")
			}
		}
	}
}
