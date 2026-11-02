package app

import (
	"github.com/berryhill/aegis/internal/delivery"
	"testing"
)

func TestRetainedRepairPRRequiresFailedPredecessor(t *testing.T) {
	p := ContributionDeliveryPolicy{RepairFailure: "failure", ExpectedOld: "old", PR: 17, Branch: "same-branch"}
	p.Binding.Run = "run"
	p.Binding.Contract = "contract"
	p.Binding.Base = "main"
	creation := delivery.Record{Outcome: delivery.Confirmed, Observation: delivery.Observation{PR: 17}, Intent: delivery.Intent{Reference: delivery.Reference{Run: "run", Contract: "contract", Candidate: "before"}, Operation: delivery.PullRequest, Head: "old", Branch: p.Branch, Repository: "owner/repo", RepositoryID: 9}}
	failed := delivery.Record{Outcome: delivery.Failed, Observation: delivery.Observation{Outcome: delivery.Failed, Code: "ci_failed"}, Intent: creation.Intent}
	failed.Intent.ID = "failure"
	failed.Intent.Operation = delivery.CheckDelivery
	failed.Intent.PR = 17
	failed.Intent.Base = "main"
	h := delivery.HistorySnapshot{Histories: [][]delivery.HistoryEnvelope{{{Record: failed}}}}
	if !retainedRepairPRAncestor(h, p, creation) {
		t.Fatal("owned creation lost across productive repair")
	}
	for _, mutation := range []string{"unknown", "pr", "root", "branch", "head", "merge"} {
		t.Run(mutation, func(t *testing.T) {
			bad := failed
			switch mutation {
			case "unknown":
				bad.Outcome = delivery.Unknown
			case "pr":
				bad.Intent.PR++
			case "root":
				bad.Intent.Run = "other"
			case "branch":
				bad.Intent.Branch = "other"
			case "head":
				bad.Intent.Head = "other"
			case "merge":
				bad.Merge = "irreversible"
			}
			h.Histories[0][0].Record = bad
			if retainedRepairPRAncestor(h, p, creation) {
				t.Fatal("unproven predecessor accepted")
			}
		})
	}
}
