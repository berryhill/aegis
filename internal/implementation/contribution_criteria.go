package implementation

import (
	"bytes"
	"encoding/json"
	"github.com/berryhill/aegis/internal/loop"
)

type CriterionStatus string

const (
	CriterionPassed      CriterionStatus = "passed"
	CriterionFailed      CriterionStatus = "failed"
	CriterionNotObserved CriterionStatus = "not_observed"
)

// Failure packets deliberately contain closed controller codes, never raw test
// output (which can contain secrets). Full bounded output remains private custody.
type CriterionObservation struct {
	Criterion   loop.ContributionCriterion `json:"criterion"`
	Status      CriterionStatus            `json:"status"`
	FailureCode string                     `json:"failure_code,omitempty"`
	Reproduce   loop.RequiredGoTest        `json:"reproduce"`
	Expected    string                     `json:"expected"`
}

func criterionObservations(output []byte, criteria []loop.ContributionCriterion) []CriterionObservation {
	states := map[string]string{}
	dec := json.NewDecoder(bytes.NewReader(output))
	for dec.More() {
		var event struct{ Action, Package, Test string }
		if dec.Decode(&event) != nil {
			states = map[string]string{}
			break
		}
		if event.Test != "" && (event.Action == "pass" || event.Action == "fail" || event.Action == "skip") {
			states[event.Package+"/"+event.Test] = event.Action
		}
	}
	out := make([]CriterionObservation, 0, len(criteria))
	for _, c := range criteria {
		o := CriterionObservation{Criterion: c, Status: CriterionNotObserved, FailureCode: "required_test_not_observed", Reproduce: c.Test, Expected: "required_test_pass"}
		switch states[c.Test.Package+"/"+c.Test.Name] {
		case "pass":
			o.Status = CriterionPassed
			o.FailureCode = ""
		case "fail":
			o.Status = CriterionFailed
			o.FailureCode = "required_test_failed"
		case "skip":
			o.FailureCode = "required_test_skipped"
		}
		out = append(out, o)
	}
	return out
}
