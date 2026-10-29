package app

import (
	"testing"

	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/delivery"
)

func TestContributionIssueScopeRequiresExactSameAuthority(t *testing.T) {
	p := delivery.Plan{Repository: "fixture/controlled", RepositoryID: 7}
	i := delivery.Intent{Operation: delivery.ReconcileIssue, Repository: p.Repository, RepositoryID: p.RepositoryID}
	operation := string(delivery.ReconcileIssue)
	resource := ContributionIssueResourceCapability(p.Repository, p.RepositoryID)
	for _, tc := range []struct {
		name string
		caps []string
		want bool
	}{
		{"absent", nil, false},
		{"operation only", []string{operation}, false},
		{"resource only", []string{resource}, false},
		{"exact same authority", []string{operation, resource}, true},
		{"other repository", []string{operation, ContributionIssueResourceCapability("other/controlled", 7)}, false},
		{"reused repository name", []string{operation, ContributionIssueResourceCapability(p.Repository, 8)}, false},
		{"operation qualified", []string{operation, "github.merge.v1:repository:fixture/controlled:7"}, false},
		{"case", []string{operation, "github.issue_disposition.v1:repository:Fixture/controlled:7"}, false},
		{"whitespace", []string{operation, resource + " "}, false},
		{"wildcard", []string{operation, "github.issue_disposition.v1:repository:*:7"}, false},
		{"prefix", []string{operation, resource + "/extra"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := core.AuthorityContext{Authority: core.EffectiveAuthority{Capabilities: tc.caps}}
			if got := contributionIssueScopeAllowed(a, p, i); got != tc.want {
				t.Fatalf("allowed=%v want=%v", got, tc.want)
			}
		})
	}
	a := core.AuthorityContext{Authority: core.EffectiveAuthority{Capabilities: []string{operation, resource}}}
	for _, bad := range []delivery.Intent{
		{Operation: delivery.Merge, Repository: p.Repository, RepositoryID: 7},
		{Operation: delivery.ReconcileIssue, Repository: "other/controlled", RepositoryID: 7},
		{Operation: delivery.ReconcileIssue, Repository: p.Repository, RepositoryID: 8},
	} {
		if contributionIssueScopeAllowed(a, p, bad) {
			t.Fatal("mismatched intent admitted")
		}
	}
	if contributionIssueScopeAllowed(a, delivery.Plan{}, i) {
		t.Fatal("empty trusted resource admitted")
	}
}
