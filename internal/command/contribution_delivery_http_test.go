package command

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/api"
	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/delivery"
	"github.com/berryhill/aegis/internal/managergateway"
	"github.com/berryhill/aegis/internal/slash"
)

// This client uses the production Serve listener, middleware and manager routes.
// It cannot install a subject extractor or inject an authenticated Subject.
type contributionHTTPFixture struct {
	client *http.Client
	token  string
	stop   func()
	origin string
}

func startContributionHTTPFixture(t *testing.T, s *app.Service) *contributionHTTPFixture {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- api.Serve(ctx, s) }()
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", s.Config.API.UnixSocket)
	}}
	client := &http.Client{Transport: transport, Timeout: 3 * time.Minute}
	var once sync.Once
	stop := func() {
		once.Do(func() {
			cancel()
			transport.CloseIdleConnections()
			select {
			case err := <-done:
				if err != nil {
					t.Error(err)
				}
			case <-time.After(15 * time.Second):
				t.Error("Serve did not stop")
			}
		})
	}
	t.Cleanup(stop)
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case err := <-done:
			t.Fatalf("Serve startup: %v", err)
		case <-deadline.C:
			t.Fatal("Serve readiness timeout")
		case <-tick.C:
			r, e := client.Get("http://unix/livez")
			if e == nil {
				r.Body.Close()
				if r.StatusCode == 200 {
					return &contributionHTTPFixture{client: client, token: s.Config.API.Token, stop: stop, origin: s.Config.API.Console.Origin}
				}
			}
		}
	}
}
func (h *contributionHTTPFixture) call(ctx context.Context, path, token string, input any, out any) error {
	wire, e := json.Marshal(input)
	if e != nil {
		return e
	}
	r, e := http.NewRequestWithContext(ctx, http.MethodPost, "http://unix"+path, bytes.NewReader(wire))
	if e != nil {
		return e
	}
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer "+h.token)
	r.Header.Set(managergateway.SessionHeader, token)
	response, e := h.client.Do(r)
	if e != nil {
		return e
	}
	defer response.Body.Close()
	b, e := io.ReadAll(response.Body)
	if e != nil {
		return e
	}
	if response.StatusCode != 200 && response.StatusCode != 201 {
		return fmt.Errorf("production route %s: status=%d %s", path, response.StatusCode, b)
	}
	return json.Unmarshal(b, out)
}
func (h *contributionHTTPFixture) Open(ctx context.Context, _ core.Subject) (out managergateway.Opened, err error) {
	err = h.call(ctx, "/v1/manager/sessions", "", struct{}{}, &out)
	return
}
func (h *contributionHTTPFixture) Execute(ctx context.Context, _ core.Subject, id, token, input string) (out slash.Result, err error) {
	err = h.call(ctx, "/v1/manager/sessions/"+id+"/commands", token, map[string]string{"input": input}, &out)
	// Keep the fixture's existing typed-result assertions after real HTTP decoding.
	if out.Data != nil && out.Data["delivery"] != nil {
		b, e := json.Marshal(out.Data["delivery"])
		if e != nil {
			return out, e
		}
		var d delivery.DeliveryResult
		if e = json.Unmarshal(b, &d); e != nil {
			return out, e
		}
		out.Data["delivery"] = &d
	}
	return
}

// Exact denials precede unrelated traffic so a rate limit cannot mask auth.
func assertContributionHTTPDenials(t *testing.T, h *contributionHTTPFixture) {
	t.Helper()
	for _, bearer := range []string{"", "wrong-bearer"} {
		r, _ := http.NewRequest("POST", "http://unix/v1/manager/sessions", strings.NewReader("{}"))
		r.Header.Set("Content-Type", "application/json")
		if bearer != "" {
			r.Header.Set("Authorization", "Bearer "+bearer)
		}
		response, err := h.client.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != 401 {
			t.Fatalf("bearer denial status=%d", response.StatusCode)
		}
	}
	if origin := h.origin; !strings.HasSuffix(origin, ":0") {
		request, _ := http.NewRequest("POST", origin+"/v1/manager/sessions", strings.NewReader("{}"))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Authorization", "Bearer "+h.token)
		response, err := (&http.Client{Timeout: 5 * time.Second}).Do(request)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != 401 {
			t.Fatalf("TCP bearer without peer status=%d", response.StatusCode)
		}
	}
	opened, err := h.Open(context.Background(), core.Subject{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = h.Execute(context.Background(), core.Subject{}, opened.ID, "foreign-session-token", "/contribute run https://github.com/fixture/controlled/issues/1"); err == nil {
		t.Fatal("foreign session token admitted")
	}
}

func assertLivePasswordNativeDenial(t *testing.T, h *contributionHTTPFixture, s *app.Service) {
	t.Helper()
	client := *h.client
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	r, _ := http.NewRequest("POST", s.Config.API.Console.Origin+"/console/login", strings.NewReader(url.Values{"password": {"fixture-password"}}.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Origin", s.Config.API.Console.Origin)
	response, err := client.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != 303 {
		t.Fatalf("live login status=%d", response.StatusCode)
	}
	events, err := s.Store.AuditEvents()
	if err != nil {
		t.Fatal(err)
	}
	var passwordSubject core.Subject
	for _, event := range events {
		if event.Type == "console_session" && event.Reason == "principal_password_authenticated" {
			passwordSubject = core.Subject{ID: event.SubjectID, PrincipalID: s.Config.Principal.ID, Kind: "principal", Issuer: "aegis-principal-auth", Method: "password", AuthenticatedAt: s.Now(), ExpiresAt: s.Now().Add(s.Config.API.Console.SessionTTL)}
		}
	}
	if passwordSubject.ID == "" || s.DashboardActionAuthentication(passwordSubject) != nil {
		t.Fatal("live login registry proof missing")
	}
	if _, decision, err := s.PreviewSessionAs(context.Background(), passwordSubject, "controller", 1, "", core.Environment{Name: "local"}); err == nil || decision.Reason != "zero_authorized_matches" {
		t.Fatalf("password acquired native charter: %v %+v", err, decision)
	}
	logout, _ := http.NewRequest("DELETE", s.Config.API.Console.Origin+"/console/session", nil)
	logout.Header.Set("Origin", s.Config.API.Console.Origin)
	logout.Header.Set("X-CSRF-Token", response.Header.Get("X-Aegis-Session-CSRF"))
	for _, cookie := range response.Cookies() {
		logout.AddCookie(cookie)
	}
	revoked, err := client.Do(logout)
	if err != nil {
		t.Fatal(err)
	}
	revoked.Body.Close()
	if revoked.StatusCode != 204 {
		t.Fatalf("logout status=%d", revoked.StatusCode)
	}
	if s.DashboardActionAuthentication(passwordSubject) == nil {
		t.Fatal("revoked live password remained valid")
	}
}
