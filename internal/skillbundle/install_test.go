//go:build linux || darwin

package skillbundle

import (
	"context"
	"errors"
	"github.com/berryhill/aegis/internal/testprocess"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestManagedInventoryTransaction(t *testing.T) {
	if os.Getenv("AEGIS_SKILL_INSTALL_CRASH") == "before_publication" {
		_, err := installArchive(context.Background(), os.Getenv("AEGIS_SKILL_TEST_ARCHIVE"), os.Getenv("AEGIS_SKILL_TEST_DIGEST"), os.Getenv("AEGIS_SKILL_TEST_REVISION"), os.Getenv("AEGIS_SKILL_TEST_HOME"), func() error { os.Exit(23); return nil })
		t.Fatalf("crash helper unexpectedly returned: %v", err)
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	proof := t.TempDir()
	home := filepath.Join(proof, "home")
	if err := os.Mkdir(home, 0700); err != nil {
		t.Fatal(err)
	}
	revision := strings.Repeat("a", 40)
	first := filepath.Join(proof, "first.tar.gz")
	digest, err := BuildArchive(root, first, "0.2.18", revision)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	got, err := InstallArchive(ctx, first, digest, revision, home)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Skills) != 15 || got.AuthorityGranted || got.ArchiveDigest != digest || got.EvidenceClass != "installed_inventory_verification" {
		t.Fatalf("unexpected inventory: %+v", got)
	}
	if _, err := InstallArchive(ctx, first, "sha256:"+strings.Repeat("0", 64), revision, home); err == nil {
		t.Fatal("accepted checksum mismatch")
	}
	if _, err := InstallArchive(ctx, first, digest, strings.Repeat("b", 40), home); err == nil {
		t.Fatal("accepted revision mismatch")
	}

	metadata := filepath.Join(home, "skills", ".hub", "audit.log")
	if err := os.WriteFile(metadata, []byte("synthetic discovery event\n"), 0600); err != nil {
		t.Fatal(err)
	}
	second := filepath.Join(proof, "second.tar.gz")
	digest2, err := BuildArchive(root, second, "0.2.19", revision)
	if err != nil {
		t.Fatal(err)
	}
	interrupted := errors.New("synthetic interruption before pointer publication")
	if _, err := installArchive(ctx, second, digest2, revision, home, func() error { return interrupted }); !errors.Is(err, interrupted) {
		t.Fatalf("interruption: %v", err)
	}
	got, err = InspectInstalled(home)
	if err != nil || got.ArchiveDigest != digest {
		t.Fatalf("interrupted update lost old inventory: %+v %v", got, err)
	}
	child := exec.Command(os.Args[0], "-test.run=^TestManagedInventoryTransaction$", "-test.parallel=1")
	child.Env = append(os.Environ(), "AEGIS_SKILL_INSTALL_CRASH=before_publication", "AEGIS_SKILL_TEST_ARCHIVE="+second, "AEGIS_SKILL_TEST_DIGEST="+digest2, "AEGIS_SKILL_TEST_REVISION="+revision, "AEGIS_SKILL_TEST_HOME="+home)
	childOutput, childErr := testprocess.CombinedOutput(child, 2*time.Minute)
	var exitErr *exec.ExitError
	if !errors.As(childErr, &exitErr) || exitErr.ExitCode() != 23 {
		t.Fatalf("crash helper failed: %v %s", childErr, childOutput)
	}
	got, err = InspectInstalled(home)
	if err != nil || got.ArchiveDigest != digest {
		t.Fatalf("process interruption lost inventory or retained lock: %+v %v", got, err)
	}
	got, err = InstallArchive(ctx, second, digest2, revision, home)
	if err != nil || got.Version != "0.2.19" {
		t.Fatalf("upgrade: %+v %v", got, err)
	}
	// Reconcile a lost success response by exact inventory; same-artifact replay
	// changes neither identity nor content and does not need a new operation ID.
	got, err = InstallArchive(ctx, second, digest2, revision, home)
	if err != nil || got.ArchiveDigest != digest2 {
		t.Fatalf("replay: %+v %v", got, err)
	}
	got, err = InstallArchive(ctx, first, digest, revision, home)
	if err != nil || got.Version != "0.2.18" {
		t.Fatalf("rollback: %+v %v", got, err)
	}
	if data, err := os.ReadFile(metadata); err != nil || string(data) != "synthetic discovery event\n" {
		t.Fatalf("metadata lost across update/rollback: %v", err)
	}
	unlock, err := lockInventory(filepath.Join(home, managedBundles, "lock"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := InspectInstalled(home); err == nil {
		t.Fatal("concurrent lock accepted")
	}
	unlock()

	local := filepath.Join(home, "skills", "aegis", "SKILL.md")
	before, err := os.ReadFile(local)
	if err != nil {
		t.Fatal(err)
	}
	changed := append(append([]byte(nil), before...), []byte("\nLocal operator note.\n")...)
	if err := os.WriteFile(local, changed, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := InspectInstalled(home); err == nil {
		t.Fatal("inventory accepted local drift")
	}
	if _, err := InstallArchive(ctx, second, digest2, revision, home); err == nil {
		t.Fatal("update overwrote local edit")
	}
	after, err := os.ReadFile(local)
	if err != nil || string(after) != string(changed) {
		t.Fatal("local edit not preserved")
	}
	if err := os.WriteFile(local, before, 0644); err != nil {
		t.Fatal(err)
	}
	// Undeclared files also prevent replacement, not only declared digest drift.
	extra := filepath.Join(home, "skills", "aegis", "local.md")
	if err := os.WriteFile(extra, []byte("local"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := InstallArchive(ctx, second, digest2, revision, home); err == nil {
		t.Fatal("accepted undeclared local file")
	}
	if err := os.Remove(extra); err != nil {
		t.Fatal(err)
	}
	cancelCtx, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := InstallArchive(cancelCtx, second, digest2, revision, home); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
	got, err = InspectInstalled(home)
	if err != nil || got.ArchiveDigest != digest {
		t.Fatalf("denials changed current inventory: %+v %v", got, err)
	}
}

func TestManagedInventoryRefusesUnmanagedAndUnsafePaths(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	proof := t.TempDir()
	archive := filepath.Join(proof, "bundle.tar.gz")
	revision := strings.Repeat("a", 40)
	digest, err := BuildArchive(root, archive, "0.2.18", revision)
	if err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(proof, "home")
	if err := os.Mkdir(home, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(home, "skills"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := InstallArchive(context.Background(), archive, digest, revision, home); err == nil {
		t.Fatal("adopted unmanaged directory")
	}
	alias := filepath.Join(proof, "alias")
	if err := os.Symlink(home, alias); err != nil {
		t.Fatal(err)
	}
	if _, err := InstallArchive(context.Background(), archive, digest, revision, alias); err == nil {
		t.Fatal("accepted symlink destination")
	}
	if _, err := InstallArchive(context.Background(), archive, digest, revision, "."); err == nil {
		t.Fatal("accepted implicit/relative destination")
	}
}
