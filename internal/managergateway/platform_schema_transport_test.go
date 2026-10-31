package managergateway

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	managerdomain "github.com/berryhill/aegis/internal/manager"
	"github.com/berryhill/aegis/internal/managerplatform"
)

func TestPlatformConversationScopedSchemaTransport(t *testing.T) {
	a, _, sub, _, _ := contributionFixture(t)
	caps, err := managerplatform.New(a, sub).Capabilities(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(caps)
	digest := fmt.Sprintf("sha256:%x", sha256.Sum256(encoded))
	largest := caps[0]
	max := 0
	for _, cap := range caps {
		data, err := json.Marshal(cap)
		if err != nil || len(data) > 24<<10 {
			t.Fatalf("capability cannot be discovered completely: %s bytes=%d err=%v", cap.Operation, len(data), err)
		}
		if len(data) > max {
			max = len(data)
			largest = cap
		}
	}
	model := &platformProposalModel{}
	g, id, token := platformGateway(t, a, sub, model)
	model.response, _ = json.Marshal(map[string]any{"schema_version": managerdomain.ResponseSchemaVersion, "kind": "proposal", "message": "schema request, not execution", "proposal": map[string]any{"operation": "platform.discover", "arguments": map[string]any{"operation": largest.Operation, "catalog_digest": digest}}})
	out := platformTurn(t, g, sub, id, token, "Which exact fields do I need?")
	for _, cap := range caps {
		row, _ := json.Marshal([]any{cap.Operation, cap.Mutation, cap.Service, cap.Availability, cap.Reason})
		if !strings.Contains(model.inputs[0], string(row)) {
			t.Fatalf("index metadata missing for %s", cap.Operation)
		}
	}
	t.Logf("catalog bytes=%d operations=%d first prompt bytes=%d", len(encoded), len(caps), len(model.inputs[0]))
	exact, _ := json.Marshal(largest)
	if !strings.Contains(out.Message, string(exact)) {
		t.Fatal("complete schema not delivered")
	}
	model.propose(t, "agents.list", map[string]any{})
	platformTurn(t, g, sub, id, token, "Read fleet identities while I collect those fields")
	if !strings.Contains(model.inputs[1], string(exact)) {
		t.Fatal("schema missing from model context")
	}
	ingress := a.Config.Manager.Ingress
	guard, err := managerdomain.NewGuard(int(ingress.MaximumMessageBytes), ingress.MaximumMessageRunes, ingress.BoundedDecodeDepth, ingress.ScanTimeout)
	if err != nil {
		t.Fatal(err)
	}
	content := []byte(strings.Join(model.inputs, "\n"))
	finding := guard.Inspect(context.Background(), managerdomain.ContentEnvelope{Source: managerdomain.SourceOperation, ManagerID: managerdomain.LogicalAgentID, SecurityContext: managerdomain.SecurityContext, RouteClass: "local", Content: content})
	t.Logf("largest operation=%s schema capability bytes=%d assembled input bytes=%d decision=%s", largest.Operation, max, len(content), finding.Decision)
	if finding.Decision != managerdomain.AllowLocal {
		t.Fatalf("schema discovery cannot pass unchanged guard: %+v", finding)
	}
}
