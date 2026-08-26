package architecture_test

import (
	"bufio"
	"fmt"
	"go/ast"
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
	"internal/reference":         {},
	"internal/registry":          {"internal/reference"},
	"internal/loop":              {},
	"internal/graph":             {"internal/reference"},
	"internal/queue":             {"internal/reference"},
	"internal/core":              {},
	"internal/execution":         {"internal/core", "internal/reference"},
	"internal/evidence":          {"internal/reference", "internal/store"},
	"internal/disposition":       {"internal/execution", "internal/reference"},
	"internal/store":             {"internal/core"},
	"internal/persistence":       {"internal/core", "internal/persistence"},
	"internal/persistence/fleet": {"internal/core", "internal/disposition", "internal/evidence", "internal/execution", "internal/graph", "internal/loop", "internal/persistence", "internal/queue", "internal/reference", "internal/registry"},
	"internal/credentials":       {"internal/credentials"},
	"internal/manager":           {"internal/config", "internal/credentials"},
	"internal/principalauth":     {},
	"internal/skillbundle":       {"internal/skillbundle"},
	"internal/runtime":           {"internal/buildinfo", "internal/core", "internal/credentials", "internal/execution", "internal/store"},
	"internal/app":               {"internal/config", "internal/core", "internal/credentials", "internal/disposition", "internal/evidence", "internal/execution", "internal/graph", "internal/loop", "internal/orchestration", "internal/persistence/fleet", "internal/queue", "internal/reference", "internal/registry", "internal/runtime", "internal/store"},
	"internal/managergateway":    {"internal/app", "internal/core", "internal/manager", "internal/slash"},
	"internal/console":           {"internal/core", "internal/principalauth"},
	"internal/api":               {"internal/app", "internal/config", "internal/console", "internal/core", "internal/managergateway", "internal/principalauth"},
}

var classifiedProductionFamilies = map[string]struct{}{
	"api": {}, "app": {}, "buildinfo": {}, "command": {}, "config": {}, "console": {},
	"core": {}, "credentials": {}, "disposition": {}, "evidence": {}, "execution": {}, "graph": {},
	"initialize": {}, "layout": {}, "loop": {}, "manager": {}, "managergateway": {}, "migration": {},
	"onboarding": {}, "orchestration": {}, "persistence": {}, "principalauth": {}, "queue": {}, "reset": {}, "runtime": {},
	"reference": {}, "registry": {}, "safefs": {}, "skillbundle": {}, "slash": {}, "store": {}, "tui": {}, "update": {},
	"userservice": {},
}

var externalDependencyOwners = map[string][]string{
	"github.com/dgraph-io/badger/v4": {"internal/persistence/authority/badger", "internal/persistence/fleet/badger"},
	"go.etcd.io/bbolt":               {"internal/credentials/bbolt", "internal/reset"},
}

var canonicalTypeOwners = map[string]string{
	"DigestRef":               "internal/reference",
	"RevisionRef":             "internal/reference",
	"AgentRevision":           "internal/registry",
	"AgentRegistration":       "internal/registry",
	"FleetSource":             "internal/registry",
	"LoopRevision":            "internal/loop",
	"LoopValidationResult":    "internal/loop",
	"GraphRevision":           "internal/graph",
	"GraphValidationResult":   "internal/graph",
	"GraphRunSnapshot":        "internal/graph",
	"Submission":              "internal/queue",
	"Rejection":               "internal/queue",
	"Item":                    "internal/queue",
	"Claim":                   "internal/queue",
	"QueueTransition":         "internal/queue",
	"GraphRun":                "internal/execution",
	"LoopExecution":           "internal/execution",
	"Attempt":                 "internal/execution",
	"Mandate":                 "internal/core",
	"AuthorityContext":        "internal/core",
	"AuthorityTransitionFact": "internal/core",
	"AuthorityTransitionRoot": "internal/core",
	"AuthorityCommand":        "internal/core",
	"AuthorityFact":           "internal/core",
	"AuthorityReceipt":        "internal/core",
	"AuthorityProjection":     "internal/core",
	"AuthorityReplay":         "internal/core",
	"AuditEvent":              "internal/core",
	"SecretRecord":            "internal/credentials",
	"EncryptedSecretVersion":  "internal/credentials",
	"CredentialBinding":       "internal/credentials",
}

