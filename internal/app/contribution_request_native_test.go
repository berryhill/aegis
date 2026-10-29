package app

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/delivery"
)

func TestContributionNativeRequestProvenance(t *testing.T) {
	s := testService(t)
	now := s.Now()
	s.Now = func() time.Time { return now }
	ctx, sub, err := s.AuthenticateContributionUnixPeerRequest(context.Background(), 4242)
	if err != nil {
		t.Fatal(err)
	}
	if sub.Method != "local-os" || sub.Issuer != "linux-so-peercred" {
		t.Fatal("transport identity changed")
	}
	if _, err = s.bindContributionRequestSubject(context.Background(), sub); !errors.Is(err, delivery.ErrDenied) {
		t.Fatal("subject alone became authentication", err)
	}
	bound, err := s.bindContributionRequestSubject(ctx, sub)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.CurrentContributionDeliverySubject(ctx); !errors.Is(err, delivery.ErrDenied) {
		t.Fatal("transport proof is not a delivery entry", err)
	}
	other := testService(t)
	if _, err = other.bindContributionRequestSubject(ctx, sub); !errors.Is(err, delivery.ErrDenied) {
		t.Fatal("cross-service proof accepted", err)
	}
	for name, mutate := range map[string]func(*core.Subject){
		"principal":       func(v *core.Subject) { v.PrincipalID = "other" },
		"method":          func(v *core.Subject) { v.Method = "password" },
		"issuer":          func(v *core.Subject) { v.Issuer = "local-os" },
		"kind":            func(v *core.Subject) { v.Kind = "service" },
		"uid":             func(v *core.Subject) { v.Claims["uid"] = "1" },
		"future":          func(v *core.Subject) { v.AuthenticatedAt = now.Add(time.Second) },
		"extended-expiry": func(v *core.Subject) { v.ExpiresAt = v.ExpiresAt.Add(time.Second) },
	} {
		t.Run(name, func(t *testing.T) {
			v := sub
			v.Claims = map[string]string{"uid": sub.Claims["uid"], "user": sub.Claims["user"]}
			mutate(&v)
			if _, e := s.bindContributionRequestSubject(ctx, v); !errors.Is(e, delivery.ErrDenied) {
				t.Fatalf("altered subject admitted: %v", e)
			}
		})
	}
	// Contexts hold value snapshots; neither input claims nor returned claims can
	// mutate the request or other concurrent extractions.
	sub.Claims["uid"] = "caller-mutation"
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			v, e := s.CurrentContributionDeliverySubject(bound)
			if e != nil || v.Claims["uid"] != "4242" {
				t.Errorf("snapshot drift: %+v %v", v, e)
				return
			}
			v.Claims["uid"] = "returned-mutation"
		})
	}
	wg.Wait()
	canceled, cancel := context.WithCancel(bound)
	cancel()
	if _, err = s.CurrentContributionDeliverySubject(canceled); !errors.Is(err, delivery.ErrDenied) {
		t.Fatal("canceled request retained authority", err)
	}
	// A new transport authentication may validate the same manager identity, but
	// cannot renew the entry's original authority deadline.
	original, err := s.CurrentContributionDeliverySubject(bound)
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Minute)
	fresh, _, err := s.AuthenticateContributionUnixPeerRequest(context.Background(), 4242)
	if err != nil {
		t.Fatal(err)
	}
	late, err := s.bindContributionRequestSubject(fresh, original)
	if err != nil {
		t.Fatal("fresh request with original manager identity", err)
	}
	out, err := s.CurrentContributionDeliverySubject(late)
	if err != nil || out.ExpiresAt != original.ExpiresAt || out.AuthenticatedAt != original.AuthenticatedAt {
		t.Fatal("request renewed manager", out, err)
	}
	now = original.ExpiresAt
	if _, err = s.CurrentContributionDeliverySubject(late); !errors.Is(err, delivery.ErrDenied) {
		t.Fatal("fresh request prolonged expired manager", err)
	}
	if _, err = s.bindContributionRequestSubject(context.Background(), original); !errors.Is(err, delivery.ErrDenied) {
		t.Fatal("old subject replay accepted", err)
	}
}
