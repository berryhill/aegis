package command

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestDevelopmentLocationDenialExplainsSupportedBuild(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", filepath.Join(root, "owner"))
	_, err := resolveDevelopmentRepository(filepath.Join(root, "go-build", "exe"))
	if err == nil {
		t.Fatal("outside-home executable accepted")
	}
	for _, text := range []string{"child of the authenticated operator home", "executable directory", "Do not use go run", "./scripts/build-source.sh ./aegis"} {
		if !strings.Contains(err.Error(), text) {
			t.Fatalf("missing guidance %q: %v", text, err)
		}
	}
}
