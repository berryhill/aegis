package api

import (
	"github.com/berryhill/aegis/internal/app"
	consoleweb "github.com/berryhill/aegis/web/console"
	"testing"
)

func TestConsoleDoerReadinessIncludesProviderAuthentication(t *testing.T) {
	for _, state := range []string{"ready", "blocked", "not_checked"} {
		model := consoleweb.DoerReviewModel{}
		consoleDoerReadiness(&model, app.DoerCandidateReadiness{ProviderAuthentication: app.DoerPrerequisiteStatus{State: state, Reason: "provider-diagnostic"}}, "")
		count := 0
		for _, row := range model.Prerequisites {
			if row.Label == "Controller provider authentication" {
				count++
				if row.Value != state+" · provider-diagnostic" {
					t.Fatalf("provider row = %+v", row)
				}
			}
		}
		if count != 1 {
			t.Fatalf("provider rows = %d", count)
		}
	}
}
