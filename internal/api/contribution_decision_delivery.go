package api

import (
	"context"
	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/core"
)

// Only the owning API installs this port. No commands, subjects or plans are
// accepted from the request; the protected companion performs its own login.
func installContributionDecisionDelivery(svc *app.Service) {
	svc.ContributionDecisionDelivery = func(ctx context.Context, sub core.Subject, key, role string) (string, error) {
		return svc.DeliverContributionDecisionAs(ctx, sub, key, role, func(ctx context.Context, in app.ContributionDecisionInput) string {
			return LaunchContributionDecisionCompanion(ctx, svc.Config.API.UnixSocket, svc.Config.API.Console.Origin, in, svc.Config.API.Token, func(int) {}, svc.ConfigFile)
		})
	}
}
