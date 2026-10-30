package managergateway

import (
	"context"
	"strings"
	"testing"
)

func TestRepairRegistrationGeneralAndDegraded(t *testing.T) {
	a, _, sub, _, _ := contributionFixture(t)
	m := &platformProposalModel{}
	g, id, token := platformGateway(t, a, sub, m)
	for _, text := range []string{"can you register an agent?", "please add an agent"} {
		if _, handled, err := DispatchDeterministicAegisTurn(context.Background(), a, sub, text); handled || err != nil {
			t.Fatalf("swallowed %q: %v", text, err)
		}
		m.propose(t, "agents.register", map[string]any{})
		out := platformTurn(t, g, sub, id, token, text)
		if !strings.Contains(out.Message, "missing_input") || m.calls == 0 {
			t.Fatal(out)
		}
		if fallback, ok := RegistrationFallback(text); !ok || fallback.Data["registered"] != false {
			t.Fatal("degraded fallback missing")
		}
	}
}
