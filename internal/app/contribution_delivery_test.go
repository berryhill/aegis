package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/berryhill/aegis/internal/delivery"
	"github.com/berryhill/aegis/internal/orchestration"
	"github.com/berryhill/aegis/internal/reference"
)

type reviewedDeliveryFixture struct {
	policy ContributionDeliveryPolicy
	err    error
}

func (r *reviewedDeliveryFixture) ResolveContributionDeliveryPolicy(context.Context, reference.DigestRef) (ContributionDeliveryPolicy, error) {
	return r.policy, r.err
}
func TestContributionDeliverySourceRequiresNativeIntegration(t *testing.T) {
	s, _, subject, loop, reader := contributionFixture(t)
	c, e := s.admitContributionAs(context.Background(), subject, "session", loop, reader.source.URL.URL, "delivery", "local-only", reader)
	if e != nil {
		t.Fatal(e)
	}
	p := ContributionDeliveryPolicy{ControllerSession: "synthetic-controller-session"}
	r := &reviewedDeliveryFixture{policy: p}
	ref := reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: "reviewed-delivery", Digest: p.Digest()}
	if _, e := NewContributionDeliverySource(s, subject, c.Key, ref, r, nil); !errors.Is(e, ErrContributionDeliveryUnavailable) {
		t.Fatal(e)
	}
	gateCalls := 0
	source, e := NewQualificationContributionDeliverySource(s, subject, c.Key, ref, r, func(context.Context, orchestration.ContributionCandidate) error { gateCalls++; return nil })
	if e != nil {
		t.Fatal(e)
	}
	called := false
	// Goes through the actual app handoff: an intake-only profile cannot fake an
	// accepted managed candidate. The gate and effect are never invoked.
	if e = source.WithAccepted(context.Background(), delivery.Reference{}, func(context.Context, delivery.Plan) error { called = true; return nil }); !errors.Is(e, delivery.ErrDenied) {
		t.Fatal(e)
	}
	if called || gateCalls != 0 {
		t.Fatal("unqualified intake reached callback")
	}
	r.err = errors.New("PRIVATE canary")
	if e = source.WithAccepted(context.Background(), delivery.Reference{}, func(context.Context, delivery.Plan) error { called = true; return nil }); e == nil || strings.Contains(e.Error(), "PRIVATE") {
		t.Fatal("resolver detail escaped")
	}
	r.err = nil
	r.policy.Actor = "changed-policy"
	if e = source.WithAccepted(context.Background(), delivery.Reference{}, func(context.Context, delivery.Plan) error { called = true; return nil }); !errors.Is(e, delivery.ErrDenied) {
		t.Fatal("policy substitution accepted")
	}
	if called {
		t.Fatal("callback retained or reached after denial")
	}
}

// Fake owning-runner transport is NOT real #377 runtime/private-proof acceptance.
func TestContributionDeliveryOwningRunnerContext(t *testing.T) {
	s, _, subject, loop, reader := contributionFixture(t)
	c, err := s.admitContributionAs(context.Background(), subject, "session", loop, reader.source.URL.URL, "delivery-context", "local-only", reader)
	if err != nil {
		t.Fatal(err)
	}
	p := ContributionDeliveryPolicy{ControllerSession: "explicit-reviewed-controller"}
	p.Binding.Reference = delivery.Reference{Run: c.RunID, Contract: c.Digest}
	r := &contextDeliveryResolver{policy: p}
	ref := reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: "reviewed-delivery", Digest: p.Digest()}
	effect := context.WithValue(context.Background(), struct{}{}, "owner-private-sentinel")
	calls := 0
	var retained func(context.Context, orchestration.ContributionCandidate) error
	src, err := newContributionDeliverySource(s, subject, c.Key, ref, r, func(ctx context.Context, session string, action func(context.Context, orchestration.ContributionCandidate) error) error {
		calls++
		if session != p.ControllerSession {
			t.Fatal("controller pin inferred/replaced")
		}
		retained = action
		r.effect = effect
		// Candidate is deliberately invalid: only context transport, never acceptance.
		return action(effect, orchestration.ContributionCandidate{})
	})
	if err != nil {
		t.Fatal(err)
	}
	invoked := false
	if err = src.WithAccepted(context.Background(), p.Binding.Reference, func(context.Context, delivery.Plan) error { invoked = true; return nil }); err == nil || invoked || calls != 1 || r.inside < 2 {
		t.Fatalf("transport calls=%d reads=%d invoked=%v err=%v", calls, r.inside, invoked, err)
	}
	before := r.inside
	if retained(effect, orchestration.ContributionCandidate{}) == nil || r.inside != before {
		t.Fatal("late owner callback usable")
	}
}

type contextDeliveryResolver struct {
	policy ContributionDeliveryPolicy
	effect context.Context
	inside int
}

func (r *contextDeliveryResolver) ResolveContributionDeliveryPolicy(ctx context.Context, _ reference.DigestRef) (ContributionDeliveryPolicy, error) {
	if r.effect != nil {
		if ctx != r.effect {
			return ContributionDeliveryPolicy{}, errors.New("effect context replaced")
		}
		r.inside++
	}
	return r.policy, nil
}
