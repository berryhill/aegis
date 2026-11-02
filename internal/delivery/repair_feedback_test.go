package delivery

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestConcreteRepairFeedbackPinnedAuthoritativeFacts(t *testing.T) {
	i := Intent{ID: digest("intent"), Operation: CheckDelivery, Repository: "owner/repo", Head: strings.Repeat("a", 40), BaseHead: strings.Repeat("b", 40), PR: 17}
	f := ReadbackFact{Kind: "check_run", HTTPStatus: 200, Target: ReadbackTarget{Repository: i.Repository, Head: i.Head}, HeadSHA: i.Head, ID: 1, App: &ReadbackUser{ID: 9}, Name: "required-ci", Status: "completed", Conclusion: "failure", ResponseDigest: digest("response")}
	record := func(f ReadbackFact, code string) Record {
		o := Observation{Outcome: Failed, Code: code}
		proof := ReadbackProof{Schema: ReadbackSchema, Intent: i, Result: o, ObservedResult: o, ObservedAt: time.Now().UTC(), Facts: []ReadbackFact{f}}
		wire, _ := json.Marshal(proof)
		o.ProofJSON, o.ProofDigest = string(wire), digest(proof)
		return Record{Intent: i, Outcome: Failed, Observation: o, Revision: 2}
	}
	good := record(f, "ci_failed")
	got, err := ConcreteRepairFeedback(good, []string{"required-ci"})
	if err != nil || got.Check != "required-ci" || got.Expected != "success" || got.Observed != "completed_failure" || got.ResponseDigest != f.ResponseDigest {
		t.Fatalf("concrete check: %+v %v", got, err)
	}
	for _, mode := range []string{"head", "required", "success", "unsafe", "oversize", "proof"} {
		t.Run(mode, func(t *testing.T) {
			bad := f
			names := []string{"required-ci"}
			switch mode {
			case "head":
				bad.HeadSHA = strings.Repeat("c", 40)
			case "required":
				names = []string{"other"}
			case "success":
				bad.Conclusion = "success"
			case "unsafe":
				bad.Name = "bearer secret"
				names = []string{bad.Name}
			case "oversize":
				bad.Name = strings.Repeat("x", 129)
				names = []string{bad.Name}
			}
			r := record(bad, "ci_failed")
			if mode == "proof" {
				r.ProofDigest = digest("tamper")
			}
			if _, err := ConcreteRepairFeedback(r, names); err == nil {
				t.Fatal("untrusted/irrelevant failure accepted")
			}
		})
	}
	dirty := false
	pull := ReadbackFact{Kind: "pull", HTTPStatus: 200, Target: ReadbackTarget{Repository: i.Repository, PR: 17}, Number: 17, Head: &ReadbackBranch{SHA: i.Head}, Base: &ReadbackBranch{SHA: i.BaseHead}, Mergeable: &dirty, MergeableState: "dirty", ResponseDigest: digest("pull")}
	got, err = ConcreteRepairFeedback(record(pull, "merge_conflict"), nil)
	if err != nil || got.Expected != "mergeable_clean" || got.Observed != "mergeable_false_dirty" {
		t.Fatalf("concrete conflict: %+v %v", got, err)
	}
}
