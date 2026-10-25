package api

import (
	"context"
	"github.com/berryhill/aegis/internal/app"
)

// LaunchContributionDecisionCompanion uses the existing inode-pinned executable,
// fd3 transport custody, restricted environment and three-minute process bound.
// Central command and route registration remain explicit integration wires.
func LaunchContributionDecisionCompanion(ctx context.Context, socket, origin string, in app.ContributionDecisionInput, bearer string, bind func(int), configFiles ...string) string {
	args := []string{"contribution-decision-companion", "--socket", socket, "--origin", origin, "--key", in.Key, "--request-id", in.ID, "--role", in.Role}
	return launchProtectedDoerCompanion(ctx, args, bearer, bind, configFiles...)
}
