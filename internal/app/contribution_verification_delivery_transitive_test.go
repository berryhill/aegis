package app

import (
	"github.com/berryhill/aegis/internal/delivery"
	"testing"
)

func TestRetainedRepairPRTransitiveSuccessors(t *testing.T) {
	p := ContributionDeliveryPolicy{RepairFailure: "failure2", ExpectedOld: "head2", PR: 17, Branch: "same"}
	p.Binding.Run, p.Binding.Contract, p.Binding.Base = "run", "contract", "main"
	create := delivery.Record{Outcome: delivery.Confirmed, Observation: delivery.Observation{PR: 17}, Intent: delivery.Intent{Reference: delivery.Reference{Run: "run", Contract: "contract", Candidate: "one"}, Operation: delivery.PullRequest, Head: "head1", Branch: "same", Base: "main", Repository: "owner/repo", RepositoryID: 9}}
	first := delivery.Record{Outcome: delivery.Failed, Observation: delivery.Observation{Outcome: delivery.Failed, Code: "ci_failed"}, Intent: create.Intent}
	first.Intent.ID, first.Intent.Operation, first.Intent.PR = "failure1", delivery.CheckDelivery, 17
	second := first
	second.Intent.ID, second.Intent.Candidate, second.Intent.Head = "failure2", "two", "head2"
	second.Intent.ExpectedOld, second.Intent.RepairFailure = "head1", "failure1"
	update := delivery.Record{Outcome: delivery.Confirmed, Observation: delivery.Observation{PR: 17}, Intent: second.Intent}
	update.Intent.Operation, update.Intent.ID = delivery.UpdatePullRequest, "update2"
	h := func(records ...delivery.Record) delivery.HistorySnapshot {
		out := delivery.HistorySnapshot{}
		for _, r := range records {
			out.Histories = append(out.Histories, []delivery.HistoryEnvelope{{Record: r}})
		}
		return out
	}
	if !retainedRepairPRAncestor(h(first, second, update), p, create) {
		t.Fatal("transitive owned creation rejected")
	}
	for _, mode := range []string{"missing-update", "foreign-update", "old-head", "pointer", "cycle", "root", "repository", "base", "duplicate-update"} {
		t.Run(mode, func(t *testing.T) {
			a, b, u := first, second, update
			switch mode {
			case "foreign-update":
				u.Intent.Candidate = "foreign"
			case "old-head":
				u.Intent.ExpectedOld = "foreign"
			case "pointer":
				u.Intent.RepairFailure = "foreign"
			case "cycle":
				a.Intent.RepairFailure = "failure2"
				a.Intent.ExpectedOld = "head2"
			case "root":
				b.Intent.Run = "foreign"
			case "repository":
				u.Intent.RepositoryID++
			case "base":
				u.Intent.Base = "foreign"
			}
			history := h(a, b, u)
			if mode == "missing-update" {
				history = h(a, b)
			}
			if mode == "duplicate-update" {
				history = h(a, b, u, u)
			}
			if retainedRepairPRAncestor(history, p, create) {
				t.Fatal("unproven successor accepted")
			}
		})
	}
}
