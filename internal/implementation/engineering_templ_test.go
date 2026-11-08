package implementation

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/berryhill/aegis/internal/loop"
)

// Actual pinned templ compiler and rendered component, not stand-in generated Go
// or a builder-authored PASS. Offline transport is independently locked by sums.
func TestEngineeringTemplActualRenderedConsistency(t *testing.T) {
	ctx := context.Background()
	must := func(e error) {
		t.Helper()
		if e != nil {
			t.Fatal(e)
		}
	}
	repository, err := filepath.Abs("../..")
	must(err)
	driver, err := exec.LookPath("go")
	must(err)
	driver, err = filepath.EvalSymlinks(driver)
	must(err)
	cacheCmd := exec.Command(driver, "env", "GOMODCACHE")
	cache, err := cacheCmd.Output()
	must(err)
	home := t.TempDir()
	bundle, err := PrepareEngineeringDependencyBundle(ctx, repository, strings.TrimSpace(string(cache)), driver, filepath.Join(home, "deps.tar"))
	must(err)
	wire, err := dependencyBundleBytes(bundle)
	must(err)
	proxy := filepath.Join(home, "proxy")
	must(materializeDependencyBundle(ctx, wire, proxy))
	workspace := filepath.Join(home, "source")
	must(os.Mkdir(workspace, 0700))
	must(os.Mkdir(filepath.Join(workspace, "web"), 0700))
	env := []string{"PATH=" + filepath.Dir(driver), "HOME=" + home, "TMPDIR=" + home, "GOCACHE=" + filepath.Join(home, "build"), "GOMODCACHE=" + filepath.Join(home, "modules"), "GOPROXY=file://" + proxy, "GOSUMDB=off", "GOTOOLCHAIN=local", "GOWORK=off", "GOENV=off", "CGO_ENABLED=0", "GOMAXPROCS=2", "GOFLAGS=-mod=readonly -modcacherw -p=1 -buildvcs=false"}
	write := func(p string, b []byte) { t.Helper(); must(os.WriteFile(filepath.Join(workspace, p), b, 0600)) }
	for _, p := range []string{"go.mod", "go.sum"} {
		b, e := os.ReadFile(filepath.Join(repository, p))
		must(e)
		write(p, b)
	}
	write("web/view.templ", []byte("package fixture\n\ntempl View() {\n <p>{ string(\"old\") }</p>\n}\n"))
	generate := func() []byte {
		t.Helper()
		cmd := exec.Command(driver, "run", templTool, "generate", "-f", "view.templ", "-stdout", "-include-timestamp=false", "-include-version=true")
		cmd.Dir = filepath.Join(workspace, "web")
		cmd.Env = env
		out, e := cmd.Output()
		must(e)
		return out
	}
	old := generate()
	if !bytes.Contains(old, []byte("FileName: `view.templ`")) || bytes.Contains(old, []byte("FileName: `web/view.templ`")) {
		t.Fatal("fixture does not exercise canonical basename error metadata")
	}
	write("web/view_templ.go", old)
	run := func(_ context.Context, c *exec.Cmd) error { return c.Run() }
	source, err := Inventory(ctx, workspace)
	must(err)
	must(checkTemplConsistency(ctx, driver, workspace, env, source, []string{"web/view.templ"}, run))
	write("web/view.templ", []byte("package fixture\n\ntempl View() {\n <p>{ string(\"NEW_REQUIRED\") }</p>\n}\n"))
	write("web/render_test.go", []byte("package fixture\nimport (\"testing\";\"context\";\"bytes\";\"strings\")\nfunc TestActualRender(t *testing.T){var b bytes.Buffer;if e:=View().Render(context.Background(),&b);e!=nil{t.Fatal(e)};if !strings.Contains(b.String(),\"NEW_REQUIRED\"){t.Fatal(\"stale rendered behavior\")}}\n"))
	source, err = Inventory(ctx, workspace)
	must(err)
	if e := checkTemplConsistency(ctx, driver, workspace, env, source, []string{"web/view.templ"}, run); !errors.Is(e, errTemplConsistency) {
		t.Fatalf("stale generated source accepted: %v", e)
	}
	render := func() ([]byte, error) {
		cmd := exec.Command(driver, "test", "-count=1", "-run", "^TestActualRender$", "./web")
		cmd.Dir = workspace
		cmd.Env = env
		return cmd.CombinedOutput()
	}
	if out, e := render(); e == nil || !bytes.Contains(out, []byte("stale rendered behavior")) {
		t.Fatalf("actual stale render did not fail: %v %s", e, out)
	}
	var admitted []string
	must(generateTemplPairs(ctx, driver, workspace, env, source, []string{"web/view.templ"}, run, func(_ context.Context, effect string) error {
		admitted = append(admitted, effect)
		return nil
	}))
	if len(admitted) != 1 || admitted[0] != "generate:web/view.templ->web/view_templ.go" {
		t.Fatalf("generation authority target drift: %v", admitted)
	}
	generated, e := os.ReadFile(filepath.Join(workspace, "web/view_templ.go"))
	must(e)
	if !bytes.Contains(generated, []byte("FileName: `view.templ`")) || bytes.Contains(generated, []byte("FileName: `web/view.templ`")) {
		t.Fatal("product generation did not preserve canonical basename error metadata")
	}
	// A metadata-only path drift must fail the exact comparison, not merely
	// compile/render successfully. The checker must leave the rejected bytes alone.
	wrong := bytes.ReplaceAll(generated, []byte("FileName: `view.templ`"), []byte("FileName: `web/view.templ`"))
	write("web/view_templ.go", wrong)
	source, err = Inventory(ctx, workspace)
	must(err)
	if e := checkTemplConsistency(ctx, driver, workspace, env, source, []string{"web/view.templ"}, run); !errors.Is(e, errTemplConsistency) {
		t.Fatalf("root-prefixed metadata accepted: %v", e)
	}
	rejected, e := Inventory(ctx, workspace)
	must(e)
	if rejected.Digest != source.Digest {
		t.Fatal("failed read-only consistency changed source")
	}
	write("web/view_templ.go", generated)
	source, err = Inventory(ctx, workspace)
	must(err)
	must(checkTemplConsistency(ctx, driver, workspace, env, source, []string{"web/view.templ"}, run))
	if out, e := render(); e != nil {
		t.Fatalf("actual regenerated render failed: %v %s", e, out)
	}
	before := source.Digest
	after, e := Inventory(ctx, workspace)
	must(e)
	if after.Digest != before {
		t.Fatal("read-only consistency changed source")
	}
	t.Log("actual stale render RED; exact pinned generation comparison RED -> GREEN; actual regenerated render GREEN; source unchanged by consistency")
}

