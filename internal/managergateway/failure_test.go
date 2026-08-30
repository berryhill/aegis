package managergateway

import (
	"errors"
	"testing"
)

func TestTurnFailuresAreClosedTypedAndSafe(t *testing.T) {
	tests := []struct {
		failure error
		kind    TurnFailureKind
	}{
		{ErrTurnRuntimeUnavailable, TurnFailureRuntimeUnavailable},
		{ErrTurnAuthorityUnavailable, TurnFailureAuthorityUnavailable},
		{ErrTurnAuthorityInvalid, TurnFailureAuthorityInvalid},
		{ErrTurnTimeout, TurnFailureTimeout},
		{ErrTurnProtocol, TurnFailureProtocol},
		{ErrTurnInternal, TurnFailureInternal},
	}
	for _, test := range tests {
		wrapped := errors.Join(errors.New("private diagnostic"), test.failure)
		var failure *TurnFailure
		if !errors.Is(wrapped, test.failure) || !errors.As(wrapped, &failure) {
			t.Fatalf("failure %v is not usable through errors.Is/errors.As", test.failure)
		}
		if failure.Kind() != test.kind {
			t.Fatalf("kind=%q want=%q", failure.Kind(), test.kind)
		}
		if errors.Is(errors.New(test.failure.Error()), test.failure) {
			t.Fatalf("plain text unexpectedly classified as %q", test.kind)
		}
	}
}

func TestTurnFailureForReasonUsesExactClosedReasons(t *testing.T) {
	if got := turnFailureForReason("prefix_manager_authority_unavailable_suffix"); !errors.Is(got, ErrTurnInternal) {
		t.Fatalf("incidental reason token classified as authority failure: %v", got)
	}
	if got := turnFailureForReason("manager_credential_authority_unavailable"); !errors.Is(got, ErrTurnAuthorityUnavailable) {
		t.Fatalf("exact authority reason classified as %v", got)
	}
}
