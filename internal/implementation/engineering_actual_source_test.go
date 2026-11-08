package implementation

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/berryhill/aegis/internal/loop"
)

// Copy the complete development SOURCE cohort, not the dirty worktree's private
// proof/cache/binary outputs. No source-prefix or test-file exclusions. The
// production inventory remains filesystem-based, and its ignored-input inclusion
// is independently asserted below.
func actualEngineeringSourceCopy(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "-c", "core.worktree="+root, "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	cmd.Dir = root
	names, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	target := t.TempDir()
	seen := map[string]bool{}
	for _, entry := range bytes.Split(names, []byte{0}) {
		p := string(entry)
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		if !filepath.IsLocal(p) {
			t.Fatal("nonlocal source")
		}
		info, err := os.Lstat(filepath.Join(root, p))
		if err != nil || !info.Mode().IsRegular() {
			t.Fatalf("unsafe/missing actual source %s: %v", p, err)
		}
		b, err := os.ReadFile(filepath.Join(root, p))
		if err != nil {
			t.Fatal(err)
		}
		dest := filepath.Join(target, p)
		if err = os.MkdirAll(filepath.Dir(dest), 0700); err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(dest, b, info.Mode().Perm()); err != nil {
			t.Fatal(err)
		}
	}
	return target
}

func actualEngineeringInventoryPolicy(t *testing.T, root string) loop.SourceInventoryPolicy {
	t.Helper()
	p := loop.SourceInventoryPolicy{MaxFiles: 4096, MaxFileBytes: 2 << 20, MaxTotalBytes: 24 << 20}
	for _, name := range []string{"internal/command/doer_companion_test.go", "internal/delivery/objects_test.go", "internal/delivery/publication_privacy_test.go", "internal/manager/contracts_test.go"} {
		b, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		p.SyntheticFixtures = append(p.SyntheticFixtures, loop.SourceFixtureIdentity{Path: name, Digest: digest(b)})
	}
	return p
}

func TestEngineeringActualIgnoredGeneratedInputCustody(t *testing.T) {
	root := actualEngineeringSourceCopy(t)
	p := actualEngineeringInventoryPolicy(t, root)
	name := "internal/implementation/ignored_generated_input.go"
	// Explicit declared fixture input, intentionally absent from all Git manifests.
	input := []byte("// Code generated for inventory custody regression. DO NOT EDIT.\npackage implementation\n")
	if err := os.WriteFile(filepath.Join(root, name), input, 0600); err != nil {
		t.Fatal(err)
	}
	s, err := InventoryWithPolicy(context.Background(), root, &p)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, f := range s.Files {
		if f.Path == name {
			found = bytes.Equal(f.Content, input) && f.Mode == 0600
		}
	}
	if !found {
		t.Fatal("untracked/ignored generated build input absent")
	}
	if err = os.Chmod(filepath.Join(root, name), 0640); err != nil {
		t.Fatal(err)
	}
	changed, err := InventoryWithPolicy(context.Background(), root, &p)
	if err != nil || changed.Digest == s.Digest {
		t.Fatal("mode drift not committed")
	}
}

func TestEngineeringActualLockedOfflineDependencies(t *testing.T) {
	root := actualEngineeringSourceCopy(t)
	policy := actualEngineeringInventoryPolicy(t, root)
	source, err := InventoryWithPolicy(context.Background(), root, &policy)
	if err != nil {
		t.Fatal(err)
	}
	driver, err := exec.LookPath("go")
	if err != nil {
		t.Fatal(err)
	}
	driver, err = filepath.EvalSymlinks(driver)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(driver, "env", "GOMODCACHE")
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	cache := strings.TrimSpace(string(out))
	output := filepath.Join(t.TempDir(), "dependencies.tar")
	bundle, err := PrepareEngineeringDependencyBundle(context.Background(), root, cache, driver, output)
	if err != nil {
		t.Fatal(err)
	}
	p := loop.ContributionExecutionPolicy{Kind: loop.EngineeringExecutionKind, Dependencies: &bundle}
	if err = qualifiedModule(source, p); err != nil {
		t.Fatal(err)
	}
	wire, err := dependencyBundleBytes(bundle)
	if err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	proxy := filepath.Join(home, "proxy")
	if err = materializeDependencyBundle(context.Background(), wire, proxy); err != nil {
		t.Fatal(err)
	}
	env := []string{"PATH=" + filepath.Dir(driver), "HOME=" + home, "TMPDIR=" + home, "GOCACHE=" + filepath.Join(home, "build"), "GOMODCACHE=" + filepath.Join(home, "modules"), "GOPROXY=file://" + proxy, "GOSUMDB=off", "GOTOOLCHAIN=local", "GOWORK=off", "GOENV=off", "CGO_ENABLED=0", "GOMAXPROCS=2", "GOFLAGS=-mod=readonly -modcacherw -p=1 -buildvcs=false"}
	// Real entire-source package+test dependency loading, not a tiny module. Go list
	// executes no repository tests, generators or model-authored commands.
	cmd = exec.Command(driver, "list", "-deps", "-test", "./...")
	cmd.Dir = root
	cmd.Env = env
	out, err = cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("actual offline package closure: %v\n%s", err, out)
	}
	if !bytes.Contains(out, []byte("github.com/berryhill/aegis/internal/managergateway")) {
		t.Fatal("actual package closure not exercised")
	}
	// The same exact bundle must reject lock and archive tampering at admission.
	var mod, sum []byte
	for _, f := range source.Files {
		if f.Path == "go.mod" {
			mod = f.Content
		}
		if f.Path == "go.sum" {
			sum = f.Content
		}
	}
	if err = validateLockedDependencyBundle(wire, mod, bytes.Replace(sum, []byte("h1:"), []byte("h1:X"), 1)); err == nil {
		t.Fatal("changed lock admitted")
	}
	old := source.Digest
	again, err := InventoryWithPolicy(context.Background(), root, &policy)
	if err != nil || again.Digest != old {
		t.Fatal("readonly dependency preparation changed source locks")
	}
	if _, err = PrepareEngineeringDependencyBundle(context.Background(), root, cache, driver, output); err == nil {
		t.Fatal("existing definition archive overwritten")
	}
	receipt, _ := json.Marshal(bundle)
	t.Logf("actual_source_files=%d proxy_bytes=%d bundle=%s package_lines=%d", len(source.Files), len(wire), receipt, bytes.Count(out, []byte{'\n'}))
}
