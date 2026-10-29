package implementation

import (
	"errors"
	"github.com/berryhill/aegis/internal/loop"
)

// ErrRecoveryEvidenceInvalid is a closed denial, never proof of no effects.
var ErrRecoveryEvidenceInvalid = errors.New("native_recovery_evidence_invalid")

// Recovery is historical effect-custody readback, never authority or a promise
// that current workspace, lifecycle, source, or successor admission will pass.
// Missing/corrupt custody is not a negative effect witness. Invalid History
// returns the typed ErrRecoveryEvidenceInvalid denial instead of trusted facts.
type Recovery struct {
	Effect     string `json:"effect"`
	Decision   string `json:"decision"`
	Reason     string `json:"reason"`
	NextAction string `json:"next_action"`
}

func (e *Executor) recovery(r Record, c loop.VerifiedImplementation) Recovery {
	v := Recovery{Effect: "unknown", Decision: "unavailable", Reason: "native_recovery_terminal_failure_required", NextAction: "retain_terminal_history"}
	if len(r.Passes) > 0 {
		p := r.Passes[len(r.Passes)-1]
		if p.ContextStateDigest != "" && p.WorkspaceDigest != "" && p.OutputDigest != "" {
			v.Effect = "apply_observed"
		} else if _, proof, err := e.recoveryPreimage(r, c); err == nil && proof == "prepared-no-implement.v1" {
			v.Effect = "no_apply_proven"
		}
	}
	switch r.State {
	case "cancelled", "expired", "revoked", "denied", "interrupted", "running", "needs_input", "succeeded":
		v.Reason = "native_recovery_" + r.State
		return v
	}
	if r.State != "failed" || len(r.Passes) == 0 {
		return v
	}
	if r.ConsumedBefore+uint16(len(r.Passes)) >= uint16(c.MaxPasses) {
		v.Reason = "native_recovery_budget_exhausted"
		return v
	}
	if v.Effect == "unknown" {
		v.Decision = "policy_decision_required"
		v.Reason = "native_recovery_effect_evidence_missing"
		v.NextAction = "preserve_history_no_supported_reconciliation"
		return v
	}
	v.Decision = "linked_requeue_requires_fresh_checks"
	v.Reason = "native_recovery_evidence_available"
	v.NextAction = "request_linked_requeue_under_existing_policy"
	return v
}
