package implementation

import "errors"

// ManagedFailure contains only reviewed controller classifications, never error text.
type ManagedFailure struct {
	State  string `json:"state,omitempty"`
	Reason string `json:"reason,omitempty"`
	Code   string `json:"code,omitempty"`
}

// IsProposalRejection recognizes only a completed turn's reviewed decode rejection.
// It does not classify old missing diagnostics or uncertain transport outcomes.
func IsProposalRejection(v ManagedFailure) bool {
	if v.State != "failed" || v.Reason != "" {
		return false
	}
	switch v.Code {
	case "doer_proposal_size_invalid", "doer_proposal_json_invalid", "doer_proposal_duplicate_field", "doer_proposal_unknown_field", "doer_proposal_shape_invalid", "doer_proposal_base64_invalid", "doer_proposal_trailing_data", "doer_proposal_edits_invalid", "doer_proposal_report_invalid":
		return true
	}
	return false
}

func (e *Executor) failureObserver(r *Record, passID string) func(ManagedFailure) error {
	return func(v ManagedFailure) error {
		if len(r.Passes) == 0 || r.Passes[len(r.Passes)-1].ID != passID || r.Passes[len(r.Passes)-1].Failure != nil {
			return errors.New("managed failure observation rejected")
		}
		r.Passes[len(r.Passes)-1].Failure = &v
		if IsProposalRejection(v) {
			return e.stage(r, passID, "proposal_rejected", v)
		}
		return e.save(*r)
	}
}
