package api

import (
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"

	consoleauth "github.com/berryhill/aegis/internal/console"
	"github.com/berryhill/aegis/internal/principalauth"
)

// Only the immutable enrollment value is shared. Record contains strings and
// integers, so callers receive independent copies, never a shared pointer.
// Each apiServiceAt still publishes its own owner-only file and constructs fresh
// stores, authority, sessions and limiters. Every login still runs the real KDF.
// Enrollment/randomness behavior itself is covered by principalauth's tests.
var apiPrincipalEnrollment = sync.OnceValues(func() (principalauth.Record, error) {
	return principalauth.Enroll("principal-1", []byte("api-principal-password"))
})

func apiPrincipalVerifier(t testing.TB) principalauth.Record {
	t.Helper()
	verifier, err := apiPrincipalEnrollment()
	if err != nil {
		t.Fatal(err)
	}
	return verifier
}

// The queue-route password differs from apiService's password. Share only this
// fixed enrollment value; every route fixture still creates and logs into a new manager.
var consoleQueuePrincipalEnrollment = sync.OnceValues(func() (principalauth.Record, error) {
	return principalauth.Enroll("principal-1", []byte("test-principal-password"))
})

func consoleQueuePrincipalVerifier(t testing.TB, principalID string) principalauth.Record {
	t.Helper()
	var verifier principalauth.Record
	var err error
	if principalID == "principal-1" {
		verifier, err = consoleQueuePrincipalEnrollment()
	} else {
		verifier, err = principalauth.Enroll(principalID, []byte("test-principal-password"))
	}
	if err != nil {
		t.Fatal(err)
	}
	return verifier
}

func TestConsoleQueuePrincipalFixtureIsolation(t *testing.T) {
	verifier := consoleQueuePrincipalVerifier(t, "principal-1")
	salt, saltErr := base64.RawStdEncoding.DecodeString(verifier.Salt)
	digest, digestErr := base64.RawStdEncoding.DecodeString(verifier.Digest)
	if verifier.PrincipalID != "principal-1" || verifier.SchemaVersion != "aegis.dev/principal-password/v1" || verifier.Algorithm != principalauth.AlgorithmScrypt || verifier.N != principalauth.MinimumN || verifier.R != 8 || verifier.P != 1 || saltErr != nil || digestErr != nil || len(salt) != 32 || len(digest) != 32 {
		t.Fatal("queue fixture changed exact production enrollment parameters")
	}
	if err := verifier.Verify([]byte("test-principal-password")); err != nil {
		t.Fatal(err)
	}
	if err := verifier.Verify([]byte("api-principal-password")); !errors.Is(err, principalauth.ErrAuthentication) {
		t.Fatalf("different fixture password was not denied: %v", err)
	}
	fallback := consoleQueuePrincipalVerifier(t, "other-principal")
	if fallback.PrincipalID != "other-principal" || fallback == verifier {
		t.Fatal("non-fixed principal reused fixed enrollment")
	}
	if err := fallback.Verify([]byte("test-principal-password")); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 8; i++ {
		t.Run("concurrent independent value", func(t *testing.T) {
			t.Parallel()
			copy := consoleQueuePrincipalVerifier(t, "principal-1")
			copy.PrincipalID, copy.Salt, copy.Digest, copy.N = "mutated", "mutated", "mutated", 1
			if consoleQueuePrincipalVerifier(t, "principal-1") != verifier {
				t.Fatal("concurrent copy mutation escaped into cached enrollment")
			}
		})
	}
}

