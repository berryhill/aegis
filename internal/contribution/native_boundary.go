package contribution

import "reflect"

// NativeBoundary is a closed producer's complete command inventory, not a list
// of caller-selected completion references. Legacy producers have no boundary.
type NativeBoundary struct {
	Role     string          `json:"role"`
	Epoch    uint64          `json:"epoch"`
	Bindings []NativeBinding `json:"bindings"`
	Closed   bool            `json:"closed"`
}

func applyNativeBoundary(c Contract, prior Continuity, e ContinuityEvent, next *Continuity) error {
	b := e.NativeBoundary
	if b == nil || len(b.Bindings) == 0 {
		return ErrNativeQuiescenceUnavailable
	}
	r, ok := prior.RoleAuthorities[b.Role]
	if !ok || r.Epoch != b.Epoch || e.SessionID != r.SessionID {
		return ErrContinuityConflict
	}
	old, exists := prior.NativeBoundaries[b.Role]
	switch e.Kind {
	case "native-boundary-open":
		if exists || b.Closed {
			return ErrContinuityFenced
		}
		if _, stopped := prior.RoleStops[b.Role]; stopped {
			return ErrContinuityFenced
		}
	case "native-boundary-extend":
		if !exists || old.Closed || b.Closed || old.Epoch != b.Epoch || len(b.Bindings) != len(old.Bindings)+1 || !reflect.DeepEqual(old.Bindings, b.Bindings[:len(old.Bindings)]) {
			return ErrContinuityConflict
		}
	case "native-boundary-closed":
		if !exists || old.Closed || !b.Closed || !reflect.DeepEqual(old.Bindings, b.Bindings) || old.Epoch != b.Epoch {
			return ErrContinuityConflict
		}
	default:
		return ErrContinuityConflict
	}
	seen := map[string]bool{}
	for i, binding := range b.Bindings {
		// Historical prefix is immutable (checked above), not rebound to the
		// current candidate when the controller advances an authoritative phase.
		fresh := e.Kind == "native-boundary-open" || (e.Kind == "native-boundary-extend" && i == len(b.Bindings)-1)
		if seen[binding.IntentID] || !ValidKey(binding.IntentID) || binding.Role != b.Role || binding.Epoch != b.Epoch || binding.Authority != r.Ref() || binding.RunID != c.RunID || binding.QueueItemID != c.QueueItemID || (fresh && binding.Candidate != prior.Candidate) || binding.PolicyDigest != c.TemplateDigest || binding.CommandDigest == "" || binding.ClaimID == "" || binding.AttemptID == "" {
			return ErrContinuityConflict
		}
		seen[binding.IntentID] = true
		if b.Closed {
			n, ok := prior.NativeEffects[binding.IntentID]
			if !ok || n.Binding != binding || n.State != "completed" {
				return ErrNativeQuiescenceUnavailable
			}
		}
	}
	for id, n := range prior.NativeEffects {
		if n.Binding.Role == b.Role && n.Binding.Epoch == b.Epoch && !seen[id] {
			return ErrNativeQuiescenceUnavailable
		}
	}
	next.NativeBoundaries = make(map[string]NativeBoundary, len(prior.NativeBoundaries)+1)
	for k, v := range prior.NativeBoundaries {
		next.NativeBoundaries[k] = v
	}
	next.NativeBoundaries[b.Role] = *b
	return nil
}

func NativeRoleQuiescent(s Continuity, role string) bool {
	r, ok := s.RoleAuthorities[role]
	if !ok {
		return false
	}
	stop, ok := s.RoleStops[role]
	if !ok || !stop.Acknowledged || stop.Authority != r || stop.Epoch != r.Epoch {
		return false
	}
	b, ok := s.NativeBoundaries[role]
	if !ok || !b.Closed || b.Epoch != r.Epoch || len(b.Bindings) == 0 {
		return false
	}
	for _, binding := range b.Bindings {
		n, ok := s.NativeEffects[binding.IntentID]
		if !ok || n.State != "completed" || n.Binding != binding {
			return false
		}
	}
	return true
}
