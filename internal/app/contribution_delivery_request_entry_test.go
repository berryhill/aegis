package app

import (
	"context"
	"errors"
	"strconv"
	"testing"

	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/delivery"
	"github.com/berryhill/aegis/internal/reference"
)

type requestObservingDeliveryPolicy struct {
	service  *Service
	expected core.Subject
	calls    int
}

func (p *requestObservingDeliveryPolicy) ResolveContributionDeliveryPolicy(ctx context.Context, _ reference.DigestRef) (ContributionDeliveryPolicy, error) {
	p.calls++
	subject, err := p.service.CurrentContributionDeliverySubject(ctx)
	if err != nil || core.Digest(subject) != core.Digest(p.expected) {
		return ContributionDeliveryPolicy{}, errors.New("authenticated owning request was not propagated")
	}
	return ContributionDeliveryPolicy{}, delivery.ErrDenied
}
func TestTypedDeliveryBindsRequestBeforePolicyResolution(t *testing.T) {
	s, _, _, lr, reader := contributionFixture(t)
	uid, err := strconv.ParseUint(s.Config.Principal.UID, 10, 32)
	if err != nil {
		t.Fatal(err)
	}
	ctx, sub, err := s.AuthenticateContributionUnixPeerRequest(context.Background(), uint32(uid))
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.admitContributionAs(ctx, sub, "session", lr, reader.source.URL.URL, "typed-request-context", "local-only", reader)
	if err != nil {
		t.Fatal(err)
	}
	resolver := &requestObservingDeliveryPolicy{service: s, expected: sub}
	ref := delivery.Reference{Run: c.RunID, Contract: c.Digest}
	policyRef := reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: "denied-policy", Digest: c.Digest}
	for _, execute := range []bool{false, true} {
		call := func(callCtx context.Context, subject core.Subject) error {
			if execute {
				_, e := s.ExecuteContributionDeliveryAs(callCtx, subject, c.Key, ref, policyRef, resolver, nil, deliveryDenyAuthority{}, deliveryDenyAdapter{}, delivery.Commit)
				return e
			}
			_, e := s.RunContributionDeliveryAs(callCtx, subject, c.Key, ref, policyRef, resolver, deliveryDenyAuthority{}, deliveryDenyAdapter{}, nil, nil)
			return e
		}
		resolver.calls = 0
		if e := call(ctx, sub); !errors.Is(e, delivery.ErrDenied) || resolver.calls != 1 {
			t.Fatalf("execute=%t policy context calls=%d err=%v", execute, resolver.calls, e)
		}
		resolver.calls = 0
		if e := call(context.Background(), sub); !errors.Is(e, delivery.ErrDenied) || resolver.calls != 0 {
			t.Fatalf("subject alone reached policy: %v", e)
		}
		foreign := sub
		foreign.PrincipalID = "foreign"
		if e := call(ctx, foreign); !errors.Is(e, delivery.ErrDenied) || resolver.calls != 0 {
			t.Fatalf("foreign subject reached policy: %v", e)
		}
	}
}

type deliveryDenyAuthority struct{}

func (deliveryDenyAuthority) Admit(context.Context, delivery.Intent, bool) error {
	return delivery.ErrDenied
}

type deliveryDenyAdapter struct{}

func (deliveryDenyAdapter) Apply(context.Context, delivery.Intent, delivery.Plan, delivery.Guard) delivery.Observation {
	panic("denied policy reached effect")
}
func (deliveryDenyAdapter) Reconcile(context.Context, delivery.Intent, delivery.Plan, delivery.Guard) delivery.Observation {
	panic("denied policy reached reconciliation")
}
