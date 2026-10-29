package app

import (
	"reflect"
	"testing"

	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/delivery"
)

// These test capability selection only. Live role, credential, custody and
// repository-actor permission checks remain separate mandatory admission gates.
func TestContributionRemoteScopeIsExplicitForEveryOperation(t *testing.T) {
	repository := "fixture/controlled"
	id := uint64(7)
	operations := []delivery.Operation{delivery.UploadBlob, delivery.UploadTree, delivery.UploadCommit, delivery.Publish, delivery.PullRequest, delivery.UpdatePullRequest, delivery.CheckDelivery, delivery.Merge, delivery.ReconcileIssue}
	readOnly := core.AuthorityContext{Authority: core.EffectiveAuthority{Capabilities: []string{"github.get_repository.v1"}, Credentials: []string{"github/read"}}}
	if approved := contributionApprovedRemoteOperations(readOnly, repository, id); len(approved) != 0 {
		t.Fatal("read reference manufactured remote operation rights")
	}
	for _, op := range operations {
		t.Run(string(op), func(t *testing.T) {
			p := delivery.Plan{Repository: repository, RepositoryID: id}
			i := delivery.Intent{Operation: op, Repository: repository, RepositoryID: id}
			resource := ContributionDeliveryResourceCapability(op, repository, id)
			for _, tc := range []struct {
				name string
				caps []string
				want bool
			}{
				{"none", nil, false},
				{"read credential only", readOnly.Authority.Capabilities, false},
				{"operation only", []string{string(op)}, false},
				{"resource only", []string{resource}, false},
				{"exact one authority", []string{string(op), resource}, true},
				{"other name", []string{string(op), ContributionDeliveryResourceCapability(op, "other/controlled", id)}, false},
				{"reused name", []string{string(op), ContributionDeliveryResourceCapability(op, repository, id+1)}, false},
				{"wildcard", []string{string(op), ContributionDeliveryResourceCapability(op, "*", id)}, false},
				{"prefix", []string{string(op), resource + "/extra"}, false},
				{"whitespace", []string{string(op), resource + " "}, false},
			} {
				t.Run(tc.name, func(t *testing.T) {
					a := core.AuthorityContext{Authority: core.EffectiveAuthority{Capabilities: tc.caps, Credentials: []string{"github/read"}}}
					if got := contributionRemoteScopeAllowed(a, p, i); got != tc.want {
						t.Fatalf("allowed=%v want=%v", got, tc.want)
					}
				})
			}
			a := core.AuthorityContext{Authority: core.EffectiveAuthority{Capabilities: []string{string(op), resource}}}
			i.RepositoryID++
			if contributionRemoteScopeAllowed(a, p, i) {
				t.Fatal("intent resource substitution admitted")
			}
		})
	}
	grant := core.AuthorityContext{}
	for _, op := range []delivery.Operation{delivery.Publish, delivery.Merge} {
		grant.Authority.Capabilities = append(grant.Authority.Capabilities, string(op), ContributionDeliveryResourceCapability(op, repository, id))
	}
	if got := contributionApprovedRemoteOperations(grant, repository, id); !reflect.DeepEqual(got, []delivery.Operation{delivery.Publish, delivery.Merge}) {
		t.Fatalf("pin not exact approved subset: %v", got)
	}
	unknown := delivery.Operation("github.unknown.v1")
	grant.Authority.Capabilities = append(grant.Authority.Capabilities, string(unknown), ContributionDeliveryResourceCapability(unknown, repository, id))
	if hasContributionRemoteCapabilities(grant, unknown, repository, id) {
		t.Fatal("unknown operation granted")
	}
	if hasContributionRemoteCapabilities(grant, delivery.Merge, repository, 0) {
		t.Fatal("empty immutable repository identity granted")
	}
	if ContributionIssueResourceCapability(repository, id) != ContributionDeliveryResourceCapability(delivery.ReconcileIssue, repository, id) {
		t.Fatal("original issue capability contract changed")
	}
}
