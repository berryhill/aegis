package console

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/principalauth"
)

func TestPrincipalPasswordLoginCreatesBoundedExactPrincipalSessionAndThrottlesFailures(t *testing.T) {
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	verifier, err := principalauth.Enroll("principal", []byte("principal-password-canary"))
	if err != nil {
		t.Fatal(err)
	}
	manager, err := New(Config{Origin: "https://console.example.test", SessionTTL: 2 * time.Minute, BootstrapTTL: 15 * time.Second, MaxPageSize: 100, PrincipalID: "principal", PrincipalAuthTTL: time.Minute, PasswordVerifier: &verifier, LoginBurst: 3, LoginWindow: time.Minute}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "https://console.example.test/console/login", nil)
	request.Header.Set("Origin", "https://console.example.test")
	sessionValue, _, expires, subject, err := manager.Login(request, "client-one", []byte("principal-password-canary"))
	if err != nil {
		t.Fatalf("correct password denied: %v", err)
	}
	if subject.PrincipalID != "principal" || subject.Method != "password" || subject.Issuer != "aegis-principal-auth" || subject.ID == "" {
		t.Fatalf("password login produced wrong subject: %+v", subject)
	}
	if !expires.Equal(now.Add(time.Minute)) || sessionValue == "" {
		t.Fatalf("session was not bounded by principal authentication: expires=%s value=%q", expires, sessionValue)
	}
	for attempt := 0; attempt < 3; attempt++ {
		_, _, _, _, loginErr := manager.Login(request, "client-two", []byte("wrong-password-value"))
		if !errors.Is(loginErr, ErrInvalidCredentials) {
			t.Fatalf("wrong password attempt %d classification=%v", attempt, loginErr)
		}
	}
	_, _, _, _, err = manager.Login(request, "client-two", []byte("principal-password-canary"))
	if !errors.Is(err, ErrLoginThrottled) {
		t.Fatalf("bounded retry did not throttle client: %v", err)
	}
}

func TestAuthorizeCommandDerivesSessionBindingAndRepeatsMutationAdmission(t *testing.T) {
	now := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)
	verifier, err := principalauth.Enroll("principal", []byte("principal-password-canary"))
	if err != nil {
		t.Fatal(err)
	}
	manager, err := New(Config{Origin: "https://console.example.test", SessionTTL: 2 * time.Minute, BootstrapTTL: 15 * time.Second, MaxPageSize: 100, PrincipalID: "principal", PrincipalAuthTTL: time.Minute, PasswordVerifier: &verifier, LoginBurst: 3, LoginWindow: time.Minute}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	login := httptest.NewRequest(http.MethodPost, "https://console.example.test/console/login", nil)
	login.Header.Set("Origin", "https://console.example.test")
	sessionValue, csrf, _, subject, err := manager.Login(login, "client-one", []byte("principal-password-canary"))
	if err != nil {
		t.Fatal(err)
	}
	command := func(csrfValue string) *http.Request {
		request := httptest.NewRequest(http.MethodPost, "https://console.example.test/console/api/commands/preview", nil)
		request.Header.Set("Origin", "https://console.example.test")
		request.Header.Set("X-CSRF-Token", csrfValue)
		request.AddCookie(&http.Cookie{Name: CookieName, Value: sessionValue})
		return request
	}
	boundSubject, binding, err := manager.AuthorizeCommand(command(csrf))
	if err != nil {
		t.Fatal(err)
	}
	if boundSubject.ID != subject.ID || !strings.HasPrefix(binding, "session-") || binding == sessionValue {
		t.Fatalf("command admission exposed or misbound session context: subject=%+v binding=%q", boundSubject, binding)
	}
	_, repeatedBinding, err := manager.AuthorizeCommand(command(csrf))
	if err != nil || repeatedBinding != binding {
		t.Fatalf("same authenticated session did not produce stable binding: binding=%q err=%v", repeatedBinding, err)
	}
	if _, _, err = manager.AuthorizeCommand(command("forged-csrf")); !errors.Is(err, ErrDenied) {
		t.Fatalf("forged CSRF admitted command: %v", err)
	}
	manager.RevokeSessionValue(sessionValue)
	if _, _, err = manager.AuthorizeCommand(command(csrf)); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("revoked session admitted command: %v", err)
	}
}

