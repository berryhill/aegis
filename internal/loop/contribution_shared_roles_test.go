package loop

import (
	"github.com/berryhill/aegis/internal/reference"
	"strings"
	"testing"
)

func TestContributionProfileSharedRoleReferences(t *testing.T) {
	refs := []reference.RevisionRef{}
	for _, id := range []string{"controller", "builder", "tester"} {
		refs = append(refs, reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: id, Revision: 1, Digest: "sha256:" + strings.Repeat("a", 64)})
	}
	for _, sharing := range [][3]int{{0, 0, 0}, {0, 0, 2}, {0, 1, 0}, {0, 1, 1}, {0, 1, 2}} {
		p := ContributionProfile{Controller: refs[sharing[0]], Builder: refs[sharing[1]], Tester: refs[sharing[2]], Repository: "owner/repo", RepositoryID: 7, Base: strings.Repeat("b", 40), ReadAction: "github.read_contribution_issue.v1", Effect: "local-only", DeadlineSeconds: 60, Invariants: []string{"preserve"}, Exclusions: []string{"no publish"}}
		if err := p.Validate(); err != nil {
			t.Fatal(sharing, err)
		}
		for _, role := range []*reference.RevisionRef{&p.Controller, &p.Builder, &p.Tester} {
			original := *role
			role.Digest = "not-a-digest"
			if p.Validate() == nil {
				t.Fatal("malformed exact ref accepted")
			}
			*role = original
		}
	}
}
