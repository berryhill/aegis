package command

import (
	"context"
	"strings"
	"testing"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/config"
)

// Acquisition errors are normalized before the companion sees them. No native
// helper, network, password, confirmation or fallback is needed by this test.
func TestDoerProtectedSetupUnavailableClassification(t *testing.T) {
	t.Setenv("DISPLAY", "")
	t.Setenv("WAYLAND_DISPLAY", "")
	cases := []struct {
		name    string
		acquire func(context.Context) ([]byte, error)
	}{
		{"no-display", acquireDoerSetupPassword},
		{"missing-helper", func(context.Context) ([]byte, error) {
			return nil, &PassphraseError{Kind: PassphraseUnavailable, reason: "independent protected desktop password helper unavailable"}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.Defaults()
			cfg.API.Console.Origin = "http://127.0.0.1:12345"
			err := executeDoerProtectedSetup(context.Background(), cfg, app.DoerProtectedSetupInput{ID: "offline", ExpectedVersion: 1, Action: "host"}, tc.acquire,
				func(context.Context, app.DoerProtectedSetupReview) string {
					t.Fatal("unavailable input reached confirmation")
					return "approve"
				},
				func(app.DoerProtectedSetupResult) error { t.Fatal("unavailable input emitted approval"); return nil })
			if err == nil || !strings.HasPrefix(err.Error(), "protected_setup_unavailable") {
				t.Fatalf("companion classification: %v", err)
			}
		})
	}
}
