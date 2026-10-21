package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/core"
	"github.com/labstack/echo/v5"
)

func queuePresentationHTTPFixture(t *testing.T) (*app.Service, core.Subject) {
	t.Helper()
	svc := apiService(t)
	configureAPIFleet(t, svc)
	now := svc.Now()
	return svc, core.Subject{ID: "http-principal", PrincipalID: svc.Config.Principal.ID, Method: "password", AuthenticatedAt: now, ExpiresAt: now.Add(time.Hour)}
}

func queuePresentationHTTPRequest(svc *app.Service, subject *core.Subject, method, path, body string) *httptest.ResponseRecorder {
	e := echo.New()
	e.HTTPErrorHandler = func(c *echo.Context, err error) {
		status, _, _ := classifyError(err)
		_ = c.NoContent(status)
	}
	g := e.Group("/v1")
	if subject != nil {
		g.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
			return func(c *echo.Context) error { c.Set("subject", *subject); return next(c) }
		})
	}
	registerQueuePresentationHTTP(g, svc)
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

func TestQueuePresentationHTTPStrictDecoding(t *testing.T) {
	svc, sub := queuePresentationHTTPFixture(t)
	ref := `{"schema_version":"aegis.reference.digest.v1","id":"item","digest":"sha256:` + strings.Repeat("a", 64) + `"}`
	for _, action := range []string{"archive", "unarchive", "requeue"} {
		body := `{"queue_item":` + ref + `,"archived":true}`
		if action == "unarchive" {
			body = `{"queue_item":` + ref + `,"archived":false}`
		}
		if action == "requeue" {
			body = `{"queue_item":` + ref + `,"idempotency_key":"new-intent"}`
		}
		for name, bad := range map[string]string{
			"identity":  strings.TrimSuffix(body, "}") + `,"subject":{"id":"forged"}}`,
			"authority": strings.TrimSuffix(body, "}") + `,"authority":"forged"}`,
			"malformed": `{`, "trailing": body + ` {}`, "path": strings.Replace(body, `"id":"item"`, `"id":"other"`, 1),
		} {
			t.Run(action+"/"+name, func(t *testing.T) {
				if rec := queuePresentationHTTPRequest(svc, &sub, http.MethodPost, "/v1/queue/item/"+action, bad); rec.Code != http.StatusBadRequest {
					t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
				}
			})
		}
	}
	for _, body := range []string{`{`, `{} {}`, `{"principal_id":"forged"}`, `{"authority":"forged"}`} {
		if rec := queuePresentationHTTPRequest(svc, &sub, http.MethodPut, "/v1/queue/preferences", body); rec.Code != http.StatusBadRequest {
			t.Fatalf("preferences strict status=%d", rec.Code)
		}
	}
	for _, action := range []string{"archive", "unarchive"} {
		archived := "false"
		if action == "unarchive" {
			archived = "true"
		}
		if rec := queuePresentationHTTPRequest(svc, &sub, http.MethodPost, "/v1/queue/item/"+action, `{"queue_item":`+ref+`,"archived":`+archived+`}`); rec.Code != http.StatusBadRequest {
			t.Fatalf("conflicting operation status=%d", rec.Code)
		}
	}
}

func TestQueuePresentationHTTPPreferencesPersistence(t *testing.T) {
	svc, sub := queuePresentationHTTPFixture(t)
	path := "/v1/queue/preferences"
	rec := queuePresentationHTTPRequest(svc, &sub, http.MethodGet, path, "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"revision":0`) {
		t.Fatalf("defaults: %d %s", rec.Code, rec.Body.String())
	}
	body := `{"request_id":"http-preferences","selection":"all","visibility":"archived","expected_revision":0}`
	rec = queuePresentationHTTPRequest(svc, &sub, http.MethodPut, path, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("save: %d %s", rec.Code, rec.Body.String())
	}
	saved := rec.Body.String()
	rec = queuePresentationHTTPRequest(svc, &sub, http.MethodPut, path, body)
	if rec.Code != http.StatusOK || rec.Body.String() != saved {
		t.Fatalf("replay: %d %s", rec.Code, rec.Body.String())
	}
	rec = queuePresentationHTTPRequest(svc, &sub, http.MethodGet, path, "")
	if rec.Code != http.StatusOK || rec.Body.String() != saved {
		t.Fatalf("readback: %d %s", rec.Code, rec.Body.String())
	}
	var got struct {
		PrincipalID string `json:"principal_id"`
		Revision    uint64 `json:"revision"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || got.PrincipalID != sub.PrincipalID || got.Revision != 1 {
		t.Fatalf("persisted principal/revision: %+v %v", got, err)
	}
}

func TestQueueRequeueHTTPFreshAuthority(t *testing.T) {
	svc, sub := queuePresentationHTTPFixture(t)
	sub.AuthenticatedAt = svc.Now().Add(-2 * svc.Config.Principal.AuthTTL)
	body := `{"queue_item":{"schema_version":"aegis.reference.digest.v1","id":"item","digest":"sha256:` + strings.Repeat("a", 64) + `"},"idempotency_key":"new-intent"}`
	rec := queuePresentationHTTPRequest(svc, &sub, http.MethodPost, "/v1/queue/item/requeue", body)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("stale authority: %d %s", rec.Code, rec.Body.String())
	}
	items, err := svc.FleetRepository.ListQueueItems(context.Background())
	if err != nil || len(items) != 0 {
		t.Fatalf("denial created work: %v %v", items, err)
	}
}

func TestQueuePresentationHTTPUnauthenticated(t *testing.T) {
	svc := apiService(t)
	configureAPIFleet(t, svc)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, svc) }()
	defer func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	}()
	waitFor(t, "unix", svc.Config.API.UnixSocket)
	client := unixClient(svc.Config.API.UnixSocket)
	for _, route := range []struct{ method, path string }{
		{http.MethodPost, "/v1/queue/item/archive"}, {http.MethodPost, "/v1/queue/item/unarchive"}, {http.MethodPost, "/v1/queue/item/requeue"}, {http.MethodGet, "/v1/queue/preferences"}, {http.MethodPut, "/v1/queue/preferences"},
	} {
		req, err := http.NewRequest(route.method, "http://unix"+route.path, nil)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("%s %s: %d", route.method, route.path, resp.StatusCode)
		}
	}
}
