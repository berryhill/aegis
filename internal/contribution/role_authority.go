package contribution

import (
	"encoding/json"
	"errors"
	"github.com/berryhill/aegis/internal/reference"
)

// RoleAuthority is an append-only association, never an authority grant. The
// application must reload and admit its exact context before every use.
var ErrNativeQuiescenceUnavailable = errors.New("contribution native quiescence unavailable")

type RoleAuthority struct {
	Epoch                  uint64              `json:"epoch"`
	SessionID              string              `json:"session_id"`
	RuntimeSessionID       string              `json:"runtime_session_id"`
	Role                   string              `json:"role"`
	Binding                RoleBinding         `json:"binding"`
	Authority              reference.DigestRef `json:"authority"`
	MandateID              string              `json:"mandate_id"`
	OriginalRuntimeBinding reference.DigestRef `json:"original_runtime_binding"`
}

func SealedRole(c Contract, role string) (RoleBinding, bool) {
	i := -1
	switch role {
	case "controller":
		i = 0
	case "builder":
		i = 1
	case "tester":
		i = 2
	}
	if i < 0 || len(c.Roles) != 3 {
		return RoleBinding{}, false
	}
	return c.Roles[i], true
}

func applyRoleAuthority(c Contract, prior Continuity, e ContinuityEvent, next *Continuity) error {
	next.Observations = make(map[string]bool, len(prior.Observations))
	for k, v := range prior.Observations {
		next.Observations[k] = v
	}
	next.RoleAuthorities = make(map[string]RoleAuthority, len(prior.RoleAuthorities))
	for k, v := range prior.RoleAuthorities {
		next.RoleAuthorities[k] = v
	}
	switch e.Kind {
	case "observation":
		if e.RoleAuthority != nil {
			return ErrContinuityConflict
		}
		next.Observations[e.SessionID] = true
	case "role-admitted":
		r := e.RoleAuthority
		if r == nil || !prior.Observations[e.SessionID] {
			return ErrContinuityConflict
		}
		sealed, ok := SealedRole(c, r.Role)
		if !ok || r.Epoch == 0 || sealed != r.Binding || r.Authority.Validate() != nil || r.OriginalRuntimeBinding.Validate() != nil || r.MandateID == "" || r.SessionID != e.SessionID || r.RuntimeSessionID == "" {
			return ErrContinuityConflict
		}
		if old, exists := prior.RoleAuthorities[r.Role]; exists {
			if stop, ok := prior.RoleStops[r.Role]; !ok || !stop.Acknowledged {
				return ErrContinuityFenced
			}
			if !NativeRoleQuiescent(prior, r.Role) {
				return ErrNativeQuiescenceUnavailable
			}
			if old.Binding != r.Binding || old.OriginalRuntimeBinding != r.OriginalRuntimeBinding {
				return ErrContinuityConflict
			}
			next.RoleStops = make(map[string]RoleStop, len(prior.RoleStops))
			for k, v := range prior.RoleStops {
				if k != r.Role {
					next.RoleStops[k] = v
				}
			}
			next.NativeBoundaries = make(map[string]NativeBoundary, len(prior.NativeBoundaries))
			for k, v := range prior.NativeBoundaries {
				if k != r.Role {
					next.NativeBoundaries[k] = v
				}
			}
		}
		if r.Epoch != prior.RoleEpochs[r.Role]+1 {
			return ErrContinuityConflict
		}
		for _, old := range prior.RoleAuthorities {
			if old.Authority == r.Authority || old.MandateID == r.MandateID || old.SessionID == r.SessionID || old.RuntimeSessionID == r.RuntimeSessionID {
				return ErrContinuityConflict
			}
		}
		next.RoleEpochs = make(map[string]uint64, len(prior.RoleEpochs)+1)
		for role, epoch := range prior.RoleEpochs {
			next.RoleEpochs[role] = epoch
		}
		next.RoleEpochs[r.Role] = r.Epoch
		next.RoleAuthorities[r.Role] = *r
	default:
		return ErrContinuityConflict
	}
	return nil
}

// Ref remains stable across unrelated continuity checkpoints.
func (r RoleAuthority) Ref() reference.DigestRef {
	b, _ := json.Marshal(r)
	return reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: r.SessionID, Digest: Digest(b)}
}
