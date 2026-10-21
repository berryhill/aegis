package orchestration

import (
	"context"
	"errors"
	"github.com/berryhill/aegis/internal/hostapproval"
)

var (
	ErrDoerControllerUnavailable = errors.New("doer_controller_unavailable")
	ErrDoerRuntimeUnqualified    = errors.New("doer_runtime_unqualified")
	ErrDoerGoIntegrity           = errors.New("doer_go_executable_untrusted")
	ErrDoerLayaIntegrity         = errors.New("doer_laya_executable_untrusted")
	ErrDoerHomeIntegrity         = errors.New("doer_laya_home_untrusted")
)

// DoerPrerequisiteReason is a closed public projection. Never publish backend text.
func DoerPrerequisiteReason(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return "doer_check_cancelled"
	case errors.Is(err, ErrDoerControllerUnavailable):
		return "doer_controller_unavailable"
	case errors.Is(err, ErrDoerRuntimeUnqualified):
		return "doer_runtime_unqualified"
	case errors.Is(err, ErrDoerUnsafeWorkspace):
		return "doer_unsafe_workspace"
	case errors.Is(err, hostapproval.ErrAbsent):
		return "doer_host_approval_absent"
	case errors.Is(err, hostapproval.ErrExpired):
		return "doer_host_approval_expired"
	case errors.Is(err, hostapproval.ErrDenied):
		return "doer_host_approval_invalid"
	case errors.Is(err, ErrDoerHostApprovalRequired):
		return "doer_host_approval_required"
	case errors.Is(err, ErrDoerGoIntegrity):
		return "doer_go_executable_untrusted"
	case errors.Is(err, ErrDoerLayaIntegrity):
		return "doer_laya_executable_untrusted"
	case errors.Is(err, ErrDoerHomeIntegrity):
		return "doer_laya_home_untrusted"
	case errors.Is(err, ErrLocalLayaUnavailable):
		return "local_laya_unavailable"
	default:
		return "doer_prerequisite_unavailable"
	}
}
