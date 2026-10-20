package orchestration

import "github.com/berryhill/aegis/internal/looprun"

func validDoerObservation(o looprun.Observation) bool {
	if o.FailureCode != "" || o.At.IsZero() || o.StepID == "" || len(o.StepID) > 255 || len(o.TransitionID) > 255 || len(o.ToStepID) > 255 {
		return false
	}
	switch o.Kind {
	case "step_start":
		return o.Outcome == "running"
	case "step_finish":
		return o.Outcome == "succeeded" || o.Outcome == "failed" || o.Outcome == "denied" || o.Outcome == "cancelled"
	case "step_retry":
		return o.Outcome == "retry"
	case "transition":
		return o.Outcome == "taken" && o.TransitionID != "" && o.ToStepID != ""
	case "verification":
		return o.Outcome == "passed" || o.Outcome == "failed"
	case "interpreter_terminal":
		return o.Outcome == "succeeded" || o.Outcome == "failed" || o.Outcome == "denied" || o.Outcome == "cancelled"
	default:
		return false
	}
}
