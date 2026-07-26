package store

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/berryhill/aegis/internal/core"
)

func FuzzAuditDecodingVerification(f *testing.F) {
	f.Add([]byte("not-json\n"))
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, b []byte) {
		s, err := Open(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(s.Root(), "audit.jsonl"), b, 0600); err != nil {
			t.Fatal(err)
		}
		_ = s.VerifyAudit()
	})
}
func TestAuditChainValid(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if err = s.AppendAudit(context.Background(), core.AuditEvent{Type: "test", Outcome: "ok", Reason: "test"}); err != nil {
			t.Fatal(err)
		}
	}
	if err = s.VerifyAudit(); err != nil {
		t.Fatal(err)
	}
}

func TestRetainedCheckpointDetectsAuditTruncation(t *testing.T) {
	root, checkpoints := t.TempDir(), t.TempDir()
	s, err := OpenWithCheckpoints(root, checkpoints)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err = s.AppendAudit(context.Background(), core.AuditEvent{Type: "test", Outcome: "ok", Reason: "checkpoint_test"}); err != nil {
			t.Fatal(err)
		}
	}
	logPath := filepath.Join(root, "audit.jsonl")
	b, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	for i := len(b) - 2; i >= 0; i-- {
		if b[i] == '\n' {
			b = b[:i+1]
			break
		}
	}
	if err = os.WriteFile(logPath, b, 0600); err != nil {
		t.Fatal(err)
	}
	if err = s.VerifyAudit(); err == nil {
		t.Fatal("truncation before retained checkpoint was accepted")
	}
}

func TestStoreRejectsTraversalSymlinkAndConcurrentConsumers(t *testing.T) {
	parent := t.TempDir()
	link := filepath.Join(parent, "state-link")
	if err := os.Symlink(t.TempDir(), link); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(link); err == nil {
		t.Fatal("symlink state root accepted")
	}
	s, err := Open(filepath.Join(parent, "state"))
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Save("approvals", "../escape", map[string]string{"status": "approved"}); err == nil {
		t.Fatal("path traversal ID accepted")
	}
	if err = s.Save("approvals", "one", map[string]string{"status": "approved"}); err != nil {
		t.Fatal(err)
	}
	var successes int
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := s.Update("approvals", "one", func(b []byte) (any, error) {
				var v map[string]string
				if jsonErr := json.Unmarshal(b, &v); jsonErr != nil {
					return nil, jsonErr
				}
				if v["status"] != "approved" {
					return nil, errors.New("already consumed")
				}
				v["status"] = "consumed"
				return v, nil
			})
			if err == nil {
				mu.Lock()
				successes++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if successes != 1 {
		t.Fatalf("successful concurrent consumers = %d, want 1", successes)
	}
}

func TestCreateIsAtomicCreateOnlyAcrossStoreInstances(t *testing.T) {
	root := t.TempDir()
	const attempts = 16
	stores := make([]*Store, attempts)
	for i := range stores {
		var err error
		stores[i], err = Open(root)
		if err != nil {
			t.Fatal(err)
		}
	}

	start := make(chan struct{})
	results := make(chan error, attempts)
	var wg sync.WaitGroup
	for i, s := range stores {
		wg.Add(1)
		go func(candidate int, candidateStore *Store) {
			defer wg.Done()
			<-start
			results <- candidateStore.Create("records", "shared", map[string]int{"candidate": candidate})
		}(i, s)
	}
	close(start)
	wg.Wait()
	close(results)

	successes := 0
	for err := range results {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, ErrAlreadyExists):
		default:
			t.Fatalf("concurrent Create returned unexpected error: %v", err)
		}
	}
	if successes != 1 {
		t.Fatalf("successful concurrent creates = %d, want 1", successes)
	}

	var stored map[string]int
	if err := stores[0].Load("records", "shared", &stored); err != nil {
		t.Fatal(err)
	}
	first := stored["candidate"]
	if first < 0 || first >= attempts {
		t.Fatalf("stored candidate = %d, want a submitted value", first)
	}
	if err := stores[0].Create("records", "shared", map[string]int{"candidate": attempts}); !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("replacement error = %v, want ErrAlreadyExists", err)
	}
	stored = nil
	if err := stores[0].Load("records", "shared", &stored); err != nil {
		t.Fatal(err)
	}
	if stored["candidate"] != first {
		t.Fatalf("rejected replacement changed authoritative value: got %d, want %d", stored["candidate"], first)
	}
}

func TestCreateRejectsInvalidPathsAndSymlinks(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		kind string
		id   string
	}{
		{"", "id"},
		{"../records", "id"},
		{"records/nested", "id"},
		{"records", ""},
		{"records", "../escape"},
		{"records", "id.json"},
	} {
		if err := s.Create(test.kind, test.id, map[string]bool{"valid": false}); err == nil {
			t.Fatalf("Create accepted kind %q and ID %q", test.kind, test.id)
		}
	}

	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(s.Root(), "linked")); err != nil {
		t.Fatal(err)
	}
	if err := s.Create("linked", "record", map[string]bool{"escaped": true}); err == nil {
		t.Fatal("Create followed a symlink namespace")
	}
	if _, err := os.Stat(filepath.Join(outside, "record.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("symlink namespace changed outside path: %v", err)
	}

	if err := os.Mkdir(filepath.Join(s.Root(), "targets"), 0700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(outside, "target.json")
	if err := os.Symlink(target, filepath.Join(s.Root(), "targets", "record.json")); err != nil {
		t.Fatal(err)
	}
	if err := s.Create("targets", "record", map[string]bool{"escaped": true}); err == nil {
		t.Fatal("Create accepted a symlink target")
	}
	if _, err := os.Stat(target); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("symlink target changed outside path: %v", err)
	}
}

