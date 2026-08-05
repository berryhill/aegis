package badger

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func authorityRoot(t *testing.T) string {
	t.Helper()
	state := filepath.Join(t.TempDir(), "state")
	if err := os.Mkdir(state, 0700); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(state, "persistence", "authority-v1")
}

func TestGenerationInitializeOpenClose(t *testing.T) {
	root := authorityRoot(t)
	generation, err := Initialize(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if generation.Schema != SchemaVersion || generation.Codec != CodecVersion {
		t.Fatalf("generation=%+v", generation)
	}
	if _, err = os.Stat(filepath.Join(root, "ACTIVE")); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(root, "CLEAN")); err != nil {
		t.Fatal(err)
	}
	store, err := Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(root, "DIRTY")); err != nil {
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatalf("idempotent close: %v", err)
	}
	if _, err = os.Stat(filepath.Join(root, "DIRTY")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("DIRTY remained: %v", err)
	}
	assertSecureGenerationModes(t, filepath.Join(root, "stores", generation.Directory))
}

func assertSecureGenerationModes(t *testing.T, root string) {
	t.Helper()
	if err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		want := os.FileMode(0600)
		if info.IsDir() {
			want = 0700
		}
		if info.Mode().Perm() != want {
			t.Errorf("mode %s=%04o, want %04o", path, info.Mode().Perm(), want)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestGenerationFailsClosed(t *testing.T) {
	root := authorityRoot(t)
	state := filepath.Dir(filepath.Dir(root))
	if err := os.Mkdir(filepath.Join(state, "sessions"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(state, "sessions", "legacy.json"), []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Initialize(context.Background(), root); err == nil {
		t.Fatal("legacy authority collision was accepted")
	}
	if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed initialization mutated root: %v", err)
	}
}

func TestOpenRejectsCorruptActiveMarker(t *testing.T) {
	root := authorityRoot(t)
	if _, err := Initialize(context.Background(), root); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "ACTIVE"), []byte("{}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(context.Background(), root); !errors.Is(err, ErrCorruptGeneration) {
		t.Fatalf("corrupt ACTIVE error=%v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "DIRTY")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("corrupt generation was marked dirty: %v", err)
	}
}
