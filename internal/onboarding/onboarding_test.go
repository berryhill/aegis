package onboarding

import (
	"context"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"testing"
)

func TestInspectAbsentAndMalformedAreReadOnlyAndDeterministic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "aegis.yaml")
	inspector := NewInspector(nil)
	first := inspector.Inspect(context.Background(), path)
	second := inspector.Inspect(context.Background(), path)
	if first.State != Uninitialized || first.Reason != second.Reason || first.NextCommand != "aegis init" {
		t.Fatalf("absent snapshots differ or are inexact: first=%+v second=%+v", first, second)
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("inspection created config: %v", err)
	}
	if err := os.WriteFile(path, []byte("unknown: true\n"), 0600); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(path)
	snapshot := inspector.Inspect(context.Background(), path)
	after, _ := os.ReadFile(path)
	if snapshot.State != RepairRequired || snapshot.NextCommand == "" || string(before) != string(after) {
		t.Fatalf("malformed inspection was not fail-closed/read-only: %+v", snapshot)
	}
}

func TestAuthorityPlanIsExactAtomicAndRejectsDrift(t *testing.T) {
	path := validPrincipalConfig(t)
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := PreviewAuthority(path, "host-file")
	if err != nil {
		t.Fatal(err)
	}
	if plan.Confirmation == "" || !strings.Contains(plan.Confirmation, plan.DeploymentID) || plan.OriginalDigest == plan.ResultDigest {
		t.Fatalf("inexact plan: %+v", plan)
	}
	if got, _ := os.ReadFile(path); string(got) != string(original) {
		t.Fatal("preview mutated configuration")
	}
	if err = os.WriteFile(path, append(original, []byte("\n")...), 0600); err != nil {
		t.Fatal(err)
	}
	if err = ApplyAuthority(plan); err == nil || !strings.Contains(err.Error(), "changed after authority preview") {
		t.Fatalf("drift was not rejected: %v", err)
	}
	if err = os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	plan, err = PreviewAuthority(path, "host-file")
	if err != nil {
		t.Fatal(err)
	}
	if err = ApplyAuthority(plan); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("atomic result permissions=%v err=%v", info.Mode().Perm(), err)
	}
	text, _ := os.ReadFile(path)
	for _, expected := range []string{plan.Database, plan.DeploymentID, plan.KEKFile, "custody: host-file"} {
		if !strings.Contains(string(text), expected) {
			t.Fatalf("applied configuration omitted %q:\n%s", expected, text)
		}
	}
}

func TestHostAuthorityInitializesBeforePublicationAndRollbackIsScoped(t *testing.T) {
	path := validPrincipalConfig(t)
	plan, err := PreviewAuthority(path, "host-file")
	if err != nil {
		t.Fatal(err)
	}
	if err = InitializeHostAuthority(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	for _, artifact := range []string{plan.Database, plan.KEKFile} {
		if info, statErr := os.Stat(artifact); statErr != nil || info.Mode().Perm() != 0600 {
			t.Fatalf("artifact=%s info=%v err=%v", artifact, info, statErr)
		}
	}
	if err = ApplyAuthority(plan); err != nil {
		CleanupHostAuthority(plan)
		t.Fatal(err)
	}
	for _, artifact := range []string{plan.Database, plan.KEKFile} {
		if _, statErr := os.Stat(artifact); statErr != nil {
			t.Fatalf("published artifact missing: %s: %v", artifact, statErr)
		}
	}

	other := validPrincipalConfig(t)
	conflict, err := PreviewAuthority(other, "host-file")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(filepath.Dir(conflict.KEKFile), 0700); err != nil {
		t.Fatal(err)
	}
	marker := []byte("preexisting-operator-file")
	if err = os.WriteFile(conflict.KEKFile, marker, 0600); err != nil {
		t.Fatal(err)
	}
	if err = InitializeHostAuthority(context.Background(), conflict); err == nil {
		t.Fatal("preexisting KEK was accepted")
	}
	got, readErr := os.ReadFile(conflict.KEKFile)
	if readErr != nil || string(got) != string(marker) {
		t.Fatalf("failed initialization removed or changed preexisting file: %q err=%v", got, readErr)
	}
}

func validPrincipalConfig(t *testing.T) string {
	t.Helper()
	current, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err = os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(dir, "state")
	path := filepath.Join(dir, "aegis.yaml")
	document := "state_dir: " + quote(state) + "\nprincipal:\n  id: principal\n  name: Local operator\n  uid: " + quote(current.Uid) + "\n  user: " + quote(current.Username) + "\n  auth_ttl: 5m\naudit:\n  checkpoint_dir: " + quote(filepath.Join(state, "audit-checkpoints")) + "\n"
	if err = os.WriteFile(path, []byte(document), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func quote(value string) string { return `"` + strings.ReplaceAll(value, `"`, `\"`) + `"` }
