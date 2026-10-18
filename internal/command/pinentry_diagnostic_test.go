package command

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestPinentryNumericDiagnosticNoFallback(t *testing.T) {
	for _, test := range []struct {
		name, code string
		kind       PassphraseErrorKind
	}{
		{"cancelled", "83886179", PassphraseCancelled},
		{"helper-failure", "83886180", PassphraseProtocol},
	} {
		t.Run(test.name, func(t *testing.T) {
			s := testPassphraseService(t, func() string { return "numeric-error" }, func() string { return test.code }, "")
			_, err := s.Acquire(context.Background(), AuthorityPassphraseRequest{Intent: PrincipalPasswordAuthenticate, Diagnostic: io.Discard})
			var pe *PassphraseError
			if !errors.As(err, &pe) || !pe.Interaction || pe.Kind != test.kind {
				t.Fatalf("%v", err)
			}
			if !strings.Contains(err.Error(), "stage=getpin") || !strings.Contains(err.Error(), "code="+test.code) || strings.Contains(err.Error(), "private-helper-diagnostic") || strings.Contains(err.Error(), "fallback") {
				t.Fatalf("unsafe or incomplete diagnostic: %v", err)
			}
		})
	}
}
