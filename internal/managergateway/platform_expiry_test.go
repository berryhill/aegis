package managergateway

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestPlatformConversationPendingExpiryAndAuthorityInvalidation(t *testing.T) {
	a, _, sub, _, _ := contributionFixture(t)
	now := a.Now()
	a.Now = func() time.Time { return now }
	sub.ExpiresAt = now.Add(20 * time.Minute)
	model := &platformProposalModel{}
	g, id, token := platformGateway(t, a, sub, model)
	original, e := a.GetFleetAgentAs(context.Background(), sub, "contribution-tester", 0)
	if e != nil {
		t.Fatal(e)
	}
	prepare := func() string {
		t.Helper()
		model.propose(t, "agents.disable", map[string]any{"expected": map[string]any{"id": "contribution-tester"}})
		out := platformTurn(t, g, sub, id, token, "Disable the selected participant")
		m := platformDigest.FindStringSubmatch(out.Message)
		if len(m) != 2 {
			t.Fatal(out.Message)
		}
		return m[1]
	}
	digest := prepare()
	now = now.Add(6 * time.Minute)
	out := platformTurn(t, g, sub, id, token, "confirm "+digest)
	if !strings.Contains(out.Message, "confirmation expired") {
		t.Fatal(out.Message)
	}
	current, e := a.GetFleetAgentAs(context.Background(), sub, "contribution-tester", 0)
	if e != nil || current.Revision.Digest != original.Revision.Digest {
		t.Fatalf("expired review mutated: %v", e)
	}
	digest = prepare()
	// Invalidate the captured authenticated principal at the owning service after
	// review. The session token is not independent mutation authority.
	principal := a.Config.Principal.ID
	a.Config.Principal.ID = "replacement-principal"
	out = platformTurn(t, g, sub, id, token, "confirm "+digest)
	if !strings.Contains(out.Message, "operation error") {
		t.Fatal(out.Message)
	}
	a.Config.Principal.ID = principal
	current, e = a.GetFleetAgentAs(context.Background(), sub, "contribution-tester", 0)
	if e != nil || current.Revision.Digest != original.Revision.Digest {
		t.Fatalf("invalid authority mutated: %v", e)
	}
}
