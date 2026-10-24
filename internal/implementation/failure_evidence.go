package implementation

import "errors"

// ManagedFailure contains only reviewed controller classifications, never error text.
type ManagedFailure struct {
	State  string `json:"state,omitempty"`
	Reason string `json:"reason,omitempty"`
	Code   string `json:"code,omitempty"`
}

func (e *Executor) failureObserver(r *Record, passID string) func(ManagedFailure) error {
	return func(v ManagedFailure) error {
		if len(r.Passes) == 0 || r.Passes[len(r.Passes)-1].ID != passID || r.Passes[len(r.Passes)-1].Failure != nil {
			return errors.New("managed failure observation rejected")
		}
		r.Passes[len(r.Passes)-1].Failure = &v
		return e.save(*r)
	}
}
