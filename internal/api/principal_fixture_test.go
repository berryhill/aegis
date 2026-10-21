package api

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

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
