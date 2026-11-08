package contribution

import (
	"github.com/berryhill/aegis/internal/loop"
	"strings"
	"testing"
)

func TestIssueEngineeringScopeIsClosedUntrustedData(t *testing.T) {
	p := loop.IssueEngineeringPolicy{WritablePrefixes: []string{"internal/feature"}, MaxWritableFiles: 4}
	body := "```aegis-engineering\n" + `{"schema_version":"aegis.contribution.issue-engineering.v1","outcome":"repair real behavior","acceptance":"trusted native journeys","writable_files":["internal/feature/value.go","internal/feature/regression_test.go"],"context_files":[],"exclusions":["no network"]}` + "\n```"
	s := Source{Body: body}
	v, err := ParseEngineeringScope(s, p)
	if err != nil || len(v.WritableFiles) != 2 {
		t.Fatal(v, err)
	}
	for _, bad := range []string{strings.Replace(body, "internal/feature/value.go", "../value.go", 1), strings.Replace(body, "internal/feature/value.go", "internal/other/value.go", 1), strings.Replace(body, `"outcome":`, `"unknown":true,"outcome":`, 1), strings.Replace(body, `"outcome":`, `"outcome":"duplicate","outcome":`, 1), body + body, strings.Replace(body, `"exclusions":["no network"]`, `"exclusions":null`, 1)} {
		if _, err := ParseEngineeringScope(Source{Body: bad}, p); err == nil {
			t.Fatal("malformed or expanded issue scope accepted")
		}
	}
	if EngineeringWorktree("/private/template", "run-one") == EngineeringWorktree("/private/template", "run-two") {
		t.Fatal("issue worktrees overlap")
	}
}
