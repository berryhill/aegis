package app

import (
	"github.com/berryhill/aegis/internal/delivery"
	"testing"
)

func TestRetainedCreationCollectionReadbackStillBindsReturnedPR(t *testing.T) {
	h, p, _ := retainedUpdateFixture(t, 7, 0, 7)
	r := h.Histories[len(h.Histories)-1][0].Record
	proof, err := delivery.ParseReadback(r.Intent, r.Observation)
	if err != nil {
		t.Fatal(err)
	}
	proof.Intent.Operation = delivery.PullRequest
	proof.Intent.PR = 0
	proof.Facts[2].Target.PR = 0 // Scoped GET /pulls collection, not /pulls/7.
	if !retainedDeliveryPublication(proof, p, 7) {
		t.Fatal("valid creation collection readback rejected")
	}
	proof.Facts[2].Target.PR = 99
	if retainedDeliveryPublication(proof, p, 7) {
		t.Fatal("foreign numbered target accepted")
	}
	proof.Intent.Operation = delivery.UpdatePullRequest
	proof.Intent.PR = 7
	proof.Facts[2].Target.PR = 0
	if retainedDeliveryPublication(proof, p, 7) {
		t.Fatal("optional update lost its exact target")
	}
}
