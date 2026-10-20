package orchestration

import (
	"github.com/berryhill/aegis/internal/looprun"
	"testing"
	"time"
)

func TestDoerObservationClosedVocabulary(t *testing.T) {
	good := looprun.Observation{Kind: "step_start", StepID: "implement", At: time.Now().UTC(), Outcome: "running"}
	if !validDoerObservation(good) {
		t.Fatal("valid start rejected")
	}
	for _, change := range []func(*looprun.Observation){
		func(o *looprun.Observation) { o.Kind = "model_claim" },
		func(o *looprun.Observation) { o.Outcome = "raw provider error" },
		func(o *looprun.Observation) { o.At = time.Time{} },
		func(o *looprun.Observation) { o.StepID = "" },
	} {
		bad := good
		change(&bad)
		if validDoerObservation(bad) {
			t.Fatalf("unsafe observation accepted: %+v", bad)
		}
	}
	edge := good
	edge.Kind = "transition"
	edge.Outcome = "taken"
	if validDoerObservation(edge) {
		t.Fatal("unbound transition accepted")
	}
	edge.TransitionID = "verify-retry"
	edge.ToStepID = "implement"
	if !validDoerObservation(edge) {
		t.Fatal("bound transition rejected")
	}
}
