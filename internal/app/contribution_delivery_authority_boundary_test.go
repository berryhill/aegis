package app

import (
	"context"
	"errors"
	"testing"

	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/delivery"
	"github.com/berryhill/aegis/internal/reference"
)

// Immutable policy identity is not principal approval. Even a fully populated
// caller intent must not widen the qualified local-only continuity constructor.
// An unwired Service deliberately panics if canonical reads are reached: these
// unsupported operations must deny before any store/reservation/effect access.
func TestContributionDeliveryAuthorityRejectsUnownedRemoteContract(t *testing.T) {
	request := contribution.EffectAdmissionRequest{Role: "controller", RoleAuthority: reference.DigestRef{ID: "controller-session"}}
	for _, op := range []delivery.Operation{delivery.TransferGraph, delivery.UploadBlob, delivery.UploadTree, delivery.UploadCommit, delivery.Publish, delivery.PullRequest, delivery.UpdatePullRequest, delivery.CheckDelivery, delivery.Merge, delivery.ReconcileIssue, "unknown"} {
		t.Run(string(op), func(t *testing.T) {
			intent := delivery.Intent{Operation: op, Actor: "fixture", Repository: "fixture/repo", RepositoryID: 1, Base: "main", Branch: "owned/feature", Head: "reviewed-head", BaseHead: "reviewed-base", PR: 1, Policy: contribution.Digest([]byte("immutable but not approval")), ID: contribution.Digest([]byte("caller intent")), Payload: contribution.Digest([]byte("caller payload"))}
			got, err := NewContributionDeliveryContinuityAuthority(context.Background(), &Service{}, core.Subject{}, "key", "controller-session", request, intent)
			if got != nil || !errors.Is(err, delivery.ErrDenied) {
				t.Fatalf("unsupported authority: %v %v", got, err)
			}
		})
	}
}

func TestContributionDeliveryAuthorityRejectsInvalidContextBeforeRead(t *testing.T) {
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	request := contribution.EffectAdmissionRequest{Role: "controller", RoleAuthority: reference.DigestRef{ID: "controller-session"}}
	intent := delivery.Intent{Operation: delivery.Commit, Policy: "policy", ID: "intent", Payload: "payload"}
	for _, ctx := range []context.Context{nil, cancelled} {
		got, err := NewContributionDeliveryContinuityAuthority(ctx, &Service{}, core.Subject{}, "key", "controller-session", request, intent)
		if got != nil || !errors.Is(err, delivery.ErrDenied) {
			t.Fatalf("invalid context: %v %v", got, err)
		}
	}
}
