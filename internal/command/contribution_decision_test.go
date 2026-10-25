package command

import (
	"context"
	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/config"
	"testing"
)

func TestContributionDecisionNoDisplayPreflight(t *testing.T) {
	t.Setenv("DISPLAY", "")
	t.Setenv("WAYLAND_DISPLAY", "")
	// Production adapter, not a callback: no password intake, HTTP request or
	// authority store can occur when native display readiness is absent.
	if got := executeContributionDecision(context.Background(), config.Config{}, app.ContributionDecisionInput{Key: "key", ID: "intent", Role: "builder"}); got != "unavailable" {
		t.Fatal(got)
	}
}
