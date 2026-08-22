package console

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/core"
)

func TestBootstrapFormatClassificationAndOriginDenialDoesNotConsume(t *testing.T) {
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	manager, err := New(Config{Origin: "https://console.example.test", SessionTTL: 2 * time.Minute, BootstrapTTL: 15 * time.Second, MaxPageSize: 100}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	subject := core.Subject{ID: "local-uid:1000", PrincipalID: "principal", AuthenticatedAt: now, ExpiresAt: now.Add(time.Minute)}
	bootstrap, err := manager.IssueBootstrap(subject)
	if err != nil {
		t.Fatal(err)
	}
	if len(bootstrap) != 64 || strings.ToLower(bootstrap) != bootstrap {
		t.Fatal("issued bootstrap does not use strict lowercase hexadecimal format")
	}
	validRequest := httptest.NewRequest(http.MethodPost, "https://console.example.test/console/session", nil)
	validRequest.Header.Set("Origin", "https://console.example.test")
	for _, malformed := range []string{"", "abc", strings.Repeat("A", 64), strings.Repeat("g", 64), strings.Repeat("a", 63), strings.Repeat("a", 65)} {
		if _, _, _, exchangeErr := manager.Exchange(validRequest, malformed); !errors.Is(exchangeErr, ErrBootstrapInvalidFormat) {
			t.Fatalf("malformed bootstrap classified as %v", exchangeErr)
		}
	}
	unknown := strings.Repeat("a", 64)
	if _, _, _, exchangeErr := manager.Exchange(validRequest, unknown); !errors.Is(exchangeErr, ErrBootstrapConsumedOrExpired) {
		t.Fatalf("unknown valid bootstrap classified as %v", exchangeErr)
	}
	crossOrigin := httptest.NewRequest(http.MethodPost, "https://console.example.test/console/session", nil)
	crossOrigin.Header.Set("Origin", "https://attacker.example")
	if _, _, _, exchangeErr := manager.Exchange(crossOrigin, bootstrap); !errors.Is(exchangeErr, ErrDenied) {
		t.Fatalf("cross-origin bootstrap exchange classified as %v", exchangeErr)
	}
	wrongHost := httptest.NewRequest(http.MethodPost, "https://other.example.test/console/session", nil)
	wrongHost.Header.Set("Origin", "https://console.example.test")
	if _, _, _, exchangeErr := manager.Exchange(wrongHost, bootstrap); !errors.Is(exchangeErr, ErrDenied) {
		t.Fatalf("wrong-host bootstrap exchange classified as %v", exchangeErr)
	}
	if _, _, _, exchangeErr := manager.Exchange(validRequest, bootstrap); exchangeErr != nil {
		t.Fatalf("cross-origin or host denial consumed bootstrap: %v", exchangeErr)
	}
	if _, _, _, exchangeErr := manager.Exchange(validRequest, bootstrap); !errors.Is(exchangeErr, ErrBootstrapConsumedOrExpired) {
		t.Fatalf("replayed bootstrap classified as %v", exchangeErr)
	}
}

func TestBootstrapExpiryAndSubjectExpiryShareUnavailableClassification(t *testing.T) {
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	request := httptest.NewRequest(http.MethodPost, "https://console.example.test/console/session", nil)
	request.Header.Set("Origin", "https://console.example.test")

	bootstrapManager, err := New(Config{Origin: "https://console.example.test", SessionTTL: 2 * time.Minute, BootstrapTTL: 15 * time.Second, MaxPageSize: 100}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	bootstrap, err := bootstrapManager.IssueBootstrap(core.Subject{ID: "local-uid:1000", PrincipalID: "principal", AuthenticatedAt: now, ExpiresAt: now.Add(time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(16 * time.Second)
	if _, _, _, exchangeErr := bootstrapManager.Exchange(request, bootstrap); !errors.Is(exchangeErr, ErrBootstrapConsumedOrExpired) {
		t.Fatalf("expired bootstrap classified as %v", exchangeErr)
	}

	now = time.Date(2026, 8, 20, 12, 1, 0, 0, time.UTC)
	subjectManager, err := New(Config{Origin: "https://console.example.test", SessionTTL: 2 * time.Minute, BootstrapTTL: 15 * time.Second, MaxPageSize: 100}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	bootstrap, err = subjectManager.IssueBootstrap(core.Subject{ID: "local-uid:1000", PrincipalID: "principal", AuthenticatedAt: now, ExpiresAt: now.Add(5 * time.Second)})
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(6 * time.Second)
	if _, _, _, exchangeErr := subjectManager.Exchange(request, bootstrap); !errors.Is(exchangeErr, ErrBootstrapConsumedOrExpired) {
		t.Fatalf("subject-expired bootstrap classified as %v", exchangeErr)
	}
}

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
	session, csrf, expires, err := manager.Exchange(request, bootstrap)
	if err != nil {
		t.Fatal(err)
	}
	if !expires.Equal(subject.ExpiresAt) {
		t.Fatalf("session expiry=%s want subject cap=%s", expires, subject.ExpiresAt)
	}
	manager.SetCookie(recorder, session)
	cookie := recorder.Result().Cookies()[0]
	if !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteStrictMode || cookie.Path != "/console" || cookie.Domain != "" {
		t.Fatalf("unsafe cookie: %+v", cookie)
	}
	if _, _, _, err = manager.Exchange(request, bootstrap); err == nil {
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

func TestReviewReceiptIsSessionBoundSingleUseExpiringAndReplaced(t *testing.T) {
	now := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)
	manager, err := New(Config{Origin: "https://console.example.test", SessionTTL: 5 * time.Minute, BootstrapTTL: 15 * time.Second, MaxPageSize: 100}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	newSession := func(subjectExpiry time.Time) *http.Request {
		t.Helper()
		bootstrap, issueErr := manager.IssueBootstrap(core.Subject{ID: "local", PrincipalID: "principal", ExpiresAt: subjectExpiry})
		if issueErr != nil {
			t.Fatal(issueErr)
		}
		exchange := httptest.NewRequest(http.MethodPost, "https://console.example.test/console/session", nil)
		exchange.Header.Set("Origin", "https://console.example.test")
		sessionValue, _, _, exchangeErr := manager.Exchange(exchange, bootstrap)
		if exchangeErr != nil {
			t.Fatal(exchangeErr)
		}
		request := httptest.NewRequest(http.MethodPost, "https://console.example.test/console/agents/registration/execute", nil)
		request.AddCookie(&http.Cookie{Name: CookieName, Value: sessionValue})
		return request
	}

	firstSession := newSession(now.Add(90 * time.Second))
	otherSession := newSession(now.Add(5 * time.Minute))
	first, err := manager.IssueReviewReceipt(firstSession, "agent-registration", []byte("first"))
	if err != nil || len(first) != 64 || strings.ToLower(first) != first {
		t.Fatalf("issued receipt=%q err=%v", first, err)
	}
	if _, err = manager.ConsumeReviewReceipt(otherSession, "agent-registration", first); !errors.Is(err, ErrReviewReceiptUnavailable) {
		t.Fatalf("cross-session receipt classified as %v", err)
	}
	if _, err = manager.ConsumeReviewReceipt(firstSession, "other-purpose", first); !errors.Is(err, ErrReviewReceiptUnavailable) {
		t.Fatalf("wrong-purpose receipt classified as %v", err)
	}
	second, err := manager.IssueReviewReceipt(firstSession, "agent-registration", []byte("second"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = manager.ConsumeReviewReceipt(firstSession, "agent-registration", first); !errors.Is(err, ErrReviewReceiptUnavailable) {
		t.Fatalf("replaced receipt classified as %v", err)
	}
	payload, err := manager.ConsumeReviewReceipt(firstSession, "agent-registration", second)
	if err != nil || string(payload) != "second" {
		t.Fatalf("consumed payload=%q err=%v", payload, err)
	}
	if _, err = manager.ConsumeReviewReceipt(firstSession, "agent-registration", second); !errors.Is(err, ErrReviewReceiptUnavailable) {
		t.Fatalf("replayed receipt classified as %v", err)
	}

	expiringSession := newSession(now.Add(30 * time.Second))
	expiring, err := manager.IssueReviewReceipt(expiringSession, "agent-registration", []byte("expires"))
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(30 * time.Second)
	if _, err = manager.ConsumeReviewReceipt(expiringSession, "agent-registration", expiring); !errors.Is(err, ErrReviewReceiptUnavailable) {
		t.Fatalf("expired receipt classified as %v", err)
	}
	for _, malformed := range []string{"", "abc", strings.Repeat("A", 64), strings.Repeat("g", 64), strings.Repeat("a", 63), strings.Repeat("a", 65)} {
		if _, err = manager.ConsumeReviewReceipt(otherSession, "agent-registration", malformed); !errors.Is(err, ErrReviewReceiptInvalidFormat) {
			t.Fatalf("malformed receipt %q classified as %v", malformed, err)
		}
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
		"Referrer-Policy":         "strict-origin",
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

func TestEmbeddedDatastarAndStylesAreSelfContained(t *testing.T) {
	if !strings.Contains(string(Styles()), "prefers-reduced-motion") {
		t.Fatal("shell styles do not respect reduced motion")
	}
	asset := string(Datastar())
	if len(asset) < 1000 || strings.Contains(asset, "localStorage") || strings.Contains(asset, "sessionStorage") {
		t.Fatal("embedded Datastar asset is absent or violates browser custody policy")
	}
}
