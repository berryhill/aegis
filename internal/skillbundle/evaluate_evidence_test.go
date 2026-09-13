package skillbundle

import (
	"encoding/json"
	"testing"
)

// Fixture validation never executes a model, an adapter, or a runtime. Its
// serialized result must not imply that any of those evidence gates passed.
func TestEvaluationReportsOnlyStructuralEvidence(t *testing.T) {
	root := repositoryRoot(t)
	manifest, err := Validate(root)
	if err != nil {
		t.Fatal(err)
	}
	result, err := Evaluate(root, manifest)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"passed", "behavioral_passed"} {
		if _, present := fields[field]; present {
			t.Errorf("misleading evidence field %q in %s", field, data)
		}
	}
	if fields["evidence_class"] != "structural_fixture_validation" || fields["status"] != "valid" {
		t.Errorf("missing explicit evidence scope: %s", data)
	}
	if fields["structurally_valid"] != float64(result.Cases) {
		t.Errorf("missing structural count: %s", data)
	}
	if fields["behavioral_execution"] != "not_run" || fields["service_execution"] != "not_run" || fields["runtime_execution"] != "not_run" {
		t.Errorf("unexecuted gates must be explicit: %s", data)
	}
}
