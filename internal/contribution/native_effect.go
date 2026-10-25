package contribution

import "github.com/berryhill/aegis/internal/reference"

// NativeBinding identifies one already-qualified, source-custody-owned command.
// Claim/attempt remain historical Queue identities, never renewed leases.
type NativeBinding struct {
	IntentID      string              `json:"intent_id"`
	RunID         string              `json:"run_id"`
	QueueItemID   string              `json:"queue_item_id"`
	ClaimID       string              `json:"claim_id"`
	AttemptID     string              `json:"attempt_id"`
	Role          string              `json:"role"`
	Epoch         uint64              `json:"epoch"`
	Authority     reference.DigestRef `json:"authority"`
	Candidate     string              `json:"candidate"`
	PolicyDigest  string              `json:"policy_digest"`
	CommandDigest string              `json:"command_digest"`
}
type NativeEffect struct {
	Binding NativeBinding `json:"binding"`
	State   string        `json:"state"`
}

func applyNativeEffect(c Contract, prior Continuity, e ContinuityEvent, next *Continuity) error {
	n := e.NativeEffect
	if n == nil {
		return ErrContinuityConflict
	}
	b := n.Binding
	r, ok := prior.RoleAuthorities[b.Role]
	if !ok || !ValidKey(b.IntentID) || b.RunID != c.RunID || b.QueueItemID != c.QueueItemID || b.ClaimID == "" || b.AttemptID == "" || b.Epoch != r.Epoch || b.Authority != r.Ref() || e.SessionID != r.SessionID || b.Candidate == "" || (e.Kind == "native-reserved" && b.Candidate != prior.Candidate) || b.PolicyDigest != c.TemplateDigest || b.CommandDigest == "" {
		return ErrContinuityConflict
	}
	old, exists := prior.NativeEffects[b.IntentID]
	switch e.Kind {
	case "native-reserved":
		if boundary, ok := prior.NativeBoundaries[b.Role]; ok {
			found := false
			for _, expected := range boundary.Bindings {
				if expected == b {
					found = true
				}
			}
			if boundary.Closed || boundary.Epoch != b.Epoch || !found {
				return ErrContinuityFenced
			}
		}
		if n.State != "reserved" || exists || prior.CancelRequested {
			return ErrContinuityFenced
		}
		if _, stopped := prior.RoleStops[b.Role]; stopped {
			return ErrContinuityFenced
		}
		for _, other := range prior.NativeEffects {
			if other.State != "completed" {
				return ErrNativeQuiescenceUnavailable
			}
		}
	case "native-completed", "native-unknown":
		if !exists || old.Binding != b || old.State != "reserved" {
			return ErrContinuityConflict
		}
		if (e.Kind == "native-completed" && n.State != "completed") || (e.Kind == "native-unknown" && n.State != "unknown") {
			return ErrContinuityConflict
		}
	default:
		return ErrContinuityConflict
	}
	next.NativeEffects = make(map[string]NativeEffect, len(prior.NativeEffects)+1)
	for k, v := range prior.NativeEffects {
		next.NativeEffects[k] = v
	}
	next.NativeEffects[b.IntentID] = *n
	return nil
}
