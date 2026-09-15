package skillbundle

import (
	"testing"
)

// Metadata validation is not execution evidence. Nevertheless a fixture must
// never advertise routing into an explicitly unavailable operation.
func TestEvaluationRejectsNonShippedRoute(t *testing.T) {
	root := repositoryRoot(t)
	for _, availability := range []string{"unavailable", "", "future"} {
		t.Run(availability, func(t *testing.T) {
			manifest, err := Validate(root)
			if err != nil {
				t.Fatal(err)
			}
			// aegis.route has a shipped happy-path fixture in the distributed suite.
			for i := range manifest.Operations {
				if manifest.Operations[i].Operation == "aegis.route" {
					manifest.Operations[i].Availability = availability
				}
			}
			_, err = Evaluate(root, manifest)
			assertDenial(t, err, "unavailable_evaluation_operation")
		})
	}
}
