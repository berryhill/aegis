package architecture_test

import (
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

const moduleInternal = "github.com/berryhill/aegis/internal/"

// allowedInternalImports makes the dependency direction of authority-bearing
// packages executable. A missing entry deliberately means that the package is
// an outer composition package rather than a protected inner layer.
var allowedInternalImports = map[string][]string{
	"internal/core":        {},
	"internal/execution":   {"internal/core"},
	"internal/evidence":    {"internal/store"},
	"internal/store":       {"internal/core"},
	"internal/persistence": {"internal/core", "internal/persistence"},
	"internal/credentials": {"internal/credentials"},
	"internal/manager":     {"internal/config", "internal/credentials"},
	"internal/runtime":     {"internal/buildinfo", "internal/core", "internal/credentials", "internal/execution", "internal/store"},
	"internal/app":         {"internal/config", "internal/core", "internal/credentials", "internal/runtime", "internal/store"},
	"internal/api":         {"internal/app", "internal/config", "internal/core"},
}

func TestProtectedPackagesRespectDependencyDirection(t *testing.T) {
	root := repositoryRoot(t)
	violations, err := inspectProductionImports(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, violation := range violations {
		t.Error(violation)
	}
}

func inspectProductionImports(root string) ([]string, error) {
	var violations []string
	err := filepath.WalkDir(filepath.Join(root, "internal"), func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			return nil
		}
		relative, err := filepath.Rel(root, filepath.Dir(path))
		if err != nil {
			return err
		}
		source := filepath.ToSlash(relative)
		protected := protectedLayer(source)

		parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, spec := range parsed.Imports {
			importPath, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				return err
			}
			if !strings.HasPrefix(importPath, moduleInternal) {
				continue
			}
			target := "internal/" + strings.TrimPrefix(importPath, moduleInternal)
			if protected != "" && !matchesAnyLayer(target, allowedInternalImports[protected]) {
				violations = append(violations, fmt.Sprintf("%s imports %s: protected layer %s allows only %v", filepath.ToSlash(path), importPath, protected, allowedInternalImports[protected]))
			}
			// CLI and HTTP are outer adapters. No production package may make them
			// an inward dependency; command is the sole HTTP composition owner.
			if matchesLayer(target, "internal/command") {
				violations = append(violations, fmt.Sprintf("%s imports outer CLI adapter %s", filepath.ToSlash(path), importPath))
			}
			if matchesLayer(target, "internal/api") && !matchesLayer(source, "internal/command") {
				violations = append(violations, fmt.Sprintf("%s imports outer HTTP adapter %s", filepath.ToSlash(path), importPath))
			}
		}
		return nil
	})
	return violations, err
}

func protectedLayer(pkg string) string {
	best := ""
	for layer := range allowedInternalImports {
		if matchesLayer(pkg, layer) && len(layer) > len(best) {
			best = layer
		}
	}
	return best
}

func matchesAnyLayer(pkg string, layers []string) bool {
	for _, layer := range layers {
		if matchesLayer(pkg, layer) {
			return true
		}
	}
	return false
}

func matchesLayer(pkg, layer string) bool {
	return pkg == layer || strings.HasPrefix(pkg, layer+"/")
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve architecture test location")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func TestBoundaryClassifierRejectsOutwardDependency(t *testing.T) {
	if matchesAnyLayer("internal/command", allowedInternalImports["internal/core"]) {
		t.Fatal("core unexpectedly permits the CLI adapter")
	}
	if !matchesAnyLayer("internal/credentials/bbolt", allowedInternalImports["internal/credentials"]) {
		t.Fatal("credential implementation subpackages must remain usable inside credential custody")
	}

	root := t.TempDir()
	coreDir := filepath.Join(root, "internal", "core")
	if err := os.MkdirAll(coreDir, 0700); err != nil {
		t.Fatal(err)
	}
	fixture := []byte("package core\n\nimport _ \"github.com/berryhill/aegis/internal/command\"\n")
	if err := os.WriteFile(filepath.Join(coreDir, "violation.go"), fixture, 0600); err != nil {
		t.Fatal(err)
	}
	violations, err := inspectProductionImports(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 2 || !strings.Contains(violations[0], "protected layer internal/core") || !strings.Contains(violations[1], "outer CLI adapter") {
		t.Fatalf("prohibited dependency did not fail deterministically: %v", violations)
	}
}
