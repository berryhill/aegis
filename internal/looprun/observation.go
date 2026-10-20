package looprun

import "time"

// Observation is content-free interpreter history, committed atomically with the
// cursor it describes. Ordinal is local to its containing checkpoint. It is not
// runtime authority, a verification receipt, or Queue terminal disposition.
// Empty observations on old checkpoints mean unknown, not inferred boundaries.
type Observation struct {
	Kind         string    `json:"kind"`
	StepID       string    `json:"step_id"`
	Visit        uint32    `json:"visit"`
	Retry        uint16    `json:"retry"`
	Pass         uint16    `json:"pass"`
	At           time.Time `json:"at"`
	TransitionID string    `json:"transition_id,omitempty"`
	ToStepID     string    `json:"to_step_id,omitempty"`
	Outcome      string    `json:"outcome,omitempty"`
	// FailureCode is populated only by the authenticated sanitized trace projection.
	FailureCode string `json:"failure_code,omitempty"`
}

func (c *Cursor) observe(kind, outcome string) {
	o := Observation{Kind: kind, StepID: c.StepID, Visit: c.Visits[c.StepID], Pass: c.ImplementationPass, At: time.Now().UTC(), Outcome: outcome}
	if c.Pending != nil {
		o.Retry = c.Pending.Attempt
	}
	c.Observations = append(c.Observations, o)
}
