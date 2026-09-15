package skillbundle

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"testing"
)

// These assertions cover explanatory projections, not live adapter responses.
func TestRegistryFixturesDeclareProjectionAndResponseEnvelope(t *testing.T) {
	var document struct {
		Fixtures []struct {
			ID             string         `json:"id"`
			Representation string         `json:"representation"`
			Result         map[string]any `json:"authoritative_result"`
		} `json:"fixtures"`
	}
	data := mustRead(t, filepath.Join(repositoryRoot(t), "skills/aegis-agent-registry/references/registry-fixtures.json"))
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	var registered, replay map[string]any
	for _, fixture := range document.Fixtures {
		if fixture.Representation != "explanatory_projection_not_wire_response" {
			t.Errorf("%s must explicitly distinguish projection from wire response", fixture.ID)
		}
		if fixture.ID != "registered-exact-participant" && fixture.ID != "duplicate-identical" {
			continue
		}
		agent, ok := fixture.Result["agent"].(map[string]any)
		if !ok || len(fixture.Result) != 2 {
			t.Errorf("%s must use the public {agent, created} envelope", fixture.ID)
			continue
		}
		if fixture.Result["created"] != (fixture.ID == "registered-exact-participant") {
			t.Errorf("%s has incorrect creation status", fixture.ID)
		}
		if fixture.ID == "registered-exact-participant" {
			registered = agent
		} else {
			replay = agent
		}
	}
	if registered == nil || replay == nil || !reflect.DeepEqual(registered, replay) {
		t.Fatal("identical registration replay must retain the same Agent projection")
	}
}
