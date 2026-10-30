package command

import (
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"syscall"
	"testing"

	"github.com/berryhill/aegis/internal/principalauth"
)

// Share only the enrollment value for this exact fixed principal/password pair.
// Record contains strings and integers, so each caller gets an independent copy.
// Each recovery fixture still publishes its own private file and owns its stores,
// authority and sessions. Password intake and every real verification KDF remain
// unchanged. Enrollment/randomness coverage belongs to principalauth's tests.
var recoveryPrincipalEnrollment = sync.OnceValues(func() (principalauth.Record, error) {
	return principalauth.Enroll("principal", []byte("synthetic-recovery-password"))
})

func TestRecoveryPrincipalFixtureValueIsolation(t *testing.T) {
	// Fail if Record ever gains reference-bearing mutable fields: value copying
	// would then need review rather than silently sharing fixture state.
	typ := reflect.TypeOf(principalauth.Record{})
	for i := 0; i < typ.NumField(); i++ {
		kind := typ.Field(i).Type.Kind()
		if kind != reflect.String && kind != reflect.Int {
			t.Fatalf("Record field %s is no longer an immutable scalar", typ.Field(i).Name)
		}
	}
	for i := 0; i < 8; i++ {
		t.Run("independent copy", func(t *testing.T) {
			t.Parallel()
			copy, err := recoveryPrincipalEnrollment()
			if err != nil {
				t.Fatal(err)
			}
			original := copy
			copy.SchemaVersion = "mutated"
			copy.PrincipalID = "different"
			copy.Algorithm = "mutated"
			copy.N, copy.R, copy.P = 1, 1, 1
			copy.Salt, copy.Digest = "mutated", "mutated"
			again, err := recoveryPrincipalEnrollment()
			if err != nil || again != original || again == copy {
				t.Fatalf("caller mutation escaped its value copy: %v", err)
			}
		})
	}
}

func TestRecoveryPrincipalFixtureFileIsolation(t *testing.T) {
	verifier, err := recoveryPrincipalEnrollment()
	if err != nil {
		t.Fatal(err)
	}
	salt, saltErr := base64.RawStdEncoding.DecodeString(verifier.Salt)
	digest, digestErr := base64.RawStdEncoding.DecodeString(verifier.Digest)
	if principalauth.MinimumN != 1<<15 {
		t.Fatal("production scrypt work factor changed; review fixture qualification")
	}
	if verifier.PrincipalID != "principal" || verifier.SchemaVersion != "aegis.dev/principal-password/v1" || verifier.Algorithm != principalauth.AlgorithmScrypt || verifier.N != principalauth.MinimumN || verifier.R != 8 || verifier.P != 1 || saltErr != nil || digestErr != nil || len(salt) != 32 || len(digest) != 32 {
		t.Fatal("recovery fixture changed exact production enrollment parameters")
	}
	paths := []string{
		filepath.Join(t.TempDir(), "auth", principalauth.FileName),
		filepath.Join(t.TempDir(), "auth", principalauth.FileName),
	}
	infos := make([]os.FileInfo, len(paths))
	for i, path := range paths {
		if err := principalauth.Publish(path, verifier); err != nil {
			t.Fatal(err)
		}
		info, err := os.Lstat(path)
		if err != nil {
			t.Fatal(err)
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || !ok || int(stat.Uid) != os.Geteuid() || stat.Nlink != 1 {
			t.Fatal("fixture verifier is not an independent owner-only regular file")
		}
		parent, err := os.Stat(filepath.Dir(path))
		if err != nil || parent.Mode().Perm() != 0700 {
			t.Fatalf("fixture verifier directory is not private: %v", err)
		}
		infos[i] = info
	}
	if os.SameFile(infos[0], infos[1]) {
		t.Fatal("recovery fixtures share a verifier inode")
	}
	if err := os.Remove(paths[0]); err != nil {
		t.Fatal(err)
	}
	if _, err := principalauth.Load(paths[0]); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("removed verifier remained available: %v", err)
	}
	loaded, err := principalauth.Load(paths[1])
	if err != nil || loaded != verifier {
		t.Fatalf("removal changed the independent verifier: %v", err)
	}
	if err := loaded.Verify([]byte("synthetic-recovery-password")); err != nil {
		t.Fatal(err)
	}
	if err := loaded.Verify([]byte("synthetic-wrong-password")); !errors.Is(err, principalauth.ErrAuthentication) {
		t.Fatalf("real KDF did not deny the wrong password: %v", err)
	}
	if err := os.Chmod(paths[1], 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := principalauth.Load(paths[1]); !errors.Is(err, principalauth.ErrUnsafeArtifact) {
		t.Fatalf("nonprivate verifier was accepted: %v", err)
	}
}
