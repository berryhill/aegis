package loop

import "testing"

func TestEngineeringTemplDerivedWritesStayWithinApprovedPolicy(t *testing.T) {
	scope := IssueEngineeringPolicy{WritablePrefixes: []string{"web"}, MaxWritableFiles: 4}
	native := ContributionExecutionPolicy{TemplConsistency: TemplModuleConsistency, TemplGeneratedWrites: true, Dependencies: &DependencyBundle{}}
	got, e := EngineeringWritableSelection([]string{"web/view.templ", "web/regression_test.go"}, scope, &native)
	if e != nil || len(got) != 3 || got[2] != "web/view_templ.go" {
		t.Fatalf("exact derived pair: %v %v", got, e)
	}
	native.TemplGeneratedWrites = false
	if _, e = EngineeringWritableSelection([]string{"web/view.templ"}, scope, &native); e == nil {
		t.Fatal("consistency implied write authority")
	}
	native.TemplGeneratedWrites = true
	scope.MaxWritableFiles = 2
	if _, e = EngineeringWritableSelection([]string{"web/view.templ", "web/regression_test.go"}, scope, &native); e == nil {
		t.Fatal("generated write escaped cumulative path bound")
	}
	scope.WritablePrefixes = []string{"feature"}
	if _, e = EngineeringWritableSelection([]string{"web/view.templ"}, scope, &native); e == nil {
		t.Fatal("derived path widened prefix permission")
	}
}
