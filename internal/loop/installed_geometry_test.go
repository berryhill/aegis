package loop

import (
	"encoding/json"
	"os/exec"
	"testing"
)

// Exercise the actual installed-publication generator against production
// validation, not a second hand-written copy of its topology.
func TestInstalledGeometryRevisions(t *testing.T) {
	out, err := exec.Command("python3", "-c", `import json, runpy
m = runpy.run_path('../../scripts/verify-installed-fleet-vertical.py')
print(json.dumps([m['geometry_revision'](i) for i in (2, 3)]))`).CombinedOutput()
	if err != nil {
		t.Fatalf("generate installed fixtures: %v: %s", err, out)
	}
	var candidates []LoopRevision
	if err := json.Unmarshal(out, &candidates); err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 2 {
		t.Fatal("missing fixture matrix")
	}
	for i, candidate := range candidates {
		want := 12
		if i == 1 {
			want = MaxSteps
		}
		if len(candidate.Steps) != want || candidate.Steps[len(candidate.Steps)-1].ID != "s0" {
			t.Fatal("fixture lost boundary or reversed declaration order")
		}
		_, validation, err := NewRevision(candidate)
		if err != nil || validation.Outcome != ValidationValid {
			t.Fatalf("fixture %d: %v: %+v", i, err, validation)
		}
	}
	broken := candidates[0]
	for i := range broken.Steps {
		if broken.Steps[i].Kind == StepGate {
			broken.Steps[i].Gate = nil
		}
	}
	if _, validation, err := NewRevision(broken); err == nil || validation.Outcome == ValidationValid {
		t.Fatal("original missing-exclusive-gate defect was accepted")
	}
}