func TestBlobRoundTripDeduplicationAndConcurrentPut(t *testing.T) {
	root := t.TempDir()
	s, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	content := []byte("abc")
	const expected = "sha256:ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
	reference, err := s.PutBlob(content)
	if err != nil {
		t.Fatal(err)
	}
	if reference != expected {
		t.Fatalf("blob reference = %q, want %q", reference, expected)
	}
	content[0] = 'x'
	got, err := s.GetBlob(reference)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, []byte("abc")) {
		t.Fatalf("stored bytes = %q, want exact original bytes", got)
	}
	if second, err := s.PutBlob([]byte("abc")); err != nil || second != reference {
		t.Fatalf("deduplicated PutBlob = %q, %v; want %q, nil", second, err, reference)
	}

	const attempts = 12
	start := make(chan struct{})
	results := make(chan error, attempts)
	var wg sync.WaitGroup
	for i := 0; i < attempts; i++ {
		other, openErr := Open(root)
		if openErr != nil {
			t.Fatal(openErr)
		}
		wg.Add(1)
		go func(candidateStore *Store) {
			defer wg.Done()
			<-start
			concurrentReference, putErr := candidateStore.PutBlob([]byte("concurrent content"))
			if putErr == nil && concurrentReference == "" {
				putErr = errors.New("PutBlob returned an empty reference")
			}
			results <- putErr
		}(other)
	}
	close(start)
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatalf("concurrent deduplicating PutBlob failed: %v", err)
		}
	}
}

func TestGetBlobRejectsMalformedReferencesAndCorruption(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	valid, err := s.PutBlob([]byte("authoritative"))
	if err != nil {
		t.Fatal(err)
	}
	for _, reference := range []string{
		"",
		"md5:" + strings.Repeat("0", 64),
		"sha256:" + strings.Repeat("0", 63),
		"sha256:" + strings.Repeat("0", 65),
		"sha256:" + strings.Repeat("G", 64),
		"sha256:" + strings.Repeat("A", 64),
		"sha256:" + strings.Repeat("0", 63) + "/",
	} {
		if _, err := s.GetBlob(reference); !errors.Is(err, ErrInvalidBlobReference) {
			t.Errorf("GetBlob(%q) error = %v, want ErrInvalidBlobReference", reference, err)
		}
	}

	digest := strings.TrimPrefix(valid, blobReferencePrefix)
	path := s.blobPath(digest)
	if err := os.WriteFile(path, []byte("altered"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetBlob(valid); !errors.Is(err, ErrBlobCorrupt) {
		t.Fatalf("GetBlob corrupted content error = %v, want ErrBlobCorrupt", err)
	}
	if _, err := s.PutBlob([]byte("authoritative")); !errors.Is(err, ErrBlobCorrupt) {
		t.Fatalf("PutBlob over corrupted digest path error = %v, want ErrBlobCorrupt", err)
	}

	symlinkReference, err := s.PutBlob([]byte("symlink candidate"))
	if err != nil {
		t.Fatal(err)
	}
	symlinkPath := s.blobPath(strings.TrimPrefix(symlinkReference, blobReferencePrefix))
	if err := os.Remove(symlinkPath); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside-blob")
	if err := os.WriteFile(outside, []byte("symlink candidate"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, symlinkPath); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetBlob(symlinkReference); !errors.Is(err, ErrBlobCorrupt) {
		t.Fatalf("GetBlob symlink target error = %v, want ErrBlobCorrupt", err)
	}
	if _, err := s.PutBlob([]byte("symlink candidate")); !errors.Is(err, ErrBlobCorrupt) {
		t.Fatalf("PutBlob symlink target error = %v, want ErrBlobCorrupt", err)
	}
}

func TestBlobNamespaceRejectsSymlinkComponent(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(s.Root(), "blobs")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PutBlob([]byte("must stay contained")); err == nil {
		t.Fatal("PutBlob followed a symlink blob namespace")
	}
	entries, err := os.ReadDir(outside)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("symlink blob namespace changed outside directory: %v", entries)
	}
	missingReference := fmt.Sprintf("%s%s", blobReferencePrefix, strings.Repeat("0", 64))
	if _, err := s.GetBlob(missingReference); err == nil {
		t.Fatal("GetBlob followed a symlink blob namespace")
	}
}

func TestPublishProvisionedIsContainedAtomicCreateOnly(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(s.Root(), "provisioned", "agent", "1", "mapping.json")
	payload := map[string]string{"digest": "first"}
	if err = s.PublishProvisioned(target, payload); err != nil {
		t.Fatal(err)
	}
	if err = s.PublishProvisioned(target, map[string]string{"digest": "replacement"}); err == nil {
		t.Fatal("create-only publication replaced an existing artifact")
	}
	var stored map[string]string
	if err = read(target, &stored); err != nil || stored["digest"] != "first" {
		t.Fatalf("published artifact changed after rejected replacement: %#v %v", stored, err)
	}
	if err = s.PublishProvisioned(filepath.Join(s.Root(), "escape.json"), payload); err == nil {
		t.Fatal("publication escaped Aegis-owned provisioned directory")
	}
	outside := t.TempDir()
	link := filepath.Join(s.Root(), "provisioned", "linked")
	if err = os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	if err = s.PublishProvisioned(filepath.Join(link, "artifact.json"), payload); err == nil {
		t.Fatal("publication followed a symlink component")
	}
	if _, err = os.Stat(filepath.Join(outside, "artifact.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("symlink publication changed outside path: %v", err)
	}
}
