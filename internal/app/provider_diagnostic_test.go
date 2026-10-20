package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/berryhill/aegis/internal/runtime/hermes"
)

func TestProviderAuthenticationDiagnosticProjection(t *testing.T) {
	for _, tc := range []struct {
		err  error
		code string
	}{
		{hermes.ErrProviderAuthAbsent, "doer_provider_auth_absent"},
		{hermes.ErrProviderAuthInvalid, "doer_provider_auth_invalid"},
		{hermes.ErrProviderAuthExpired, "doer_provider_auth_expired"},
		{hermes.ErrProviderAuthUnauthorized, "doer_provider_auth_unauthorized"},
		{hermes.ErrProviderAuthRuntimeUnqualified, "doer_provider_auth_runtime_unqualified"},
		{errors.New("doer_provider_auth_native_pool_invalid secret"), ""},
		{nil, ""},
	} {
		if got := ProviderAuthenticationFailureCode(tc.err); got != tc.code {
			t.Fatalf("code=%q want=%q", got, tc.code)
		}
		if tc.err != nil {
			if got := ProviderAuthenticationFailureCode(fmt.Errorf("private text: %w", tc.err)); got != tc.code {
				t.Fatalf("wrapped code=%q want=%q", got, tc.code)
			}
		}
	}
	for _, d := range []hermes.ProviderFormatDiagnostic{{}, {Parameter: "temperature", Code: "unsupported_parameter"}} {
		before, _ := json.Marshal(d)
		after, _ := json.Marshal(ProviderFormatDiagnostic{Parameter: d.Parameter, Code: d.Code})
		if string(before) != string(after) {
			t.Fatalf("wire drift: %s != %s", before, after)
		}
	}
}
