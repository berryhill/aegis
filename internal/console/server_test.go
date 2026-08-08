package console

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/core"
)

func TestAuthenticatedSessionExchangeCSRFAndExpiry(t *testing.T) {
	now := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	manager, err := New(Config{Origin: "https://console.example.test", SessionTTL: 2 * time.Minute, BootstrapTTL: 15 * time.Second, MaxPageSize: 100}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	subject := core.Subject{ID: "local-uid:1000", PrincipalID: "principal", AuthenticatedAt: now, ExpiresAt: now.Add(time.Minute)}
	bootstrap, err := manager.IssueBootstrap(subject)
	if err != nil {
		t.Fatal(err)
	}
	if bootstrap == "" {
		t.Fatal("empty bootstrap")
	}
	request := httptest.NewRequest(http.MethodPost, "https://console.example.test/console/session", strings.NewReader(`{"bootstrap":"`+bootstrap+`"}`))
	request.Header.Set("Origin", "https://console.example.test")
	recorder := httptest.NewRecorder()
	session, csrf, err := manager.Exchange(request, bootstrap)
	if err != nil {
		t.Fatal(err)
	}
	manager.SetCookie(recorder, session)
	cookie := recorder.Result().Cookies()[0]
	if !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteStrictMode || cookie.Path != "/console" || cookie.Domain != "" {
		t.Fatalf("unsafe cookie: %+v", cookie)
	}
	if _, _, err = manager.Exchange(request, bootstrap); err == nil {
		t.Fatal("bootstrap replay accepted")
	}
	authenticated := httptest.NewRequest(http.MethodGet, "https://console.example.test/console/api/state", nil)
	authenticated.AddCookie(cookie)
	got, err := manager.Authenticate(authenticated)
	if err != nil || got.PrincipalID != subject.PrincipalID {
		t.Fatalf("authentication subject=%+v err=%v", got, err)
	}
	mutation := httptest.NewRequest(http.MethodDelete, "https://console.example.test/console/session", nil)
	mutation.AddCookie(cookie)
	mutation.Header.Set("Origin", "https://console.example.test")
	mutation.Header.Set("X-CSRF-Token", csrf)
	if _, err = manager.AuthorizeMutation(mutation); err != nil {
		t.Fatalf("same-origin mutation denied: %v", err)
	}
	mutation.Header.Set("Origin", "https://attacker.example")
	if _, err = manager.AuthorizeMutation(mutation); err == nil {
		t.Fatal("cross-origin mutation accepted")
	}
	now = now.Add(2 * time.Minute)
	if _, err = manager.Authenticate(authenticated); err == nil {
		t.Fatal("expired session accepted")
	}
}

func TestSecurityHeadersOriginAndPaginationBounds(t *testing.T) {
	manager, err := New(Config{Origin: "http://127.0.0.1:8443", SessionTTL: time.Minute, BootstrapTTL: 10 * time.Second, MaxPageSize: 50}, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	manager.ApplySecurityHeaders(recorder.Header(), true)
	for key, want := range map[string]string{
		"Content-Security-Policy": "default-src 'none'",
		"X-Content-Type-Options":  "nosniff",
		"X-Frame-Options":         "DENY",
		"Referrer-Policy":         "no-referrer",
		"Cache-Control":           "no-store",
	} {
		if !strings.Contains(recorder.Header().Get(key), want) {
			t.Fatalf("%s=%q", key, recorder.Header().Get(key))
		}
	}
	if got, err := manager.Page("25"); err != nil || got != 25 {
		t.Fatalf("page=%d err=%v", got, err)
	}
	for _, value := range []string{"0", "51", "not-a-number"} {
		if _, err := manager.Page(value); err == nil {
			t.Fatalf("invalid page size %q accepted", value)
		}
	}
	if _, err := New(Config{Origin: "http://192.0.2.10:8443", SessionTTL: time.Minute, BootstrapTTL: time.Second, MaxPageSize: 10}, time.Now); err == nil {
		t.Fatal("plaintext non-loopback console origin accepted")
	}
}

func TestShellHasAccessibleStatesAndNoInlineExecutableContent(t *testing.T) {
	html := string(Shell())
	for _, required := range []string{"<nav", "<main", "aria-live", "id=\"authentication-status\"", "data-state=\"loading\"", "data-state=\"empty\"", "data-state=\"unavailable\"", "data-state=\"error\"", "list-detail", "inspector"} {
		if !strings.Contains(html, required) {
			t.Fatalf("shell missing %q", required)
		}
	}
	if !strings.Contains(string(Styles()), "prefers-reduced-motion") {
		t.Fatal("shell styles do not respect reduced motion")
	}
	if strings.Contains(html, "<script>") || strings.Contains(html, "localStorage") || strings.Contains(html, "sessionStorage") || strings.Contains(html, "transport-secret") {
		t.Fatal("shell contains inline executable or reusable browser storage material")
	}
}