var standardLibraryOnlyLayers = map[string]struct{}{
	"internal/reference": {},
	"internal/registry":  {},
	"internal/loop":      {},
	"internal/graph":     {},
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
				if _, standardLibraryOnly := standardLibraryOnlyLayers[protected]; standardLibraryOnly && strings.Contains(strings.Split(importPath, "/")[0], ".") {
					violations = append(violations, fmt.Sprintf("%s imports non-standard dependency %s: protected layer %s is standard-library-only", filepath.ToSlash(path), importPath, protected))
				}
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

func TestNormativeFleetControlScopeRemainsCompleteAndNonContradictory(t *testing.T) {
	root := repositoryRoot(t)
	required := map[string][]string{
		"AGENTS.md":                  {"Agent Registry", "Loop", "Graph", "Execution Queue", "not release-defining fleet-control gates"},
		"specs/MVP.md":               {"Agent Registry", "Loops", "Graphs", "Execution Queue", "/v1/agents", "/v1/loops", "/v1/graphs", "/v1/queue", "supporting infrastructure, not release-defining gates"},
		"specs/README.md":            {"Agent Registry", "Loop", "Graph", "Execution Queue"},
		"specs/CANONICAL_DOMAINS.md": {"## Agent Registry", "## Loops", "## Graphs", "## Execution Queue", "## Disposition"},
		"specs/PLUMBING.md":          {"new bounded Graph domain", "do not restore the former universal aggregate"},
		"specs/STORAGE.md":           {"Fleet-control definitions", "Fleet-control lifecycle", "shared `state/persistence/fleet-v1`", "Badger `github.com/dgraph-io/badger/v4` `v4.9.5`", "fixed 256 MiB free reserve"},
		"specs/CONTROL_PLANE_API.md": {"/v1/agents", "/v1/loops", "/v1/graphs", "/v1/queue", "Readiness is evaluated per attempted action"},
		"docs/launch/OPEN_SOURCE_LAUNCH_AND_GROWTH_PLAN.md": {"implemented substrate", "installed fixture proves Registry", "atomic single-winner claim/lease"},
	}
	for name, terms := range required {
		content, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatalf("read normative surface %s: %v", name, err)
		}
		for _, term := range terms {
			if !strings.Contains(string(content), term) {
				t.Errorf("normative surface %s lost required fleet-control term %q", name, term)
			}
		}
	}

	mvp, err := os.ReadFile(filepath.Join(root, "specs", "MVP.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, contradiction := range []string{
		"The release-defining use edge is the typed `github.get_repository.v1` operation",
		"A configured principal can place one personal credential under encrypted Aegis custody, approve one exact grant",
	} {
		if strings.Contains(string(mvp), contradiction) {
			t.Errorf("credential-centric release contradiction remains in specs/MVP.md: %q", contradiction)
		}
	}
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

func TestReferenceLayerRejectsInternalAndThirdPartyDependencies(t *testing.T) {
	cases := map[string]struct {
		importPath string
		want       string
	}{
		"internal dependency": {
			importPath: "github.com/berryhill/aegis/internal/core",
			want:       "protected layer internal/reference allows only []",
		},
		"third-party dependency": {
			importPath: "github.com/example/dependency",
			want:       "protected layer internal/reference is standard-library-only",
		},
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			directory := filepath.Join(root, "internal", "reference")
			if err := os.MkdirAll(directory, 0700); err != nil {
				t.Fatal(err)
			}
			fixture := []byte(fmt.Sprintf("package reference\nimport _ %q\n", test.importPath))
			if err := os.WriteFile(filepath.Join(directory, "violation.go"), fixture, 0600); err != nil {
				t.Fatal(err)
			}
			violations, err := inspectProductionImports(root)
			if err != nil {
				t.Fatal(err)
			}
			if len(violations) != 1 || !strings.Contains(violations[0], test.want) {
				t.Fatalf("reference dependency violation did not fail deterministically: %v", violations)
			}
		})
	}
}

