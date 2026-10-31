package manager

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

type changingPlatformCatalog struct {
	caps  []PlatformCapability
	calls int
	err   error
}

func (p *changingPlatformCatalog) Capabilities(context.Context) ([]PlatformCapability, error) {
	p.calls++
	return p.caps, p.err
}
func (*changingPlatformCatalog) Prepare(context.Context, string, json.RawMessage) (PlatformIntent, error) {
	return PlatformIntent{}, errors.New("unexpected prepare")
}
func (*changingPlatformCatalog) Execute(context.Context, PlatformIntent) (any, error) {
	return nil, errors.New("unexpected execute")
}

func catalogSession(t *testing.T, p Platform, g *fakeGateway) *Session {
	t.Helper()
	guard, err := NewGuard(256<<10, 256<<10, 2, 250*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	s, err := NewSession(context.Background(), SessionConfig{SessionID: "catalog-session", SubjectID: "subject", PrincipalID: "principal", Route: certifiedRoute(t), Gateway: g, GatewaySessionID: "gateway-session", Guard: guard, Operations: &fakeOperations{}, Platform: p, Confirm: func(context.Context, string) (bool, error) { t.Fatal("unexpected confirmation"); return false, nil }, Intake: func(context.Context, string) ([]byte, error) { t.Fatal("unexpected intake"); return nil, nil }, Receipt: func(context.Context, SessionReceipt) error { return nil }, MaximumResponseBytes: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestPlatformCatalogDeliveryAndRefresh(t *testing.T) {
	p := &changingPlatformCatalog{caps: []PlatformCapability{{Operation: "agents.list", InputSchema: map[string]any{"type": "object"}, Service: "ListFleetAgentsAs"}}}
	g := &fakeGateway{outputs: [][]byte{messageEnvelope("first"), messageEnvelope("second"), messageEnvelope("third")}}
	s := catalogSession(t, p, g)
	for turn := 0; turn < 3; turn++ {
		if turn == 2 {
			p.caps[0].Operation = "loops.list"
		}
		if _, err := s.Handle(context.Background(), "What can I operate?"); err != nil {
			t.Fatal(err)
		}
	}
	if p.calls != 3 {
		t.Fatalf("catalog admission skipped: %d", p.calls)
	}
	if !strings.Contains(g.inputs[0], `["agents.list",false,`) || !strings.Contains(g.inputs[1], "catalog unchanged:") || strings.Contains(g.inputs[1], `"input_schema"`) || !strings.Contains(g.inputs[2], `["loops.list",false,`) {
		t.Fatal("catalog missing, repeated or changed schema suppressed")
	}
	p.err = errors.New("principal revoked")
	if _, err := s.Handle(context.Background(), "List again"); err == nil || len(g.inputs) != 3 {
		t.Fatal("catalog cache bypassed fresh admission")
	}
	// A fresh conversation has no inherited catalog-delivery claim.
	p.err = nil
	nextGateway := &fakeGateway{outputs: [][]byte{messageEnvelope("fresh")}}
	next := catalogSession(t, p, nextGateway)
	if _, err := next.Handle(context.Background(), "List again"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(nextGateway.inputs[0], `["loops.list",false,`) {
		t.Fatal("new session inherited old catalog cache")
	}
}

func TestPlatformCatalogFailedDeliveryIsNotCached(t *testing.T) {
	p := &changingPlatformCatalog{caps: []PlatformCapability{{Operation: "agents.list"}}}
	g := &fakeGateway{}
	s := catalogSession(t, p, g)
	if _, err := s.Handle(context.Background(), "List agents"); err == nil {
		t.Fatal("expected transport failure")
	}
	g.outputs = [][]byte{messageEnvelope("retry")}
	if _, err := s.Handle(context.Background(), "List agents again"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(g.inputs[1], `["agents.list",false,`) {
		t.Fatal("failed delivery suppressed catalog")
	}
}
