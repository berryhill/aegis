package managergateway

import (
	"context"
	"strings"
	"testing"

	managerdomain "github.com/berryhill/aegis/internal/manager"
)

// Exercise the bytes assembled by the production session, then the guard used
// by the process-bound inference proxy. The proposal producer alone is synthetic.
func TestPlatformConversationInferenceGuard(t *testing.T) {
	a, _, sub, _, _ := contributionFixture(t)
	model := &platformProposalModel{}
	g, id, token := platformGateway(t, a, sub, model)
	for range 4 {
		model.propose(t, "agents.list", map[string]any{})
		platformTurn(t, g, sub, id, token, "Which registered participants can I use?")
	}
	ingress := a.Config.Manager.Ingress
	guard, err := managerdomain.NewGuard(int(ingress.MaximumMessageBytes), ingress.MaximumMessageRunes, ingress.BoundedDecodeDepth, ingress.ScanTimeout)
	if err != nil {
		t.Fatal(err)
	}
	content := []byte(strings.Join(model.inputs, "\n"))
	finding := guard.Inspect(context.Background(), managerdomain.ContentEnvelope{Source: managerdomain.SourceOperation, ManagerID: managerdomain.LogicalAgentID, SecurityContext: managerdomain.SecurityContext, RouteClass: "local", Content: content})
	t.Logf("assembled model input bytes=%d; decision=%s; reason=%s; detector=%s", len(content), finding.Decision, finding.Reason, finding.DetectorID)
	if finding.Decision != managerdomain.AllowLocal {
		t.Fatalf("production platform input cannot pass inference guard: %+v", finding)
	}
}
