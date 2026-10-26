package contribution

// RoleStop retains a role-process-only fact. Native source/check effects are
// separately owned and NOT covered; even an acknowledgement never releases
// source custody or admits a successor epoch without their trusted join.
type RoleStop struct {
	Authority    RoleAuthority `json:"authority"`
	Epoch        uint64        `json:"epoch"`
	PID          int           `json:"pid"`
	ProcessStart string        `json:"process_start"`
	BootID       string        `json:"boot_id,omitempty"`
	ProofKind    string        `json:"proof_kind,omitempty"`
	Acknowledged bool          `json:"acknowledged"`
}

func applyRoleStop(prior Continuity, e ContinuityEvent, next *Continuity) error {
	r := e.RoleStop
	if r == nil || r.Epoch == 0 || r.Epoch != prior.RoleEpochs[r.Authority.Role] || r.Epoch != r.Authority.Epoch || r.PID <= 0 || r.ProcessStart == "" {
		return ErrContinuityConflict
	}
	current, ok := prior.RoleAuthorities[r.Authority.Role]
	if !ok || current != r.Authority || e.SessionID != current.SessionID {
		return ErrContinuityConflict
	}
	old, exists := prior.RoleStops[current.Role]
	switch e.Kind {
	case "role-stop-requested":
		if exists || r.Acknowledged || (r.BootID != "" || r.ProofKind != "") {
			return ErrContinuityFenced
		}
	case "role-stop-acknowledged":
		expected := *r
		expected.Acknowledged = false
		expected.BootID = ""
		expected.ProofKind = ""
		if r.ProofKind != "" && r.ProofKind != "owned-wait.v1" && r.ProofKind != "recovered-pidfd-exit.v1" {
			return ErrContinuityConflict
		}
		if r.ProofKind == "recovered-pidfd-exit.v1" && (current.BootID == "" || current.BootID != r.BootID || current.PID != r.PID || current.ProcessStart != r.ProcessStart) {
			return ErrContinuityConflict
		}
		if !exists || old != expected || !r.Acknowledged || r.BootID == "" {
			return ErrContinuityConflict
		}
	default:
		return ErrContinuityConflict
	}
	next.RoleStops = make(map[string]RoleStop, len(prior.RoleStops)+1)
	for k, v := range prior.RoleStops {
		next.RoleStops[k] = v
	}
	next.RoleStops[current.Role] = *r
	return nil
}
