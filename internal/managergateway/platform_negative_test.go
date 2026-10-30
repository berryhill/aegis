package managergateway

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/reference"
	"github.com/berryhill/aegis/internal/registry"
)

// Synthetic proposal producer only: these denials traverse the production
// gateway, manager decoder, owning services and independent durable readback.
func TestPlatformConversationAuthorityNegatives(t *testing.T) {
	a, _, sub, _, _ := contributionFixture(t)
	model := &platformProposalModel{}
	g, id, token := platformGateway(t, a, sub, model)
	ctx := context.Background()
	target := "contribution-tester"
	read := func() app.FleetAgent {
		t.Helper()
		v, e := a.GetFleetAgentAs(ctx, sub, target, 0)
		if e != nil {
			t.Fatal(e)
		}
		return v
	}
	original := read()
	assertUnchanged := func() {
		t.Helper()
		if v := read(); v.Revision.Digest != original.Revision.Digest {
			t.Fatalf("denied turn mutated Registry: %+v", v)
		}
	}
	preview := func() string {
		t.Helper()
		model.propose(t, "agents.disable", map[string]any{"expected": map[string]any{"id": target}})
		out := platformTurn(t, g, sub, id, token, "Disable the selected registered participant")
		m := platformDigest.FindStringSubmatch(out.Message)
		if len(m) != 2 {
			t.Fatal(out.Message)
		}
		return m[1]
	}
	digest := preview()
	platformTurn(t, g, sub, id, token, "cancel")
	out := platformTurn(t, g, sub, id, token, "confirm "+digest)
	if !strings.Contains(out.Message, "no pending") {
		t.Fatal(out.Message)
	}
	assertUnchanged()
	for _, input := range []any{map[string]any{}, map[string]any{"expected": map[string]any{"id": "contribution-*"}}, map[string]any{"expected": map[string]any{"id": target}, "authority": map[string]any{"principal": "owner"}}} {
		model.propose(t, "agents.disable", input)
		out = platformTurn(t, g, sub, id, token, "Change the selected participant lifecycle")
		if strings.Contains(out.Message, "type confirm") {
			t.Fatalf("invalid reference/authority prepared: %s", out.Message)
		}
		assertUnchanged()
	}
	for _, raw := range []string{`{"schema_version":"aegis.manager.response.v1","kind":"proposal","message":"ok","proposal":{"operation":"platform.call","arguments":{"operation":"agents.disable","input":{"expected":{"id":"contribution-tester"},"expected":{"id":"contribution-controller"}}}}}`, `{"broken":`, `{"schema_version":"aegis.manager.response.v1","kind":"proposal","message":"ok","proposal":{"operation":"platform.call","arguments":{"operation":"agents.disable","input":{},"approval":true}}}`} {
		// Decode failures deliberately degrade this runtime; create a fresh session.
		fresh := &platformProposalModel{response: []byte(raw)}
		next, nid, ntoken := platformGateway(t, a, sub, fresh)
		result, e := next.Turn(ctx, sub, nid, ntoken, "Apply the requested participant change")
		if e == nil && strings.Contains(result.Message, "type confirm") {
			t.Fatal("malformed proposal accepted")
		}
		assertUnchanged()
	}
	// A new conversation has no authority to consume an old pending review.
	digest = preview()
	next, nid, ntoken := platformGateway(t, a, sub, &platformProposalModel{})
	out = platformTurn(t, next, sub, nid, ntoken, "confirm "+digest)
	if !strings.Contains(out.Message, "no pending") {
		t.Fatal(out.Message)
	}
	assertUnchanged()
	// A competing canonical revision invalidates exactly the reviewed predecessor.
	ref := reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: target, Revision: original.Revision.Revision, Digest: original.Revision.Digest}
	changed, e := a.SetAgentLifecycleAs(ctx, sub, target, app.SetAgentLifecycleInput{Expected: ref, Lifecycle: registry.LifecycleDisabled})
	if e != nil {
		t.Fatal(e)
	}
	out = platformTurn(t, g, sub, id, token, "confirm "+digest)
	if !strings.Contains(out.Message, "error") {
		t.Fatalf("stale review succeeded: %s", out.Message)
	}
	if v := read(); v.Revision.Digest != changed.Revision.Digest {
		t.Fatal("stale confirmation added revision")
	}
	out = platformTurn(t, g, sub, id, token, "confirm "+digest)
	if !strings.Contains(out.Message, "no pending") {
		t.Fatal(out.Message)
	}
	// Fresh authenticated adapter still denies an expired captured subject.
	expired := sub
	expired.ExpiresAt = a.Now().Add(-time.Second)
	expiredModel := &platformProposalModel{}
	eg, eid, etoken := platformGateway(t, a, sub, expiredModel)
	if _, e = eg.Turn(ctx, expired, eid, etoken, "confirm "+digest); e == nil {
		t.Fatal("expired gateway accepted confirmation")
	}
	// A changed principal cannot reuse the existing manager capability.
	other := sub
	other.PrincipalID = "model-supplied-principal"
	if _, e = g.Turn(ctx, other, id, token, "confirm "+digest); e == nil {
		t.Fatal("foreign principal accepted")
	}
}
