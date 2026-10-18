package command

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"testing"
)

func TestUpgradeHistoryInterruptedStagingDoesNotPoisonRetry(t *testing.T) {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	content := []byte(`{"retained":"exact-original-intent"}`)
	sum := sha256.Sum256(content)
	archive := "superseded-" + hex.EncodeToString(sum[:]) + ".json"
	// Model process death during an earlier staging write. It must not have
	// claimed the final name, and the next operation must preserve this evidence.
	staged := ".history-00000000000000000000000000000000"
	if err := root.WriteFile(staged, content[:5], 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := root.Lstat(archive); !os.IsNotExist(err) {
		t.Fatalf("final name published early: %v", err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		if err := publishUpgradeHistory(root, archive, content); err != nil {
			t.Fatal(err)
		}
		stored, err := root.ReadFile(archive)
		if err != nil || !bytes.Equal(stored, content) {
			t.Fatalf("incomplete published bytes: %v", err)
		}
		info, err := root.Stat(archive)
		if err != nil || !privateUpgradeFile(info) {
			t.Fatalf("unsafe archive: %v", err)
		}
	}
	stored, err := root.ReadFile(staged)
	if err != nil || !bytes.Equal(stored, content[:5]) {
		t.Fatalf("interruption evidence changed: %v", err)
	}
	if err := publishUpgradeHistory(root, archive, []byte("different")); err == nil {
		t.Fatal("conflicting published history accepted")
	}
	stored, err = root.ReadFile(archive)
	if err != nil || !bytes.Equal(stored, content) {
		t.Fatalf("collision overwrote history: %v", err)
	}
}
