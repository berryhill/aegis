package badger

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/core"
	badgerdb "github.com/dgraph-io/badger/v4"
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

func TestInspectDistinguishesExactAbsenceReadyAndExistingInvalidStateWithoutMutation(t *testing.T) {
	ctx := context.Background()
	root := authorityRoot(t)
	absent := Inspect(ctx, root)
	if absent.State != StateAbsent || absent.Err != nil {
		t.Fatalf("absent inspection=%+v", absent)
	}
	if _, err := os.Lstat(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("absent inspection mutated root: %v", err)
	}

	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	invalid := Inspect(ctx, root)
	if invalid.State != StateInvalid || invalid.Err == nil {
		t.Fatalf("markerless inspection=%+v", invalid)
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatalf("invalid inspection mutated root: entries=%v err=%v", entries, err)
	}
	if _, err = Initialize(ctx, root); err == nil {
		t.Fatal("initialization replaced markerless existing state")
	}
	entries, err = os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatalf("denied initialization mutated markerless root: entries=%v err=%v", entries, err)
	}

	if err = os.Remove(root); err != nil {
		t.Fatal(err)
	}
	generation, err := Initialize(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	ready := Inspect(ctx, root)
	if ready.State != StateReady || ready.Err != nil || ready.Generation.GenerationID != generation.GenerationID {
		t.Fatalf("ready inspection=%+v generation=%+v", ready, generation)
	}
	repeated := Inspect(ctx, root)
	if repeated.State != StateReady || repeated.Generation.GenerationID != generation.GenerationID {
		t.Fatalf("repeat inspection changed generation identity: %+v", repeated)
	}
}

func TestInitializeEmptyRejectsAConcurrentPopulatedGeneration(t *testing.T) {
	ctx := context.Background()
	root := authorityRoot(t)
	if _, err := Initialize(ctx, root); err != nil {
		t.Fatal(err)
	}
	store, err := Open(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	key, err := encodeKey(KeyMandate, []string{"populated"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.db.Update(func(txn *badgerdb.Txn) error {
		return txn.Set(key, []byte("preserve"))
	}); err != nil {
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = InitializeEmpty(ctx, root); err == nil || !strings.Contains(err.Error(), "not an empty generation") {
		t.Fatalf("populated concurrent generation accepted: %v", err)
	}
}

func TestInspectAndInitializeDenyUnsafeExistingRootsWithoutReplacement(t *testing.T) {
	for _, test := range []struct {
		name  string
		setup func(*testing.T, string)
	}{
		{name: "insecure", setup: func(t *testing.T, root string) {
			if err := os.MkdirAll(root, 0755); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "populated legacy", setup: func(t *testing.T, root string) {
			if err := os.MkdirAll(root, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "legacy.db"), []byte("preserve"), 0600); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "symlink", setup: func(t *testing.T, root string) {
			target := filepath.Join(filepath.Dir(root), "target")
			if err := os.MkdirAll(target, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Dir(root), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, root); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := authorityRoot(t)
			test.setup(t, root)
			before, _ := os.Lstat(root)
			inspection := Inspect(context.Background(), root)
			if inspection.State != StateInvalid || inspection.Err == nil {
				t.Fatalf("inspection=%+v", inspection)
			}
			if _, err := Initialize(context.Background(), root); err == nil {
				t.Fatal("unsafe existing root was replaced")
			}
			after, err := os.Lstat(root)
			if err != nil || before.Mode() != after.Mode() {
				t.Fatalf("denial changed root: before=%v after=%v err=%v", before.Mode(), after.Mode(), err)
			}
		})
	}
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
	if err := os.Mkdir(filepath.Join(state, "authority-contexts"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(state, "authority-contexts", "legacy.json"), []byte("{}"), 0600); err != nil {
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

func TestOpenRefusesSymlinkedCleanMarkerWithoutRemovingTarget(t *testing.T) {
	root := authorityRoot(t)
	if _, err := Initialize(context.Background(), root); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "operator-marker")
	active, err := os.ReadFile(filepath.Join(root, "ACTIVE"))
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(target, active, 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(filepath.Join(root, "CLEAN")); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(target, filepath.Join(root, "CLEAN")); err != nil {
		t.Fatal(err)
	}
	if store, err := Open(context.Background(), root); err == nil {
		_ = store.Close()
		t.Fatal("open accepted a symlinked CLEAN marker")
	}
	if content, err := os.ReadFile(target); err != nil || string(content) != string(active) {
		t.Fatalf("marker target changed: content=%q err=%v", content, err)
	}
	if info, err := os.Lstat(filepath.Join(root, "CLEAN")); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("denial removed or replaced symlink: info=%v err=%v", info, err)
	}
}
