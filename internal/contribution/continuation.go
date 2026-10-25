package contribution

import (
	"github.com/berryhill/aegis/internal/reference"
	"time"
)

// Continuation is an append-only lease fact for one unchanged canonical attempt.
// It grants no authority: the owning application reloads the live controller
// mandate, independent role epochs and native joins before each effect.
type Continuation struct {
	ClaimID    string              `json:"claim_id"`
	AttemptID  string              `json:"attempt_id"`
	Controller reference.DigestRef `json:"controller"`
	Epoch      uint64              `json:"epoch"`
	ExpiresAt  time.Time           `json:"expires_at"`
}

func applyContinuation(c Contract, prior Continuity, e ContinuityEvent, next *Continuity) error {
	r := e.Continuation
	controller, ok := prior.RoleAuthorities["controller"]
	if r == nil || !ok || r.ClaimID == "" || r.AttemptID == "" || r.Controller != controller.Ref() || r.Epoch != controller.Epoch || !r.ExpiresAt.After(e.At) || r.ExpiresAt.After(c.Deadline) {
		return ErrContinuityConflict
	}
	if _, stopped := prior.RoleStops["controller"]; stopped {
		return ErrContinuityFenced
	}
	if prior.Continuation != nil && (prior.Continuation.ClaimID != r.ClaimID || prior.Continuation.AttemptID != r.AttemptID || prior.Continuation.Epoch >= r.Epoch) {
		return ErrContinuityConflict
	}
	copy := *r
	next.Continuation = &copy
	return nil
}
