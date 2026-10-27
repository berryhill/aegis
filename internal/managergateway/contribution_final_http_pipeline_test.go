package managergateway

import (
	"context"
	"github.com/berryhill/aegis/internal/delivery"
)

type finalQualificationPipeline struct {
	local  *delivery.Git
	remote delivery.Adapter
}

func (a finalQualificationPipeline) Apply(ctx context.Context, i delivery.Intent, p delivery.Plan, g delivery.Guard) delivery.Observation {
	if i.Operation == delivery.Commit {
		return a.local.Commit(ctx, p.Commit, g)
	}
	return a.remote.Apply(ctx, i, p, g)
}
func (a finalQualificationPipeline) Reconcile(ctx context.Context, i delivery.Intent, p delivery.Plan, g delivery.Guard) delivery.Observation {
	if i.Operation == delivery.Commit {
		if g(ctx) != nil {
			return delivery.Observation{Outcome: delivery.Denied}
		}
		return a.local.Reconcile(ctx, p.Commit)
	}
	return a.remote.Reconcile(ctx, i, p, g)
}
