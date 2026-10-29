package app

import (
	"github.com/berryhill/aegis/internal/delivery"
	"testing"
)

func TestRetainedDeliveryReviewedRequirements(t *testing.T) {
	p := ContributionDeliveryPolicy{RequiredChecks: []string{"ci"}, RequiredReviewers: []string{"reviewer"}, PostMergeChecks: []string{"post"}, ProtectionPolicy: "protected", MergeMethod: "squash", Tree: "tree", Workspace: "work"}
	r := delivery.ReadbackRequirements{Checks: []string{"ci"}, Reviewers: []string{"reviewer"}, PostChecks: []string{"post"}, Protection: "protected", Method: "squash", Tree: "tree", Worktree: "work", Policy: p.Digest(), Delivery: "no-merge"}
	if !retainedDeliveryRequirements(&r, p, "no-merge") {
		t.Fatal("exact reviewed requirements rejected")
	}
	for _, kind := range []string{"checks", "reviewers", "post", "protection", "tree", "scope", "policy"} {
		copy := r
		switch kind {
		case "checks":
			copy.Checks = []string{"other"}
		case "reviewers":
			copy.Reviewers = nil
		case "post":
			copy.PostChecks = nil
		case "protection":
			copy.Protection = "other"
		case "tree":
			copy.Tree = "other"
		case "scope":
			copy.Delivery = "verified-protected-base-merge"
		case "policy":
			copy.Policy = "other"
		}
		if retainedDeliveryRequirements(&copy, p, "no-merge") {
			t.Fatal("substitution accepted", kind)
		}
	}
	if retainedDeliveryRequirements(nil, p, "no-merge") {
		t.Fatal("missing requirements accepted")
	}
}

func TestRetainedDeliveryClosedScope(t *testing.T) {
	for _, scope := range []string{"local-only", "no-publish", "no-merge", "verified-protected-base-merge"} {
		ops := retainedDeliveryRequired(scope)
		if len(ops) == 0 || ops[0] != delivery.Commit {
			t.Fatal(scope)
		}
	}
	if retainedDeliveryRequired("model-PASS") != nil {
		t.Fatal("unknown scope accepted")
	}
}
