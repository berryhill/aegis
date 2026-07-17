package store

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
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
