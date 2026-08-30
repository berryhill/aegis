package managergateway

import (
	"context"
	"errors"
	"fmt"

	managerdomain "github.com/berryhill/aegis/internal/manager"
)

type managerStartupFailure struct {
	reason string
	cause  error
}

func (e *managerStartupFailure) Error() string { return fmt.Sprintf("%s: %v", e.reason, e.cause) }
func (e *managerStartupFailure) Unwrap() error { return e.cause }

func startupFailure(reason string, cause error) error {
	return &managerStartupFailure{reason: reason, cause: cause}
}

func startupFailureReason(err error) (string, bool) {
	var failure *managerStartupFailure
	if errors.As(err, &failure) {
		return failure.reason, true
	}
	return "", false
}

// TurnFailureKind is the closed failure taxonomy emitted by the manager turn
// producer boundary. Adapters must not infer a kind from error text.
type TurnFailureKind uint8

const (
	TurnFailureRuntimeUnavailable TurnFailureKind = iota + 1
	TurnFailureAuthorityUnavailable
	TurnFailureAuthorityInvalid
	TurnFailureTimeout
	TurnFailureProtocol
	TurnFailureInternal
)

// TurnFailure contains only a closed category. It deliberately does not wrap
// producer diagnostics, prompts, model output, or authority details.
type TurnFailure struct {
	kind TurnFailureKind
}

func (e *TurnFailure) Error() string {
	switch e.kind {
	case TurnFailureRuntimeUnavailable:
		return "manager turn runtime unavailable"
	case TurnFailureAuthorityUnavailable:
		return "manager turn authority unavailable"
	case TurnFailureAuthorityInvalid:
		return "manager turn authority invalid"
	case TurnFailureTimeout:
		return "manager turn deadline exceeded"
	case TurnFailureProtocol:
		return "manager turn protocol failure"
	default:
		return "manager turn internal failure"
	}
}

func (e *TurnFailure) Kind() TurnFailureKind {
	if e == nil {
		return TurnFailureInternal
	}
	return e.kind
}

var (
	ErrTurnRuntimeUnavailable   = &TurnFailure{kind: TurnFailureRuntimeUnavailable}
	ErrTurnAuthorityUnavailable = &TurnFailure{kind: TurnFailureAuthorityUnavailable}
	ErrTurnAuthorityInvalid     = &TurnFailure{kind: TurnFailureAuthorityInvalid}
	ErrTurnTimeout              = &TurnFailure{kind: TurnFailureTimeout}
	ErrTurnProtocol             = &TurnFailure{kind: TurnFailureProtocol}
	ErrTurnInternal             = &TurnFailure{kind: TurnFailureInternal}
)

func canonicalTurnFailure(kind TurnFailureKind) error {
	switch kind {
	case TurnFailureRuntimeUnavailable:
		return ErrTurnRuntimeUnavailable
	case TurnFailureAuthorityUnavailable:
		return ErrTurnAuthorityUnavailable
	case TurnFailureAuthorityInvalid:
		return ErrTurnAuthorityInvalid
	case TurnFailureTimeout:
		return ErrTurnTimeout
	case TurnFailureProtocol:
		return ErrTurnProtocol
	default:
		return ErrTurnInternal
	}
}

func normalizeTurnFailure(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return ErrTurnTimeout
	}
	var failure *TurnFailure
	if errors.As(err, &failure) {
		return canonicalTurnFailure(failure.Kind())
	}
	return ErrTurnInternal
}

func turnFailureForReason(reason string) error {
	switch reason {
	case managerdomain.ReasonAuthorityUnavailable:
		return ErrTurnAuthorityUnavailable
	case managerdomain.ReasonAuthorityInvalid:
		return ErrTurnAuthorityInvalid
	case managerdomain.ReasonGatewayProtocol, managerdomain.ReasonResponseInvalid, managerdomain.ReasonRouteMismatch:
		return ErrTurnProtocol
	case managerdomain.ReasonModelAbsent,
		managerdomain.ReasonDigestMismatch,
		managerdomain.ReasonNotCertified,
		managerdomain.ReasonRuntimeUnsupported,
		managerdomain.ReasonContextUnsupported,
		managerdomain.ReasonOllamaUnavailable,
		managerdomain.ReasonModelLoadFailed:
		return ErrTurnRuntimeUnavailable
	default:
		return ErrTurnInternal
	}
}
