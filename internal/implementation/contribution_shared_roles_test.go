package implementation

import "testing"

func TestContributionRoleSameAgentRequiresSeparateExecution(t *testing.T) {
	builder := roleIdentity("builder")
	tester := roleIdentity("tester")
	tester.AgentDigest = builder.AgentDigest
	tester.CharterDigest = builder.CharterDigest
	if !independentContributionRole(builder, tester) {
		t.Fatal("same Agent and charter denied with separate execution")
	}
	for _, field := range []string{"session", "authority", "mandate", "runtime"} {
		t.Run(field, func(t *testing.T) {
			changed := tester
			switch field {
			case "session":
				changed.SessionID = builder.SessionID
			case "authority":
				changed.AuthorityID = builder.AuthorityID
			case "mandate":
				changed.MandateID = builder.MandateID
			case "runtime":
				changed.RuntimeSessionID = builder.RuntimeSessionID
			}
			if independentContributionRole(builder, changed) {
				t.Fatal("substituted execution binding admitted")
			}
		})
	}
}
