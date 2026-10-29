package orchestration

import (
	"errors"
	"github.com/berryhill/aegis/internal/looprun"
	hermesruntime "github.com/berryhill/aegis/internal/runtime/hermes"
)

// doerFailure retains causal errors for controller classification, but its
// presentation and persisted Loop packet contain only a closed code.
type doerFailure struct {
	code  string
	cause error
}

func (e *doerFailure) Error() string { return e.code }
func (e *doerFailure) Unwrap() error { return e.cause }
func (e *doerFailure) As(target any) bool {
	if p, ok := target.(**looprun.Failure); ok {
		*p = &looprun.Failure{Code: e.code, Message: "Doer step failed"}
		return true
	}
	return false
}
func isDoerFailureCode(code string) bool {
	if hermesruntime.IsAttemptFailureReason(code) {
		return true
	}
	switch code {
	case "step_error", "doer_runtime_preparation_failed", "doer_proposal_decode_failed",
		"doer_proposal_output_invalid", "doer_implement_failed", "doer_report_invalid",
		"doer_proposal_size_invalid", "doer_proposal_json_invalid", "doer_proposal_duplicate_field",
		"doer_proposal_unknown_field", "doer_proposal_shape_invalid", "doer_proposal_base64_invalid",
		"doer_proposal_trailing_data", "doer_proposal_edits_invalid", "doer_proposal_report_invalid",
		"doer_selected_edit_missing", "doer_apply_failed",
		"doer_provider_auth_absent", "doer_provider_auth_invalid", "doer_provider_auth_expired",
		"doer_provider_auth_unauthorized", "doer_provider_auth_runtime_unqualified":
		return true
	}
	return false
}

// ReviewedDoerFailureCode exposes the existing closed controller vocabulary to
// presentation adapters. Unknown/backend prose is never a public diagnostic.
func ReviewedDoerFailureCode(code string) string {
	if isDoerFailureCode(code) {
		return code
	}
	return ""
}

func closedDoerFailure(code string, cause error) error {
	if !isDoerFailureCode(code) {
		code = "step_error"
	}
	return &doerFailure{code: code, cause: cause}
}
func doerTurnFailure(result hermesruntime.AttemptTurnResult, err error) error {
	if hermesruntime.IsAttemptFailureReason(result.Attempt.Reason) {
		return closedDoerFailure(result.Attempt.Reason, err)
	}
	for _, auth := range []error{hermesruntime.ErrProviderAuthAbsent, hermesruntime.ErrProviderAuthInvalid, hermesruntime.ErrProviderAuthExpired, hermesruntime.ErrProviderAuthUnauthorized, hermesruntime.ErrProviderAuthRuntimeUnqualified} {
		if errors.Is(err, auth) {
			return closedDoerFailure(auth.Error(), err)
		}
	}
	return closedDoerFailure("doer_runtime_preparation_failed", err)
}
