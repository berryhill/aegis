package console

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/principalauth"
)

func TestDashboardIssuedDeadlinesReauthFailuresAndRevocation(t *testing.T) {
	start := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	now := start
	verifier, err := principalauth.Enroll("principal", []byte("principal-password-canary"))
	if err != nil {
		t.Fatal(err)
	}
	manager, err := New(Config{Origin: "https://console.example.test", SessionTTL: 6 * time.Hour, ActionAuthTTL: 15 * time.Minute, MaxPageSize: 100, PrincipalID: "principal", PasswordVerifier: &verifier, LoginBurst: 10, LoginWindow: time.Minute}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "https://console.example.test/console/login", nil)
	request.Header.Set("Origin", "https://console.example.test")
	value, csrf, expiry, subject, err := manager.Login(request, "client", []byte("principal-password-canary"))
	if err != nil {
		t.Fatal(err)
	}
	if !expiry.Equal(start.Add(6*time.Hour)) || !subject.AuthenticatedAt.Equal(start) {
		t.Fatal("incorrect issued session")
	}
	request.AddCookie(&http.Cookie{Name: CookieName, Value: value})
	request.Header.Set("X-CSRF-Token", csrf)
	now = start.Add(14 * time.Minute)
	if _, err := manager.AuthorizeMutation(request); err != nil {
		t.Fatal(err)
	}
	// A future policy expansion cannot reinterpret an issued action deadline.
	manager.config.ActionAuthTTL = 6 * time.Hour
	now = start.Add(15 * time.Minute)
	if _, err := manager.AuthorizeMutation(request); !errors.Is(err, ErrReauthenticationRequired) {
		t.Fatal("old action subject extended")
	}
	if err := manager.ValidateActionSubject(subject); !errors.Is(err, ErrReauthenticationRequired) {
		t.Fatal("service hook extended old subject")
	}
	if _, err := manager.Authenticate(request); err != nil {
		t.Fatal(err)
	}
	if _, _, _, _, err := manager.Login(request, "client", []byte("wrong-password")); err == nil {
		t.Fatal("wrong password accepted")
	}
	saved := manager.config.PasswordVerifier
	manager.config.PasswordVerifier = nil
	if _, _, _, _, err := manager.Login(request, "client", []byte("principal-password-canary")); err == nil {
		t.Fatal("unavailable verifier issued identity")
	}
	manager.config.PasswordVerifier = saved
	if _, err := manager.AuthorizeMutation(request); !errors.Is(err, ErrReauthenticationRequired) {
		t.Fatal("failed/unavailable reauth renewed identity")
	}
	newValue, _, newExpiry, newSubject, err := manager.Login(request, "client", []byte("principal-password-canary"))
	if err != nil {
		t.Fatal(err)
	}
	if !newSubject.AuthenticatedAt.Equal(now) || !newExpiry.Equal(now.Add(6*time.Hour)) {
		t.Fatal("successful authentication did not start new bounded window")
	}
	manager.RevokeSessionValue(value)
	if manager.ValidateActionSubject(subject) == nil {
		t.Fatal("revoked in-flight subject admitted")
	}
	if manager.ValidateActionSubject(newSubject) != nil {
		t.Fatal("new session denied")
	}
	altered := newSubject
	altered.AuthenticatedAt = altered.AuthenticatedAt.Add(time.Second)
	if manager.ValidateActionSubject(altered) == nil {
		t.Fatal("changed authentication time admitted")
	}
	altered = newSubject
	altered.PrincipalID = "other"
	if manager.ValidateActionSubject(altered) == nil {
		t.Fatal("principal substitution admitted")
	}
	// Concurrent service admission shares generation/revocation checks and no
	// request changes the original deadlines. All accesses have a fixed clock.
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if manager.ValidateActionSubject(newSubject) != nil {
				t.Error("concurrent fresh admission failed")
			}
		}()
	}
	wg.Wait()
	now = newSubject.AuthenticatedAt.Add(-time.Nanosecond)
	if manager.ValidateActionSubject(newSubject) == nil {
		t.Fatal("backwards clock admitted")
	}
	now = newSubject.AuthenticatedAt.Add(6 * time.Hour)
	if manager.ValidateActionSubject(newSubject) == nil {
		t.Fatal("exact expiry admitted")
	}
	manager.RevokeSessionValue(newValue)
}

func TestDashboardValidatorRejectsUnissuedSubject(t *testing.T) {
	manager, err := New(Config{Origin: "https://console.example.test", SessionTTL: 6 * time.Hour, MaxPageSize: 100}, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if manager.ValidateActionSubject(core.Subject{ID: "password:unissued", AuthenticatedAt: time.Now(), ExpiresAt: time.Now().Add(6 * time.Hour)}) == nil {
		t.Fatal("unissued subject admitted")
	}
}
