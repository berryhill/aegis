package credentials

import (
	"errors"
	"fmt"
	"testing"
)

// TestVaultClassifierReturnsInitializedForNoError pins the healthy code path:
// a vault status read that succeeds is "initialized", not "unavailable".
func TestVaultClassifierReturnsInitializedForNoError(t *testing.T) {
	if got := VaultClassifier(nil); got != VaultStateInitialized {
		t.Fatalf("expected initialized, got %q", got)
	}
}

// TestVaultClassifierReturnsEmptyForNotFound pins the difference between an
// empty vault and a missing or unavailable one. A NotFound returned by
// Status() means the repository is open and the schema is healthy, but no
// metadata has been written yet.
func TestVaultClassifierReturnsEmptyForNotFound(t *testing.T) {
	if got := VaultClassifier(ErrNotFound); got != VaultStateEmpty {
		t.Fatalf("expected empty, got %q", got)
	}
}

// TestVaultClassifierReturnsLockedForPassphraseFailure covers the locked
// vault. This is the state the dashboard reports when a passphrase-protected
// KEK cannot be unlocked with the provided passphrase. The error is the
// canonical sentinel; nothing else may surface as "locked".
func TestVaultClassifierReturnsLockedForPassphraseFailure(t *testing.T) {
	if got := VaultClassifier(ErrPassphraseAuthentication); got != VaultStateLocked {
		t.Fatalf("expected locked, got %q", got)
	}
	wrapped := fmt.Errorf("open: %w", ErrPassphraseAuthentication)
	if got := VaultClassifier(wrapped); got != VaultStateLocked {
		t.Fatalf("expected locked for wrapped sentinel, got %q", got)
	}
}

// TestVaultClassifierReturnsUnavailableForOtherErrors pins the "unavailable"
// fallback. A bbolt file-system fault, a schema mismatch, or any non-sentinel
// error must surface as "unavailable" so the dashboard can render a clear
// degraded state without leaking the underlying cause.
func TestVaultClassifierReturnsUnavailableForOtherErrors(t *testing.T) {
	for _, err := range []error{
		errors.New("disk fault"),
		errors.New("schema mismatch: expected 1 got 2"),
		fmt.Errorf("wrapped: %w", errors.New("io error")),
	} {
		if got := VaultClassifier(err); got != VaultStateUnavailable {
			t.Fatalf("expected unavailable for %v, got %q", err, got)
		}
	}
}

// TestVaultClassifierDoesNotConfuseErrNotFoundWithErrRevoked pins the
// distinction between the two most common domain errors. Both can appear
// during a credential list, but only ErrNotFound is the "vault is empty"
// signal.
func TestVaultClassifierDoesNotConfuseErrNotFoundWithErrRevoked(t *testing.T) {
	if got := VaultClassifier(ErrRevoked); got != VaultStateUnavailable {
		t.Fatalf("ErrRevoked must not be classified as empty: got %q", got)
	}
	if got := VaultClassifier(ErrConflict); got != VaultStateUnavailable {
		t.Fatalf("ErrConflict must not be classified as empty: got %q", got)
	}
}

// TestHasInitializedVaultRequiresAllThreeFields pins the precondition for
// rendering the credentials surface. The dashboard must not assert a count
// when any of the three fields is missing; this is the
// "credential-independent MVI" guarantee at the typed read-model level.
func TestHasInitializedVaultRequiresAllThreeFields(t *testing.T) {
	cases := []struct {
		name     string
		custody  string
		database string
		deploy   string
		want     bool
	}{
		{"all set", "host-file", "/state/credentials.db", "deployment-1", true},
		{"missing custody", "", "/state/credentials.db", "deployment-1", false},
		{"missing database", "host-file", "", "deployment-1", false},
		{"missing deployment", "host-file", "/state/credentials.db", "", false},
		{"all empty", "", "", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := HasInitializedVault(tc.custody, tc.database, tc.deploy); got != tc.want {
				t.Fatalf("HasInitializedVault(%q,%q,%q)=%v want %v", tc.custody, tc.database, tc.deploy, got, tc.want)
			}
		})
	}
}
