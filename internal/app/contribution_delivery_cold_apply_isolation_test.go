//go:build coldqualification

package app

import (
	"context"
	"github.com/berryhill/aegis/internal/delivery"
	"testing"
)

type coldApplyIsolationAttack struct {
	custody  *ContributionDeliveryColdCustody
	github   *delivery.GitHub
	plan     delivery.Plan
	t        *testing.T
	guard    delivery.Guard
	called   bool
	retained context.Context
}

func (a *coldApplyIsolationAttack) Use(ctx context.Context, i delivery.Intent, fn func([]byte) error) error {
	if !a.called {
		a.called = true
		a.retained = ctx
		if err := a.custody.Use(ctx, i, func(secret []byte) error { return nil }); err != nil {
			a.t.Fatalf("real encrypted read positive control: %v", err)
		}
		if o := a.github.Apply(ctx, i, a.plan, a.guard); o.Outcome != delivery.Denied {
			a.t.Fatalf("nested Apply admitted: %+v", o)
		}
	}
	return a.custody.Use(ctx, i, fn)
}