func TestConsoleQueueRouteFixtureManagerIsolation(t *testing.T) {
	svc := apiService(t)
	first, firstCookie, firstCSRF := consoleQueueRouteFixture(t, svc)
	second, secondCookie, secondCSRF := consoleQueueRouteFixture(t, svc)
	if first == second || firstCookie.Value == secondCookie.Value || firstCSRF == secondCSRF {
		t.Fatal("queue fixtures share manager, session or CSRF state")
	}
	request := func(cookie *http.Cookie) *http.Request {
		r := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/console/queue", nil)
		r.AddCookie(cookie)
		return r
	}
	if subject, err := first.Authenticate(request(firstCookie)); err != nil || subject.PrincipalID != svc.Config.Principal.ID {
		t.Fatalf("first real login did not bind principal: %v", err)
	}
	if _, err := second.Authenticate(request(firstCookie)); !errors.Is(err, consoleauth.ErrUnauthenticated) {
		t.Fatalf("second manager admitted first session: %v", err)
	}
	if _, err := first.Authenticate(request(secondCookie)); !errors.Is(err, consoleauth.ErrUnauthenticated) {
		t.Fatalf("first manager admitted second session: %v", err)
	}
	first.RevokeSessionValue(firstCookie.Value)
	if _, err := first.Authenticate(request(firstCookie)); !errors.Is(err, consoleauth.ErrUnauthenticated) {
		t.Fatalf("revoked session remained authenticated: %v", err)
	}
	if _, err := second.Authenticate(request(secondCookie)); err != nil {
		t.Fatalf("revoking first fixture affected second: %v", err)
	}
	login := httptest.NewRequest(http.MethodPost, "http://127.0.0.1/console/login", nil)
	login.Header.Set("Origin", "http://127.0.0.1")
	for i := 0; i < 3; i++ {
		if _, _, _, _, err := first.Login(login, "test-client", []byte("wrong-principal-password")); !errors.Is(err, consoleauth.ErrInvalidCredentials) {
			t.Fatalf("real login did not deny wrong password: %v", err)
		}
	}
	if _, _, _, _, err := first.Login(login, "test-client", []byte("test-principal-password")); !errors.Is(err, consoleauth.ErrLoginThrottled) {
		t.Fatalf("first manager did not retain independent throttle: %v", err)
	}
	if _, _, _, _, err := second.Login(login, "test-client", []byte("test-principal-password")); err != nil {
		t.Fatalf("first manager failures leaked into second limiter: %v", err)
	}
}

func BenchmarkConsoleQueuePrincipalEnrollment(b *testing.B) {
	b.Run("enroll", func(b *testing.B) {
		for b.Loop() {
			if _, err := principalauth.Enroll("principal-1", []byte("test-principal-password")); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("fixture", func(b *testing.B) {
		_ = consoleQueuePrincipalVerifier(b, "principal-1")
		b.ResetTimer()
		for b.Loop() {
			_ = consoleQueuePrincipalVerifier(b, "principal-1")
		}
	})
}

func TestAPIPrincipalFixtureIsolation(t *testing.T) {
	first, second := apiService(t), apiService(t)
	firstPath := filepath.Join(first.Config.StateDir, "auth", principalauth.FileName)
	secondPath := filepath.Join(second.Config.StateDir, "auth", principalauth.FileName)
	firstInfo, err := os.Stat(firstPath)
	if err != nil {
		t.Fatal(err)
	}
	secondInfo, err := os.Stat(secondPath)
	if err != nil {
		t.Fatal(err)
	}
	if os.SameFile(firstInfo, secondInfo) || firstInfo.Mode().Perm() != 0600 || secondInfo.Mode().Perm() != 0600 || first.Authority == second.Authority {
		t.Fatal("fixtures share verifier files or authority, or have unsafe modes")
	}
	if err := os.Remove(firstPath); err != nil {
		t.Fatal(err)
	}
	verifier, err := principalauth.Load(secondPath)
	if err != nil {
		t.Fatal(err)
	}
	if verifier != apiPrincipalVerifier(t) || verifier.N != principalauth.MinimumN || verifier.R != 8 || verifier.P != 1 {
		t.Fatal("fixture verifier changed or weakened")
	}
	if err := verifier.Verify([]byte("api-principal-password")); err != nil {
		t.Fatal(err)
	}
	if err := verifier.Verify([]byte("wrong-principal-password")); err == nil {
		t.Fatal("fixture accepted wrong password")
	}
	for i := 0; i < 8; i++ {
		t.Run("independent value", func(t *testing.T) {
			t.Parallel()
			copy := apiPrincipalVerifier(t)
			copy.Digest = "mutated"
			if apiPrincipalVerifier(t).Digest == copy.Digest {
				t.Fatal("fixture shares mutable verifier state")
			}
		})
	}
}

// Compare production enrollment cost with the only work elided by the fixture.
func BenchmarkAPIPrincipalEnrollment(b *testing.B) {
	b.Run("enroll", func(b *testing.B) {
		for b.Loop() {
			if _, err := principalauth.Enroll("principal-1", []byte("api-principal-password")); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("fixture", func(b *testing.B) {
		_ = apiPrincipalVerifier(b)
		for b.Loop() {
			_ = apiPrincipalVerifier(b)
		}
	})
}