func TestPersistenceModulesRemainPinned(t *testing.T) {
	versions, err := directModuleVersions(filepath.Join(repositoryRoot(t), "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"github.com/dgraph-io/badger/v4": "v4.9.5",
		"go.etcd.io/bbolt":               "v1.5.0",
	}
	for module, version := range want {
		if versions[module] != version {
			t.Errorf("persistence module %s=%q, want exact %q", module, versions[module], version)
		}
	}
}

func TestPersistenceEnginesHaveOnePackageOwner(t *testing.T) {
	root := repositoryRoot(t)
	violations, err := inspectExternalDependencyOwners(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, violation := range violations {
		t.Error(violation)
	}
}

func TestPersistenceEngineOwnerClassifierRejectsOuterImport(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "internal", "app")
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	fixture := []byte("package app\nimport _ \"go.etcd.io/bbolt\"\n")
	if err := os.WriteFile(filepath.Join(directory, "violation.go"), fixture, 0600); err != nil {
		t.Fatal(err)
	}
	violations, err := inspectExternalDependencyOwners(root)
	if err != nil || len(violations) != 1 || !strings.Contains(violations[0], "outside owners") {
		t.Fatalf("external dependency owner violation did not fail deterministically: %v, %v", violations, err)
	}
}

func TestCanonicalPersistentTypesHaveOneDomainOwner(t *testing.T) {
	root := repositoryRoot(t)
	violations, err := inspectCanonicalTypeOwners(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, violation := range violations {
		t.Error(violation)
	}
}

func TestCanonicalTypeOwnerClassifierRejectsDuplicateSchema(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "internal", "app")
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	fixture := []byte("package app\ntype Mandate struct{}\n")
	if err := os.WriteFile(filepath.Join(directory, "violation.go"), fixture, 0600); err != nil {
		t.Fatal(err)
	}
	violations, err := inspectCanonicalTypeOwners(root)
	if err != nil || len(violations) != 1 || !strings.Contains(violations[0], "owned by internal/core") {
		t.Fatalf("canonical type owner violation did not fail deterministically: %v, %v", violations, err)
	}
}

func TestEveryProductionPackageFamilyIsClassified(t *testing.T) {
	entries, err := os.ReadDir(filepath.Join(repositoryRoot(t), "internal"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if !entry.IsDir() || entry.Name() == "architecture" {
			continue
		}
		if _, ok := classifiedProductionFamilies[entry.Name()]; !ok {
			t.Errorf("internal/%s is not classified by the architecture contract", entry.Name())
		}
	}
}

func directModuleVersions(path string) (map[string]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	versions := map[string]string{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) >= 2 && (strings.Contains(fields[0], ".") || strings.HasPrefix(fields[0], "go.")) && strings.HasPrefix(fields[1], "v") {
			versions[fields[0]] = fields[1]
		}
	}
	return versions, scanner.Err()
}

func inspectExternalDependencyOwners(root string) ([]string, error) {
	var violations []string
	err := filepath.WalkDir(filepath.Join(root, "internal"), func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			return walkErr
		}
		relative, err := filepath.Rel(root, filepath.Dir(path))
		if err != nil {
			return err
		}
		source := filepath.ToSlash(relative)
		parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, spec := range parsed.Imports {
			importPath, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				return err
			}
			for dependency, owners := range externalDependencyOwners {
				if importPath == dependency && !matchesAnyLayer(source, owners) {
					violations = append(violations, fmt.Sprintf("%s imports %s outside owners %v", filepath.ToSlash(path), dependency, owners))
				}
			}
		}
		return nil
	})
	return violations, err
}

func inspectCanonicalTypeOwners(root string) ([]string, error) {
	var violations []string
	err := filepath.WalkDir(filepath.Join(root, "internal"), func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			return walkErr
		}
		relative, err := filepath.Rel(root, filepath.Dir(path))
		if err != nil {
			return err
		}
		source := filepath.ToSlash(relative)
		parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return err
		}
		for _, declaration := range parsed.Decls {
			general, ok := declaration.(*ast.GenDecl)
			if !ok || general.Tok != token.TYPE {
				continue
			}
			for _, spec := range general.Specs {
				typeSpec := spec.(*ast.TypeSpec)
				owner, governed := canonicalTypeOwners[typeSpec.Name.Name]
				if governed && !matchesLayer(source, owner) {
					violations = append(violations, fmt.Sprintf("%s declares canonical persistent type %s owned by %s", filepath.ToSlash(path), typeSpec.Name.Name, owner))
				}
			}
		}
		return nil
	})
	return violations, err
}