func TestAuthenticatedPasswordRotationInvalidatesPriorSessionsAndBootstraps(t *testing.T) {
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	verifier, err := principalauth.Enroll("principal", []byte("current-principal-password"))
	if err != nil {
		t.Fatal(err)
	}
	manager, err := New(Config{Origin: "https://console.example.test", SessionTTL: 2 * time.Minute, BootstrapTTL: 15 * time.Second, MaxPageSize: 100, PrincipalID: "principal", PrincipalAuthTTL: time.Minute, PasswordVerifier: &verifier, LoginBurst: 3, LoginWindow: time.Minute}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	login := httptest.NewRequest(http.MethodPost, "https://console.example.test/console/login", nil)
	login.Header.Set("Origin", "https://console.example.test")
	sessionValue, csrf, _, _, err := manager.Login(login, "client-one", []byte("current-principal-password"))
	if err != nil {
		t.Fatal(err)
	}
	bootstrap, err := manager.IssueBootstrap(core.Subject{ID: "local-uid:1000", PrincipalID: "principal", AuthenticatedAt: now, ExpiresAt: now.Add(time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	rotate := httptest.NewRequest(http.MethodPost, "https://console.example.test/console/password", nil)
	rotate.Header.Set("Origin", "https://console.example.test")
	rotate.Header.Set("X-CSRF-Token", csrf)
	rotate.AddCookie(&http.Cookie{Name: CookieName, Value: sessionValue})
	var persisted principalauth.Record
	err = manager.RotatePassword(rotate, "client-one", []byte("current-principal-password"), []byte("replacement-principal-password"), []byte("replacement-principal-password"), true, func(current, replacement principalauth.Record, _ core.Subject) error {
		persisted = replacement
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = persisted.Verify([]byte("replacement-principal-password")); err != nil {
		t.Fatalf("replacement verifier not supplied to persistence: %v", err)
	}
	authenticated := httptest.NewRequest(http.MethodGet, "https://console.example.test/console", nil)
	authenticated.AddCookie(&http.Cookie{Name: CookieName, Value: sessionValue})
	if _, err = manager.Authenticate(authenticated); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("prior-generation session remained valid: %v", err)
	}
	exchange := httptest.NewRequest(http.MethodPost, "https://console.example.test/console/session", nil)
	exchange.Header.Set("Origin", "https://console.example.test")
	if _, _, _, err = manager.Exchange(exchange, bootstrap); !errors.Is(err, ErrBootstrapConsumedOrExpired) {
		t.Fatalf("prior-generation bootstrap remained valid: %v", err)
	}
	if _, _, _, _, err = manager.Login(login, "client-two", []byte("current-principal-password")); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("old password accepted after rotation: %v", err)
	}
	if _, _, _, _, err = manager.Login(login, "client-three", []byte("replacement-principal-password")); err != nil {
		t.Fatalf("new password denied after rotation: %v", err)
	}
}

func TestPasswordRotationFailsClosedBeforeReplacement(t *testing.T) {
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	verifier, err := principalauth.Enroll("principal", []byte("current-principal-password"))
	if err != nil {
		t.Fatal(err)
	}
	manager, err := New(Config{Origin: "https://console.example.test", SessionTTL: 2 * time.Minute, BootstrapTTL: 15 * time.Second, MaxPageSize: 100, PrincipalID: "principal", PrincipalAuthTTL: time.Minute, PasswordVerifier: &verifier, LoginBurst: 3, LoginWindow: time.Minute}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	login := httptest.NewRequest(http.MethodPost, "https://console.example.test/console/login", nil)
	login.Header.Set("Origin", "https://console.example.test")
	sessionValue, csrf, _, _, err := manager.Login(login, "client", []byte("current-principal-password"))
	if err != nil {
		t.Fatal(err)
	}
	rotate := httptest.NewRequest(http.MethodPost, "https://console.example.test/console/password", nil)
	rotate.Header.Set("Origin", "https://console.example.test")
	rotate.Header.Set("X-CSRF-Token", csrf)
	rotate.AddCookie(&http.Cookie{Name: CookieName, Value: sessionValue})
	called := false
	for name, test := range map[string]struct {
		current, next, confirm []byte
		approved               bool
	}{
		"wrong current": {[]byte("wrong-current-password"), []byte("replacement-principal-password"), []byte("replacement-principal-password"), true},
		"mismatch":      {[]byte("current-principal-password"), []byte("replacement-principal-password"), []byte("different-principal-password"), true},
		"not approved":  {[]byte("current-principal-password"), []byte("replacement-principal-password"), []byte("replacement-principal-password"), false},
	} {
		t.Run(name, func(t *testing.T) {
			called = false
			err := manager.RotatePassword(rotate, "client", test.current, test.next, test.confirm, test.approved, func(_, _ principalauth.Record, _ core.Subject) error { called = true; return nil })
			if err == nil || called {
				t.Fatalf("unsafe rotation err=%v replacement_called=%v", err, called)
			}
		})
	}
	if err = manager.RotatePassword(rotate, "client", []byte("current-principal-password"), []byte("replacement-principal-password"), []byte("replacement-principal-password"), true, func(_, _ principalauth.Record, _ core.Subject) error { return errors.New("audit unavailable") }); err == nil {
		t.Fatal("replacement callback failure was ignored")
	}
	if _, _, _, _, err = manager.Login(login, "after-failure", []byte("current-principal-password")); err != nil {
		t.Fatalf("failed replacement changed active verifier: %v", err)
	}
}

func TestPasswordRotationThrottlesWrongCurrentPasswordAndResetsAfterWindow(t *testing.T) {
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	verifier, err := principalauth.Enroll("principal", []byte("current-principal-password"))
	if err != nil {
		t.Fatal(err)
	}
	manager, err := New(Config{Origin: "https://console.example.test", SessionTTL: 2 * time.Minute, BootstrapTTL: 15 * time.Second, MaxPageSize: 100, PrincipalID: "principal", PrincipalAuthTTL: time.Minute, PasswordVerifier: &verifier, LoginBurst: 2, LoginWindow: 30 * time.Second}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	login := httptest.NewRequest(http.MethodPost, "https://console.example.test/console/login", nil)
	login.Header.Set("Origin", "https://console.example.test")
	sessionValue, csrf, _, _, err := manager.Login(login, "client", []byte("current-principal-password"))
	if err != nil {
		t.Fatal(err)
	}
	rotate := httptest.NewRequest(http.MethodPost, "https://console.example.test/console/password", nil)
	rotate.Header.Set("Origin", "https://console.example.test")
	rotate.Header.Set("X-CSRF-Token", csrf)
	rotate.AddCookie(&http.Cookie{Name: CookieName, Value: sessionValue})
	replace := func(_, _ principalauth.Record, _ core.Subject) error { return nil }
	for attempt := 0; attempt < 2; attempt++ {
		if err = manager.RotatePassword(rotate, "client", []byte("wrong-current-password"), []byte("replacement-principal-password"), []byte("replacement-principal-password"), true, replace); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("wrong-current attempt %d classification=%v", attempt, err)
		}
	}
	if err = manager.RotatePassword(rotate, "client", []byte("current-principal-password"), []byte("replacement-principal-password"), []byte("replacement-principal-password"), true, replace); !errors.Is(err, ErrLoginThrottled) {
		t.Fatalf("rotation retry limit not enforced: %v", err)
	}
	now = now.Add(30 * time.Second)
	if err = manager.RotatePassword(rotate, "client", []byte("current-principal-password"), []byte("replacement-principal-password"), []byte("replacement-principal-password"), true, replace); err != nil {
		t.Fatalf("rotation throttle did not reset at window boundary: %v", err)
	}
}

func TestPasswordSessionCookieUsesActualBoundedExpiry(t *testing.T) {
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	recorder := httptest.NewRecorder()
	manager, err := New(Config{Origin: "https://console.example.test", SessionTTL: 10 * time.Minute, BootstrapTTL: 15 * time.Second, MaxPageSize: 100}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	manager.SetCookieUntil(recorder, "opaque-session", now.Add(time.Minute))
	cookies := recorder.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookie count=%d", len(cookies))
	}
	cookie := cookies[0]
	if cookie.MaxAge != 60 || !cookie.Expires.Equal(now.Add(time.Minute)) || !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteStrictMode || cookie.Path != "/console" {
		t.Fatalf("password session cookie is not exactly bounded and protected: %+v", cookie)
	}
}

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
