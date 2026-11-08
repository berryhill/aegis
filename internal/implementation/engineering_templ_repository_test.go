package implementation

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestEngineeringTemplRepositoryScopedConsistency(t *testing.T) {
	repository, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	driver, err := exec.LookPath("go")
	if err != nil {
		t.Fatal(err)
	}
	const input = "web/console/contribution_run.templ"
	source := SourceInventory{}
	for _, path := range []string{"go.mod", input, templOutput(input)} {
		content, err := os.ReadFile(filepath.Join(repository, path))
		if err != nil {
			t.Fatal(err)
		}
		source.Files = append(source.Files, SourceFile{Path: path, Content: content})
	}
	env := append(os.Environ(), "GOPROXY=off", "GOSUMDB=off", "GOWORK=off", "GOFLAGS=-mod=readonly -p=1 -buildvcs=false")
	if err := checkTemplConsistency(context.Background(), driver, repository, env, source, []string{input}, func(_ context.Context, c *exec.Cmd) error { return c.Run() }); err != nil {
		t.Fatal("actual repository generated source rejected", err)
	}
	for _, file := range source.Files {
		after, err := os.ReadFile(filepath.Join(repository, file.Path))
		if err != nil || !bytes.Equal(after, file.Content) {
			t.Fatal("read-only repository check changed source", file.Path, err)
		}
	}
}