func TestEngineeringTemplPolicyAndPairs(t *testing.T) {
	c := loop.VerifiedImplementation{SchemaVersion: loop.RepositoryEngineeringSchema, WritableFiles: []string{"web/view.templ", "web/view_templ.go"}, RepositoryContext: &loop.RepositoryContext{Files: []loop.RepositoryContextFile{{Path: "web/view.templ"}, {Path: "web/view_templ.go"}}}}
	p := loop.ContributionExecutionPolicy{TemplConsistency: loop.TemplModuleConsistency, Dependencies: &loop.DependencyBundle{}}
	if validateTemplSelection(c, p) != nil {
		t.Fatal("exact pairs denied")
	}
	p.TemplConsistency = ""
	if validateTemplSelection(c, p) == nil {
		t.Fatal("unapproved generator accepted")
	}
	p.TemplConsistency = loop.TemplModuleConsistency
	c.WritableFiles = c.WritableFiles[:1]
	if validateTemplSelection(c, p) == nil {
		t.Fatal("implicit generated write admitted")
	}
	if got := selectedTemplPairs([]string{"web/view_templ.go", "web/view.templ"}); len(got) != 1 || got[0] != "web/view.templ" {
		t.Fatal(got)
	}
	c.SchemaVersion = loop.RepositoryEngineeringSchema
	if _, pass, e := (&Executor{}).checkCandidate(context.Background(), c, nil); e == nil || pass {
		t.Fatal("unqualified selected-file native path accepted template")
	}
}
