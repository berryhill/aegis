package managergateway

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/berryhill/aegis/internal/implementation"
	"github.com/berryhill/aegis/internal/loop"
)

func hugeAssetExpected(path, original string) string {
	if strings.HasSuffix(path, ".templ") {
		return "// Issue-specific reviewed asset update.\n" + strings.Replace(original, "before", "NEW_REQUIRED", 1)
	}
	return "Issue-specific reviewed asset update.\n" + original
}
func hugeTemplBundle(t *testing.T, root string) *loop.DependencyBundle {
	t.Helper()
	repository, e := filepath.Abs("../..")
	if e != nil {
		t.Fatal(e)
	}
	driver, e := exec.LookPath("go")
	if e != nil {
		t.Fatal(e)
	}
	driver, e = filepath.EvalSymlinks(driver)
	if e != nil {
		t.Fatal(e)
	}
	cmd := exec.Command(driver, "env", "GOMODCACHE")
	cache, e := cmd.Output()
	if e != nil {
		t.Fatal(e)
	}
	bundle, e := implementation.PrepareEngineeringDependencyBundle(context.Background(), repository, strings.TrimSpace(string(cache)), driver, filepath.Join(root, "templ-deps.tar"))
	if e != nil {
		t.Fatal(e)
	}
	return &bundle
}
func prepareHugeTemplFixture(t *testing.T, root, repo string, files map[string]string) {
	t.Helper()
	repository, e := filepath.Abs("../..")
	if e != nil {
		t.Fatal(e)
	}
	for _, p := range []string{"go.mod", "go.sum"} {
		b, e := os.ReadFile(filepath.Join(repository, p))
		if e != nil {
			t.Fatal(e)
		}
		files[p] = string(b)
	}
	lines := strings.SplitN(files["go.mod"], "\n", 2)
	files["go.mod"] = "module synthetic\n" + lines[1]
	seed := filepath.Join(root, "templ-seed")
	if e = os.MkdirAll(filepath.Join(seed, "web"), 0700); e != nil {
		t.Fatal(e)
	}
	for _, p := range []string{"go.mod", "go.sum", "web/feature.templ"} {
		if e = os.WriteFile(filepath.Join(seed, p), []byte(files[p]), 0600); e != nil {
			t.Fatal(e)
		}
	}
	driver, e := exec.LookPath("go")
	if e != nil {
		t.Fatal(e)
	}
	cmd := exec.Command(driver, "env", "GOMODCACHE")
	cache, e := cmd.Output()
	if e != nil {
		t.Fatal(e)
	}
	cmd = exec.Command(driver, "run", "github.com/a-h/templ/cmd/templ", "generate", "-f", "web/feature.templ", "-include-timestamp=false", "-include-version=true")
	cmd.Dir = seed
	cmd.Env = append(os.Environ(), "GOPROXY=file://"+strings.TrimSpace(string(cache))+"/cache/download", "GOSUMDB=off", "GOTOOLCHAIN=local", "GOWORK=off", "GOMODCACHE="+filepath.Join(root, "seed-modules"), "GOFLAGS=-mod=readonly -modcacherw -p=1 -buildvcs=false", "GOMAXPROCS=2")
	if out, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("actual initial templ: %s %v", out, e)
	}
	b, e := os.ReadFile(filepath.Join(seed, "web/feature_templ.go"))
	if e != nil {
		t.Fatal(e)
	}
	files["web/feature_templ.go"] = string(b)
	files["main.go"] = "package main\nimport(\"context\";\"os\";\"synthetic/web\")\nfunc main(){if e:=web.Feature().Render(context.Background(),os.Stdout);e!=nil{panic(e)}}\n"
	files["main_test.go"] = strings.Replace(files["main_test.go"], "import \"testing\"", "import(\"testing\";\"context\";\"bytes\";\"os\";\"os/exec\";\"synthetic/web\")", 1)
	files["main_test.go"] = strings.Replace(files["main_test.go"], "func TestBehavior(t *testing.T){", "func TestBehavior(t *testing.T){var b bytes.Buffer;if e:=web.Feature().Render(context.Background(),&b);e!=nil{t.Fatal(e)};if b.String()!=\"<p>NEW_REQUIRED</p>\"{t.Fatal(\"stale rendered templ behavior\",b.String())};if candidate:=os.Getenv(\"AEGIS_CONTRIBUTION_CANDIDATE\");candidate!=\"\"{out,e:=exec.Command(candidate).Output();if e!=nil||string(out)!=b.String(){t.Fatal(\"candidate compiled render differs\",e,string(out))}};", 1)
	_ = repo
}
