package console

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/principalauth"
)

// A shorter explicit browser lifetime must win even while principal freshness
// remains valid. No admission path or pending review may extend that identity.
func TestShortBrowserLifetimeExpiresBeforePrincipalFreshness(t *testing.T) {
	start := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	now := start
	verifier, err := principalauth.Enroll("principal", []byte("principal-password-canary"))
	if err != nil {
		t.Fatal(err)
	}
	manager, err := New(Config{Origin: "https://console.example.test", SessionTTL: 5 * time.Minute, MaxPageSize: 100, PrincipalID: "principal", PrincipalAuthTTL: 15 * time.Minute, PasswordVerifier: &verifier, LoginBurst: 3, LoginWindow: time.Minute}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "https://console.example.test/console/login", nil)
	request.Header.Set("Origin", "https://console.example.test")
	value, csrf, expires, _, err := manager.Login(request, "client", []byte("principal-password-canary"))
	if err != nil {
		t.Fatal(err)
	}
	if !expires.Equal(start.Add(5 * time.Minute)) {
		t.Fatal("explicit shorter expiry changed")
	}
	response := httptest.NewRecorder()
	manager.SetCookieUntil(response, value, expires)
	request.AddCookie(response.Result().Cookies()[0])
	request.Header.Set("X-CSRF-Token", csrf)
	now = expires.Add(-time.Nanosecond)
	if subject, err := manager.AuthorizeMutation(request); err != nil || !subject.ExpiresAt.Equal(expires) {
		t.Fatalf("fresh authority exceeded browser expiry: %v", err)
	}
	receipt, err := manager.IssueReviewReceipt(request, "test", []byte("metadata"))
	if err != nil {
		t.Fatal(err)
	}
	now = expires
	if _, err := manager.Authenticate(request); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("expired read: %v", err)
	}
	if _, err := manager.AuthorizeMutation(request); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("expired mutation: %v", err)
	}
	if _, err := manager.AuthorizeSessionMutation(request); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("expired logout: %v", err)
	}
	if _, err := manager.ConsumeReviewReceipt(request, "test", receipt); err == nil {
		t.Fatal("receipt survived browser expiry")
	}
}

// Browser identity remains usable without extending sensitive-action freshness.
func TestOneHourBrowserIdentityAndIndependentFreshness(t *testing.T) {
	start := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	now := start
	verifier, err := principalauth.Enroll("principal", []byte("principal-password-canary"))
	if err != nil {
		t.Fatal(err)
	}
	manager, err := New(Config{Origin: "https://console.example.test", SessionTTL: time.Hour, MaxPageSize: 100, PrincipalID: "principal", PrincipalAuthTTL: 15 * time.Minute, PasswordVerifier: &verifier, LoginBurst: 3, LoginWindow: time.Minute}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "https://console.example.test/console/login", nil)
	request.Header.Set("Origin", "https://console.example.test")
	value, csrf, expires, subject, err := manager.Login(request, "client", []byte("principal-password-canary"))
	if err != nil {
		t.Fatal(err)
	}
	if !expires.Equal(start.Add(time.Hour)) || !subject.ExpiresAt.Equal(expires) {
		t.Fatal("browser identity lifetime was clamped")
	}
	response := httptest.NewRecorder()
	manager.SetCookieUntil(response, value, expires)
	cookies := response.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].Expires.Equal(expires) || cookies[0].MaxAge != 3600 || !cookies[0].HttpOnly || !cookies[0].Secure {
		t.Fatal("incorrect session cookie bounds or protection")
	}
	request.AddCookie(cookies[0])
	request.Header.Set("X-CSRF-Token", csrf)
	for _, elapsed := range []time.Duration{5*time.Minute + time.Second, 15 * time.Minute, 16 * time.Minute, time.Hour - time.Nanosecond} {
		now = start.Add(elapsed)
		identity, err := manager.Authenticate(request)
		if err != nil || !identity.ExpiresAt.Equal(expires) {
			t.Fatalf("read at %s failed or renewed identity: %v", elapsed, err)
		}
		mutation, err := manager.AuthorizeMutation(request)
		if elapsed < 15*time.Minute {
			if err != nil || !mutation.ExpiresAt.Equal(start.Add(15*time.Minute)) {
				t.Fatalf("fresh mutation bounds: %v", err)
			}
		} else if !errors.Is(err, ErrReauthenticationRequired) {
			t.Fatalf("stale mutation at %s: %v", elapsed, err)
		}
		if _, err := manager.AuthorizeSessionMutation(request); err != nil {
			t.Fatalf("logout admission at %s: %v", elapsed, err)
		}
	}
	// A receipt minted just before freshness expires must not inherit the
	// remaining browser hour, even when consumed through the manager directly.
	now = start.Add(15*time.Minute - time.Second)
	receipt, err := manager.IssueReviewReceipt(request, "test", []byte("reviewed-metadata"))
	if err != nil {
		t.Fatal(err)
	}
	now = start.Add(15 * time.Minute)
	if _, err := manager.ConsumeReviewReceipt(request, "test", receipt); !errors.Is(err, ErrReviewReceiptUnavailable) {
		t.Fatalf("receipt survived freshness expiry: %v", err)
	}
	if _, err := manager.IssueReviewReceipt(request, "test", []byte("reviewed-metadata")); !errors.Is(err, ErrReauthenticationRequired) {
		t.Fatalf("stale identity minted receipt: %v", err)
	}
	request.Header.Set("X-CSRF-Token", "invalid")
	if _, err := manager.AuthorizeSessionMutation(request); !errors.Is(err, ErrDenied) {
		t.Fatalf("logout accepted invalid CSRF: %v", err)
	}
	request.Header.Set("X-CSRF-Token", csrf)
	request.Header.Set("Origin", "https://untrusted.example.test")
	if _, err := manager.AuthorizeSessionMutation(request); !errors.Is(err, ErrDenied) {
		t.Fatalf("logout accepted cross-origin request: %v", err)
	}
	request.Header.Set("Origin", "https://console.example.test")
	for _, elapsed := range []time.Duration{time.Hour, time.Hour + time.Second} {
		now = start.Add(elapsed)
		if _, err := manager.Authenticate(request); !errors.Is(err, ErrUnauthenticated) {
			t.Fatalf("expired identity at %s: %v", elapsed, err)
		}
	}
}
