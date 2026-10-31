package app

import (
	"github.com/berryhill/aegis/internal/orchestration"
	"testing"
)

func TestContributionRepairPolicyReconstructedFromNative(t *testing.T) {
	p := ContributionDeliveryPolicy{BaseHead: "base", RepairFailure: "caller", ExpectedOld: "caller", PR: 99}
	c := orchestration.ContributionCandidate{Pass: 2, Digest: "new"}
	r := orchestration.ContributionDeliveryRepair{AfterPass: 1, Failure: "failed", Head: "old", Base: "base", PR: 7, Candidate: "before"}
	got, err := contributionRepairPolicy(p, c, []orchestration.ContributionDeliveryRepair{r})
	if err != nil || got.RepairFailure != r.Failure || got.ExpectedOld != r.Head || got.PR != r.PR || got.BaseHead != p.BaseHead {
		t.Fatal(got, err)
	}
	for _, name := range []string{"pass", "base", "candidate", "pr", "failure", "head"} {
		t.Run(name, func(t *testing.T) {
			bad := r
			switch name {
			case "pass":
				bad.AfterPass = 2
			case "base":
				bad.Base = "foreign"
			case "candidate":
				bad.Candidate = c.Digest
			case "pr":
				bad.PR = 0
			case "failure":
				bad.Failure = ""
			case "head":
				bad.Head = ""
			}
			if _, err := contributionRepairPolicy(p, c, []orchestration.ContributionDeliveryRepair{bad}); err == nil {
				t.Fatal("invalid native successor accepted")
			}
		})
	}
	initial, err := contributionRepairPolicy(p, c, nil)
	if err != nil || initial.RepairFailure != "" || initial.ExpectedOld != "" {
		t.Fatal("caller successor survived absent native lineage")
	}
}
