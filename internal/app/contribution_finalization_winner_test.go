package app

import (
	"github.com/berryhill/aegis/internal/delivery"
	"github.com/berryhill/aegis/internal/evidence"
	"testing"
)

func TestContributionWinnerRequiresExactVerifiedRoot(t *testing.T) {
	a := &ContributionPreClosureAuthorization{owner: "owner", key: "key", run: "run", contract: "contract", candidate: "candidate", policy: "policy", native: "native", claim: "claim", attempt: "attempt", scope: "verified-protected-base-merge", outcome: "succeeded"}
	ref := delivery.Reference{Run: "run", Contract: a.contract, Candidate: a.candidate}
	result := ContributionResult{Report: evidence.ContributionTerminalReport{Owner: a.owner, Key: a.key, Run: ref.Run, Contract: a.contract, Candidate: a.candidate, Policy: a.policy, Native: a.native, Claim: a.claim, Attempt: a.attempt, Scope: a.scope, Outcome: a.outcome, PreClosure: "preclosure"}}
	// Relationship-only unit fixture. Production always calls strict canonical
	// result getter first; this predicate cannot mint or index completion evidence.
	if !contributionWinnerMatches(result, a, "preclosure", ref) {
		t.Fatal("exact witness mismatch")
	}
	for _, kind := range []string{"owner", "key", "run", "contract", "candidate", "policy", "native", "claim", "attempt", "scope", "outcome", "cause", "preclosure"} {
		bad := result
		switch kind {
		case "owner":
			bad.Report.Owner = "other"
		case "key":
			bad.Report.Key = "other"
		case "run":
			bad.Report.Run = "other"
		case "contract":
			bad.Report.Contract = "other"
		case "candidate":
			bad.Report.Candidate = "other"
		case "policy":
			bad.Report.Policy = "other"
		case "native":
			bad.Report.Native = "other"
		case "claim":
			bad.Report.Claim = "other"
		case "attempt":
			bad.Report.Attempt = "other"
		case "scope":
			bad.Report.Scope = "local-only"
		case "outcome":
			bad.Report.Outcome = "failed"
		case "cause":
			bad.Report.Cause = "other"
		case "preclosure":
			bad.Report.PreClosure = "other"
		}
		if contributionWinnerMatches(bad, a, "preclosure", ref) {
			t.Fatal("unrelated indexed result accepted", kind)
		}
	}
	for _, badRef := range []delivery.Reference{{Run: "other", Contract: ref.Contract, Candidate: ref.Candidate}, {Run: ref.Run, Contract: "other", Candidate: ref.Candidate}, {Run: ref.Run, Contract: ref.Contract, Candidate: "other"}} {
		if contributionWinnerMatches(result, a, "preclosure", badRef) {
			t.Fatal("mismatched requested reference accepted", badRef)
		}
	}
	if contributionWinnerMatches(result, nil, "preclosure", ref) {
		t.Fatal("zero authorization accepted")
	}
}
