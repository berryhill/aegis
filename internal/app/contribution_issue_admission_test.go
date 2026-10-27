package app

import (
	"context"
	"testing"

	"github.com/berryhill/aegis/internal/delivery"
)

func TestContributionIssueAdmissionRequiresVerifierOwnedLiveFrame(t *testing.T) {
	for name, adapter := range map[string]*ContributionCompletionAdapter{
		"nil":      nil,
		"zero":     {},
		"no-frame": {verifier: &ContributionFinalVerifier{service: &Service{}}},
	} {
		t.Run(name, func(t *testing.T) {
			for _, operation := range []delivery.Operation{delivery.ReconcileIssue, delivery.Merge, delivery.Commit} {
				if err := adapter.VerifyContributionIssueIntent(context.Background(), delivery.Intent{Operation: operation}); err == nil {
					t.Fatal("caller-built adapter authorized issue intent", operation)
				}
			}
		})
	}
}
