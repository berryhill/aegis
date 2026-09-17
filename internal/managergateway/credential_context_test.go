package managergateway

import (
	"context"
	"testing"
)

func TestCredentialFollowupClearedByInterveningTurn(t *testing.T) {
	for _, input := range []string{"list our agents", "tell me about code review", "/status"} {
		t.Run(input, func(t *testing.T) {
			s, subject, token := intakeService(t)
			preparedIntake(t, s, subject, token)
			if input == "/status" {
				_, _ = s.Execute(context.Background(), subject, "test-session", token, input)
			} else {
				_, _ = s.TurnWithProtectedIntake(context.Background(), subject, "test-session", token, input, true)
			}
			result, err := s.TurnWithProtectedIntake(context.Background(), subject, "test-session", token, "make another one", true)
			if err != nil || result.Kind != "credential_context_required" || result.Intake != nil {
				t.Fatalf("stale context reopened intake: %+v %v", result, err)
			}
		})
	}
}
