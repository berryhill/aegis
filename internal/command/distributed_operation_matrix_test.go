package command

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// This is structural coverage only. It never executes a command, opens a
// product store, authenticates a subject, starts a server or grants authority.
// Test-file references are navigation, not behavioral acceptance evidence.
func TestDistributedOperationMatrix(t *testing.T) {
	const repo = "../.."
	var matrix struct {
		SchemaVersion       int                             `json:"schema_version"`
		EvidenceClass       string                          `json:"evidence_class"`
		TransportBoundary   string                          `json:"transport_boundary"`
		AuthorityBoundary   string                          `json:"authority_boundary"`
		Classifications     map[string]string               `json:"classifications"`
		Coverage            string                          `json:"coverage"`
		ExcludedCLI         []struct{ Path, Reason string } `json:"excluded_cli"`
		FrameworkExclusions []string                        `json:"framework_exclusions"`
		Owners              []struct {
			Skill, Classification, Authority string
			Sources, Tests, CLI, HTTP        []string
			TransportClassification          string `json:"transport_classification"`
		} `json:"owners"`
		Gaps []struct{ Skill, Operation, Classification, Reason string } `json:"gaps"`
	}
	data, err := os.ReadFile(filepath.Join(repo, "skills/aegis/references/operation-matrix.v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&matrix); err != nil {
		t.Fatal(err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		t.Fatalf("trailing matrix input: %v", err)
	}
	if matrix.SchemaVersion != 1 || matrix.EvidenceClass != "structural-coverage-only" || matrix.AuthorityBoundary == "" || matrix.TransportBoundary == "" || matrix.Coverage == "" {
		t.Fatal("missing version/evidence/authority contract")
	}
	allowed := map[string]bool{"shipped": true, "missing-adapter": true, "advisory": true, "unavailable": true}
	if len(matrix.Classifications) != len(allowed) {
		t.Fatal("classification vocabulary drift")
	}
	for classification := range allowed {
		if matrix.Classifications[classification] == "" {
			t.Errorf("missing classification %s", classification)
		}
	}
	checkReference := func(path string, test bool) {
		t.Helper()
		if filepath.IsAbs(path) || filepath.Clean(path) != path || strings.HasPrefix(path, "../") {
			t.Fatalf("unsafe reference %q", path)
		}
		info, err := os.Stat(filepath.Join(repo, path))
		if err != nil || !info.Mode().IsRegular() {
			t.Fatalf("missing regular reference %s: %v", path, err)
		}
		if test && !strings.HasSuffix(path, "_test.go") {
			t.Errorf("not a Go test reference: %s", path)
		}
	}
	skills := map[string]string{}
	cli := map[string]string{}
	http := map[string]string{}
	insert := func(index map[string]string, key, owner string) {
		t.Helper()
		if key == "" {
			t.Fatal("empty operation")
		}
		if previous, exists := index[key]; exists {
			t.Fatalf("duplicate primary owner for %s: %s / %s", key, previous, owner)
		}
		index[key] = owner
	}
	for _, owner := range matrix.Owners {
		insert(skills, owner.Skill, owner.Skill)
		checkReference("skills/"+owner.Skill+"/SKILL.md", false)
		if !allowed[owner.Classification] || !allowed[owner.TransportClassification] || owner.Authority == "" || len(owner.Sources) == 0 || len(owner.Tests) == 0 {
			t.Fatalf("incomplete owner contract: %s", owner.Skill)
		}
		for _, path := range owner.Sources {
			checkReference(path, false)
		}
		for _, path := range owner.Tests {
			checkReference(path, true)
		}
		if owner.TransportClassification != "shipped" && len(owner.CLI)+len(owner.HTTP) > 0 {
			t.Fatalf("unshipped transport catalogued as public: %s", owner.Skill)
		}
		for _, path := range owner.CLI {
			insert(cli, path, owner.Skill)
		}
		for _, route := range owner.HTTP {
			insert(http, route, owner.Skill)
		}
	}
	installedSkills, err := filepath.Glob(filepath.Join(repo, "skills/*/SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	expectedSkills := map[string]string{}
	for _, path := range installedSkills {
		expectedSkills[filepath.Base(filepath.Dir(path))] = "skill"
	}
	if len(skills) != 15 {
		t.Fatalf("want all 15 primary skills, got %d", len(skills))
	}
	distributedSameKeys(t, "primary skill", skills, expectedSkills)
	for _, gap := range matrix.Gaps {
		if skills[gap.Skill] == "" || !allowed[gap.Classification] || gap.Classification == "shipped" || gap.Operation == "" || gap.Reason == "" {
			t.Fatalf("invalid gap: %+v", gap)
		}
	}
	if len(matrix.Gaps) == 0 || len(matrix.FrameworkExclusions) == 0 {
		t.Fatal("missing explicit gaps/framework exclusions")
	}
	root := NewRoot(Dependencies{In: strings.NewReader(""), Out: io.Discard, Err: io.Discard, Version: "structural-test", IsTerminal: func(io.Reader, io.Writer) bool { return false }})
	actualCLI, hidden := map[string]string{}, map[string]string{}
	var walk func(*cobra.Command, bool)
	walk = func(cmd *cobra.Command, ancestorHidden bool) {
		isHidden := ancestorHidden || cmd.Hidden
		if isHidden {
			hidden[cmd.CommandPath()] = "hidden"
		} else {
			actualCLI[cmd.CommandPath()] = "public"
		}
		// Canonical names own all aliases; no command execution or framework init.
		for _, child := range cmd.Commands() {
			walk(child, isHidden)
		}
	}
	walk(root, false)
	distributedSameKeys(t, "public CLI", cli, actualCLI)
	exclusions := map[string]string{}
	for _, excluded := range matrix.ExcludedCLI {
		if excluded.Reason == "" {
			t.Fatal("hidden exclusion needs reason")
		}
		insert(exclusions, excluded.Path, excluded.Reason)
		if cli[excluded.Path] != "" {
			t.Fatalf("hidden CLI exposed: %s", excluded.Path)
		}
	}
	distributedSameKeys(t, "hidden CLI exclusions", exclusions, hidden)
	distributedSameKeys(t, "public HTTP source registrations", http, distributedHTTPRegistrations(t, repo))
	t.Logf("structural only: %d owners, %d public CLI nodes, %d HTTP registrations, %d hidden exclusions", len(skills), len(cli), len(http), len(hidden))
}

func distributedSameKeys(t *testing.T, label string, declared, actual map[string]string) {
	t.Helper()
	var missing, stale []string
	for key := range actual {
		if _, ok := declared[key]; !ok {
			missing = append(missing, key)
		}
	}
	for key := range declared {
		if _, ok := actual[key]; !ok {
			stale = append(stale, key)
		}
	}
	sort.Strings(missing)
	sort.Strings(stale)
	if len(missing)+len(stale) > 0 {
		t.Errorf("%s coverage drift; missing=%q stale=%q", label, missing, stale)
	}
}

// Parse public Echo registrations without constructing a service/server. Current
// public registration receivers are e (root) and g (/v1). Fail closed on new
// registration shapes rather than silently excluding dynamic routes. This does
// not prove middleware execution, handler behavior or live route reachability.
func distributedHTTPRegistrations(t *testing.T, repo string) map[string]string {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(repo, "internal/api/*.go"))
	if err != nil {
		t.Fatal(err)
	}
	files := []*ast.File{}
	fset := token.NewFileSet()
	constants := map[string]string{}
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, file)
		ast.Inspect(file, func(node ast.Node) bool {
			spec, ok := node.(*ast.ValueSpec)
			if !ok {
				return true
			}
			for i, value := range spec.Values {
				literal, ok := value.(*ast.BasicLit)
				if ok && literal.Kind == token.STRING && i < len(spec.Names) {
					constants[spec.Names[i].Name], _ = strconv.Unquote(literal.Value)
				}
			}
			return true
		})
	}
	routes := map[string]string{}
	groups := 0
	dynamic := 0
	for _, file := range files {
		// Resolve the console domain loop from its actual typed literal and constants.
		domains := []string{}
		ast.Inspect(file, func(node ast.Node) bool {
			loop, ok := node.(*ast.RangeStmt)
			if !ok {
				return true
			}
			values, ok := loop.X.(*ast.CompositeLit)
			if !ok {
				return true
			}
			array, ok := values.Type.(*ast.ArrayType)
			if !ok {
				return true
			}
			kind, ok := array.Elt.(*ast.Ident)
			if !ok || kind.Name != "consoleDomain" {
				return true
			}
			for _, element := range values.Elts {
				name, ok := element.(*ast.Ident)
				if !ok || constants[name.Name] == "" {
					t.Fatal("unsupported console domain registration")
				}
				domains = append(domains, constants[name.Name])
			}
			return true
		})
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			receiver, ok := selector.X.(*ast.Ident)
			if !ok {
				return true
			}
			method := selector.Sel.Name
			if method == "Group" && receiver.Name == "e" {
				if len(call.Args) != 1 {
					t.Fatal("unsupported HTTP group")
				}
				literal, ok := call.Args[0].(*ast.BasicLit)
				if !ok || literal.Value != `"/v1"` {
					t.Fatal("HTTP group prefix drift")
				}
				groups++
			}
			switch method {
			case "GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS", "Any", "Match":
			default:
				return true
			}
			if receiver.Name != "e" && receiver.Name != "g" {
				t.Fatalf("unreviewed HTTP receiver %s at %s", receiver.Name, fset.Position(call.Pos()))
			}
			if len(call.Args) < 2 {
				t.Fatal("unsupported route registration")
			}
			prefix := ""
			if receiver.Name == "g" {
				prefix = "/v1"
			}
			routePaths := []string{}
			if literal, ok := call.Args[0].(*ast.BasicLit); ok && literal.Kind == token.STRING {
				value, err := strconv.Unquote(literal.Value)
				if err != nil {
					t.Fatal(err)
				}
				routePaths = append(routePaths, value)
			} else {
				expression, ok := call.Args[0].(*ast.BinaryExpr)
				if !ok || expression.Op != token.ADD || receiver.Name != "e" || method != "GET" {
					t.Fatal("unreviewed dynamic route")
				}
				literal, ok := expression.X.(*ast.BasicLit)
				conversion, conversionOK := expression.Y.(*ast.CallExpr)
				if !ok || literal.Value != `"/console/"` || !conversionOK || len(conversion.Args) != 1 {
					t.Fatal("unreviewed console route expression")
				}
				name, ok := conversion.Fun.(*ast.Ident)
				if !ok || name.Name != "string" {
					t.Fatal("unreviewed console route conversion")
				}
				variable, ok := conversion.Args[0].(*ast.Ident)
				if !ok || variable.Name != "domain" || len(domains) == 0 {
					t.Fatal("unreviewed console domain expression")
				}
				for _, domain := range domains {
					routePaths = append(routePaths, "/console/"+domain)
				}
				dynamic++
			}
			for _, path := range routePaths {
				key := method + " " + prefix + path
				if routes[key] != "" {
					t.Fatalf("duplicate route %s", key)
				}
				routes[key] = fset.Position(call.Pos()).String()
			}
			return true
		})
	}
	if groups != 1 || dynamic != 1 {
		t.Fatalf("registration layout drift: /v1 groups=%d dynamic console loops=%d", groups, dynamic)
	}
	return routes
}
