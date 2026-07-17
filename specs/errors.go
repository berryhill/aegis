package specs

import (
	"errors"
	"fmt"
)

var (
	ErrAmbiguousStanza      = errors.New("multiple trust stanzas matched")
	ErrApprovalConsumed     = errors.New("approval already consumed")
	ErrApprovalExpired      = errors.New("approval expired")
	ErrApprovalMismatch     = errors.New("approval does not match exact artifact")
	ErrAuthenticationFailed = errors.New("authentication failed")
	ErrCharterUnapproved    = errors.New("charter revision is not approved")
	ErrDefaultDeny          = errors.New("request denied by default")
	ErrMandateExpired       = errors.New("mandate expired")
	ErrNoMatchingStanza     = errors.New("no authorized trust stanza matched")
	ErrProvisioningDenied   = errors.New("provisioning denied")
	ErrRuntimeUnsupported   = errors.New("runtime or runtime version unsupported")
	ErrSessionRevoked       = errors.New("session revoked")
)

type ErrorCode string

const (
	CodeAmbiguous       ErrorCode = "ambiguous"
	CodeDenied          ErrorCode = "denied"
	CodeExpired         ErrorCode = "expired"
	CodeInvalid         ErrorCode = "invalid"
	CodeNotFound        ErrorCode = "not_found"
	CodeRuntimeFailure  ErrorCode = "runtime_failure"
	CodeUnauthenticated ErrorCode = "unauthenticated"
	CodeUnavailable     ErrorCode = "unavailable"
)

type ContractError struct {
	Code      ErrorCode
	Operation string
	Field     string
	Reason    string
	Cause     error
}

func (e *ContractError) Error() string {
	if e.Field != "" {
		return fmt.Sprintf("%s: %s: %s", e.Operation, e.Field, e.Reason)
	}
	return fmt.Sprintf("%s: %s", e.Operation, e.Reason)
}

func (e *ContractError) Unwrap() error { return e.Cause }

type Violation struct {
	Feature string
	Field   string
	Rule    string
}

type ValidationError struct {
	Violations []Violation
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("specification validation failed with %d violation(s)", len(e.Violations))
}
