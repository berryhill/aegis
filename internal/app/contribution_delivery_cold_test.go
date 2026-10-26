package app

import (
	"context"
	"testing"
	"time"
)

// Real app same-owner control admission and Badger, no provider execution.
func TestPublicationOwnerDrainUsesCallLifetimeAndStickyStop(t *testing.T) {
	ctx := context.Background()
	s, store, sub, loop, reader := contributionFixture(t)
	s.Now = time.Now
	sub = localImportPrincipal(s)
	reader.source.ObservedAt = s.Now()
	reader.source.CreatedAt = s.Now()
	reader.source.UpdatedAt = s.Now()
	configureLocalImportFleet(t, s, store)
	c, err := s.admitContributionAs(ctx, sub, "session", loop, reader.source.URL.URL, "publication-drain", "local-only", reader)
	if err != nil {
		t.Fatal(err)
	}
	o := &ContributionDeliveryPublicationOwner{service: s, active: make(map[string]int), changed: make(chan struct{})}
	leave, err := o.enter(ctx, sub, c.Key)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.ControlContributionAs(ctx, sub, "session", c.Key, true, c.Effect); err != nil {
		t.Fatal(err)
	}
	if _, err = o.enter(ctx, sub, c.Key); err == nil {
		t.Fatal("new call after sticky stop")
	}
	bounded, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()
	if err = o.drain(bounded, c.Key); err == nil {
		t.Fatal("active call falsely drained")
	}
	leave()
	if err = o.drain(ctx, c.Key); err != nil {
		t.Fatal(err)
	}
	// Empty tracking is only a lifetime fact: no durable absence is inferred here.
	absent, err := store.ContributionDeliveryAuthoritativeAbsence(ctx, sub.PrincipalID, c.Key)
	if err != nil || !absent {
		t.Fatal(absent, err)
	}
}
