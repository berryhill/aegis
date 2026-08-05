package badger

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/core"
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

func TestRestartAfterProcessCrashFailsClosedBeforeAuthorityRead(t *testing.T) {
	for _, test := range []struct {
		name string
		mode string
	}{
		{name: "mandate only", mode: "mandate"},
		{name: "complete binding", mode: "binding"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := authorityRoot(t)
			ready := filepath.Join(filepath.Dir(root), "crash-ready")
			command := exec.Command(os.Args[0], "-test.run=^TestAuthorityCrashHelper$")
			command.Env = append(os.Environ(),
				"AEGIS_AUTHORITY_CRASH_ROOT="+root,
				"AEGIS_AUTHORITY_CRASH_MODE="+test.mode,
				"AEGIS_AUTHORITY_CRASH_READY="+ready,
			)
			if err := command.Start(); err != nil {
				t.Fatal(err)
			}
			deadline := time.Now().Add(10 * time.Second)
			for {
				if _, err := os.Stat(ready); err == nil {
					break
				} else if !errors.Is(err, os.ErrNotExist) {
					_ = command.Process.Kill()
					_ = command.Wait()
					t.Fatal(err)
				}
				if time.Now().After(deadline) {
					_ = command.Process.Kill()
					_ = command.Wait()
					t.Fatal("crash helper did not reach its committed boundary")
				}
				time.Sleep(10 * time.Millisecond)
			}
			if err := command.Process.Kill(); err != nil {
				t.Fatal(err)
			}
			if err := command.Wait(); err == nil {
				t.Fatal("crash helper exited cleanly instead of being interrupted")
			}

			if store, err := Open(context.Background(), root); !errors.Is(err, ErrCorruptGeneration) {
				if store != nil {
					_ = store.Close()
				}
				t.Fatalf("unclean generation was authorized after crash: store=%v err=%v", store, err)
			}
			if _, err := os.Stat(filepath.Join(root, "DIRTY")); err != nil {
				t.Fatalf("crash evidence was not retained: %v", err)
			}
			if _, err := os.Stat(filepath.Join(root, "CLEAN")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("crashed generation was incorrectly certified clean: %v", err)
			}
		})
	}
}

func TestAuthorityCrashHelper(t *testing.T) {
	root := os.Getenv("AEGIS_AUTHORITY_CRASH_ROOT")
	if root == "" {
		t.Skip("subprocess crash helper")
	}
	if _, err := Initialize(context.Background(), root); err != nil {
		t.Fatal(err)
	}
	store, err := Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	mandate, authority := badgerAuthorityBinding("mandate-crash", "authority-crash", "session-crash")
	if err = store.CreateMandate(context.Background(), mandate); err != nil {
		t.Fatal(err)
	}
	if os.Getenv("AEGIS_AUTHORITY_CRASH_MODE") == "binding" {
		if err = store.CreateAuthorityContext(context.Background(), authority); err != nil {
			t.Fatal(err)
		}
		fact := badgerTransitionFact("fact-crash", 1, authority, "", core.AuthorityStateActive, authority.IssuedAt, "")
		if _, err = store.AppendAuthorityTransitionFact(context.Background(), fact); err != nil {
			t.Fatal(err)
		}
	}
	if err = os.WriteFile(os.Getenv("AEGIS_AUTHORITY_CRASH_READY"), []byte("committed\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for {
		time.Sleep(time.Hour)
	}
}
