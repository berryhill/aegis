package command

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"
)

// Dispatch qualification is deliberately separate from the real owning-service
// recipe tests. A help node or an HTTP registration cannot satisfy this check.
func TestOwningServiceInventoryMatchesDispatch(t *testing.T) {
	data, err := os.ReadFile("../../skills/aegis/references/operation-matrix.v1.json")
	if err != nil {
		t.Fatal(err)
	}
	var matrix struct {
		Online map[string][]string `json:"owning_service_cli"`
	}
	if err := json.Unmarshal(data, &matrix); err != nil {
		t.Fatal(err)
	}
	file, err := parser.ParseFile(token.NewFileSet(), "fleet_online.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	actual := map[string]string{}
	ast.Inspect(file, func(n ast.Node) bool {
		sw, ok := n.(*ast.SwitchStmt)
		if !ok {
			return true
		}
		name, ok := sw.Tag.(*ast.Ident)
		if !ok || name.Name != "operation" {
			return true
		}
		for _, statement := range sw.Body.List {
			clause := statement.(*ast.CaseClause)
			for _, expr := range clause.List {
				literal, ok := expr.(*ast.BasicLit)
				if !ok || literal.Kind != token.STRING {
					t.Fatal("nonliteral dispatch requires inventory review")
				}
				value, err := strconv.Unquote(literal.Value)
				if err != nil {
					t.Fatal(err)
				}
				actual[value] = "dispatch"
			}
		}
		return false
	})
	if len(actual) == 0 {
		t.Fatal("no actual operation switch")
	}
	declared := map[string]string{}
	for _, operation := range matrix.Online["shipped"] {
		if declared[operation] != "" {
			t.Fatalf("duplicate %s", operation)
		}
		declared[operation] = "shipped"
	}
	distributedSameKeys(t, "owning-service dispatch (not help)", declared, actual)
	if len(matrix.Online) != 4 {
		t.Fatal("missing closed availability tiers")
	}
	for _, classification := range []string{"missing-adapter", "advisory", "unavailable"} {
		if len(matrix.Online[classification]) == 0 {
			t.Fatalf("missing %s", classification)
		}
		for _, operation := range matrix.Online[classification] {
			if actual[operation] != "" {
				t.Errorf("supported dispatch misclassified: %s", operation)
			}
		}
	}
	// The protected terminal decision path is explicitly not an agent approval
	// operation, although it has a distinct product-owned dispatch before switch.
	source, err := os.ReadFile("fleet_online.go")
	if err != nil {
		t.Fatal(err)
	}
	if len(matrix.Online["advisory"]) != 1 || matrix.Online["advisory"][0] != "loops setup-approve" || !strings.Contains(string(source), "runDoerProtectedSetup(cmd, args, options)") {
		t.Fatal("protected operator boundary drift")
	}
}
