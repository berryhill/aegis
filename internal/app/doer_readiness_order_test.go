package app

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

// Transport qualification belongs to the credential-resolution stage. Repeating
// it after receipt/runtime readiness can overwrite an earlier ready row while
// leaving downstream rows ready. Keep this ordering invariant explicit without
// bypassing the production transport fingerprint gate for a synthetic fixture.
func TestDoerReadinessQualifiesTransportOnlyThroughProviderResolution(t *testing.T) {
	f, err := parser.ParseFile(token.NewFileSet(), "doer_readiness.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	resolutions := 0
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if sel.Sel.Name == "QualifyProviderTransport" {
			t.Error("readiness repeats transport qualification outside provider resolution")
		}
		if sel.Sel.Name == "resolveControllerProviderAuthentication" {
			resolutions++
		}
		return true
	})
	if resolutions != 1 {
		t.Fatalf("provider resolutions = %d, want 1", resolutions)
	}
}
