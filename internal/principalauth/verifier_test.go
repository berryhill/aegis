package principalauth

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestVerifierRoundTripPersistsOnlyMemoryHardRecord(t *testing.T) {
	password := []byte("principal-password-canary")
	record, err := Enroll("principal", password)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := record.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encoded, password) || bytes.Contains(encoded, []byte("principal-password-canary")) {
		t.Fatal("plaintext password was retained in verifier record")
	}
	if record.Algorithm != AlgorithmScrypt || record.N < MinimumN || len(record.Salt) == 0 || len(record.Digest) == 0 {
		t.Fatalf("record is not a bounded memory-hard verifier: %+v", record)
	}
	if err := record.Verify([]byte("principal-password-canary")); err != nil {
		t.Fatalf("correct password denied: %v", err)
	}
	if err := record.Verify([]byte("wrong-password-value")); !errors.Is(err, ErrAuthentication) {
		t.Fatalf("wrong password classification=%v", err)
	}
}

func TestPublishAndLoadRequireOwnerOnlyRegularFile(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "auth", FileName)
	record, err := Enroll("principal", []byte("principal-password-canary"))
	if err != nil {
		t.Fatal(err)
	}
	if err = Publish(path, record); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("verifier permissions=%v err=%v", info.Mode().Perm(), err)
	}
	loaded, err := Load(path)
	if err != nil || loaded.PrincipalID != "principal" {
		t.Fatalf("load=%+v err=%v", loaded, err)
	}
	if err = os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err = Load(path); !errors.Is(err, ErrUnsafeArtifact) {
		t.Fatalf("unsafe verifier accepted: %v", err)
	}
}

func TestReplaceRequiresExactCurrentRecordAndAtomicallyPublishesReplacement(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "auth", FileName)
	current, err := Enroll("principal", []byte("current-principal-password"))
	if err != nil {
		t.Fatal(err)
	}
	replacement, err := Enroll("principal", []byte("replacement-principal-password"))
	if err != nil {
		t.Fatal(err)
	}
	stale, err := Enroll("principal", []byte("stale-principal-password"))
	if err != nil {
		t.Fatal(err)
	}
	if err = Publish(path, current); err != nil {
		t.Fatal(err)
	}
	if err = Replace(path, stale, replacement); !errors.Is(err, ErrVerifierChanged) {
		t.Fatalf("stale replacement classification=%v", err)
	}
	loaded, err := Load(path)
	if err != nil || loaded != current {
		t.Fatalf("stale replacement changed record: loaded=%+v err=%v", loaded, err)
	}
	if err = Replace(path, current, replacement); err != nil {
		t.Fatal(err)
	}
	loaded, err = Load(path)
	if err != nil || loaded != replacement {
		t.Fatalf("replacement not published: loaded=%+v err=%v", loaded, err)
	}
	if info, statErr := os.Stat(path); statErr != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("replacement mode=%v err=%v", info.Mode().Perm(), statErr)
	}
}

func TestConcurrentReplacementAllowsExactlyOneExactSuccess(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth", FileName)
	current, err := Enroll("principal", []byte("current-principal-password"))
	if err != nil {
		t.Fatal(err)
	}
	first, err := Enroll("principal", []byte("first-replacement-password"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Enroll("principal", []byte("second-replacement-password"))
	if err != nil {
		t.Fatal(err)
	}
	if err = Publish(path, current); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	errorsByAttempt := make(chan error, 2)
	var workers sync.WaitGroup
	for _, replacement := range []Record{first, second} {
		workers.Add(1)
		go func(replacement Record) {
			defer workers.Done()
			<-start
			errorsByAttempt <- Replace(path, current, replacement)
		}(replacement)
	}
	close(start)
	workers.Wait()
	close(errorsByAttempt)
	successes, changed := 0, 0
	for replaceErr := range errorsByAttempt {
		switch {
		case replaceErr == nil:
			successes++
		case errors.Is(replaceErr, ErrVerifierChanged):
			changed++
		default:
			t.Fatalf("unexpected concurrent replacement error: %v", replaceErr)
		}
	}
	if successes != 1 || changed != 1 {
		t.Fatalf("concurrent replacement outcomes successes=%d changed=%d", successes, changed)
	}
	loaded, err := Load(path)
	if err != nil || loaded != first && loaded != second {
		t.Fatalf("concurrent replacement left invalid record: loaded=%+v err=%v", loaded, err)
	}
}

func TestInterruptedReplacementBeforeActivationPreservesCurrentRecord(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth", FileName)
	current, err := Enroll("principal", []byte("current-principal-password"))
	if err != nil {
		t.Fatal(err)
	}
	replacement, err := Enroll("principal", []byte("replacement-principal-password"))
	if err != nil {
		t.Fatal(err)
	}
	if err = Publish(path, current); err != nil {
		t.Fatal(err)
	}
	interrupted := errors.New("simulated interruption before activation")
	if err = replace(path, current, replacement, func() error { return interrupted }); !errors.Is(err, interrupted) {
		t.Fatalf("interruption classification=%v", err)
	}
	loaded, err := Load(path)
	if err != nil || loaded != current {
		t.Fatalf("interrupted replacement changed current record: loaded=%+v err=%v", loaded, err)
	}
}
