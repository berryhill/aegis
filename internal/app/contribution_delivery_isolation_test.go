package app

import (
	"github.com/berryhill/aegis/internal/delivery"
	"testing"
)

// No native build is needed to regress policy ownership at both copy boundaries.
func TestContributionDeliveryPolicyIsolation(t *testing.T) {
	canonical := ContributionDeliveryPolicy{AllowedFiles: []string{"file.go"}, RequiredChecks: []string{"check"}, RequiredReviewers: []string{"reviewer"}, PostMergeChecks: []string{"post"}}
	digest := canonical.Digest()
	resolved := canonical.snapshot()
	resolved.AllowedFiles[0] = "other.go"
	resolved.RequiredChecks[0] = "other-check"
	resolved.RequiredReviewers[0] = "other-reviewer"
	resolved.PostMergeChecks[0] = "other-post"
	if canonical.Digest() != digest {
		t.Fatal("resolution aliases canonical resolver policy")
	}
	resolved = canonical.snapshot()
	plan := resolved.projectPolicy(delivery.Plan{})
	plan.RequiredChecks[0] = "callback-check"
	plan.RequiredReviewers[0] = "callback-reviewer"
	plan.PostMergeChecks[0] = "callback-post"
	if resolved.Digest() != digest || canonical.Digest() != digest {
		t.Fatal("Plan aliases resolved or canonical policy")
	}
}
